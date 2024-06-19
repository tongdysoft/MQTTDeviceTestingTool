package main

import (
	"crypto/tls"
	"fmt"
	"time"
)

// TLSMaterial 是构建完成的 TLS 配置及其可审计的证书加载报告。
type TLSMaterial struct {
	Config      *tls.Config
	Server      *ServerIdentity
	ClientTrust *ClientTrustStore
	Report      CertificateReport
}

// BuildTLSMaterial 是最终 tls.Config 的唯一构建入口。
// 它在创建监听器之前完成服务器身份加载、设备信任库加载和认证策略设置，
// 调用方不得在之后重新构造或覆盖 ClientAuth。
//
// 服务器证书链只来自 -server-cert，ClientCAs 只来自 -client-ca；
// 两者不会互相替代，也不需要提供服务器 CA 私钥。
func (c *Config) BuildTLSMaterial() (*TLSMaterial, error) {
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

	password := c.KeyPassword
	if password == nil {
		// 嵌入式调用 StartServer 时也能使用密码文件、环境变量或交互提示；
		// 只有确认私钥加密后才会真正读取密码。
		password, err = ResolveKeyPassword(PasswordSourceOptions{File: c.ServerKeyPasswordFile})
		if err != nil {
			return nil, err
		}
	}

	identity, err := LoadServerIdentityFiles(c.ServerCertFile, c.ServerKeyFile, IdentityOptions{
		Password:               password,
		AllowInvalidServerCert: c.AllowInvalidServerCert,
		Now:                    time.Now(),
	})
	if err != nil {
		return nil, err
	}

	report := identity.Report
	report.ServerKeyPasswordSource = passwordSourceName(password, report.ServerKeyEncrypted)
	report.Warnings = append(report.Warnings, passwordWarnings(password)...)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{identity.Certificate},
		MinVersion:   minVersion,
		MaxVersion:   maxVersion,
		ClientAuth:   tls.NoClientCert,
	}

	material := &TLSMaterial{
		Server: identity,
		Report: report,
	}

	if c.Mode == ModeMTLS {
		trust, err := LoadClientTrustStoreFile(c.ClientCAFile)
		if err != nil {
			return nil, err
		}
		material.ClientTrust = trust
		material.Report.ClientCAs = len(trust.Certificates)
		material.Report.Warnings = append(material.Report.Warnings, trust.Warnings...)
		tlsConfig.ClientCAs = trust.Pool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	} else if c.ClientCAFile != "" {
		// 该分支已被 Validate 拦截，这里保留防御性检查，避免将来静默忽略 CA。
		return nil, fmt.Errorf("mode %q does not use a client CA", c.Mode)
	}

	material.Config = tlsConfig
	return material, nil
}

func passwordSourceName(password *KeyPasswordSpec, encrypted bool) string {
	if !encrypted {
		return "not used (key not encrypted)"
	}
	if password == nil {
		return "none"
	}
	return password.Source()
}

func passwordWarnings(password *KeyPasswordSpec) []string {
	if password == nil {
		return nil
	}
	return password.Warnings()
}
