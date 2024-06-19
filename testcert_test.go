package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youmark/pkcs8"
)

// 测试证书统一从这里生成，避免不同测试各自实现一套证书逻辑。
var testSerial atomic.Int64

type testCertOptions struct {
	CommonName  string
	NotBefore   time.Time
	NotAfter    time.Time
	IsCA        bool
	KeyUsage    x509.KeyUsage
	ExtKeyUsage []x509.ExtKeyUsage
	DNSNames    []string
	IPAddresses []net.IP
	Key         crypto.Signer
	Serial      *big.Int
}

type testCert struct {
	Certificate *x509.Certificate
	Key         crypto.Signer
	PEM         []byte
}

type testCertificateFiles struct {
	certFile string
	keyFile  string
	caFile   string
	certPEM  []byte
	keyPEM   []byte
	caPEM    []byte
}

func newTestCert(t *testing.T, opts testCertOptions, parent *testCert) *testCert {
	t.Helper()
	if opts.NotBefore.IsZero() {
		opts.NotBefore = time.Now().Add(-time.Hour)
	}
	if opts.NotAfter.IsZero() {
		opts.NotAfter = time.Now().Add(time.Hour)
	}
	if opts.Key == nil {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate ECDSA key: %v", err)
		}
		opts.Key = key
	}
	serial := opts.Serial
	if serial == nil {
		serial = big.NewInt(testSerial.Add(1))
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: opts.CommonName},
		NotBefore:             opts.NotBefore,
		NotAfter:              opts.NotAfter,
		IsCA:                  opts.IsCA,
		BasicConstraintsValid: true,
		KeyUsage:              opts.KeyUsage,
		ExtKeyUsage:           opts.ExtKeyUsage,
		DNSNames:              opts.DNSNames,
		IPAddresses:           opts.IPAddresses,
	}
	if opts.IsCA && template.KeyUsage == 0 {
		template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
	}

	parentCert := template
	signer := opts.Key
	if parent != nil {
		parentCert = parent.Certificate
		signer = parent.Key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parentCert, opts.Key.Public(), signer)
	if err != nil {
		t.Fatalf("create certificate %q: %v", opts.CommonName, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse generated certificate %q: %v", opts.CommonName, err)
	}
	return &testCert{
		Certificate: cert,
		Key:         opts.Key,
		PEM:         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func newTestCA(t *testing.T, commonName string, notBefore, notAfter time.Time) *testCert {
	t.Helper()
	return newTestCert(t, testCertOptions{
		CommonName: commonName,
		NotBefore:  notBefore,
		NotAfter:   notAfter,
		IsCA:       true,
	}, nil)
}

func newTestServerLeaf(t *testing.T, commonName string, parent *testCert, notBefore, notAfter time.Time, extKeyUsage []x509.ExtKeyUsage) *testCert {
	t.Helper()
	return newTestCert(t, testCertOptions{
		CommonName:  commonName,
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: extKeyUsage,
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}, parent)
}

func pemForCerts(certs ...*testCert) []byte {
	var result []byte
	for _, cert := range certs {
		result = append(result, cert.PEM...)
	}
	return result
}

func pemForX509Certs(certs ...*x509.Certificate) []byte {
	var result []byte
	for _, cert := range certs {
		result = append(result, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})...)
	}
	return result
}

func pkcs8DER(t *testing.T, key crypto.Signer) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS#8 key: %v", err)
	}
	return der
}

func pkcs8PEM(t *testing.T, key crypto.Signer) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8DER(t, key)})
}

func pkcs1PEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func sec1PEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal SEC1 key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

func legacyEncryptedPEM(t *testing.T, blockType string, der []byte, password string) []byte {
	t.Helper()
	block, err := x509.EncryptPEMBlock(rand.Reader, blockType, der, []byte(password), x509.PEMCipherAES256)
	if err != nil {
		t.Fatalf("encrypt legacy PEM block: %v", err)
	}
	return pem.EncodeToMemory(block)
}

func encryptedPKCS8PEM(t *testing.T, key crypto.Signer, password string) []byte {
	t.Helper()
	der, err := pkcs8.MarshalPrivateKey(key, []byte(password), nil)
	if err != nil {
		t.Fatalf("marshal encrypted PKCS#8 key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: der})
}

func writeTestMaterialFiles(t *testing.T, prefix string, certPEM, keyPEM, caPEM []byte) testCertificateFiles {
	t.Helper()
	dir := t.TempDir()
	files := testCertificateFiles{
		certFile: filepath.Join(dir, prefix+".pem"),
		keyFile:  filepath.Join(dir, prefix+".key"),
		caFile:   filepath.Join(dir, prefix+"-ca.pem"),
		certPEM:  certPEM,
		keyPEM:   keyPEM,
		caPEM:    caPEM,
	}
	for path, content := range map[string][]byte{
		files.certFile: certPEM,
		files.keyFile:  keyPEM,
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if caPEM != nil {
		if err := os.WriteFile(files.caFile, caPEM, 0o600); err != nil {
			t.Fatalf("write %s: %v", files.caFile, err)
		}
	}
	return files
}

func generateTestCertificateFilesWithEKU(t *testing.T, notBefore, notAfter time.Time, extKeyUsage []x509.ExtKeyUsage) testCertificateFiles {
	t.Helper()
	ca := newTestCA(t, "MQTT Test CA", notBefore, notAfter)
	leaf := newTestServerLeaf(t, "localhost", ca, notBefore, notAfter, extKeyUsage)
	return writeTestMaterialFiles(t, "server", leaf.PEM, pkcs8PEM(t, leaf.Key), ca.PEM)
}

func generateTestCertificateFiles(t *testing.T, notBefore, notAfter time.Time) testCertificateFiles {
	t.Helper()
	return generateTestCertificateFilesWithEKU(t, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
}
