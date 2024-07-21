package main

import (
	"bytes"
	"fmt"
	"strings"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

type MQTTHookOptions struct {
	Server      *mqtt.Server
	Diagnostics *Diagnostics
}
type MQTTHook struct {
	mqtt.HookBase
	config *MQTTHookOptions
}

func (h *MQTTHook) ID() string {
	return "events"
}

func (h *MQTTHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnConnect,
		mqtt.OnDisconnect,
		mqtt.OnSubscribed,
		mqtt.OnUnsubscribed,
		mqtt.OnPublished,
		mqtt.OnPublish,
		mqtt.OnPacketSent,
		mqtt.OnACLCheck,
	}, []byte{b})
}

func (h *MQTTHook) Init(config any) error {
	h.Log.Info("initialised")
	if _, ok := config.(*MQTTHookOptions); !ok && config != nil {
		return mqtt.ErrInvalidConfigType
	}

	h.config = config.(*MQTTHookOptions)
	if h.config.Server == nil {
		return mqtt.ErrInvalidConfigType
	}
	if h.config.Diagnostics == nil {
		return mqtt.ErrInvalidConfigType
	}
	// h.config.Server.Subscribe("#", 1, h.subscribeCallback)
	return nil
}

// subscribe 回调处理订阅主题的消息
// func (h *MQTTHook) subscribeCallback(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {}

// 有设备连接到服务器
func (h *MQTTHook) OnConnect(cl *mqtt.Client, pk packets.Packet) error {
	// OnConnect 在 OnConnectAuthenticate 之前触发，只表示收到 CONNECT。
	// 这里绝不序列化 Client 或 Packet，避免把 MQTT 密码写入日志。
	if len(pk.Connect.Password) > 0 {
		h.config.Diagnostics.AddSecret(string(pk.Connect.Password))
	}
	clientID := cl.ID
	username := string(cl.Properties.Username)
	h.config.Diagnostics.MQTTConnectReceived(connID(cl), clientID, username, cl.Properties.ProtocolVersion, cl.Properties.Clean)
	logFileStr(true, strCL(cl), lang("CONNECT"), "CONNECT received (authentication pending)")
	return nil
}

// 设备断开连接
func (h *MQTTHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
	h.config.Diagnostics.MQTTDisconnected(connID(cl), cl.ID, err, expire)
	logFileStr(true, strCL(cl), lang("DISCONNECT"), h.config.Diagnostics.SanitizeError(err))
}

// 收到订阅请求
func (h *MQTTHook) OnSubscribed(cl *mqtt.Client, pk packets.Packet, reasonCodes []byte) {
	outcomes := make([]SubscriptionOutcome, 0, len(pk.Filters))
	for i, filter := range pk.Filters {
		code := byte(0xFF)
		if i < len(reasonCodes) {
			code = reasonCodes[i]
		}
		granted := code
		if code > 2 {
			granted = 0
		}
		outcomes = append(outcomes, SubscriptionOutcome{
			Filter:       string(filter.Filter),
			RequestedQoS: filter.Qos,
			GrantedQoS:   granted,
			Code:         code,
		})
	}
	h.config.Diagnostics.Subscribed(connID(cl), cl.ID, outcomes)
	logFileStr(true, strCL(cl), lang("SUBSCRIBED"), fmt.Sprintf("%s (QOS%d)", pkFilters(pk.Filters), pk.FixedHeader.Qos))
}

// 收到取消订阅请求
func (h *MQTTHook) OnUnsubscribed(cl *mqtt.Client, pk packets.Packet) {
	filters := make([]string, 0, len(pk.Filters))
	for _, filter := range pk.Filters {
		filters = append(filters, string(filter.Filter))
	}
	h.config.Diagnostics.Unsubscribed(connID(cl), cl.ID, filters)
	logFileStr(true, strCL(cl), lang("UNSUBSCRIBED"), pkFilters(pk.Filters))
}

// 客户端发送消息时
func (h *MQTTHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	var clID *string = &cl.ID
	var topic *string = &pk.TopicName
	var payload string = string(pk.Payload)
	// only-* 过滤只影响负载记录，不影响诊断事件与汇总计数。
	dataAllowed := true
	if onlyIdE && !in(&onlyIdS, clID) {
		dataAllowed = false
	}
	if onlyTopicE && !in(&onlyTopicS, topic) {
		dataAllowed = false
	}
	if onlyPayloadE {
		inWord := false
		for _, word := range onlyPayloadS {
			if strings.Contains(payload, word) {
				inWord = true
				break
			}
		}
		if !inWord {
			dataAllowed = false
		}
	}
	var eventPayload []byte
	if dataAllowed {
		if logDataE {
			logFileStr(false, strCL(cl), *topic, payload)
		}
		if h.config.Diagnostics.logPayloadEnabled() {
			eventPayload = pk.Payload
		}
	}
	h.config.Diagnostics.Publish(connID(cl), cl.ID, *topic, pk.FixedHeader.Qos, pk.FixedHeader.Retain, eventPayload)
	return pk, nil
}

// OnPacketSent 用于读取 CONNACK 返回码。认证结果以 CONNACK 为准，
// 而不是以收到 CONNECT 为准。
func (h *MQTTHook) OnPacketSent(cl *mqtt.Client, pk packets.Packet, b []byte) {
	if pk.FixedHeader.Type != packets.Connack {
		return
	}
	h.config.Diagnostics.MQTTConnack(connID(cl), cl.ID, pk.ReasonCode, pk.SessionPresent, cl.Properties.ProtocolVersion)
}

// OnACLCheck 只观察“前序 hook 已拒绝”的发布授权。
// 该 hook 注册在 auth.Hook 之后：若前序 hook 允许，聚合结果会提前返回 true，
// 本方法不会被调用；本方法返回 false 不会放宽或绕过任何限制。
func (h *MQTTHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	if write {
		h.config.Diagnostics.ACLDenied(connID(cl), cl.ID, topic)
	}
	return false
}

func connID(cl *mqtt.Client) string {
	if cl == nil || cl.Net.Conn == nil {
		return ""
	}
	if identified, ok := cl.Net.Conn.(interface{ DiagnosticsConnID() string }); ok {
		return identified.DiagnosticsConnID()
	}
	return ""
}
