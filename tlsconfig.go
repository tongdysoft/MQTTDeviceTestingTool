package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"
)

// BuildTLSConfig 是最终 tls.Config 的唯一构建入口。
// 它在创建监听器之前完成证书加载、客户端信任库加载和认证策略设置，
// 调用方不得在之后重新构造或覆盖 ClientAuth。
func (c *Config) BuildTLSConfig() (*tls.Config, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Mode == ModePlain {
		return nil, nil
	}

	minVersion, err := ParseTLSVersion(c.TLSMinVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid -tls-min: %w", err)
	}
	maxVersion, err := ParseTLSVersion(c.TLSMaxVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid -tls-max: %w", err)
	}

	certPEM, err := os.ReadFile(c.ServerCertFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read server certificate %s: %w", c.ServerCertFile, err)
	}
	keyPEM, err := os.ReadFile(c.ServerKeyFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read server key %s: %w", c.ServerKeyFile, err)
	}
	cert, err := LoadServerCertificate(certPEM, keyPEM, c.ServerKeyPassword)
	if err != nil {
		return nil, err
	}
	if !c.AllowInvalidServerCert {
		if err := ValidateServerCertificatePeriod(cert.Leaf, time.Now()); err != nil {
			return nil, fmt.Errorf("%w (use -allow-invalid-server-cert only for deliberate invalid-certificate tests)", err)
		}
		if err := ValidateServerCertificateUsage(cert.Leaf); err != nil {
			return nil, fmt.Errorf("%w (use -allow-invalid-server-cert only for deliberate invalid-certificate tests)", err)
		}
	}

	// 只在这里构造一次 tls.Config，确保后续不会丢失 ClientAuth。
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   minVersion,
		MaxVersion:   maxVersion,
		ClientAuth:   tls.NoClientCert,
	}

	if c.Mode == ModeMTLS {
		pool, err := LoadClientCAPool(c.ClientCAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.ClientCAs = pool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	} else if c.ClientCAFile != "" {
		// 该分支已被 Validate 拦截，这里保留防御性检查，避免将来静默忽略 CA。
		return nil, fmt.Errorf("mode %q does not use a client CA", c.Mode)
	}

	return tlsConfig, nil
}

// LoadClientCAPool 读取用于验证设备证书的 CA 信任库。
// 它只影响客户端证书验证，不能与服务器证书链或服务器私钥混用。
func LoadClientCAPool(path string) (*x509.CertPool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read client CA %s: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(content) {
		return nil, fmt.Errorf("client CA file %s contains no valid PEM certificates", path)
	}
	return pool, nil
}
