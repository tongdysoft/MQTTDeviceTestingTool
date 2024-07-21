package main

// 本文件验证诊断事件与结果导出建立在真实监听路径上：
//   - TLS 握手失败且没有 MQTT CONNECT 时仍有事件；
//   - 成功握手包含证书摘要；
//   - 认证失败准确落在 mqtt_auth 阶段；
//   - 结果文件可解析、关闭后内容完整；
//   - 哨兵密码不会出现在终端、事件或汇总中；
//   - 证据不完整时不做超出观察的自动判定。

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const sentinelPassword = "sentinel-password-9f3a-do-not-log"

func waitForEvent(t *testing.T, diag *Diagnostics, timeout time.Duration, match func(Event) bool) Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, event := range diag.Events() {
			if match(event) {
				return event
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for diagnostic event; current events: %s", eventSummary(diag.Events()))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func eventSummary(events []Event) string {
	var parts []string
	for _, event := range events {
		parts = append(parts, event.Stage+"/"+event.Action+"/"+event.Result)
	}
	return strings.Join(parts, ", ")
}

func readJSONLEvents(t *testing.T, path string) []Event {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("events file is empty")
	}
	if content[len(content)-1] != '\n' {
		t.Fatal("events file does not end with a complete JSONL line")
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	events := make([]Event, 0, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("events line %d is empty", i+1)
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("events line %d is not valid JSON: %v\n%s", i+1, err, line)
		}
		if _, err := time.Parse(time.RFC3339Nano, event.Time); err != nil {
			t.Fatalf("event %d time %q is not RFC3339 with timezone: %v", i+1, event.Time, err)
		}
		events = append(events, event)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Seq != events[i-1].Seq+1 {
			t.Fatalf("event sequence jumped from %d to %d", events[i-1].Seq, events[i].Seq)
		}
	}
	return events
}

func readRunSummary(t *testing.T, path string) RunSummary {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read summary file: %v", err)
	}
	var summary RunSummary
	if err := json.Unmarshal(content, &summary); err != nil {
		t.Fatalf("summary file is not valid JSON: %v", err)
	}
	return summary
}

func mtlsMaterial(t *testing.T) (serverChain []*testCert, deviceCA *testCert, deviceLeaf *testCert, roots *x509.CertPool) {
	t.Helper()
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Diagnostics Server CA", notBefore, notAfter)
	deviceCA = newTestCA(t, "Diagnostics Device CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	deviceLeaf = newTestClientLeaf(t, "device-diagnostics", deviceCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)
	return []*testCert{serverLeaf}, deviceCA, deviceLeaf, testCertPool(t, serverCA)
}

func TestDiagnosticsTLSFailureWithoutCONNECT(t *testing.T) {
	serverChain, deviceCA, _, roots := mtlsMaterial(t)
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:        ModeMTLS,
		ServerChain: serverChain,
		ClientCAs:   []*testCert{deviceCA},
		ResultsDir:  results,
		ExpectTLS:   "failure",
	})

	// 客户端没有设备证书，服务端在 TLS 握手中拒绝；客户端不会发送 MQTT CONNECT。
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	clientConfig := testClientTLSConfig(t, roots, "localhost")
	// 固定 TLS 1.2：1.3 客户端可能在读到服务端拒绝前就返回握手完成，
	// 这里需要确定性的“握手失败”路径。
	clientConfig.MaxVersion = tls.VersionTLS12
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, clientConfig)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected the mTLS handshake to fail without a device certificate; ClientAuth=%v events=%s",
			server.TLSConfig().ClientAuth, eventSummary(server.Diagnostics().Events()))
	}

	failure := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageTLS && event.Result == ResultFailure
	})
	if failure.ConnectionID == "" {
		t.Fatal("TLS failure event has no connection id")
	}
	if failure.Remote == "" {
		t.Fatal("TLS failure event has no remote address")
	}
	if failure.TLS == nil || failure.TLS.FailureKind == "" {
		t.Fatalf("TLS failure event lacks failure classification: %#v", failure.TLS)
	}
	if failure.TLS.ClientCertificatePresent != nil || failure.TLS.PeerCertificateCount != nil {
		t.Fatalf("failed handshake must not assert unobserved certificate facts: %#v", failure.TLS)
	}
	if failure.Certainty == "" {
		t.Fatal("TLS failure event lacks certainty")
	}
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.ConnectionID == failure.ConnectionID && event.Stage == StageTransport && event.Action == "close"
	})

	for _, event := range server.Diagnostics().Events() {
		if event.ConnectionID == failure.ConnectionID && event.Stage == StageMQTTConnect {
			t.Fatalf("MQTT CONNECT event appeared without a completed TLS handshake: %#v", event)
		}
	}

	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	events := readJSONLEvents(t, filepath.Join(results, eventsFileName))
	if len(events) == 0 {
		t.Fatal("no events were written")
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Output.Status != OutputComplete || !summary.Output.EventsComplete {
		t.Fatalf("summary output status = %#v, want complete", summary.Output)
	}
	if summary.RunID == "" {
		t.Fatal("summary has no run id")
	}
	if len(summary.Connections) != 1 {
		t.Fatalf("summary connections = %d, want 1", len(summary.Connections))
	}
	connSummary := summary.Connections[0]
	if connSummary.Observed.TLS != ResultFailure {
		t.Fatalf("observed TLS = %q, want failure", connSummary.Observed.TLS)
	}
	if connSummary.Observed.MQTTConnect != "not_received" {
		t.Fatalf("observed MQTT connect = %q, want not_received", connSummary.Observed.MQTTConnect)
	}
	if connSummary.Verdict != VerdictPass {
		t.Fatalf("verdict = %q, want pass for expected TLS failure; reasons: %v", connSummary.Verdict, connSummary.Failures)
	}
}

func TestDiagnosticsSuccessIncludesCertificateSummary(t *testing.T) {
	serverChain, deviceCA, deviceLeaf, roots := mtlsMaterial(t)
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:             ModeMTLS,
		ServerChain:      serverChain,
		ClientCAs:        []*testCert{deviceCA},
		ResultsDir:       results,
		ExpectTLS:        "success",
		ExpectConnect:    "accepted",
		ExpectClientCert: "present",
		RunID:            "run-diagnostics-success",
		DeviceModel:      "demo-device",
		FirmwareVersion:  "1.2.3",
		TestCase:         "mtls-success",
		Note:             "自动化测试",
	})

	client := requireConnect(t, addr, mqttClientOptions{
		TLSConfig: testClientTLSConfig(t, roots, "localhost", deviceLeaf),
	})
	granted, err := client.Subscribe("diagnostics/allowed", 1)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if granted != 1 {
		t.Fatalf("subscribe granted QoS = %d, want 1", granted)
	}
	if err := client.Publish("diagnostics/allowed", []byte("payload-must-not-be-logged"), 1); err != nil {
		t.Fatalf("publish: %v", err)
	}

	success := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageTLS && event.Result == ResultSuccess
	})
	if success.TLS == nil {
		t.Fatal("successful TLS event lacks TLS details")
	}
	if success.TLS.Version == "" || success.TLS.CipherSuite == "" {
		t.Fatalf("TLS details missing version or cipher suite: %#v", success.TLS)
	}
	if success.TLS.ClientCertificatePresent == nil || !*success.TLS.ClientCertificatePresent ||
		success.TLS.PeerCertificateCount == nil || *success.TLS.PeerCertificateCount == 0 {
		t.Fatalf("client certificate was not recorded: %#v", success.TLS)
	}
	if success.TLS.VerifiedChainCount == nil || *success.TLS.VerifiedChainCount == 0 {
		t.Fatalf("verified chain count = 0, want at least 1")
	}
	wantFingerprint := certificateInfo(deviceLeaf.Certificate).SHA256
	if got := success.TLS.PeerCertificates[0].SHA256; got != wantFingerprint {
		t.Fatalf("peer certificate SHA-256 = %s, want %s", got, wantFingerprint)
	}
	if success.TLS.PeerCertificates[0].Serial == "" || success.TLS.PeerCertificates[0].Subject == "" {
		t.Fatalf("peer certificate summary is incomplete: %#v", success.TLS.PeerCertificates[0])
	}
	if success.TLS.PeerCertificates[0].NotBefore == "" || success.TLS.PeerCertificates[0].NotAfter == "" {
		t.Fatalf("peer certificate validity is missing: %#v", success.TLS.PeerCertificates[0])
	}

	publishEvent := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StagePublish && event.Result == ResultAccepted
	})
	if _, ok := publishEvent.Context["payload"]; ok {
		t.Fatal("payload was recorded without -log-payload")
	}
	if _, ok := publishEvent.Context["payload_base64"]; ok {
		t.Fatal("payload was recorded without -log-payload")
	}
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageSubscribe && event.Result == ResultGranted
	})

	if err := client.Disconnect(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageDisconnect
	})

	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	_ = readJSONLEvents(t, filepath.Join(results, eventsFileName))
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.RunID != "run-diagnostics-success" {
		t.Fatalf("run id = %q", summary.RunID)
	}
	if summary.Metadata.DeviceModel != "demo-device" || summary.Metadata.FirmwareVersion != "1.2.3" ||
		summary.Metadata.TestCase != "mtls-success" || summary.Metadata.Note != "自动化测试" {
		t.Fatalf("metadata was not preserved: %#v", summary.Metadata)
	}
	if summary.EffectiveConfig.Mode != string(ModeMTLS) || !summary.EffectiveConfig.ClientCertificateNeeded {
		t.Fatalf("effective config does not reflect mTLS: %#v", summary.EffectiveConfig)
	}
	if len(summary.EffectiveConfig.ServerCertificates) == 0 {
		t.Fatal("effective config lacks server certificate summary")
	}
	if summary.Verdict != VerdictPass {
		t.Fatalf("run verdict = %q, want pass; reasons: %v", summary.Verdict, summary.VerdictReasons)
	}
	if len(summary.Connections) != 1 || summary.Connections[0].Verdict != VerdictPass {
		t.Fatalf("connection verdict is not pass: %#v", summary.Connections)
	}
	final := server.Diagnostics().Summary()
	if final.Output.Status != OutputComplete || !final.Output.EventsComplete || final.EndedAt == "" {
		t.Fatalf("in-memory summary after close is not final: %#v", final.Output)
	}
	if final.RunID != summary.RunID || final.Counts.Events != summary.Counts.Events {
		t.Fatalf("in-memory summary differs from exported summary: %#v vs %#v", final.Counts, summary.Counts)
	}
}

func TestDiagnosticsAuthenticationFailureStage(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Diagnostics Auth Server CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	roots := testCertPool(t, serverCA)
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:          ModeTLS,
		ServerChain:   []*testCert{serverLeaf},
		Users:         []byte(integrationAuthUsers),
		ResultsDir:    results,
		ExpectTLS:     "success",
		ExpectConnect: "accepted",
	})

	ctx, cancel := contextWithTestTimeout()
	defer cancel()
	client, err := dialMQTT(ctx, addr, mqttClientOptions{
		TLSConfig: testClientTLSConfig(t, roots, "localhost"),
		Username:  "alice",
		Password:  "wrong-password",
	})
	if client != nil {
		_ = client.Close()
	}
	var refused *mqttConnackRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("expected CONNACK refusal, got %T: %v", err, err)
	}
	if refused.Code != 0x05 {
		t.Fatalf("CONNACK code = 0x%02x, want 0x05", refused.Code)
	}

	rejected := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageMQTTAuth && event.Result == ResultRejected
	})
	if rejected.ReasonCode != "0x05" {
		t.Fatalf("auth failure reason code = %q, want 0x05", rejected.ReasonCode)
	}
	connectEvent := waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.ConnectionID == rejected.ConnectionID && event.Stage == StageMQTTConnect
	})
	if connectEvent.Result != ResultReceived {
		t.Fatalf("CONNECT event result = %q, want received (not authenticated)", connectEvent.Result)
	}
	if connectEvent.Context["authenticated"] != "false" {
		t.Fatalf("CONNECT event claims authentication: %#v", connectEvent.Context)
	}
	for _, event := range server.Diagnostics().Events() {
		if event.ConnectionID == rejected.ConnectionID && event.Stage == StageTLS && event.Result == ResultFailure {
			t.Fatal("MQTT authentication failure was misclassified as a TLS failure")
		}
	}

	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	events := readJSONLEvents(t, filepath.Join(results, eventsFileName))
	if len(events) == 0 {
		t.Fatal("no events were written")
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Verdict != VerdictFail {
		t.Fatalf("run verdict = %q, want fail", summary.Verdict)
	}
	if len(summary.Connections) != 1 {
		t.Fatalf("connections = %d", len(summary.Connections))
	}
	conn := summary.Connections[0]
	if conn.Observed.TLS != ResultSuccess {
		t.Fatalf("observed TLS = %q, want success", conn.Observed.TLS)
	}
	if conn.Observed.MQTTAuth != ResultRejected {
		t.Fatalf("observed MQTT auth = %q, want rejected", conn.Observed.MQTTAuth)
	}
	if len(conn.Failures) == 0 {
		t.Fatal("failed connection has no failure reason")
	}
}

func TestDiagnosticsRedactsSentinelPassword(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Diagnostics Redaction CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	roots := testCertPool(t, serverCA)
	results := t.TempDir()
	var terminal strings.Builder
	server, addr := startTestServer(t, testServerSpec{
		Mode:             ModeTLS,
		ServerChain:      []*testCert{serverLeaf},
		Users:            []byte(integrationAuthUsers),
		ResultsDir:       results,
		DiagnosticWriter: &terminal,
	})

	ctx, cancel := contextWithTestTimeout()
	defer cancel()
	client, err := dialMQTT(ctx, addr, mqttClientOptions{
		TLSConfig: testClientTLSConfig(t, roots, "localhost"),
		Username:  "alice",
		Password:  sentinelPassword,
	})
	if client != nil {
		_ = client.Close()
	}
	var refused *mqttConnackRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("expected CONNACK refusal, got %T: %v", err, err)
	}
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageMQTTAuth && event.Result == ResultRejected
	})
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	if strings.Contains(terminal.String(), sentinelPassword) {
		t.Fatal("sentinel password leaked into terminal output")
	}

	eventsPath := filepath.Join(results, eventsFileName)
	summaryPath := filepath.Join(results, summaryFileName)
	for _, path := range []string{eventsPath, summaryPath} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(content), sentinelPassword) {
			t.Fatalf("sentinel password leaked into %s", path)
		}
	}
	for _, event := range readJSONLEvents(t, eventsPath) {
		encoded, _ := json.Marshal(event)
		if strings.Contains(string(encoded), sentinelPassword) {
			t.Fatalf("sentinel password leaked into event %#v", event)
		}
	}
}

func TestDiagnosticsIncompleteEvidenceIsUndetermined(t *testing.T) {
	serverChain, deviceCA, _, roots := mtlsMaterial(t)
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:             ModeMTLS,
		ServerChain:      serverChain,
		ClientCAs:        []*testCert{deviceCA},
		ResultsDir:       results,
		ExpectConnect:    "accepted",
		ExpectClientCert: "present",
	})

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	clientConfig := testClientTLSConfig(t, roots, "localhost")
	clientConfig.MaxVersion = tls.VersionTLS12
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, clientConfig)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected the mTLS handshake to fail without a device certificate")
	}
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageTLS && event.Result == ResultFailure
	})
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Verdict != VerdictUndetermined {
		t.Fatalf("run verdict = %q, want undetermined; reasons: %v", summary.Verdict, summary.VerdictReasons)
	}
	if len(summary.Connections) != 1 {
		t.Fatalf("connections = %d", len(summary.Connections))
	}
	connSummary := summary.Connections[0]
	if connSummary.Verdict != VerdictUndetermined {
		t.Fatalf("connection verdict = %q, want undetermined", connSummary.Verdict)
	}
	reasons := strings.Join(connSummary.UndeterminedReason, " ")
	if !strings.Contains(reasons, "无法观察") && !strings.Contains(reasons, "不足以") {
		t.Fatalf("undetermined reasons do not explain missing evidence: %q", reasons)
	}
}

// timeoutError 仅用于验证分类器对超时的确定性标注。
type diagTimeoutError struct{}

func (diagTimeoutError) Error() string   { return "i/o timeout" }
func (diagTimeoutError) Timeout() bool   { return true }
func (diagTimeoutError) Temporary() bool { return true }

func TestClassifyTLSFailureKeepsUncertainty(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		kind      string
		alert     string
		certainty string
	}{
		{
			name:      "unknown alert",
			err:       tls.AlertError(200),
			kind:      "alert",
			alert:     "unknown_alert",
			certainty: CertaintyUncertain,
		},
		{
			name:      "known alert",
			err:       tls.AlertError(40),
			kind:      "alert",
			alert:     "handshake_failure",
			certainty: CertaintyObserved,
		},
		{
			name:      "peer closed",
			err:       io.EOF,
			kind:      "peer_closed",
			certainty: CertaintyUncertain,
		},
		{
			name:      "timeout",
			err:       diagTimeoutError{},
			kind:      "timeout",
			certainty: CertaintyUncertain,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyTLSFailure(test.err)
			if got.Kind != test.kind {
				t.Fatalf("kind = %q, want %q", got.Kind, test.kind)
			}
			if got.Alert != test.alert {
				t.Fatalf("alert = %q, want %q", got.Alert, test.alert)
			}
			if got.Certainty != test.certainty {
				t.Fatalf("certainty = %q, want %q", got.Certainty, test.certainty)
			}
			if got.ClientCertMissing {
				t.Fatal("failure must not assert a device-side client certificate cause")
			}
		})
	}
}

func TestDiagnosticsResultsFileErrors(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err := StartServer(StartOptions{
		Config:    &Config{Listen: "127.0.0.1:0", Mode: ModePlain, ResultsDir: filePath},
		LogWriter: io.Discard,
	})
	if err == nil {
		t.Fatal("expected StartServer to reject a results path that is a file")
	}

	results := t.TempDir()
	if err := os.WriteFile(filepath.Join(results, eventsFileName), []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("write existing events: %v", err)
	}
	_, err = StartServer(StartOptions{
		Config:    &Config{Listen: "127.0.0.1:0", Mode: ModePlain, ResultsDir: results},
		LogWriter: io.Discard,
	})
	if err == nil {
		t.Fatal("expected StartServer to refuse overwriting an existing events file")
	}
}

func TestDiagnosticsSubscriptionAuthorizationAndDisconnectStages(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Diagnostics Stage CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	roots := testCertPool(t, serverCA)
	results := t.TempDir()
	server, addr := startTestServer(t, testServerSpec{
		Mode:          ModeTLS,
		ServerChain:   []*testCert{serverLeaf},
		Users:         []byte(integrationAuthUsers),
		ResultsDir:    results,
		ExpectConnect: "accepted",
	})
	client := requireConnect(t, addr, mqttClientOptions{
		TLSConfig: testClientTLSConfig(t, roots, "localhost"),
		Username:  "alice",
		Password:  "alice-secret",
	})

	if granted, err := client.Subscribe("allowed/#", 1); err != nil || granted != 1 {
		t.Fatalf("allowed subscribe: granted=%d err=%v", granted, err)
	}
	if granted, err := client.Subscribe("denied/#", 1); err != nil || granted != 0x80 {
		t.Fatalf("denied subscribe: granted=0x%02x err=%v", granted, err)
	}
	unsubscribeForTest(t, client, "allowed/#")
	if err := client.Publish("allowed/topic", []byte("ok"), 1); err != nil {
		t.Fatalf("allowed publish: %v", err)
	}
	// ACL 拒绝 QoS 1 发布会返回错误并断开连接，这是现有服务端行为。
	_ = client.Publish("denied/topic", []byte("blocked"), 1)

	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageSubscribe && event.Result == ResultDenied
	})
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageUnsubscribe
	})
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageAuthorization && event.Result == ResultDenied
	})
	waitForEvent(t, server.Diagnostics(), 3*time.Second, func(event Event) bool {
		return event.Stage == StageDisconnect && event.Result == ResultFailure
	})

	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if len(summary.Connections) != 1 {
		t.Fatalf("connections = %d", len(summary.Connections))
	}
	conn := summary.Connections[0]
	if conn.Observed.Subscriptions != 2 || conn.Observed.SubscriptionsDenied != 1 {
		t.Fatalf("subscription counts = %d/%d, want 2/1",
			conn.Observed.Subscriptions, conn.Observed.SubscriptionsDenied)
	}
	if conn.Observed.Publishes != 1 {
		t.Fatalf("publish count = %d, want 1 (denied publish must not be recorded as accepted)",
			conn.Observed.Publishes)
	}
	if conn.Observed.AuthorizationDenied != 1 {
		t.Fatalf("authorization denied count = %d, want 1", conn.Observed.AuthorizationDenied)
	}
	if conn.Observed.Disconnect != "observed" {
		t.Fatalf("disconnect not observed: %#v", conn.Observed)
	}
	if conn.Verdict != VerdictPass {
		t.Fatalf("verdict = %q, want pass; failures=%v undetermined=%v",
			conn.Verdict, conn.Failures, conn.UndeterminedReason)
	}
}

func unsubscribeForTest(t *testing.T, client *mqttTestClient, filter string) {
	t.Helper()
	id := client.nextPacketID()
	var body []byte
	body = append(body, byte(id>>8), byte(id))
	body = appendUTF8(body, filter)
	if err := client.writePacket(0xA2, body); err != nil {
		t.Fatalf("write UNSUBSCRIBE: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		fixed, packet, err := client.readPacket(time.Until(deadline))
		if err != nil {
			t.Fatalf("read UNSUBACK: %v", err)
		}
		if fixed>>4 != 11 {
			continue
		}
		if len(packet) < 2 || binary.BigEndian.Uint16(packet[0:2]) != id {
			t.Fatalf("UNSUBACK packet id does not match %d", id)
		}
		return
	}
}

func TestDiagnosticsConcurrentWritesRemainValidJSONL(t *testing.T) {
	results := t.TempDir()
	cfg := &Config{Mode: ModePlain}
	diag, err := NewDiagnostics(DiagnosticsOptions{
		Config:      cfg,
		RunID:       "run-concurrent",
		ResultsDir:  results,
		Terminal:    io.Discard,
		ErrorWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("NewDiagnostics: %v", err)
	}
	const workers = 12
	const perWorker = 40
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				diag.Publish("", "", "concurrent/topic", 0, false, nil)
			}
		}(i)
	}
	wg.Wait()
	if err := diag.Close(); err != nil {
		t.Fatalf("close diagnostics: %v", err)
	}
	events := readJSONLEvents(t, filepath.Join(results, eventsFileName))
	want := workers*perWorker + 2 // run/start + run/stop
	if len(events) != want {
		t.Fatalf("events = %d, want %d", len(events), want)
	}
	summary := readRunSummary(t, filepath.Join(results, summaryFileName))
	if summary.Output.Status != OutputComplete || !summary.Output.EventsComplete {
		t.Fatalf("output status = %#v", summary.Output)
	}
	if summary.Counts.PublishEvents != workers*perWorker {
		t.Fatalf("publish count = %d", summary.Counts.PublishEvents)
	}
	if summary.Counts.Events != uint64(want) {
		t.Fatalf("event count = %d, want %d", summary.Counts.Events, want)
	}
}

func TestDiagnosticsRedactionHelpers(t *testing.T) {
	privateKey := "-----BEGIN RSA PRIVATE KEY-----\nMIIE-secret-material\n-----END RSA PRIVATE KEY-----"
	certificate := "-----BEGIN CERTIFICATE-----\nMIIB-public-certificate\n-----END CERTIFICATE-----"
	text := "prefix\n" + certificate + "\n" + privateKey + "\nsuffix"
	redacted := redactPEMPrivateKeys(text)
	if strings.Contains(redacted, "MIIE-secret-material") {
		t.Fatalf("private key material was not redacted: %s", redacted)
	}
	if !strings.Contains(redacted, "MIIB-public-certificate") {
		t.Fatalf("public certificate block was removed unexpectedly: %s", redacted)
	}
	if !strings.HasPrefix(redacted, "prefix") || !strings.HasSuffix(redacted, "suffix") {
		t.Fatalf("text around PEM blocks was not preserved: %s", redacted)
	}

	diag := &Diagnostics{}
	diag.AddSecret("a1")
	got := diag.SanitizeError(errors.New("password=a1 in text\nsecond line\x1b[31m"))
	if strings.Contains(got, "a1") {
		t.Fatalf("short password was not redacted: %q", got)
	}
	if strings.ContainsAny(got, "\n\r\x1b") {
		t.Fatalf("control characters were not removed: %q", got)
	}
}
