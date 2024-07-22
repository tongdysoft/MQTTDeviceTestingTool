package main

// 测试服务器启动辅助：使用项目真实的 StartServer 路径，
// 在回环地址动态分配端口，并通过 t.Cleanup 关闭监听与后台 goroutine。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// synchronizedBuffer 收集服务器日志，避免测试结束后仍有 goroutine 写入 t.Logf。
type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type testServerSpec struct {
	Mode SecurityMode
	// ServerChain 的第一项是服务器叶证书，其后是中间 CA；plain 模式可为空。
	ServerChain []*testCert
	// ClientCAs 是 mTLS 下用于验证设备证书的信任锚。
	ClientCAs []*testCert
	// Users 直接传给 StartServer，避免为每次测试生成用户文件。
	Users []byte

	TLSMin string
	TLSMax string

	// AllowInvalidServerCert 是既定的反例测试通道，只放行服务器证书语义检查，
	// 不改变客户端验证逻辑，也不改变 ClientAuth。
	AllowInvalidServerCert bool

	// 诊断与结果导出（测试用）。
	ResultsDir       string
	ExpectTLS        string
	ExpectConnect    string
	ExpectClientCert string
	RunID            string
	DeviceModel      string
	FirmwareVersion  string
	TestCase         string
	Note             string
	// DiagnosticWriter 用于断言终端输出；为空时丢弃。
	DiagnosticWriter io.Writer

	// 连接生命周期与记录过滤（零值使用默认值）。
	HandshakeTimeout        time.Duration
	ConnectTimeout          time.Duration
	ShutdownTimeout         time.Duration
	MaxConcurrentHandshakes int
	RecordOnlyClientIDs     []string
	RecordOnlyTopics        []string
	RecordOnlyPayloads      []string
	LogPayload              bool
}

// startTestServer 启动真实 MQTT 服务并返回实际绑定地址。
func startTestServer(t *testing.T, spec testServerSpec) (*RunningServer, string) {
	t.Helper()

	cfg := &Config{Listen: "127.0.0.1:0", Mode: spec.Mode}
	if spec.Mode != ModePlain {
		if len(spec.ServerChain) == 0 {
			t.Fatal("TLS test server requires a server certificate chain")
		}
		dir := t.TempDir()
		certPath := filepath.Join(dir, "server.pem")
		keyPath := filepath.Join(dir, "server.key")
		if err := os.WriteFile(certPath, pemForCerts(spec.ServerChain...), 0o600); err != nil {
			t.Fatalf("write server certificate: %v", err)
		}
		leaf := spec.ServerChain[0]
		if err := os.WriteFile(keyPath, pkcs8PEM(t, leaf.Key), 0o600); err != nil {
			t.Fatalf("write server key: %v", err)
		}
		cfg.ServerCertFile = certPath
		cfg.ServerKeyFile = keyPath

		if len(spec.ClientCAs) > 0 {
			caPath := filepath.Join(dir, "client-ca.pem")
			if err := os.WriteFile(caPath, pemForCerts(spec.ClientCAs...), 0o600); err != nil {
				t.Fatalf("write client CA: %v", err)
			}
			cfg.ClientCAFile = caPath
		}
		if spec.TLSMin != "" {
			cfg.TLSMinVersion = spec.TLSMin
		}
		if spec.TLSMax != "" {
			cfg.TLSMaxVersion = spec.TLSMax
		}
		cfg.AllowInvalidServerCert = spec.AllowInvalidServerCert
	}

	logs := &synchronizedBuffer{}
	cfg.ResultsDir = spec.ResultsDir
	cfg.ExpectTLS = spec.ExpectTLS
	cfg.ExpectConnect = spec.ExpectConnect
	cfg.ExpectClientCert = spec.ExpectClientCert
	cfg.RunID = spec.RunID
	cfg.DeviceModel = spec.DeviceModel
	cfg.FirmwareVersion = spec.FirmwareVersion
	cfg.TestCase = spec.TestCase
	cfg.Note = spec.Note
	cfg.HandshakeTimeout = spec.HandshakeTimeout
	cfg.ConnectTimeout = spec.ConnectTimeout
	cfg.ShutdownTimeout = spec.ShutdownTimeout
	cfg.MaxConcurrentHandshakes = spec.MaxConcurrentHandshakes
	cfg.RecordOnlyClientIDs = spec.RecordOnlyClientIDs
	cfg.RecordOnlyTopics = spec.RecordOnlyTopics
	cfg.RecordOnlyPayloads = spec.RecordOnlyPayloads
	cfg.LogPayload = spec.LogPayload
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate test config: %v", err)
	}
	diagnosticWriter := spec.DiagnosticWriter
	if diagnosticWriter == nil {
		diagnosticWriter = io.Discard
	}
	diag, err := NewDiagnostics(DiagnosticsOptions{
		Config:     cfg,
		RunID:      cfg.RunID,
		ResultsDir: cfg.ResultsDir,
		Metadata: RunMetadata{
			DeviceModel:     cfg.DeviceModel,
			FirmwareVersion: cfg.FirmwareVersion,
			TestCase:        cfg.TestCase,
			Note:            cfg.Note,
		},
		Terminal:    diagnosticWriter,
		ErrorWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("create test diagnostics: %v", err)
	}
	server, err := StartServer(StartOptions{Config: cfg, UserData: spec.Users, LogWriter: logs, Diagnostics: diag})
	if err != nil {
		t.Fatalf("StartServer(%s): %v", spec.Mode, err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close test server: %v", err)
		}
		if log := logs.String(); log != "" {
			t.Logf("test server log:\n%s", log)
		}
	})
	return server, server.Addr()
}

// requireConnect 建立真实 MQTT 连接，失败即终止当前测试。
func requireConnect(t *testing.T, addr string, opts mqttClientOptions) *mqttTestClient {
	t.Helper()
	ctx, cancel := contextWithTestTimeout()
	defer cancel()

	client, err := dialMQTT(ctx, addr, opts)
	if err != nil {
		t.Fatalf("MQTT connect to %s failed: %v", addr, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// requireNoConnack 断言失败发生在 CONNACK 之前（TLS、传输或协议层），
// 并拒绝把 MQTT 认证拒绝误判为 TLS 失败。
func requireNoConnack(t *testing.T, err error) *mqttClientError {
	t.Helper()
	if err == nil {
		t.Fatal("expected the connection to fail, but MQTT CONNECT succeeded")
	}
	var refused *mqttConnackRefusedError
	if errors.As(err, &refused) {
		t.Fatalf("got MQTT CONNACK refusal 0x%02x; expected a TLS or transport failure: %v", refused.Code, err)
	}
	var clientErr *mqttClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("unexpected error type %T: %v", err, err)
	}
	if clientErr.Stage == mqttStageTCP {
		t.Fatalf("TCP dial failed instead of TLS/MQTT failure: %v", err)
	}
	return clientErr
}

// requireConnackRefused 断言服务器返回了指定返回码的 CONNACK 拒绝。
func requireConnackRefused(t *testing.T, err error, wantCode byte) {
	t.Helper()
	if err == nil {
		t.Fatal("expected MQTT CONNECT to be refused, but it succeeded")
	}
	var refused *mqttConnackRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("expected CONNACK refusal 0x%02x, got %T: %v", wantCode, err, err)
	}
	if refused.Code != wantCode {
		t.Fatalf("CONNACK return code = 0x%02x, want 0x%02x", refused.Code, wantCode)
	}
}

func contextWithTestTimeout() (ctx context.Context, cancel context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
