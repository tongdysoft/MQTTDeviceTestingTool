package main

// 这是一个只用于测试的最小 MQTT 3.1.1 客户端。
// 它按协议处理报文边界（剩余长度变长编码）、CONNACK/SUBACK/PUBACK，
// 用于验证项目真实监听与 MQTT 服务路径，而不是只探测 TCP 或 TLS 是否可连。

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type mqttStage string

const (
	mqttStageTCP          mqttStage = "tcp-dial"
	mqttStageTLS          mqttStage = "tls-handshake"
	mqttStageConnectWrite mqttStage = "mqtt-connect-write"
	mqttStageConnectRead  mqttStage = "mqtt-connect-read"
	mqttStageWrite        mqttStage = "mqtt-write"
	mqttStageRead         mqttStage = "mqtt-read"
)

// mqttClientError 保留失败发生的阶段，便于区分 TLS 失败与 MQTT 层失败。
type mqttClientError struct {
	Stage mqttStage
	Err   error
}

func (e *mqttClientError) Error() string {
	return fmt.Sprintf("MQTT client %s: %v", e.Stage, e.Err)
}

func (e *mqttClientError) Unwrap() error { return e.Err }

// mqttConnackRefusedError 表示收到结构合法的 CONNACK，但返回码非 0。
// 与 TLS/传输失败是不同类型，测试可据此区分认证失败与握手失败。
type mqttConnackRefusedError struct {
	Code           byte
	SessionPresent bool
}

func (e *mqttConnackRefusedError) Error() string {
	return fmt.Sprintf("MQTT CONNECT refused with return code 0x%02x", e.Code)
}

type mqttMessage struct {
	Topic   string
	Payload []byte
	QoS     byte
}

type mqttClientOptions struct {
	ClientID  string
	Username  string
	Password  string
	KeepAlive uint16
	TLSConfig *tls.Config
	Timeout   time.Duration
}

type mqttTestClient struct {
	conn   net.Conn
	reader *bufio.Reader
	opts   mqttClientOptions

	tlsState tls.ConnectionState
	hasTLS   bool

	packetID uint16
	pending  []mqttMessage

	closeOnce sync.Once
	closeErr  error
}

var testMQTTClientID atomic.Uint64

const defaultMQTTTestTimeout = 5 * time.Second

func dialMQTT(ctx context.Context, addr string, opts mqttClientOptions) (*mqttTestClient, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultMQTTTestTimeout
	}
	if opts.ClientID == "" {
		opts.ClientID = fmt.Sprintf("mqtt-test-client-%d", testMQTTClientID.Add(1))
	}

	dialer := &net.Dialer{Timeout: opts.Timeout}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, &mqttClientError{Stage: mqttStageTCP, Err: err}
	}

	conn := net.Conn(raw)
	if opts.TLSConfig != nil {
		tlsConn := tls.Client(raw, opts.TLSConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, &mqttClientError{Stage: mqttStageTLS, Err: err}
		}
		_ = raw.SetDeadline(time.Time{})
		conn = tlsConn
	}

	client := &mqttTestClient{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		opts:     opts,
		packetID: 1,
	}
	if opts.TLSConfig != nil {
		client.hasTLS = true
		client.tlsState = conn.(*tls.Conn).ConnectionState()
	}

	if err := client.sendConnect(); err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := client.readConnack(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *mqttTestClient) sendConnect() error {
	var body []byte
	body = appendUTF8(body, "MQTT")
	body = append(body, 4) // MQTT 3.1.1

	var flags byte = 0x02 // clean session
	var payload []byte
	payload = appendUTF8(payload, c.opts.ClientID)
	if c.opts.Username != "" {
		flags |= 0x80
		payload = appendUTF8(payload, c.opts.Username)
		if c.opts.Password != "" {
			flags |= 0x40
			payload = appendUTF8(payload, c.opts.Password)
		}
	}
	keepAlive := c.opts.KeepAlive
	if keepAlive == 0 {
		keepAlive = 30
	}
	body = append(body, flags)
	body = append(body, byte(keepAlive>>8), byte(keepAlive))
	body = append(body, payload...)

	if err := c.writePacket(0x10, body); err != nil {
		return &mqttClientError{Stage: mqttStageConnectWrite, Err: err}
	}
	return nil
}

func (c *mqttTestClient) readConnack() error {
	fixed, body, err := c.readPacket(c.opts.Timeout)
	if err != nil {
		return &mqttClientError{Stage: mqttStageConnectRead, Err: err}
	}
	if fixed>>4 != 2 {
		return &mqttClientError{Stage: mqttStageConnectRead,
			Err: fmt.Errorf("expected CONNACK, got packet type %d", fixed>>4)}
	}
	if len(body) < 2 {
		return &mqttClientError{Stage: mqttStageConnectRead,
			Err: fmt.Errorf("CONNACK payload too short: %d bytes", len(body))}
	}
	sessionPresent := body[0]&0x01 != 0
	if body[1] != 0 {
		return &mqttConnackRefusedError{Code: body[1], SessionPresent: sessionPresent}
	}
	return nil
}

func (c *mqttTestClient) TLSVersion() uint16 {
	if !c.hasTLS {
		return 0
	}
	return c.tlsState.Version
}

// Subscribe 发送订阅请求并等待对应 SUBACK，返回服务器授予的 QoS 或 0x80（拒绝）。
func (c *mqttTestClient) Subscribe(filter string, qos byte) (byte, error) {
	id := c.nextPacketID()
	var body []byte
	body = append(body, byte(id>>8), byte(id))
	body = appendUTF8(body, filter)
	body = append(body, qos)
	if err := c.writePacket(0x82, body); err != nil {
		return 0, &mqttClientError{Stage: mqttStageWrite, Err: err}
	}

	deadline := time.Now().Add(c.opts.Timeout)
	for {
		fixed, packet, err := c.readPacket(time.Until(deadline))
		if err != nil {
			return 0, &mqttClientError{Stage: mqttStageRead, Err: err}
		}
		switch fixed >> 4 {
		case 9: // SUBACK
			if len(packet) < 3 {
				return 0, &mqttClientError{Stage: mqttStageRead,
					Err: fmt.Errorf("SUBACK payload too short: %d bytes", len(packet))}
			}
			if binary.BigEndian.Uint16(packet[0:2]) != id {
				return 0, &mqttClientError{Stage: mqttStageRead,
					Err: fmt.Errorf("SUBACK packet id %d does not match %d", binary.BigEndian.Uint16(packet[0:2]), id)}
			}
			return packet[2], nil
		case 13: // PINGRESP
			continue
		case 3: // 订阅确认前到达的消息，先缓存
			message, err := c.decodePublish(fixed, packet)
			if err != nil {
				return 0, err
			}
			c.pending = append(c.pending, message)
		default:
			// 忽略与本操作无关的报文，避免误判；协议错误会在后续读取中暴露。
		}
	}
}

func (c *mqttTestClient) Publish(topic string, payload []byte, qos byte) error {
	if qos > 1 {
		return fmt.Errorf("test client only supports QoS 0 and 1, got %d", qos)
	}
	id := c.nextPacketID()
	body := appendUTF8(nil, topic)
	if qos > 0 {
		body = append(body, byte(id>>8), byte(id))
	}
	body = append(body, payload...)

	if err := c.writePacket(0x30|(qos<<1), body); err != nil {
		return &mqttClientError{Stage: mqttStageWrite, Err: err}
	}
	if qos == 0 {
		return nil
	}

	deadline := time.Now().Add(c.opts.Timeout)
	for {
		fixed, packet, err := c.readPacket(time.Until(deadline))
		if err != nil {
			return &mqttClientError{Stage: mqttStageRead, Err: err}
		}
		switch fixed >> 4 {
		case 4: // PUBACK
			if len(packet) < 2 {
				return &mqttClientError{Stage: mqttStageRead,
					Err: fmt.Errorf("PUBACK payload too short: %d bytes", len(packet))}
			}
			if binary.BigEndian.Uint16(packet[0:2]) == id {
				return nil
			}
		case 3:
			message, err := c.decodePublish(fixed, packet)
			if err != nil {
				return err
			}
			c.pending = append(c.pending, message)
		case 13:
			continue
		default:
		}
	}
}

// NextPublish 在给定时间内等待一条 PUBLISH；超时返回 net.Error（Timeout）。
func (c *mqttTestClient) NextPublish(timeout time.Duration) (mqttMessage, error) {
	if len(c.pending) > 0 {
		message := c.pending[0]
		c.pending = c.pending[1:]
		return message, nil
	}

	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return mqttMessage{}, &mqttClientError{Stage: mqttStageRead, Err: timeoutError{}}
		}
		fixed, packet, err := c.readPacket(remaining)
		if err != nil {
			return mqttMessage{}, &mqttClientError{Stage: mqttStageRead, Err: err}
		}
		switch fixed >> 4 {
		case 3:
			return c.decodePublish(fixed, packet)
		case 13, 4, 9, 11:
			continue
		default:
			return mqttMessage{}, &mqttClientError{Stage: mqttStageRead,
				Err: fmt.Errorf("unexpected packet type %d while waiting for PUBLISH", fixed>>4)}
		}
	}
}

func (c *mqttTestClient) decodePublish(fixed byte, body []byte) (mqttMessage, error) {
	qos := (fixed >> 1) & 0x03
	if len(body) < 2 {
		return mqttMessage{}, &mqttClientError{Stage: mqttStageRead,
			Err: fmt.Errorf("PUBLISH payload too short: %d bytes", len(body))}
	}
	topicLen := int(binary.BigEndian.Uint16(body[0:2]))
	if len(body) < 2+topicLen {
		return mqttMessage{}, &mqttClientError{Stage: mqttStageRead,
			Err: fmt.Errorf("PUBLISH topic length %d exceeds payload %d", topicLen, len(body))}
	}
	topic := string(body[2 : 2+topicLen])
	offset := 2 + topicLen
	var id uint16
	if qos > 0 {
		if len(body) < offset+2 {
			return mqttMessage{}, &mqttClientError{Stage: mqttStageRead,
				Err: fmt.Errorf("PUBLISH packet id missing")}
		}
		id = binary.BigEndian.Uint16(body[offset : offset+2])
		offset += 2
	}
	message := mqttMessage{Topic: topic, Payload: append([]byte(nil), body[offset:]...), QoS: qos}
	if qos == 1 {
		if err := c.writePacket(0x40, []byte{byte(id >> 8), byte(id)}); err != nil {
			return mqttMessage{}, &mqttClientError{Stage: mqttStageWrite, Err: err}
		}
	}
	return message, nil
}

func (c *mqttTestClient) Ping(timeout time.Duration) error {
	if err := c.writePacket(0xc0, nil); err != nil {
		return &mqttClientError{Stage: mqttStageWrite, Err: err}
	}
	deadline := time.Now().Add(timeout)
	for {
		fixed, _, err := c.readPacket(time.Until(deadline))
		if err != nil {
			return &mqttClientError{Stage: mqttStageRead, Err: err}
		}
		if fixed>>4 == 13 {
			return nil
		}
	}
}

func (c *mqttTestClient) Disconnect() error {
	_ = c.writePacket(0xe0, nil)
	return c.Close()
}

func (c *mqttTestClient) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.conn.Close()
	})
	return c.closeErr
}

func (c *mqttTestClient) nextPacketID() uint16 {
	id := c.packetID
	c.packetID++
	if c.packetID == 0 {
		c.packetID = 1
	}
	return id
}

func (c *mqttTestClient) writePacket(fixed byte, body []byte) error {
	packet := make([]byte, 0, len(body)+8)
	packet = append(packet, fixed)
	packet = appendRemainingLength(packet, len(body))
	packet = append(packet, body...)
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.opts.Timeout))
	_, err := c.conn.Write(packet)
	return err
}

func (c *mqttTestClient) readPacket(timeout time.Duration) (byte, []byte, error) {
	if timeout <= 0 {
		timeout = c.opts.Timeout
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	fixed, err := c.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length, err := readRemainingLength(c.reader)
	if err != nil {
		return 0, nil, err
	}
	if length > 1<<20 {
		return 0, nil, fmt.Errorf("MQTT packet too large: %d bytes", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return 0, nil, err
	}
	return fixed, body, nil
}

func readRemainingLength(reader *bufio.Reader) (int, error) {
	multiplier := 1
	value := 0
	for i := 0; i < 4; i++ {
		b, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		value += int(b&0x7f) * multiplier
		if b&0x80 == 0 {
			return value, nil
		}
		multiplier *= 128
	}
	return 0, fmt.Errorf("malformed MQTT remaining length")
}

func appendRemainingLength(dst []byte, length int) []byte {
	for {
		digit := byte(length % 128)
		length /= 128
		if length > 0 {
			digit |= 0x80
		}
		dst = append(dst, digit)
		if length == 0 {
			return dst
		}
	}
}

func appendUTF8(dst []byte, value string) []byte {
	dst = append(dst, byte(len(value)>>8), byte(len(value)))
	return append(dst, value...)
}

// timeoutError 明确标记测试客户端的等待超时，避免与连接关闭混淆。
type timeoutError struct{}

func (timeoutError) Error() string   { return "timed out waiting for MQTT packet" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
