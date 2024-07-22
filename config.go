package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListen   = "0.0.0.0:1883"
	defaultTLSMin   = "1.2"
	defaultTLSMax   = "1.3"
	defaultLanguage = "auto"

	defaultHandshakeTimeout        = 10 * time.Second
	defaultConnectTimeout          = 30 * time.Second
	defaultShutdownTimeout         = 10 * time.Second
	defaultMaxConcurrentHandshakes = 64
	maxConcurrentHandshakesLimit   = 4096
)

// SecurityMode 是启动时明确选择的连接安全模式。
type SecurityMode string

const (
	// ModePlain 为普通 MQTT，不建立 TLS，也不接受任何 TLS 材料。
	ModePlain SecurityMode = "plain"
	// ModeTLS 为单向 TLS：服务器出示证书，不请求设备证书。
	ModeTLS SecurityMode = "tls"
	// ModeMTLS 为强制双向 TLS：服务器出示证书，并要求且验证设备证书。
	ModeMTLS SecurityMode = "mtls"
)

// Config 是启动参数经过解析和校验后的唯一配置来源。
type Config struct {
	Listen                 string
	Mode                   SecurityMode
	ServerCertFile         string
	ServerKeyFile          string
	ServerKeyPasswordFile  string
	KeyPassword            *KeyPasswordSpec
	ClientCAFile           string
	TLSMinVersion          string
	TLSMaxVersion          string
	AllowInvalidServerCert bool

	UserFile string

	// RecordOnly* 只限制写入 CSV 与诊断事件的消息内容，不影响连接、
	// 认证、订阅或发布；真正的访问控制由 -users 的 auth/acl 配置负责。
	RecordOnlyClientIDs []string
	RecordOnlyTopics    []string
	RecordOnlyPayloads  []string

	// 连接生命周期。零值会在 Validate 中补默认值。
	HandshakeTimeout        time.Duration
	ConnectTimeout          time.Duration
	ShutdownTimeout         time.Duration
	MaxConcurrentHandshakes int

	// DebugHTTPListen 为空表示不启动 pprof HTTP 监听。
	DebugHTTPListen string

	LogDataFile   string
	LogStatusFile string
	LogFile       string
	FileTimestamp bool
	Monochrome    bool
	ShowVersion   bool
	Language      string

	// 诊断与结果导出。
	RunID            string
	DeviceModel      string
	FirmwareVersion  string
	TestCase         string
	Note             string
	ResultsDir       string
	LogPayload       bool
	ExpectTLS        string
	ExpectConnect    string
	ExpectClientCert string

	// tlsVersionsSet 记录用户是否显式传入 TLS 版本参数。
	tlsVersionsSet bool
}

// ParseConfig 解析命令行参数并完成全部静态校验，不启动服务、不退出进程。
func ParseConfig(args []string) (*Config, error) {
	cfg := &Config{
		Listen:           defaultListen,
		Mode:             ModePlain,
		TLSMinVersion:    defaultTLSMin,
		TLSMaxVersion:    defaultTLSMax,
		Language:         defaultLanguage,
		ExpectTLS:        expectationAny,
		ExpectConnect:    expectationAny,
		ExpectClientCert: expectationAny,
	}

	fs := flag.NewFlagSet("mqtt-test-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		mode            string
		recordClientIDs string
		recordTopics    string
		recordPayloads  string
	)
	fs.StringVar(&cfg.Listen, "listen", defaultListen, "Listen address (IP:port)")
	fs.StringVar(&mode, "mode", string(ModePlain), "Security mode: plain, tls, mtls")
	fs.StringVar(&cfg.ServerCertFile, "server-cert", "", "Server certificate chain PEM file (required for tls/mtls)")
	fs.StringVar(&cfg.ServerKeyFile, "server-key", "", "Server private key PEM file (required for tls/mtls)")
	fs.StringVar(&cfg.ServerKeyPasswordFile, "server-key-password-file", "", "File containing the server key password (trailing newline stripped)")
	fs.StringVar(&cfg.ClientCAFile, "client-ca", "", "CA bundle used to verify device certificates (required for mtls)")
	fs.StringVar(&cfg.TLSMinVersion, "tls-min", defaultTLSMin, "Minimum TLS version: 1.2 or 1.3")
	fs.StringVar(&cfg.TLSMaxVersion, "tls-max", defaultTLSMax, "Maximum TLS version: 1.2 or 1.3")
	fs.BoolVar(&cfg.AllowInvalidServerCert, "allow-invalid-server-cert", false, "Allow semantic-only server certificate problems (expiry, EKU/key usage, CA-as-leaf, chain expiry) for deliberate negative tests")
	fs.StringVar(&cfg.UserFile, "users", "", "Users and permissions file (.yaml/.json); this is the access-control configuration")
	fs.StringVar(&recordClientIDs, "record-only-client-id", "", "Only record message data from these client IDs (comma separated; does not block connections)")
	fs.StringVar(&recordTopics, "record-only-topic", "", "Only record message data on these topics (comma separated; does not block publish/subscribe)")
	fs.StringVar(&recordPayloads, "record-only-payload", "", "Only record messages containing these words (comma separated; does not block delivery)")
	fs.DurationVar(&cfg.HandshakeTimeout, "handshake-timeout", defaultHandshakeTimeout, "Maximum time for a TLS handshake")
	fs.DurationVar(&cfg.ConnectTimeout, "connect-timeout", defaultConnectTimeout, "Maximum time from accept (or completed TLS handshake) until an MQTT CONNECT packet")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", defaultShutdownTimeout, "Maximum time to wait for connections and pending output during shutdown")
	fs.IntVar(&cfg.MaxConcurrentHandshakes, "max-handshakes", defaultMaxConcurrentHandshakes, "Maximum concurrent TLS handshakes; further connections wait in the listen backlog")
	fs.StringVar(&cfg.DebugHTTPListen, "pprof-listen", "", "Enable pprof HTTP diagnostics on this address (disabled by default, e.g. 127.0.0.1:6060)")
	fs.StringVar(&cfg.LogDataFile, "log-data", "", "Log message data to a CSV file")
	fs.StringVar(&cfg.LogStatusFile, "log-status", "", "Log state changes to a CSV file")
	fs.StringVar(&cfg.LogFile, "log", "", "Save text log to a file")
	fs.BoolVar(&cfg.FileTimestamp, "log-timestamp", false, "Use Unix timestamps in logged CSV files")
	fs.BoolVar(&cfg.Monochrome, "monochrome", false, "Use a monochrome color scheme")
	fs.StringVar(&cfg.Language, "lang", defaultLanguage, "Language: auto, en, chs")
	fs.BoolVar(&cfg.ShowVersion, "version", false, "Print version info and exit")
	fs.StringVar(&cfg.RunID, "run-id", "", "Run identifier (default: generated)")
	fs.StringVar(&cfg.DeviceModel, "device-model", "", "Device model for the run summary")
	fs.StringVar(&cfg.FirmwareVersion, "firmware", "", "Device firmware version for the run summary")
	fs.StringVar(&cfg.TestCase, "test-case", "", "Test case identifier for the run summary")
	fs.StringVar(&cfg.Note, "note", "", "Free-form operator note for the run summary")
	fs.StringVar(&cfg.ResultsDir, "results", "", "Write events.jsonl and summary.json into this directory")
	fs.BoolVar(&cfg.LogPayload, "log-payload", false, "Include application payloads in diagnostic events (default: off)")
	fs.StringVar(&cfg.ExpectTLS, "expect-tls", expectationAny, "Expected TLS outcome: any, success, failure")
	fs.StringVar(&cfg.ExpectConnect, "expect-connect", expectationAny, "Expected MQTT authentication outcome: any, accepted, rejected")
	fs.StringVar(&cfg.ExpectClientCert, "expect-client-cert", expectationAny, "Expected verified device certificate: any, present, absent")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	var valueErr error
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "tls-min" || f.Name == "tls-max" {
			cfg.tlsVersionsSet = true
		}
		// 零值在 Config 中表示“使用默认值”，但用户显式传入的 0/负数必须报错。
		switch f.Name {
		case "handshake-timeout":
			if cfg.HandshakeTimeout <= 0 {
				valueErr = fmt.Errorf("-handshake-timeout must be greater than zero")
			}
		case "connect-timeout":
			if cfg.ConnectTimeout <= 0 {
				valueErr = fmt.Errorf("-connect-timeout must be greater than zero")
			}
		case "shutdown-timeout":
			if cfg.ShutdownTimeout <= 0 {
				valueErr = fmt.Errorf("-shutdown-timeout must be greater than zero")
			}
		case "max-handshakes":
			if cfg.MaxConcurrentHandshakes < 1 || cfg.MaxConcurrentHandshakes > maxConcurrentHandshakesLimit {
				valueErr = fmt.Errorf("-max-handshakes must be between 1 and %d", maxConcurrentHandshakesLimit)
			}
		}
	})
	if valueErr != nil {
		return nil, valueErr
	}
	cfg.RecordOnlyClientIDs = splitList(recordClientIDs)
	cfg.RecordOnlyTopics = splitList(recordTopics)
	cfg.RecordOnlyPayloads = splitList(recordPayloads)
	cfg.Mode = SecurityMode(strings.ToLower(strings.TrimSpace(mode)))

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate 补齐默认值并检查模式、参数冲突、数值范围和文件可读性。
func (c *Config) Validate() error {
	c.applyDefaults()

	switch c.Mode {
	case ModePlain:
		if c.ServerCertFile != "" || c.ServerKeyFile != "" || c.ServerKeyPasswordFile != "" || c.KeyPassword != nil || c.ClientCAFile != "" {
			return fmt.Errorf("mode %q does not accept TLS materials; set -mode tls or -mode mtls", c.Mode)
		}
		if c.AllowInvalidServerCert {
			return fmt.Errorf("-allow-invalid-server-cert is only valid with -mode tls or -mode mtls")
		}
		if c.tlsVersionsSet || c.TLSMinVersion != defaultTLSMin || c.TLSMaxVersion != defaultTLSMax {
			return fmt.Errorf("-tls-min/-tls-max are only valid with -mode tls or -mode mtls")
		}
	case ModeTLS, ModeMTLS:
		if c.ServerCertFile == "" {
			return fmt.Errorf("mode %q requires -server-cert", c.Mode)
		}
		if c.ServerKeyFile == "" {
			return fmt.Errorf("mode %q requires -server-key", c.Mode)
		}
		if c.Mode == ModeTLS && c.ClientCAFile != "" {
			return fmt.Errorf("mode %q does not verify device certificates; -client-ca requires -mode mtls", c.Mode)
		}
		if c.Mode == ModeMTLS && c.ClientCAFile == "" {
			return fmt.Errorf("mode %q requires -client-ca to verify device certificates", c.Mode)
		}
		min, err := ParseTLSVersion(c.TLSMinVersion)
		if err != nil {
			return fmt.Errorf("invalid -tls-min: %w", err)
		}
		max, err := ParseTLSVersion(c.TLSMaxVersion)
		if err != nil {
			return fmt.Errorf("invalid -tls-max: %w", err)
		}
		if min > max {
			return fmt.Errorf("-tls-min (%s) must not be higher than -tls-max (%s)", c.TLSMinVersion, c.TLSMaxVersion)
		}
	default:
		return fmt.Errorf("unknown mode %q; supported modes: plain, tls, mtls", c.Mode)
	}

	if err := validateListenAddress(c.Listen); err != nil {
		return err
	}
	if c.HandshakeTimeout <= 0 {
		return fmt.Errorf("-handshake-timeout must be greater than zero")
	}
	if c.ConnectTimeout <= 0 {
		return fmt.Errorf("-connect-timeout must be greater than zero")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("-shutdown-timeout must be greater than zero")
	}
	if c.MaxConcurrentHandshakes < 1 || c.MaxConcurrentHandshakes > maxConcurrentHandshakesLimit {
		return fmt.Errorf("-max-handshakes must be between 1 and %d", maxConcurrentHandshakesLimit)
	}
	if c.DebugHTTPListen != "" {
		if err := validateListenAddress(c.DebugHTTPListen); err != nil {
			return fmt.Errorf("invalid -pprof-listen: %w", err)
		}
	}
	if c.RunID != "" {
		if err := validateRunID(c.RunID); err != nil {
			return err
		}
	}
	if err := validateExpectations(Expectations{
		TLS:               c.ExpectTLS,
		MQTTConnect:       c.ExpectConnect,
		ClientCertificate: c.ExpectClientCert,
	}); err != nil {
		return err
	}
	if c.ResultsDir != "" {
		if info, err := os.Stat(c.ResultsDir); err == nil && !info.IsDir() {
			return fmt.Errorf("-results path is not a directory: %s", c.ResultsDir)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot access -results path %s: %w", c.ResultsDir, err)
		}
	}
	if err := validateDistinctLogPaths(c.LogFile, c.LogStatusFile, c.LogDataFile); err != nil {
		return err
	}

	switch c.Language {
	case "auto", "en", "chs":
	default:
		return fmt.Errorf("unknown language %q; supported languages: auto, en, chs", c.Language)
	}

	for _, f := range []struct {
		label string
		path  string
	}{
		{"server certificate", c.ServerCertFile},
		{"server key", c.ServerKeyFile},
		{"server key password", c.ServerKeyPasswordFile},
		{"client CA", c.ClientCAFile},
		{"users file", c.UserFile},
	} {
		if f.path == "" {
			continue
		}
		if err := validateReadableFile(f.label, f.path); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = defaultListen
	}
	if c.Mode == "" {
		c.Mode = ModePlain
	}
	if c.TLSMinVersion == "" {
		c.TLSMinVersion = defaultTLSMin
	}
	if c.TLSMaxVersion == "" {
		c.TLSMaxVersion = defaultTLSMax
	}
	if c.Language == "" {
		c.Language = defaultLanguage
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = defaultHandshakeTimeout
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = defaultConnectTimeout
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	if c.MaxConcurrentHandshakes == 0 {
		c.MaxConcurrentHandshakes = defaultMaxConcurrentHandshakes
	}
	c.ExpectTLS = normalizeExpectation(c.ExpectTLS, "tls", []string{expectationAny, "success", "failure"})
	c.ExpectConnect = normalizeExpectation(c.ExpectConnect, "mqtt connect", []string{expectationAny, "accepted", "rejected"})
	c.ExpectClientCert = normalizeExpectation(c.ExpectClientCert, "client certificate", []string{expectationAny, "present", "absent"})
}

// ParseTLSVersion 把用户可读的版本号转换为 crypto/tls 常量。
// 为避免误用不安全协议，只接受 TLS 1.2 与 TLS 1.3。
func ParseTLSVersion(value string) (uint16, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, " ", "")
	v = strings.TrimPrefix(v, "tls")
	v = strings.TrimPrefix(v, "v")
	switch v {
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	case "1.0", "1.1":
		return 0, fmt.Errorf("TLS %s is not allowed; supported versions are 1.2 and 1.3", v)
	default:
		return 0, fmt.Errorf("unsupported TLS version %q; supported versions are 1.2 and 1.3", value)
	}
}

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	if port == "" {
		return fmt.Errorf("invalid listen address %q: port is required", address)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("invalid listen port %q: must be 0-65535 (0 selects an ephemeral port)", port)
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return fmt.Errorf("invalid listen host %q", host)
	}
	return nil
}

func validateDistinctLogPaths(paths ...string) error {
	seen := map[string]struct{}{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			return fmt.Errorf("log files must use distinct paths: %s", path)
		}
		seen[path] = struct{}{}
	}
	return nil
}

func validateReadableFile(label, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s file does not exist: %s", label, path)
		}
		return fmt.Errorf("cannot access %s file %s: %w", label, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s path is a directory, not a file: %s", label, path)
	}
	return nil
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

// RecordFilter 决定一条消息是否写入 CSV 与诊断负载。
// 它不参与认证或授权，不会被用于允许/拒绝连接、订阅或发布。
type RecordFilter struct {
	clientIDs map[string]struct{}
	topics    map[string]struct{}
	words     []string
}

// NewRecordFilter 把配置中的记录过滤条件转换为只读集合。
func (c *Config) NewRecordFilter() RecordFilter {
	filter := RecordFilter{}
	if len(c.RecordOnlyClientIDs) > 0 {
		filter.clientIDs = make(map[string]struct{}, len(c.RecordOnlyClientIDs))
		for _, id := range c.RecordOnlyClientIDs {
			filter.clientIDs[id] = struct{}{}
		}
	}
	if len(c.RecordOnlyTopics) > 0 {
		filter.topics = make(map[string]struct{}, len(c.RecordOnlyTopics))
		for _, topic := range c.RecordOnlyTopics {
			filter.topics[topic] = struct{}{}
		}
	}
	filter.words = append(filter.words, c.RecordOnlyPayloads...)
	return filter
}

// Empty 表示没有任何记录过滤条件。
func (f RecordFilter) Empty() bool {
	return len(f.clientIDs) == 0 && len(f.topics) == 0 && len(f.words) == 0
}

// Matches 判断消息内容是否应被记录。多个条件同时存在时必须全部满足。
func (f RecordFilter) Matches(clientID, topic string, payload []byte) bool {
	if len(f.clientIDs) > 0 {
		if _, ok := f.clientIDs[clientID]; !ok {
			return false
		}
	}
	if len(f.topics) > 0 {
		if _, ok := f.topics[topic]; !ok {
			return false
		}
	}
	if len(f.words) > 0 {
		text := string(payload)
		matched := false
		for _, word := range f.words {
			if strings.Contains(text, word) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
