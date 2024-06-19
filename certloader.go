package main

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/youmark/pkcs8"
)

// MaterialKind 标识证书材料加载失败的阶段，便于调用方和测试判断错误类型。
type MaterialKind string

const (
	MaterialFileRead          MaterialKind = "file_read"
	MaterialPEMDecode         MaterialKind = "pem_decode"
	MaterialCertificateParse  MaterialKind = "certificate_parse"
	MaterialPrivateKeyParse   MaterialKind = "private_key_parse"
	MaterialUnsupportedKey    MaterialKind = "unsupported_key_format"
	MaterialPasswordRequired  MaterialKind = "password_required"
	MaterialPasswordIncorrect MaterialKind = "password_incorrect"
	MaterialPasswordUnused    MaterialKind = "password_unused"
	MaterialKeyMismatch       MaterialKind = "key_mismatch"
	MaterialChain             MaterialKind = "chain"
	MaterialTrustStore        MaterialKind = "trust_store"
	MaterialValidity          MaterialKind = "validity"
	MaterialUsage             MaterialKind = "usage"
)

// MaterialError 保留失败阶段、文件路径和底层错误。
// 错误文本只包含路径、阶段和标准库错误，绝不包含私钥内容或密码。
type MaterialError struct {
	Kind    MaterialKind
	Path    string
	Message string
	Err     error
}

func (e *MaterialError) Error() string {
	var b strings.Builder
	if e.Path != "" {
		b.WriteString(e.Path)
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *MaterialError) Unwrap() error {
	return e.Err
}

// MaterialErrorKind 返回错误所属的证书材料阶段；非 MaterialError 返回空字符串。
func MaterialErrorKind(err error) MaterialKind {
	var materialErr *MaterialError
	if errors.As(err, &materialErr) {
		return materialErr.Kind
	}
	return ""
}

func newMaterialError(kind MaterialKind, path, format string, args ...any) *MaterialError {
	return &MaterialError{Kind: kind, Path: path, Message: fmt.Sprintf(format, args...)}
}

func wrapMaterialError(kind MaterialKind, path, message string, err error) *MaterialError {
	return &MaterialError{Kind: kind, Path: path, Message: message, Err: err}
}

func remapMaterialKind(err error, kind MaterialKind) error {
	var materialErr *MaterialError
	if errors.As(err, &materialErr) {
		copied := *materialErr
		copied.Kind = kind
		return &copied
	}
	return err
}

// CertificateSummary 是可安全写入日志的证书摘要。
type CertificateSummary struct {
	Subject   string
	Issuer    string
	NotBefore time.Time
	NotAfter  time.Time
	IsCA      bool
}

// CertificateReport 记录本次加载实际接受的链、被忽略的区块和放行的语义检查。
type CertificateReport struct {
	ServerChain             []CertificateSummary
	ServerKeyFormat         string
	ServerKeyPasswordSource string
	ServerKeyEncrypted      bool
	ClientCAs               int
	IgnoredPEMBlocks        int
	Warnings                []string
	Bypassed                []string
}

// ServerIdentity 是已解析、已配对并通过结构检查的服务器身份材料。
type ServerIdentity struct {
	Certificate tls.Certificate
	Chain       []*x509.Certificate
	Report      CertificateReport
}

// IdentityOptions 控制服务器身份加载。Path 字段只用于错误信息。
type IdentityOptions struct {
	CertPath               string
	KeyPath                string
	Password               *KeyPasswordSpec
	AllowInvalidServerCert bool
	Now                    time.Time
}

type keyFormatInfo struct {
	Format          string
	LegacyEncrypted bool
	Encrypted       bool
	IgnoredBlocks   int
}

// LoadServerIdentityFiles 读取并加载服务器证书链与私钥。
// 文件读取失败与文件内容错误使用不同的 MaterialKind。
func LoadServerIdentityFiles(certPath, keyPath string, opts IdentityOptions) (*ServerIdentity, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, wrapMaterialError(MaterialFileRead, certPath, "cannot read server certificate file", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, wrapMaterialError(MaterialFileRead, keyPath, "cannot read server key file", err)
	}
	opts.CertPath = certPath
	opts.KeyPath = keyPath
	return LoadServerIdentityPEM(certPEM, keyPEM, opts)
}

// LoadServerIdentityPEM 从 PEM 内容加载服务器证书链与私钥。
//
// 证书文件行为：
//   - CERTIFICATE 区块按文件顺序组成链，叶证书必须在第一位；
//   - 非 CERTIFICATE 区块（例如合并文件中的私钥）会被忽略并计入报告；
//   - 空文件、无 CERTIFICATE 区块、损坏区块或尾部非 PEM 数据会报错。
//
// 私钥文件行为：
//   - 必须且只能包含一个受支持的私钥区块；
//   - 非私钥区块会被忽略并计入报告；
//   - 空文件、无密钥区块、多个密钥区块或尾部非 PEM 数据会报错。
func LoadServerIdentityPEM(certPEM, keyPEM []byte, opts IdentityOptions) (*ServerIdentity, error) {
	chain, ignoredCertBlocks, err := parseCertificateChainPEM(certPEM, opts.CertPath)
	if err != nil {
		return nil, err
	}

	key, keyInfo, err := parsePrivateKeyPEM(keyPEM, opts.KeyPath, opts.Password)
	if err != nil {
		return nil, err
	}

	leafIndex := -1
	for i, cert := range chain {
		if publicKeyMatches(cert, key) {
			leafIndex = i
			break
		}
	}
	if leafIndex < 0 {
		return nil, newMaterialError(MaterialKeyMismatch, opts.KeyPath,
			"server private key does not match any certificate in the server certificate file")
	}
	if leafIndex != 0 {
		return nil, newMaterialError(MaterialChain, opts.CertPath,
			"server private key matches certificate %d (%s); the leaf certificate must be first in the chain",
			leafIndex+1, certificateName(chain[leafIndex]))
	}

	if err := validateChainStructure(chain, opts.CertPath); err != nil {
		return nil, err
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	failures, warnings := inspectServerChain(chain, now)

	report := CertificateReport{
		ServerKeyFormat:         keyInfo.Format,
		ServerKeyPasswordSource: passwordSourceName(opts.Password, keyInfo.Encrypted),
		ServerKeyEncrypted:      keyInfo.Encrypted,
		IgnoredPEMBlocks:        ignoredCertBlocks + keyInfo.IgnoredBlocks,
		Warnings:                warnings,
	}
	for _, cert := range chain {
		report.ServerChain = append(report.ServerChain, CertificateSummary{
			Subject:   certificateName(cert),
			Issuer:    cert.Issuer.String(),
			NotBefore: cert.NotBefore,
			NotAfter:  cert.NotAfter,
			IsCA:      cert.IsCA,
		})
	}
	if ignoredCertBlocks > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"ignored %d non-certificate PEM block(s) in the server certificate file", ignoredCertBlocks))
	}
	if keyInfo.IgnoredBlocks > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"ignored %d non-key PEM block(s) in the server key file", keyInfo.IgnoredBlocks))
	}
	if keyInfo.LegacyEncrypted {
		report.Warnings = append(report.Warnings, "server key uses legacy RFC 1423 PEM encryption for compatibility; prefer an unencrypted key with restricted permissions or encrypted PKCS#8")
	}

	if len(failures) > 0 {
		if !opts.AllowInvalidServerCert {
			return nil, semanticError(failures, opts.CertPath)
		}
		for _, failure := range failures {
			report.Bypassed = append(report.Bypassed, failure.String())
		}
	}

	rawChain := make([][]byte, len(chain))
	for i, cert := range chain {
		rawChain[i] = cert.Raw
	}
	return &ServerIdentity{
		Certificate: tls.Certificate{
			Certificate: rawChain,
			PrivateKey:  key,
			Leaf:        chain[0],
		},
		Chain:  chain,
		Report: report,
	}, nil
}

func semanticError(failures []semanticFailure, path string) error {
	messages := make([]string, 0, len(failures))
	for _, failure := range failures {
		messages = append(messages, failure.String())
	}
	return &MaterialError{
		Kind:    failures[0].kind,
		Path:    path,
		Message: strings.Join(messages, "; ") + " (use -allow-invalid-server-cert only for deliberate invalid-certificate tests)",
	}
}

func decodePEMBlocks(data []byte, path, role string) ([]*pem.Block, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, newMaterialError(MaterialPEMDecode, path, "%s file is empty", role)
	}
	if idx := bytes.Index(data, []byte("-----BEGIN")); idx > 0 && !allowedPEMPrefix(data[:idx]) {
		return nil, newMaterialError(MaterialPEMDecode, path,
			"%s file contains non-PEM data before the first PEM block", role)
	}
	beginMarkers := bytes.Count(data, []byte("-----BEGIN"))
	var blocks []*pem.Block
	rest := data
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			if len(bytes.TrimSpace(rest)) > 0 {
				return nil, newMaterialError(MaterialPEMDecode, path,
					"%s file contains malformed or trailing non-PEM data", role)
			}
			break
		}
		blocks = append(blocks, block)
		rest = remaining
	}
	if beginMarkers != len(blocks) {
		return nil, newMaterialError(MaterialPEMDecode, path,
			"%s file contains %d PEM BEGIN marker(s) but only %d valid PEM block(s); malformed or unterminated PEM data",
			role, beginMarkers, len(blocks))
	}
	return blocks, nil
}

// allowedPEMPrefix 接受 OpenSSL 在 PEM 块前输出的常见元数据，拒绝其他任意垃圾内容。
func allowedPEMPrefix(prefix []byte) bool {
	prefix = bytes.TrimPrefix(prefix, []byte{0xEF, 0xBB, 0xBF}) // 容忍 Windows 编辑器写入的 UTF-8 BOM
	for _, rawLine := range strings.Split(strings.TrimSpace(string(prefix)), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		switch {
		case line == "Bag Attributes", line == "MAC verified OK",
			strings.HasPrefix(line, "Bag Attributes:"),
			strings.HasPrefix(line, "Key Attributes:"),
			strings.HasPrefix(line, "friendlyName:"),
			strings.HasPrefix(line, "localKeyID:"),
			strings.HasPrefix(line, "subject="),
			strings.HasPrefix(line, "issuer="),
			strings.HasPrefix(line, "serial="),
			strings.HasPrefix(line, "keyid:"):
		default:
			return false
		}
	}
	return true
}

func parseCertificateChainPEM(data []byte, path string) ([]*x509.Certificate, int, error) {
	blocks, err := decodePEMBlocks(data, path, "server certificate")
	if err != nil {
		return nil, 0, err
	}
	var chain []*x509.Certificate
	ignored := 0
	for i, block := range blocks {
		if block.Type != "CERTIFICATE" {
			ignored++
			continue
		}
		certs, err := x509.ParseCertificates(block.Bytes)
		if err != nil {
			return nil, ignored, wrapMaterialError(MaterialCertificateParse, path,
				fmt.Sprintf("cannot parse certificate block %d", i+1), err)
		}
		if len(certs) == 0 {
			return nil, ignored, newMaterialError(MaterialCertificateParse, path,
				"certificate block %d contains no certificate", i+1)
		}
		chain = append(chain, certs...)
	}
	if len(chain) == 0 {
		return nil, ignored, newMaterialError(MaterialCertificateParse, path,
			"server certificate file contains no CERTIFICATE block")
	}
	return chain, ignored, nil
}

func isPrivateKeyPEMType(blockType string) bool {
	return strings.HasSuffix(blockType, "PRIVATE KEY")
}

func isSupportedKeyPEMType(blockType string) bool {
	switch blockType {
	case "RSA PRIVATE KEY", "EC PRIVATE KEY", "PRIVATE KEY", "ENCRYPTED PRIVATE KEY":
		return true
	default:
		return false
	}
}

func parsePrivateKeyPEM(data []byte, path string, password *KeyPasswordSpec) (crypto.Signer, keyFormatInfo, error) {
	var info keyFormatInfo
	blocks, err := decodePEMBlocks(data, path, "server key")
	if err != nil {
		return nil, info, err
	}
	var keyBlocks []*pem.Block
	for _, block := range blocks {
		if isPrivateKeyPEMType(block.Type) {
			keyBlocks = append(keyBlocks, block)
		} else {
			info.IgnoredBlocks++
		}
	}
	if len(keyBlocks) == 0 {
		return nil, info, newMaterialError(MaterialPEMDecode, path,
			"server key file contains no private key block")
	}
	if len(keyBlocks) > 1 {
		return nil, info, newMaterialError(MaterialPEMDecode, path,
			"server key file contains %d private key blocks; exactly one is required", len(keyBlocks))
	}
	return parsePrivateKeyBlock(keyBlocks[0], path, password, info)
}

func parsePrivateKeyBlock(block *pem.Block, path string, password *KeyPasswordSpec, info keyFormatInfo) (crypto.Signer, keyFormatInfo, error) {
	info.Format = block.Type
	if !isSupportedKeyPEMType(block.Type) {
		return nil, info, newMaterialError(MaterialUnsupportedKey, path,
			"unsupported server private key PEM type %q; supported types: RSA PRIVATE KEY, EC PRIVATE KEY, PRIVATE KEY, ENCRYPTED PRIVATE KEY",
			block.Type)
	}

	encryptedPKCS8 := block.Type == "ENCRYPTED PRIVATE KEY"
	legacyEncrypted := x509.IsEncryptedPEMBlock(block)

	if encryptedPKCS8 || legacyEncrypted {
		info.Encrypted = true
		if password == nil {
			return nil, info, newMaterialError(MaterialPasswordRequired, path,
				"server key is encrypted, but no password source is available; use -server-key-password-file, %s, or an interactive terminal",
				ServerKeyPasswordEnvVar)
		}
		secret, err := password.Password()
		if err != nil {
			return nil, info, wrapMaterialError(MaterialPasswordRequired, path,
				"cannot obtain server key password", err)
		}
		if secret == "" {
			return nil, info, newMaterialError(MaterialPasswordRequired, path,
				"server key is encrypted, but the provided password is empty")
		}
		info.LegacyEncrypted = legacyEncrypted
		if encryptedPKCS8 {
			key, err := pkcs8.ParsePKCS8PrivateKey(block.Bytes, []byte(secret))
			if err != nil {
				return nil, info, classifyEncryptedPKCS8Error(err, path)
			}
			return signerFromKey(key, path, info)
		}
		der, err := x509.DecryptPEMBlock(block, []byte(secret))
		if err != nil {
			if errors.Is(err, x509.IncorrectPasswordError) {
				return nil, info, wrapMaterialError(MaterialPasswordIncorrect, path,
					"cannot decrypt legacy encrypted server key (wrong password)", err)
			}
			if strings.Contains(strings.ToLower(err.Error()), "unsupported") {
				return nil, info, wrapMaterialError(MaterialUnsupportedKey, path,
					"unsupported legacy encrypted PEM parameters for server key", err)
			}
			return nil, info, wrapMaterialError(MaterialPrivateKeyParse, path,
				"cannot decrypt legacy encrypted server key", err)
		}
		block = &pem.Block{Type: block.Type, Bytes: der}
	} else if password != nil && password.Configured() {
		return nil, info, newMaterialError(MaterialPasswordUnused, path,
			"server key password source %q was provided, but the private key is not encrypted", password.Source())
	}

	var key any
	var err error
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, info, newMaterialError(MaterialUnsupportedKey, path,
			"unsupported server private key PEM type %q", block.Type)
	}
	if err != nil {
		return nil, info, classifyKeyParseError(err, path)
	}
	return signerFromKey(key, path, info)
}

func classifyEncryptedPKCS8Error(err error, path string) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "incorrect password"),
		strings.Contains(text, "invalid padding"),
		strings.Contains(text, "input not full blocks"),
		strings.Contains(text, "cipher:"):
		return wrapMaterialError(MaterialPasswordIncorrect, path,
			"cannot decrypt encrypted PKCS#8 server key (wrong password or corrupted key)", err)
	case strings.Contains(text, "unsupported"),
		strings.Contains(text, "only PKCS #5"),
		strings.Contains(text, "only PBES2"),
		strings.Contains(text, "invalid KDF parameters"),
		strings.Contains(text, "invalid cipher parameters"):
		return wrapMaterialError(MaterialUnsupportedKey, path,
			"unsupported encrypted PKCS#8 server key parameters", err)
	default:
		return wrapMaterialError(MaterialPrivateKeyParse, path,
			"cannot parse encrypted PKCS#8 server key", err)
	}
}

func classifyKeyParseError(err error, path string) error {
	if strings.Contains(err.Error(), "unknown public key algorithm") {
		return wrapMaterialError(MaterialUnsupportedKey, path,
			"unsupported server private key algorithm", err)
	}
	return wrapMaterialError(MaterialPrivateKeyParse, path,
		"cannot parse server private key", err)
}

func signerFromKey(key any, path string, info keyFormatInfo) (crypto.Signer, keyFormatInfo, error) {
	switch key.(type) {
	case *rsa.PrivateKey:
		info.Format = "RSA (PKCS#1/PKCS#8)"
	case *ecdsa.PrivateKey:
		info.Format = "ECDSA (SEC1/PKCS#8)"
	case ed25519.PrivateKey:
		info.Format = "Ed25519 (PKCS#8)"
	default:
		return nil, info, newMaterialError(MaterialUnsupportedKey, path,
			"unsupported server private key algorithm %T; supported algorithms: RSA, ECDSA, Ed25519", key)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, info, newMaterialError(MaterialUnsupportedKey, path,
			"server private key type %T cannot be used for TLS signing", key)
	}
	return signer, info, nil
}

func publicKeyMatches(cert *x509.Certificate, key crypto.Signer) bool {
	certDER, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return false
	}
	keyDER, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return false
	}
	return bytes.Equal(certDER, keyDER)
}

func validateChainStructure(chain []*x509.Certificate, path string) error {
	for i := 1; i < len(chain); i++ {
		child := chain[i-1]
		parent := chain[i]
		if !parent.IsCA {
			return newMaterialError(MaterialChain, path,
				"certificate %d (%s) is not a CA certificate and cannot be an intermediate",
				i+1, certificateName(parent))
		}
		if parent.KeyUsage != 0 && parent.KeyUsage&x509.KeyUsageCertSign == 0 {
			return newMaterialError(MaterialChain, path,
				"certificate %d (%s) has no keyCertSign usage and cannot be an intermediate",
				i+1, certificateName(parent))
		}
		if !bytes.Equal(child.RawIssuer, parent.RawSubject) {
			return newMaterialError(MaterialChain, path,
				"certificate %d issuer %q does not match certificate %d subject %q; the chain must be ordered leaf-first",
				i, child.Issuer.String(), i+1, parent.Subject.String())
		}
		if err := child.CheckSignatureFrom(parent); err != nil {
			return wrapMaterialError(MaterialChain, path,
				fmt.Sprintf("certificate %d (%s) is not signed by certificate %d (%s); the chain must be ordered leaf-first",
					i, certificateName(child), i+1, certificateName(parent)), err)
		}
	}
	return nil
}

type semanticFailure struct {
	kind    MaterialKind
	check   string
	message string
}

func (f semanticFailure) String() string {
	return f.check + ": " + f.message
}

func inspectServerChain(chain []*x509.Certificate, now time.Time) ([]semanticFailure, []string) {
	var failures []semanticFailure
	var warnings []string
	leaf := chain[0]

	addValidityFailure := func(cert *x509.Certificate, label string) {
		switch {
		case now.Before(cert.NotBefore):
			failures = append(failures, semanticFailure{
				kind: MaterialValidity, check: label + " validity",
				message: fmt.Sprintf("%s is not valid before %s", certificateName(cert), cert.NotBefore.Format(time.RFC3339)),
			})
		case now.After(cert.NotAfter):
			failures = append(failures, semanticFailure{
				kind: MaterialValidity, check: label + " validity",
				message: fmt.Sprintf("%s expired at %s", certificateName(cert), cert.NotAfter.Format(time.RFC3339)),
			})
		}
	}

	addValidityFailure(leaf, "server certificate")
	if !ekuAllowsServerAuth(leaf) {
		failures = append(failures, semanticFailure{
			kind: MaterialUsage, check: "server certificate extended key usage",
			message: fmt.Sprintf("%s extended key usage does not permit TLS server authentication: %s",
				certificateName(leaf), ekuSummary(leaf)),
		})
	}
	if !keyUsageAllowsTLS(leaf) {
		failures = append(failures, semanticFailure{
			kind: MaterialUsage, check: "server certificate key usage",
			message: fmt.Sprintf("%s key usage does not permit TLS server authentication: %v",
				certificateName(leaf), leaf.KeyUsage),
		})
	}
	if leaf.KeyUsage != 0 && leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		warnings = append(warnings, fmt.Sprintf(
			"server certificate %s has no digitalSignature key usage; TLS 1.3 handshakes may reject it",
			certificateName(leaf)))
	}
	if leaf.IsCA {
		failures = append(failures, semanticFailure{
			kind: MaterialUsage, check: "server certificate CA attribute",
			message: fmt.Sprintf("%s is a CA certificate and should not be used as a TLS server identity",
				certificateName(leaf)),
		})
	}

	if len(leaf.DNSNames) == 0 && len(leaf.IPAddresses) == 0 && len(leaf.EmailAddresses) == 0 && len(leaf.URIs) == 0 {
		warnings = append(warnings, fmt.Sprintf(
			"server certificate %s has no subject alternative names; clients that require SAN will reject it",
			certificateName(leaf)))
	}
	if isSelfSigned(leaf) {
		warnings = append(warnings, fmt.Sprintf(
			"server certificate %s is self-signed; clients must trust it explicitly", certificateName(leaf)))
	}
	if len(leaf.UnhandledCriticalExtensions) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"server certificate %s has unhandled critical extensions %v; some clients may reject it",
			certificateName(leaf), leaf.UnhandledCriticalExtensions))
	}

	for i := 1; i < len(chain); i++ {
		cert := chain[i]
		addValidityFailure(cert, fmt.Sprintf("certificate %d", i+1))
		if !ekuAllowsServerAuth(cert) {
			failures = append(failures, semanticFailure{
				kind: MaterialUsage, check: fmt.Sprintf("certificate %d extended key usage", i+1),
				message: fmt.Sprintf("%s extended key usage does not permit TLS server authentication: %s",
					certificateName(cert), ekuSummary(cert)),
			})
		}
		if len(cert.UnhandledCriticalExtensions) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"certificate %d (%s) has unhandled critical extensions %v; some clients may reject the chain",
				i+1, certificateName(cert), cert.UnhandledCriticalExtensions))
		}
	}

	last := chain[len(chain)-1]
	if !isSelfSigned(last) {
		warnings = append(warnings, fmt.Sprintf(
			"server certificate chain ends with non-self-signed certificate %s; clients must already trust its issuer",
			certificateName(last)))
	}

	return failures, warnings
}

func ekuAllowsServerAuth(cert *x509.Certificate) bool {
	if len(cert.ExtKeyUsage) == 0 {
		return len(cert.UnknownExtKeyUsage) == 0
	}
	for _, usage := range cert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageAny || usage == x509.ExtKeyUsageServerAuth {
			return true
		}
	}
	return false
}

func ekuSummary(cert *x509.Certificate) string {
	if len(cert.ExtKeyUsage) == 0 && len(cert.UnknownExtKeyUsage) > 0 {
		return fmt.Sprintf("unknown OIDs only: %v", cert.UnknownExtKeyUsage)
	}
	return fmt.Sprintf("%v", cert.ExtKeyUsage)
}

func keyUsageAllowsTLS(cert *x509.Certificate) bool {
	if cert.KeyUsage == 0 {
		return true
	}
	if cert.KeyUsage&x509.KeyUsageDigitalSignature != 0 {
		return true
	}
	if _, ok := cert.PublicKey.(*rsa.PublicKey); ok && cert.KeyUsage&x509.KeyUsageKeyEncipherment != 0 {
		return true
	}
	return false
}

func isSelfSigned(cert *x509.Certificate) bool {
	if !bytes.Equal(cert.RawSubject, cert.RawIssuer) {
		return false
	}
	return cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil
}

func certificateName(cert *x509.Certificate) string {
	if subject := cert.Subject.String(); subject != "" {
		return subject
	}
	return "<empty subject>"
}

// ClientTrustStore 是用于验证设备证书的信任库。
// 它只包含 -client-ca 的材料，绝不使用服务器证书链填充。
type ClientTrustStore struct {
	Pool          *x509.CertPool
	Certificates  []*x509.Certificate
	IgnoredBlocks int
	Warnings      []string
}

// LoadClientTrustStoreFile 读取设备证书信任源。
func LoadClientTrustStoreFile(path string) (*ClientTrustStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, wrapMaterialError(MaterialFileRead, path, "cannot read client CA file", err)
	}
	return LoadClientTrustStorePEM(data, path)
}

// LoadClientTrustStorePEM 解析一个或多个 CA 证书。
//
// 行为：
//   - 所有 CERTIFICATE 区块都会被加入信任库，支持多个 CA；
//   - 非 CERTIFICATE 区块会被忽略并产生提示；
//   - 空文件、无 CERTIFICATE 区块、损坏区块或尾部非 PEM 数据会报错；
//   - 非 CA 证书会给出提示，仍可作为精确的叶证书固定信任锚使用。
func LoadClientTrustStorePEM(data []byte, path string) (*ClientTrustStore, error) {
	blocks, err := decodePEMBlocks(data, path, "client CA")
	if err != nil {
		return nil, remapMaterialKind(err, MaterialTrustStore)
	}
	store := &ClientTrustStore{Pool: x509.NewCertPool()}
	for i, block := range blocks {
		if block.Type != "CERTIFICATE" {
			store.IgnoredBlocks++
			continue
		}
		certs, err := x509.ParseCertificates(block.Bytes)
		if err != nil {
			return nil, wrapMaterialError(MaterialTrustStore, path,
				fmt.Sprintf("cannot parse client CA certificate block %d", i+1), err)
		}
		if len(certs) == 0 {
			return nil, newMaterialError(MaterialTrustStore, path,
				"client CA certificate block %d contains no certificate", i+1)
		}
		store.Certificates = append(store.Certificates, certs...)
	}
	if len(store.Certificates) == 0 {
		return nil, newMaterialError(MaterialTrustStore, path,
			"client CA file contains no CERTIFICATE block")
	}

	now := time.Now()
	for _, cert := range store.Certificates {
		store.Pool.AddCert(cert)
		if !cert.IsCA {
			store.Warnings = append(store.Warnings, fmt.Sprintf(
				"client trust anchor %s is not marked as a CA; it may not be usable as a trust anchor and only explicit leaf pinning in some clients can match it",
				certificateName(cert)))
		} else if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			store.Warnings = append(store.Warnings, fmt.Sprintf(
				"client CA certificate %s has no keyCertSign usage and cannot issue device certificates",
				certificateName(cert)))
		}
		if now.Before(cert.NotBefore) {
			store.Warnings = append(store.Warnings, fmt.Sprintf(
				"client CA certificate %s is not valid before %s; device certificate verification will fail",
				certificateName(cert), cert.NotBefore.Format(time.RFC3339)))
		} else if now.After(cert.NotAfter) {
			store.Warnings = append(store.Warnings, fmt.Sprintf(
				"client CA certificate %s expired at %s; device certificate verification will fail",
				certificateName(cert), cert.NotAfter.Format(time.RFC3339)))
		}
	}
	if store.IgnoredBlocks > 0 {
		store.Warnings = append(store.Warnings, fmt.Sprintf(
			"ignored %d non-certificate PEM block(s) in the client CA file", store.IgnoredBlocks))
	}
	return store, nil
}
