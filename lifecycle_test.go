package main

// 本文件覆盖连接生命周期与资源管理：
//   - 慢 TLS 握手受超时与并发上限约束，正常客户端只是排队而非失败；
//   - 不发送 MQTT CONNECT 的连接会超时关闭；
//   - 停止服务会关闭监听与现有连接，并可在有界时间内重复调用；
//   - 端口占用导致启动失败时仍完成诊断收尾，释放后可重试；
//   - 认证失败后可在同一服务上重试成功；
//   - 终端输出故障/队列溢出不会破坏结构化事件；
//   - 记录过滤器只影响记录，不阻断 MQTT 通信。

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSlowTLSHandshakeTimesOutAndQueuesHealthyClient(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Lifecycle Slow Server CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	roots := testCertPool(t, serverCA)
	results := t.TempDir()

	server, addr := startTestServer(t, testServerSpec{
		Mode:                    ModeTLS,
		ServerChain:             []*testCert{serverLeaf},
		ResultsDir:              results,
		HandshakeTimeout:        300 * time.Millisecond,
		ConnectTimeout:          2 * time.Second,
		ShutdownTimeout:         3 * time.Second,
		MaxConcurrentHandshakes: 1,
	})

	slow, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial slow connection: %v", err)
	}
	defer slow.Close()
	slowRead := make(chan error, 1)
	go func() {
		_ = slow.SetReadDeadline(time.Now().Add(3 * time.Second))
		buffer := make([]byte, 1)
		_, readErr := slow.Read(buffer)
		slowRead <- readErr
	}()

	clientConfig := testClientTLSConfig(t, roots, "localhost")
	type connectResult struct {
		client *mqttTestClient
		err    error
	}
	healthy := make(chan connectResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		client, dialErr := dialMQTT(ctx, addr, mqttClientOptions{TLSConfig: clientConfig})
		healthy <- connectResult{client: client, err: dialErr}
	}()

	failure := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageTLS && event.Result == ResultFailure
	})
	if failure.TLS == nil || failure.TLS.FailureKind != "timeout" {
		t.Fatalf("slow handshake failure kind = %#v, want timeout", failure.TLS)
	}
	if err := <-slowRead; err == nil {
		t.Fatal("slow connection was not closed after the handshake timeout")
	}

	result := <-healthy
	if result.err != nil {
		t.Fatalf("healthy client failed while one handshake slot was occupied: %v", result.err)
	}
	defer func() { _ = result.client.Close() }()
	mustRoundTrip(t, result.client, "lifecycle/slow-handshake", "ok")

	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Counts.TLSFailure == 0 || summary.Counts.TLSSuccess == 0 {
		t.Fatalf("expected one TLS failure and one success, got %#v", summary.Counts)
	}
	if summary.Counts.Connections != 2 {
		t.Fatalf("connection count = %d, want 2", summary.Counts.Connections)
	}
}

func TestEstablishedConnectionsDoNotOccupyHandshakeSlots(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Lifecycle Slot Server CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	roots := testCertPool(t, serverCA)

	server, addr := startTestServer(t, testServerSpec{
		Mode:                    ModeTLS,
		ServerChain:             []*testCert{serverLeaf},
		HandshakeTimeout:        2 * time.Second,
		ConnectTimeout:          2 * time.Second,
		MaxConcurrentHandshakes: 1,
	})
	clientConfig := testClientTLSConfig(t, roots, "localhost")

	// 单次握手槽位不应被已经建立的长时间连接永久占用。
	var clients []*mqttTestClient
	for i := 0; i < 3; i++ {
		client := requireConnect(t, addr, mqttClientOptions{TLSConfig: clientConfig})
		clients = append(clients, client)
		mustRoundTrip(t, client, fmt.Sprintf("slots/%d", i), "alive")
	}
	for _, client := range clients {
		_ = client.Close()
	}
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
}

func TestStopDuringSlowHandshakeIsBounded(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Lifecycle Stop Server CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	server, addr := startTestServer(t, testServerSpec{
		Mode:             ModeTLS,
		ServerChain:      []*testCert{serverLeaf},
		HandshakeTimeout: 5 * time.Second,
		ShutdownTimeout:  time.Second,
	})

	slow, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial slow connection: %v", err)
	}
	defer slow.Close()
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageTransport && event.Action == "accept"
	})

	started := time.Now()
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Close() waited %s for a handshake with a 5s timeout", elapsed)
	}
}

func TestConnectTimeoutClosesSilentClient(t *testing.T) {
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:            ModePlain,
		ResultsDir:      results,
		ConnectTimeout:  250 * time.Millisecond,
		ShutdownTimeout: 2 * time.Second,
	})

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial silent connection: %v", err)
	}
	defer conn.Close()
	readDone := make(chan error, 1)
	go func() {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buffer := make([]byte, 1)
		_, readErr := conn.Read(buffer)
		readDone <- readErr
	}()

	closeEvent := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageTransport && event.Action == "close" && strings.Contains(event.Reason, "timeout")
	})
	if closeEvent.ConnectionID == "" {
		t.Fatal("connect timeout close event has no connection id")
	}
	if err := <-readDone; err == nil {
		t.Fatal("silent client was not disconnected after the CONNECT timeout")
	}
	for _, event := range server.Diagnostics().Events() {
		if event.ConnectionID == closeEvent.ConnectionID && event.Stage == StageMQTTConnect {
			t.Fatal("MQTT CONNECT was recorded although the client never sent one")
		}
	}

	client := requireConnect(t, addr, mqttClientOptions{})
	mustRoundTrip(t, client, "lifecycle/connect-timeout", "still-serving")
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
}

func TestStopClosesConnectionsAndIsIdempotent(t *testing.T) {
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:            ModePlain,
		ResultsDir:      results,
		ShutdownTimeout: 2 * time.Second,
	})
	client := requireConnect(t, addr, mqttClientOptions{})
	mustRoundTrip(t, client, "lifecycle/stop", "before-stop")

	started := time.Now()
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Close() took %s, want a bounded shutdown", elapsed)
	}
	if _, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		t.Fatal("listener still accepts connections after Close")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
	if _, err := client.NextPublish(300 * time.Millisecond); err == nil {
		t.Fatal("existing client connection still delivers data after Close")
	}

	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Output.Status != OutputComplete || !summary.Output.EventsComplete {
		t.Fatalf("shutdown output status = %#v, want complete", summary.Output)
	}
	if summary.EndedAt == "" {
		t.Fatal("summary has no end time after shutdown")
	}
	final := server.Diagnostics().Summary()
	if final.EndedAt == "" || final.Counts.Connections != 1 {
		t.Fatalf("final summary is not complete: %#v", final)
	}
}

func TestStartServerFailsOnOccupiedPortAndRetries(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := holder.Addr().String()
	results := t.TempDir()

	_, err = StartServer(StartOptions{
		Config: &Config{
			Listen:          addr,
			Mode:            ModePlain,
			ResultsDir:      results,
			RunID:           "run-port-in-use",
			ShutdownTimeout: 2 * time.Second,
		},
		LogWriter: io.Discard,
	})
	if err == nil {
		t.Fatal("StartServer succeeded on an occupied port")
	}
	if !strings.Contains(err.Error(), "cannot listen on") {
		t.Fatalf("startup error = %v, want listen failure", err)
	}
	// 失败的启动同样要完成诊断收尾，而不是留下半开的文件或监听器。
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Output.Status != OutputComplete || !summary.Output.EventsComplete {
		t.Fatalf("failed startup output status = %#v", summary.Output)
	}

	if err := holder.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	server, err := StartServer(StartOptions{
		Config: &Config{
			Listen:          addr,
			Mode:            ModePlain,
			ShutdownTimeout: 2 * time.Second,
		},
		LogWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("retry after releasing the port failed: %v", err)
	}
	if server.Addr() != addr {
		t.Fatalf("retry bound %s, want %s", server.Addr(), addr)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("close retried server: %v", err)
	}
}

func TestFailedAuthenticationCanRetry(t *testing.T) {
	server, addr := startTestServer(t, testServerSpec{
		Mode:  ModePlain,
		Users: []byte(integrationAuthUsers),
	})

	ctx, cancel := contextWithTestTimeout()
	wrong, err := dialMQTT(ctx, addr, mqttClientOptions{Username: "alice", Password: "wrong-password"})
	cancel()
	if wrong != nil {
		_ = wrong.Close()
	}
	var refused *mqttConnackRefusedError
	if !errors.As(err, &refused) || refused.Code != 0x05 {
		t.Fatalf("first attempt error = %v, want CONNACK 0x05", err)
	}

	client := requireConnect(t, addr, mqttClientOptions{Username: "alice", Password: "alice-secret"})
	mustRoundTrip(t, client, "allowed/retry", "retry-ok")
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
}

func TestConcurrentClientsWithinBudget(t *testing.T) {
	clients := runtime.GOMAXPROCS(0) / 2
	if clients < 1 {
		clients = 1
	}
	if clients > 8 {
		clients = 8
	}
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:            ModePlain,
		ResultsDir:      results,
		ConnectTimeout:  5 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	})

	errs := make(chan error, clients)
	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, err := dialMQTT(ctx, addr, mqttClientOptions{})
			if err != nil {
				errs <- fmt.Errorf("client %d connect: %w", index, err)
				return
			}
			defer client.Close()
			topic := fmt.Sprintf("concurrent/%d", index)
			if granted, err := client.Subscribe(topic, 1); err != nil {
				errs <- fmt.Errorf("client %d subscribe: %w", index, err)
				return
			} else if granted != 1 {
				errs <- fmt.Errorf("client %d subscribe granted QoS %d", index, granted)
				return
			}
			if err := client.Publish(topic, []byte("payload"), 1); err != nil {
				errs <- fmt.Errorf("client %d publish: %w", index, err)
				return
			}
			if message, err := client.NextPublish(3 * time.Second); err != nil {
				errs <- fmt.Errorf("client %d receive: %w", index, err)
				return
			} else if message.Topic != topic {
				errs <- fmt.Errorf("client %d received topic %q, want %q", index, message.Topic, topic)
			}
		}(i)
	}
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent clients did not finish within the budget")
	}
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Counts.Connections != uint64(clients) {
		t.Fatalf("connections = %d, want %d", summary.Counts.Connections, clients)
	}
	if summary.Counts.PublishEvents < uint64(clients) {
		t.Fatalf("publishes = %d, want at least %d", summary.Counts.PublishEvents, clients)
	}
}

type failingTerminal struct{}

func (failingTerminal) Write([]byte) (int, error) {
	return 0, errors.New("terminal is unavailable")
}

func TestTerminalFailureKeepsStructuredEventsComplete(t *testing.T) {
	results := t.TempDir()
	diag, err := NewDiagnostics(DiagnosticsOptions{
		Config:      &Config{Mode: ModePlain, ShutdownTimeout: 2 * time.Second},
		RunID:       "run-terminal-failure",
		ResultsDir:  results,
		Terminal:    failingTerminal{},
		ErrorWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("NewDiagnostics: %v", err)
	}
	const publishes = 50
	for i := 0; i < publishes; i++ {
		diag.Publish("", "", "terminal/topic", 0, false, nil)
	}
	if err := diag.Close(); err == nil {
		t.Fatal("Close() = nil, want the terminal write failure to be reported")
	}

	events := readJSONLEvents(t, filepath.Join(results, eventsFileName))
	if len(events) != publishes+2 {
		t.Fatalf("structured events = %d, want %d (terminal failures must not drop them)", len(events), publishes+2)
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Output.Status != OutputComplete || !summary.Output.EventsComplete {
		t.Fatalf("structured output status = %#v, want complete", summary.Output)
	}
	if len(summary.Output.WriteErrors) == 0 {
		t.Fatal("terminal write failure was not recorded in write_errors")
	}
}

// gatedWriter 在 release 关闭前阻塞，用来模拟很慢的终端。
type gatedWriter struct {
	release chan struct{}
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	<-w.release
	return len(p), nil
}

func TestTerminalQueueOverflowIsCountedAndDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	results := t.TempDir()
	diag, err := NewDiagnostics(DiagnosticsOptions{
		Config:      &Config{Mode: ModePlain, ShutdownTimeout: 2 * time.Second},
		RunID:       "run-terminal-overflow",
		ResultsDir:  results,
		Terminal:    &gatedWriter{release: release},
		ErrorWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("NewDiagnostics: %v", err)
	}

	const events = terminalQueueSize + 100
	started := time.Now()
	for i := 0; i < events; i++ {
		diag.Publish("", "", "overflow/topic", 0, false, nil)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("emitting events blocked on a slow terminal for %s", elapsed)
	}
	if dropped := diag.Summary().Counts.DroppedTerminal; dropped == 0 {
		t.Fatal("terminal queue overflow was not counted")
	}

	close(release)
	if err := diag.Close(); err == nil {
		t.Fatal("Close() = nil, want the queue overflow to be reported")
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Counts.Events != events+2 {
		t.Fatalf("event count = %d, want %d", summary.Counts.Events, events+2)
	}
	if summary.Counts.DroppedTerminal == 0 {
		t.Fatal("summary does not report dropped terminal events")
	}
	if !summary.Output.EventsComplete || summary.Output.Status != OutputComplete {
		t.Fatalf("structured output was marked incomplete: %#v", summary.Output)
	}
}

func TestDiagnosticsCloseIsBoundedWithStuckTerminal(t *testing.T) {
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	diag, err := NewDiagnostics(DiagnosticsOptions{
		Config:      &Config{Mode: ModePlain, ShutdownTimeout: 100 * time.Millisecond},
		RunID:       "run-stuck-terminal",
		Terminal:    &gatedWriter{release: release},
		ErrorWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("NewDiagnostics: %v", err)
	}
	diag.Publish("", "", "stuck/topic", 0, false, nil)

	started := time.Now()
	err = diag.Close()
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Close() blocked for %s on a stuck terminal", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "did not flush") {
		t.Fatalf("Close() error = %v, want terminal flush timeout", err)
	}
	close(release)
	released = true
}

func TestEventLogReportsWriteFailure(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close pipe reader: %v", err)
	}
	log := &EventLog{
		terminal:    io.Discard,
		errorWriter: io.Discard,
		data:        writer,
		errorSeen:   map[string]struct{}{},
	}
	log.Data("client", "topic", "payload")
	if len(log.Errors()) == 0 {
		t.Fatal("write failure to a closed pipe was not reported")
	}
	if !log.dataBroken {
		t.Fatal("data log was not marked broken after a write failure")
	}
	_ = log.Close()
}

func TestOpenEventLogRejectsUnwritablePath(t *testing.T) {
	if _, err := OpenEventLog(EventLogOptions{
		DataPath: filepath.Join(t.TempDir(), "missing", "data.csv"),
	}); err == nil {
		t.Fatal("OpenEventLog accepted a path in a missing directory")
	}
}

func TestRecordFilterDoesNotBlockMQTT(t *testing.T) {
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:                ModePlain,
		ResultsDir:          results,
		LogPayload:          true,
		RecordOnlyClientIDs: []string{"some-other-device"},
	})
	client := requireConnect(t, addr, mqttClientOptions{ClientID: "device-1"})
	mustRoundTrip(t, client, "devices/1/state", "record-filter-payload")

	publish := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StagePublish && event.Result == ResultAccepted
	})
	if _, ok := publish.Context["payload"]; ok {
		t.Fatal("payload was recorded although the client ID did not match the record filter")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Counts.PublishEvents == 0 {
		t.Fatal("record filter suppressed the publish event instead of only its payload")
	}
}
