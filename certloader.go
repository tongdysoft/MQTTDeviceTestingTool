package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LoadServerCertificate 解析服务器证书链和私钥，并保证两者匹配。
//
// 支持的私钥格式：
//   - 未加密的 PKCS#1 / SEC1 / PKCS#8 PEM 私钥；
//   - 旧式 RFC 1423 加密 PEM 私钥（需提供 password）。
//
// Go 标准库不支持加密的 PKCS#8（ENCRYPTED PRIVATE KEY），遇到时会明确报错，
// 不会忽略密码或尝试自行实现解密。password 非空但私钥未加密时同样报错，
// 避免用户误以为密码已经生效。
func LoadServerCertificate(certPEM, keyPEM []byte, password string) (tls.Certificate, error) {
	block, err := findPrivateKeyBlock(keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}

	keyPEMForPair := keyPEM
	wasEncrypted := false
	switch {
	case block.Type == "ENCRYPTED PRIVATE KEY":
		return tls.Certificate{}, errors.New("encrypted PKCS#8 private keys are not supported; provide an unencrypted key or a legacy RFC 1423 encrypted PEM key")
	case x509.IsEncryptedPEMBlock(block):
		if password == "" {
			return tls.Certificate{}, errors.New("server key is encrypted; -server-key-password is required")
		}
		der, err := x509.DecryptPEMBlock(block, []byte(password))
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("cannot decrypt server key: %w", err)
		}
		keyPEMForPair = pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der})
		wasEncrypted = true
	case password != "":
		return tls.Certificate{}, errors.New("server key password was provided, but the private key is not encrypted")
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEMForPair)
	if err != nil {
		if wasEncrypted {
			return tls.Certificate{}, fmt.Errorf("cannot load encrypted server certificate and key (wrong password or mismatched pair): %w", err)
		}
		return tls.Certificate{}, fmt.Errorf("cannot load server certificate and key: %w", err)
	}
	if len(cert.Certificate) == 0 {
		return tls.Certificate{}, errors.New("server certificate file contains no certificates")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("cannot parse server leaf certificate: %w", err)
	}
	cert.Leaf = leaf
	return cert, nil
}

// ValidateServerCertificatePeriod 检查服务器叶证书的有效期。
// 仅用于启动时的严格检查；测试反例可通过明确开关跳过这一项检查。
func ValidateServerCertificatePeriod(leaf *x509.Certificate, now time.Time) error {
	if leaf == nil {
		return errors.New("server certificate has no parsed leaf")
	}
	if now.Before(leaf.NotBefore) {
		return fmt.Errorf("server certificate is not valid before %s", leaf.NotBefore.Format(time.RFC3339))
	}
	if now.After(leaf.NotAfter) {
		return fmt.Errorf("server certificate expired at %s", leaf.NotAfter.Format(time.RFC3339))
	}
	return nil
}

// ValidateServerCertificateUsage 检查服务器证书是否允许 TLS 服务器身份用途。
// 未声明扩展密钥用途的证书按 X.509 语义视为不限用途，允许使用。
func ValidateServerCertificateUsage(leaf *x509.Certificate) error {
	if leaf == nil {
		return errors.New("server certificate has no parsed leaf")
	}
	if len(leaf.ExtKeyUsage) == 0 {
		return nil
	}
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageAny || usage == x509.ExtKeyUsageServerAuth {
			return nil
		}
	}
	return fmt.Errorf("server certificate extended key usage does not permit TLS server authentication: %v", leaf.ExtKeyUsage)
}

func findPrivateKeyBlock(keyPEM []byte) (*pem.Block, error) {
	rest := keyPEM
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, errors.New("server key file contains no PEM private key block")
		}
		if strings.Contains(block.Type, "PRIVATE KEY") {
			return block, nil
		}
		rest = remaining
	}
}
