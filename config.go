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
)

const (
	defaultListen   = "0.0.0.0:1883"
	defaultTLSMin   = "1.2"
	defaultTLSMax   = "1.3"
	defaultLanguage = "auto"
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
	ServerKeyPassword      string
	ClientCAFile           string
	TLSMinVersion          string
	TLSMaxVersion          string
	AllowInvalidServerCert bool

	UserFile string

	OnlyClientIDs []string
	OnlyTopics    []string
	OnlyPayloads  []string

	LogDataFile   string
	LogStatusFile string
	LogFile       string
	FileTimestamp bool
	Monochrome    bool
	ShowVersion   bool
	Language      string

	// tlsVersionsSet 记录用户是否显式传入 TLS 版本参数。
	tlsVersionsSet bool
}

// ParseConfig 解析命令行参数并完成全部静态校验，不启动服务、不退出进程。
func ParseConfig(args []string) (*Config, error) {
	cfg := &Config{
		Listen:        defaultListen,
		Mode:          ModePlain,
		TLSMinVersion: defaultTLSMin,
		TLSMaxVersion: defaultTLSMax,
		Language:      defaultLanguage,
	}

	fs := flag.NewFlagSet("mqtt-test-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		mode          string
		onlyClientIDs string
		onlyTopics    string
		onlyPayloads  string
	)
	fs.StringVar(&cfg.Listen, "listen", defaultListen, "Listen address (IP:port)")
	fs.StringVar(&mode, "mode", string(ModePlain), "Security mode: plain, tls, mtls")
	fs.StringVar(&cfg.ServerCertFile, "server-cert", "", "Server certificate chain PEM file (required for tls/mtls)")
	fs.StringVar(&cfg.ServerKeyFile, "server-key", "", "Server private key PEM file (required for tls/mtls)")
	fs.StringVar(&cfg.ServerKeyPassword, "server-key-password", "", "Password for a legacy RFC 1423 encrypted server key")
	fs.StringVar(&cfg.ClientCAFile, "client-ca", "", "CA bundle used to verify device certificates (required for mtls)")
	fs.StringVar(&cfg.TLSMinVersion, "tls-min", defaultTLSMin, "Minimum TLS version: 1.2 or 1.3")
	fs.StringVar(&cfg.TLSMaxVersion, "tls-max", defaultTLSMax, "Maximum TLS version: 1.2 or 1.3")
	fs.BoolVar(&cfg.AllowInvalidServerCert, "allow-invalid-server-cert", false, "Skip server certificate validity-period and serverAuth EKU checks (test use)")
	fs.StringVar(&cfg.UserFile, "users", "", "Users and permissions file (.yaml/.json)")
	fs.StringVar(&onlyClientIDs, "only-client-id", "", "Only allow these client IDs (comma separated)")
	fs.StringVar(&onlyTopics, "only-topic", "", "Only allow these topics (comma separated)")
	fs.StringVar(&onlyPayloads, "only-payload", "", "Only allow messages containing these words (comma separated)")
	fs.StringVar(&cfg.LogDataFile, "log-data", "", "Log message data to a CSV file")
	fs.StringVar(&cfg.LogStatusFile, "log-status", "", "Log state changes to a CSV file")
	fs.StringVar(&cfg.LogFile, "log", "", "Save text log to a file")
	fs.BoolVar(&cfg.FileTimestamp, "log-timestamp", false, "Use Unix timestamps in logged CSV files")
	fs.BoolVar(&cfg.Monochrome, "monochrome", false, "Use a monochrome color scheme")
	fs.StringVar(&cfg.Language, "lang", defaultLanguage, "Language: auto, en, chs")
	fs.BoolVar(&cfg.ShowVersion, "version", false, "Print version info and exit")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "tls-min" || f.Name == "tls-max" {
			cfg.tlsVersionsSet = true
		}
	})
	cfg.OnlyClientIDs = splitList(onlyClientIDs)
	cfg.OnlyTopics = splitList(onlyTopics)
	cfg.OnlyPayloads = splitList(onlyPayloads)
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
		if c.ServerCertFile != "" || c.ServerKeyFile != "" || c.ServerKeyPassword != "" || c.ClientCAFile != "" {
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
