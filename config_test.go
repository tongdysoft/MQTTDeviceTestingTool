package main

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig(nil)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.Mode != ModePlain {
		t.Errorf("Mode = %q, want %q", cfg.Mode, ModePlain)
	}
	if cfg.Listen != defaultListen {
		t.Errorf("Listen = %q, want %q", cfg.Listen, defaultListen)
	}
	if cfg.TLSMinVersion != defaultTLSMin || cfg.TLSMaxVersion != defaultTLSMax {
		t.Errorf("TLS range = %s-%s, want %s-%s", cfg.TLSMinVersion, cfg.TLSMaxVersion, defaultTLSMin, defaultTLSMax)
	}
}

func TestParseConfigRejectsTLSMaterialInPlainMode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"server cert", []string{"-server-cert", "server.pem"}, "does not accept TLS materials"},
		{"server key", []string{"-server-key", "server.key"}, "does not accept TLS materials"},
		{"client CA", []string{"-client-ca", "ca.pem"}, "does not accept TLS materials"},
		{"key password", []string{"-server-key-password-file", "do-not-echo"}, "does not accept TLS materials"},
		{"explicit TLS min", []string{"-tls-min", "1.2"}, "-tls-min/-tls-max"},
		{"explicit TLS max", []string{"-tls-max", "1.3"}, "-tls-min/-tls-max"},
		{"invalid cert switch", []string{"-allow-invalid-server-cert"}, "only valid with"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseConfig(test.args)
			if err == nil {
				t.Fatal("ParseConfig() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error = %q, want substring %q", err, test.want)
			}
			if strings.Contains(err.Error(), "do-not-echo") {
				t.Errorf("error leaked key password: %q", err)
			}
		})
	}
}

func TestValidateModeRequirements(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "tls requires server certificate",
			cfg:  Config{Mode: ModeTLS, ServerKeyFile: "server.key"},
			want: "requires -server-cert",
		},
		{
			name: "tls requires server key",
			cfg:  Config{Mode: ModeTLS, ServerCertFile: "server.pem"},
			want: "requires -server-key",
		},
		{
			name: "mtls requires client CA",
			cfg:  Config{Mode: ModeMTLS, ServerCertFile: "server.pem", ServerKeyFile: "server.key"},
			want: "requires -client-ca",
		},
		{
			name: "tls rejects client CA",
			cfg:  Config{Mode: ModeTLS, ServerCertFile: "server.pem", ServerKeyFile: "server.key", ClientCAFile: "ca.pem"},
			want: "requires -mode mtls",
		},
		{
			name: "unknown mode",
			cfg:  Config{Mode: "secure"},
			want: "unknown mode",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestParseTLSVersion(t *testing.T) {
	for _, value := range []string{"1.2", "tls1.2", "TLS 1.3", "1.3"} {
		if _, err := ParseTLSVersion(value); err != nil {
			t.Errorf("ParseTLSVersion(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"1.0", "1.1", "1.4", ""} {
		if _, err := ParseTLSVersion(value); err == nil {
			t.Errorf("ParseTLSVersion(%q) error = nil, want error", value)
		}
	}
}

func TestParseConfigRejectsInvertedTLSRange(t *testing.T) {
	_, err := ParseConfig([]string{
		"-mode", "tls",
		"-server-cert", "server.pem",
		"-server-key", "server.key",
		"-tls-min", "1.3",
		"-tls-max", "1.2",
	})
	if err == nil || !strings.Contains(err.Error(), "must not be higher") {
		t.Fatalf("ParseConfig() error = %v, want inverted range error", err)
	}
}

func TestValidateRejectsMissingCertificateFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Mode:           ModeTLS,
		ServerCertFile: filepath.Join(dir, "missing.pem"),
		ServerKeyFile:  filepath.Join(dir, "missing.key"),
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Validate() error = %v, want missing file error", err)
	}
}

func TestBuildTLSMaterial(t *testing.T) {
	files := generateTestCertificateFiles(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))

	plain := &Config{Mode: ModePlain}
	if got, err := plain.BuildTLSMaterial(); err != nil || got != nil {
		t.Fatalf("plain BuildTLSMaterial() = (%v, %v), want (nil, nil)", got, err)
	}

	tlsCfg := &Config{
		Mode:           ModeTLS,
		ServerCertFile: files.certFile,
		ServerKeyFile:  files.keyFile,
	}
	material, err := tlsCfg.BuildTLSMaterial()
	if err != nil {
		t.Fatalf("tls BuildTLSMaterial() error = %v", err)
	}
	got := material.Config
	if got.ClientAuth != tls.NoClientCert {
		t.Errorf("tls ClientAuth = %v, want NoClientCert", got.ClientAuth)
	}
	if got.ClientCAs != nil {
		t.Error("tls ClientCAs != nil, want nil")
	}
	if len(got.Certificates) != 1 || got.Certificates[0].Leaf == nil {
		t.Errorf("tls Certificates = %d, want one certificate with parsed leaf", len(got.Certificates))
	}
	if material.Report.ServerKeyEncrypted {
		t.Error("unencrypted key reported as encrypted")
	}
	if !strings.Contains(material.Report.ServerKeyPasswordSource, "not used") {
		t.Errorf("password source = %q, want not-used note for unencrypted key", material.Report.ServerKeyPasswordSource)
	}
	if got.MinVersion != tls.VersionTLS12 || got.MaxVersion != tls.VersionTLS13 {
		t.Errorf("tls range = %x-%x, want TLS 1.2-1.3", got.MinVersion, got.MaxVersion)
	}

	mtlsCfg := &Config{
		Mode:           ModeMTLS,
		ServerCertFile: files.certFile,
		ServerKeyFile:  files.keyFile,
		ClientCAFile:   files.caFile,
		TLSMinVersion:  "1.3",
		TLSMaxVersion:  "1.3",
	}
	material, err = mtlsCfg.BuildTLSMaterial()
	if err != nil {
		t.Fatalf("mtls BuildTLSMaterial() error = %v", err)
	}
	got = material.Config
	if got.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("mtls ClientAuth = %v, want RequireAndVerifyClientCert", got.ClientAuth)
	}
	if got.ClientCAs == nil {
		t.Fatal("mtls ClientCAs = nil, want loaded CA pool")
	}
	if got.MinVersion != tls.VersionTLS13 || got.MaxVersion != tls.VersionTLS13 {
		t.Errorf("mtls range = %x-%x, want TLS 1.3 only", got.MinVersion, got.MaxVersion)
	}
}

func TestBuildTLSMaterialRejectsExpiredServerCertificateUnlessAllowed(t *testing.T) {
	files := generateTestCertificateFiles(t, time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour))
	cfg := &Config{
		Mode:           ModeTLS,
		ServerCertFile: files.certFile,
		ServerKeyFile:  files.keyFile,
	}
	if _, err := cfg.BuildTLSMaterial(); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("BuildTLSMaterial() error = %v, want expired certificate error", err)
	}

	cfg.AllowInvalidServerCert = true
	if _, err := cfg.BuildTLSMaterial(); err != nil {
		t.Fatalf("BuildTLSMaterial() with allow-invalid error = %v", err)
	}
}

func TestStartServerTLSAndMTLSBind(t *testing.T) {
	files := generateTestCertificateFiles(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	tests := []struct {
		mode       SecurityMode
		clientCA   string
		clientAuth tls.ClientAuthType
	}{
		{ModeTLS, "", tls.NoClientCert},
		{ModeMTLS, files.caFile, tls.RequireAndVerifyClientCert},
	}
	for _, test := range tests {
		t.Run(string(test.mode), func(t *testing.T) {
			cfg := &Config{
				Listen:         "127.0.0.1:0",
				Mode:           test.mode,
				ServerCertFile: files.certFile,
				ServerKeyFile:  files.keyFile,
				ClientCAFile:   test.clientCA,
			}
			server, err := StartServer(StartOptions{Config: cfg, LogWriter: io.Discard})
			if err != nil {
				t.Fatalf("StartServer() error = %v", err)
			}
			defer func() { _ = server.Close() }()

			if server.TLSConfig() == nil {
				t.Fatal("TLSConfig() = nil, want TLS config")
			}
			if server.TLSConfig().ClientAuth != test.clientAuth {
				t.Errorf("ClientAuth = %v, want %v", server.TLSConfig().ClientAuth, test.clientAuth)
			}
			if _, port, err := net.SplitHostPort(server.Addr()); err != nil || port == "0" {
				t.Errorf("Addr() = %q, %v; want bound dynamic port", server.Addr(), err)
			}
		})
	}
}

func TestBuildTLSMaterialRejectsWrongServerEKUUnlessAllowed(t *testing.T) {
	files := generateTestCertificateFilesWithEKU(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	cfg := &Config{
		Mode:           ModeTLS,
		ServerCertFile: files.certFile,
		ServerKeyFile:  files.keyFile,
	}
	if _, err := cfg.BuildTLSMaterial(); err == nil || !strings.Contains(err.Error(), "extended key usage") {
		t.Fatalf("BuildTLSMaterial() error = %v, want server EKU error", err)
	}

	cfg.AllowInvalidServerCert = true
	if _, err := cfg.BuildTLSMaterial(); err != nil {
		t.Fatalf("BuildTLSMaterial() with allow-invalid error = %v", err)
	}
}

func TestStartServerPlainReturnsBoundAddress(t *testing.T) {
	cfg := &Config{Listen: "127.0.0.1:0", Mode: ModePlain}
	server, err := StartServer(StartOptions{Config: cfg, LogWriter: io.Discard})
	if err != nil {
		t.Fatalf("StartServer() error = %v", err)
	}
	defer func() {
		if err := server.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	host, port, err := net.SplitHostPort(server.Addr())
	if err != nil {
		t.Fatalf("Addr() = %q, %v", server.Addr(), err)
	}
	if host != "127.0.0.1" || port == "0" || port == "" {
		t.Errorf("Addr() = %q, want bound 127.0.0.1 port", server.Addr())
	}
	if server.TLSConfig() != nil {
		t.Error("plain TLSConfig() != nil, want nil")
	}
}
