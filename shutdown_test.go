package main

// 本文件覆盖关闭预算与握手期限：
//   - TLS 握手的读阻塞与写阻塞都能被同一个绝对期限终止；
//   - 握手成功后清除期限，超过原握手期限仍能正常收发 MQTT；
//   - 关闭正在握手的连接记录为主动关闭，而不是设备证书失败；
//   - MQTT 5 DISCONNECT 的同步写不会绕过关闭预算；
//   - 多连接通知共享一次预算，不会逐连接累积；
//   - 重复/并发 Close、以及清理超时都有确定的行为。
//
// 受控连接用于制造确定的背压；会话路径仍尽量使用真实 mochi 服务与回环连接。

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newShutdownTestDiagnostics(t *testing.T, runID string, mode SecurityMode) *Diagnostics {
	t.Helper()
	diag, err := NewDiagnostics(DiagnosticsOptions{
		Config:      &Config{Mode: mode, ShutdownTimeout: time.Second},
		RunID:       runID,
		Terminal:    io.Discard,
		ErrorWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("NewDiagnostics: %v", err)
	}
	t.Cleanup(func() { _ = diag.Close() })
	return diag
}

func testServerTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	notBefore, notAfter := validServerWindow()
	ca := newTestCA(t, "Shutdown Test CA", notBefore, notAfter)
	leaf := newTestServerLeaf(t, "localhost", ca, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	certificate, err := tls.X509KeyPair(pemForCerts(leaf), pkcs8PEM(t, leaf.Key))
	if err != nil {
		t.Fatalf("build server certificate: %v", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}
}

func newShutdownTestListener(t *testing.T, opts listenerOptions) *diagnosticListener {
	t.Helper()
	listener := newDiagnosticListener(opts)
	if listener.diag == nil {
		listener.diag = newShutdownTestDiagnostics(t, "run-shutdown-listener", ModeTLS)
	}
	t.Cleanup(listener.cancel)
	return listener
}

func findDiagnosticsEvent(diag *Diagnostics, match func(Event) bool) (Event, bool) {
	for _, event := range diag.Events() {
		if match(event) {
			return event, true
		}
	}
	return Event{}, false
}

// deadlineBlockedConn 是受控连接：Read/Write 在期限或 Close 前阻塞。
// 它模拟对端不读造成的确定背压，并严格执行 SetDeadline。
type deadlineBlockedConn struct {
	remote net.Addr

	mu            sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time

	closed    chan struct{}
	closeOnce sync.Once
}

func newDeadlineBlockedConn() *deadlineBlockedConn {
	return &deadlineBlockedConn{
		remote: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 43210},
		closed: make(chan struct{}),
	}
}

func (c *deadlineBlockedConn) LocalAddr() net.Addr  { return c.remote }
func (c *deadlineBlockedConn) RemoteAddr() net.Addr { return c.remote }

func (c *deadlineBlockedConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.readDeadline = deadline
	c.writeDeadline = deadline
	c.mu.Unlock()
	return nil
}

func (c *deadlineBlockedConn) SetReadDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.readDeadline = deadline
	c.mu.Unlock()
	return nil
}

func (c *deadlineBlockedConn) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.writeDeadline = deadline
	c.mu.Unlock()
	return nil
}

func (c *deadlineBlockedConn) Read([]byte) (int, error) {
	c.mu.Lock()
	deadline := c.readDeadline
	c.mu.Unlock()
	return 0, c.wait("read", deadline)
}

func (c *deadlineBlockedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	deadline := c.writeDeadline
	c.mu.Unlock()
	if err := c.wait("write", deadline); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *deadlineBlockedConn) wait(op string, deadline time.Time) error {
	var timer *time.Timer
	var expired <-chan time.Time
	if !deadline.IsZero() {
		timer = time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-c.closed:
		return &net.OpError{Op: op, Net: "pipe", Err: net.ErrClosed}
	case <-expired:
		return &net.OpError{Op: op, Net: "pipe", Err: os.ErrDeadlineExceeded}
	}
}

func (c *deadlineBlockedConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *deadlineBlockedConn) Closed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// captureClientHelloConn 截获 tls.Client 写出的 ClientHello，随后停止读取，
// 用于给服务端制造“ClientHello 已收到、ServerHello 写不出去”的确定背压。
type captureClientHelloConn struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	wrote  chan struct{}
	closed chan struct{}
	once   sync.Once
}

func newCaptureClientHelloConn() *captureClientHelloConn {
	return &captureClientHelloConn{
		wrote:  make(chan struct{}),
		closed: make(chan struct{}),
	}
}

func (c *captureClientHelloConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, io.EOF
}

func (c *captureClientHelloConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	_, _ = c.buffer.Write(p)
	c.mu.Unlock()
	select {
	case <-c.wrote:
	default:
		close(c.wrote)
	}
	return len(p), nil
}

func (c *captureClientHelloConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *captureClientHelloConn) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (c *captureClientHelloConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }
func (c *captureClientHelloConn) SetDeadline(time.Time) error {
	return nil
}
func (c *captureClientHelloConn) SetReadDeadline(time.Time) error  { return nil }
func (c *captureClientHelloConn) SetWriteDeadline(time.Time) error { return nil }

func captureClientHello(t *testing.T) []byte {
	t.Helper()
	conn := newCaptureClientHelloConn()
	client := tls.Client(conn, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "localhost",
		MinVersion:         tls.VersionTLS12,
	})
	handshakeDone := make(chan error, 1)
	go func() { handshakeDone <- client.Handshake() }()

	select {
	case <-conn.wrote:
	case <-time.After(2 * time.Second):
		_ = conn.Close()
		t.Fatal("client did not produce a ClientHello")
	}
	_ = conn.Close()
	select {
	case <-handshakeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("captured TLS client did not stop after its connection was closed")
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.buffer.Len() == 0 {
		t.Fatal("captured ClientHello is empty")
	}
	return append([]byte(nil), conn.buffer.Bytes()...)
}

func waitHandled(t *testing.T, done <-chan struct{}, budget time.Duration, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(budget):
		t.Fatalf("%s did not finish within %s", what, budget)
	}
}

func TestHandshakeReadBlockEndsAtDeadline(t *testing.T) {
	diag := newShutdownTestDiagnostics(t, "run-handshake-read-timeout", ModeTLS)
	listener := newShutdownTestListener(t, listenerOptions{
		diagnostics:      diag,
		tlsConfig:        testServerTLSConfig(t),
		handshakeTimeout: 100 * time.Millisecond,
		shutdownTimeout:  time.Second,
		maxHandshakes:    1,
	})

	serverSide, peer := net.Pipe()
	defer peer.Close()
	if !listener.track(serverSide) {
		t.Fatal("listener refused to track the test connection")
	}

	establishCalled := make(chan struct{}, 1)
	handled := make(chan struct{})
	released := make(chan struct{}, 1)
	started := time.Now()
	go func() {
		listener.handle(serverSide, func(string, net.Conn) error {
			establishCalled <- struct{}{}
			return nil
		}, func() { released <- struct{}{} })
		close(handled)
	}()

	waitHandled(t, handled, 3*time.Second, "handshake read timeout")
	elapsed := time.Since(started)
	if elapsed < 50*time.Millisecond {
		t.Fatalf("handshake returned after %s, before the read deadline could fire", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("handshake read block lasted %s with a 100ms deadline", elapsed)
	}
	select {
	case <-establishCalled:
		t.Fatal("establish was called although the handshake never completed")
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("handshake slot was not released after the timeout")
	}
	if active := listener.ActiveCount(); active != 0 {
		t.Fatalf("active connection count = %d after the handshake returned", active)
	}

	event, ok := findDiagnosticsEvent(diag, func(event Event) bool {
		return event.Stage == StageTLS && event.Result == ResultFailure
	})
	if !ok {
		t.Fatal("no TLS handshake failure was recorded for the silent peer")
	}
	if event.TLS == nil || event.TLS.FailureKind != "timeout" {
		t.Fatalf("handshake failure kind = %#v, want timeout", event.TLS)
	}
}

func TestHandshakeWriteBlockEndsAtDeadline(t *testing.T) {
	hello := captureClientHello(t)

	diag := newShutdownTestDiagnostics(t, "run-handshake-write-timeout", ModeTLS)
	listener := newShutdownTestListener(t, listenerOptions{
		diagnostics:      diag,
		tlsConfig:        testServerTLSConfig(t),
		handshakeTimeout: 100 * time.Millisecond,
		shutdownTimeout:  time.Second,
		maxHandshakes:    1,
	})

	serverSide, peer := net.Pipe()
	defer peer.Close()
	if !listener.track(serverSide) {
		t.Fatal("listener refused to track the test connection")
	}

	handled := make(chan struct{})
	released := make(chan struct{}, 1)
	started := time.Now()
	go func() {
		listener.handle(serverSide, func(string, net.Conn) error {
			return fmt.Errorf("establish must not be called")
		}, func() { released <- struct{}{} })
		close(handled)
	}()

	// 写入 ClientHello 后不再读取：服务端的 ServerHello/证书写入会阻塞。
	written := make(chan error, 1)
	go func() {
		_, err := peer.Write(hello)
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("write captured ClientHello: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not read the ClientHello")
	}

	waitHandled(t, handled, 3*time.Second, "handshake write block")
	elapsed := time.Since(started)
	if elapsed < 50*time.Millisecond {
		t.Fatalf("handshake returned after %s, before the write deadline could fire", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("handshake write block lasted %s with a 100ms deadline", elapsed)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("handshake slot was not released after the write timeout")
	}

	event, ok := findDiagnosticsEvent(diag, func(event Event) bool {
		return event.Stage == StageTLS && event.Result == ResultFailure
	})
	if !ok {
		t.Fatal("no TLS handshake failure was recorded for the blocked write")
	}
	if event.TLS == nil || event.TLS.FailureKind != "timeout" {
		t.Fatalf("handshake failure kind = %#v, want timeout (write deadline covers writes too)", event.TLS)
	}
}

func TestHandshakeDeadlineClearedAfterSuccess(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Shutdown Success Server CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	roots := testCertPool(t, serverCA)

	server, addr := startTestServer(t, testServerSpec{
		Mode:             ModeTLS,
		ServerChain:      []*testCert{serverLeaf},
		HandshakeTimeout: 200 * time.Millisecond,
		ConnectTimeout:   2 * time.Second,
		ShutdownTimeout:  2 * time.Second,
	})
	client := requireConnect(t, addr, mqttClientOptions{TLSConfig: testClientTLSConfig(t, roots, "localhost")})

	// 超过原握手期限后，读写都不应再受握手期限影响。
	time.Sleep(600 * time.Millisecond)
	mustRoundTrip(t, client, "shutdown/after-handshake", "still-alive")
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
}

func TestCloseDuringHandshakeIsReportedAsShutdown(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Shutdown Handshake CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})

	server, addr := startTestServer(t, testServerSpec{
		Mode:             ModeTLS,
		ServerChain:      []*testCert{serverLeaf},
		HandshakeTimeout: 10 * time.Second,
		ShutdownTimeout:  500 * time.Millisecond,
	})
	slow, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial slow connection: %v", err)
	}
	defer slow.Close()
	accept := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageTransport && event.Action == "accept"
	})

	started := time.Now()
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Close() waited %s for a 10s handshake", elapsed)
	}

	var aborted bool
	for _, event := range server.Diagnostics().Events() {
		if event.ConnectionID != accept.ConnectionID {
			continue
		}
		if event.Stage == StageTLS {
			if event.Result == ResultFailure {
				t.Fatalf("handshake aborted by shutdown was recorded as a TLS failure: %#v", event)
			}
			if event.Result == ResultClosed && event.Context["failure_kind"] == "server_shutdown" {
				aborted = true
			}
		}
	}
	if !aborted {
		t.Fatal("shutdown during handshake did not record an explicit server_shutdown aborted handshake")
	}
	if counts := server.Diagnostics().Summary().Counts; counts.TLSFailure != 0 {
		t.Fatalf("tls_failure = %d, want 0 (active shutdown is not a certificate conclusion)", counts.TLSFailure)
	}
}

// mqtt5ConnectPacket 构造最小 MQTT 5 CONNECT：clean start、无属性。
func mqtt5ConnectPacket(clientID string) []byte {
	body := make([]byte, 0, 32)
	body = appendUTF8(body, "MQTT")
	body = append(body, 5)     // protocol level
	body = append(body, 0x02)  // clean start
	body = append(body, 0, 60) // keepalive
	body = append(body, 0)     // properties length
	body = appendUTF8(body, clientID)

	packet := []byte{0x10}
	packet = appendRemainingLength(packet, len(body))
	return append(packet, body...)
}

func TestShutdownBoundsBlockedMQTT5DisconnectWrite(t *testing.T) {
	results := t.TempDir()
	server, _ := startTestServer(t, testServerSpec{
		Mode:            ModePlain,
		ResultsDir:      results,
		ConnectTimeout:  5 * time.Second,
		ShutdownTimeout: 200 * time.Millisecond,
	})

	listener := server.listener
	serverSide, peer := net.Pipe()
	defer peer.Close()

	// 通过受控连接模拟真实 accept 路径：同样的 track、CONNECT 期限与 establish。
	listener.slots <- struct{}{}
	if !listener.track(serverSide) {
		t.Fatal("listener refused to track the controlled connection")
	}
	handled := make(chan struct{})
	go func() {
		listener.handle(serverSide, server.server.EstablishConnection, func() { <-listener.slots })
		close(handled)
	}()

	if _, err := peer.Write(mqtt5ConnectPacket("blocked-disconnect")); err != nil {
		t.Fatalf("write MQTT 5 CONNECT: %v", err)
	}
	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set peer read deadline: %v", err)
	}
	fixed := make([]byte, 2)
	if _, err := io.ReadFull(peer, fixed); err != nil {
		t.Fatalf("read CONNACK header: %v", err)
	}
	if fixed[0] != 0x20 {
		t.Fatalf("CONNACK packet type = 0x%02x, want 0x20", fixed[0])
	}
	body := make([]byte, int(fixed[1]))
	if _, err := io.ReadFull(peer, body); err != nil {
		t.Fatalf("read CONNACK body: %v", err)
	}
	if len(body) < 2 || body[1] != 0 {
		t.Fatalf("CONNACK reason code = %#v, want success", body)
	}

	// 不再读取对端：mochi 关闭时的 DISCONNECT 写会阻塞在 net.Pipe 上。
	waitForEvent(t, server.Diagnostics(), 2*time.Second, func(event Event) bool {
		return event.Stage == StageMQTTConnect && event.Action == "connect"
	})

	started := time.Now()
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	elapsed := time.Since(started)
	if elapsed > 2*time.Second {
		t.Fatalf("Close() took %s with a 200ms shutdown budget", elapsed)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("Close() returned after %s; the blocked DISCONNECT write was not actually exercised", elapsed)
	}
	waitHandled(t, handled, time.Second, "MQTT 5 connection handler")

	// 服务端已关闭该管道；对端读取应立即失败，而不是继续等待。
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	var buffer [1]byte
	if _, err := peer.Read(buffer[:]); err == nil {
		t.Fatal("pipe peer still open after shutdown")
	}

	var shutdownClose bool
	for _, event := range server.Diagnostics().Events() {
		if event.Stage == StageTransport && event.Action == "close" && event.Reason == "server_shutdown" {
			shutdownClose = true
		}
		// 主动关闭不能把断连记成设备侧失败。
		if event.Stage == StageDisconnect && event.Action == "disconnect" && event.Result == ResultFailure {
			t.Fatalf("disconnect during shutdown was reported as failure: reason=%q", event.Reason)
		}
	}
	if !shutdownClose {
		for _, event := range server.Diagnostics().Events() {
			t.Logf("event stage=%s action=%s reason=%q conn=%s", event.Stage, event.Action, event.Reason, event.ConnectionID)
		}
		t.Fatal("shutdown-closed connection was not recorded with reason server_shutdown")
	}
}

func TestConnectionNotificationsShareOneShutdownDeadline(t *testing.T) {
	diag := newShutdownTestDiagnostics(t, "run-shared-shutdown-deadline", ModePlain)
	listener := newShutdownTestListener(t, listenerOptions{
		diagnostics:     diag,
		shutdownTimeout: 200 * time.Millisecond,
		maxHandshakes:   1,
	})

	const connections = 6
	conns := make([]*deadlineBlockedConn, 0, connections)
	for i := 0; i < connections; i++ {
		conn := newDeadlineBlockedConn()
		if !listener.track(conn) {
			t.Fatal("listener refused to track a controlled connection")
		}
		conns = append(conns, conn)
		go func(c *deadlineBlockedConn) {
			<-c.closed
			listener.untrack(c)
		}(conn)
	}

	var timedOutWrites atomic.Int64
	started := time.Now()
	listener.Close(func(string) {
		for _, conn := range conns {
			if _, err := conn.Write([]byte{0xE0, 0x00}); err != nil {
				timedOutWrites.Add(1)
			}
		}
	})
	elapsed := time.Since(started)

	if elapsed > 800*time.Millisecond {
		t.Fatalf("notification phase took %s; per-connection budgets would accumulate to %s",
			elapsed, time.Duration(connections)*200*time.Millisecond)
	}
	if got := timedOutWrites.Load(); got != connections {
		t.Fatalf("timed-out notification writes = %d, want %d (all writes must use the same absolute deadline)", got, connections)
	}
	if err := listener.CloseError(); err != nil {
		t.Fatalf("CloseError() = %v, want nil after all connections were closed", err)
	}
	if active := listener.ActiveCount(); active != 0 {
		t.Fatalf("active connection count = %d after Close", active)
	}
	for _, conn := range conns {
		if !conn.Closed() {
			t.Fatal("controlled connection was not closed by shutdown")
		}
	}
}

func TestShutdownCleanupTimeoutIsReported(t *testing.T) {
	diag := newShutdownTestDiagnostics(t, "run-shutdown-cleanup-timeout", ModePlain)
	listener := newShutdownTestListener(t, listenerOptions{
		diagnostics:     diag,
		shutdownTimeout: 100 * time.Millisecond,
		maxHandshakes:   1,
	})

	conn := newDeadlineBlockedConn()
	if !listener.track(conn) {
		t.Fatal("listener refused to track the controlled connection")
	}
	release := make(chan struct{})
	handled := make(chan struct{})
	go func() {
		listener.handle(conn, func(string, net.Conn) error {
			<-release
			return nil
		}, func() {})
		close(handled)
	}()

	started := time.Now()
	listener.Close(nil)
	elapsed := time.Since(started)
	if elapsed > 2*time.Second {
		t.Fatalf("Close() blocked for %s while the handler ignored connection close", elapsed)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("Close() returned after %s, before the shutdown budget could expire", elapsed)
	}
	if err := listener.CloseError(); err == nil {
		t.Fatal("CloseError() = nil, want an explicit cleanup timeout")
	}
	event, ok := findDiagnosticsEvent(diag, func(event Event) bool {
		return event.Stage == StageRun && event.Action == "shutdown" && event.Result == ResultFailure
	})
	if !ok || event.Context["component"] != "connections" {
		t.Fatalf("cleanup timeout event = %#v, want a run/shutdown failure for connections", event)
	}

	// 测试自身必须回收这个处理 goroutine，不能依赖 go test 总超时。
	close(release)
	waitHandled(t, handled, 2*time.Second, "stuck handler")
	if active := listener.ActiveCount(); active != 0 {
		t.Fatalf("active connection count = %d after the handler was released", active)
	}
}

func TestRunningServerCloseRepeatedAndConcurrent(t *testing.T) {
	server, addr := startTestServer(t, testServerSpec{
		Mode:            ModePlain,
		ConnectTimeout:  2 * time.Second,
		ShutdownTimeout: 300 * time.Millisecond,
	})
	client := requireConnect(t, addr, mqttClientOptions{})

	const callers = 4
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	started := time.Now()
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- server.Close()
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	elapsed := time.Since(started)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Close() error = %v", err)
		}
	}
	if elapsed > 2*time.Second {
		t.Fatalf("concurrent Close() calls took %s", elapsed)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("repeated Close() error = %v", err)
	}
	if _, err := client.NextPublish(300 * time.Millisecond); err == nil {
		t.Fatal("client connection still delivers data after Close")
	}
}
