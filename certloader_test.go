package main

import (
	"bytes"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireMaterialKind(t *testing.T, err error, want MaterialKind) *MaterialError {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want MaterialError kind %s", want)
	}
	var materialErr *MaterialError
	if !errors.As(err, &materialErr) {
		t.Fatalf("error %v is not a MaterialError, want kind %s", err, want)
	}
	if materialErr.Kind != want {
		t.Fatalf("MaterialError kind = %s, want %s (error: %v)", materialErr.Kind, want, err)
	}
	return materialErr
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want substring %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err, want)
	}
}

func pemBlock(blockType string, data []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: data})
}

func TestLoadServerIdentityChainOrderAndExtraBlocks(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	root := newTestCA(t, "Root CA", notBefore, notAfter)
	intermediate := newTestCert(t, testCertOptions{
		CommonName: "Intermediate CA", NotBefore: notBefore, NotAfter: notAfter, IsCA: true,
	}, root)
	leaf := newTestServerLeaf(t, "localhost", intermediate, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	keyPEM := pkcs8PEM(t, leaf.Key)

	certPEM := append([]byte("Bag Attributes\n"), leaf.PEM...)
	certPEM = append(certPEM, pemBlock("COMMENT", []byte("ignored block"))...)
	certPEM = append(certPEM, intermediate.PEM...)
	keyPEMWithCert := append([]byte{}, keyPEM...)
	keyPEMWithCert = append(keyPEMWithCert, root.PEM...)

	identity, err := LoadServerIdentityPEM(certPEM, keyPEMWithCert, IdentityOptions{Now: time.Now()})
	if err != nil {
		t.Fatalf("LoadServerIdentityPEM() error = %v", err)
	}
	if len(identity.Chain) != 2 {
		t.Fatalf("chain length = %d, want 2", len(identity.Chain))
	}
	if identity.Chain[0].Subject.CommonName != "localhost" || identity.Chain[1].Subject.CommonName != "Intermediate CA" {
		t.Errorf("chain order = [%s, %s], want leaf then intermediate",
			identity.Chain[0].Subject.CommonName, identity.Chain[1].Subject.CommonName)
	}
	if len(identity.Certificate.Certificate) != 2 ||
		!bytes.Equal(identity.Certificate.Certificate[0], leaf.Certificate.Raw) ||
		!bytes.Equal(identity.Certificate.Certificate[1], intermediate.Certificate.Raw) {
		t.Error("tls.Certificate chain is not leaf-first with the expected raw certificates")
	}
	if identity.Report.IgnoredPEMBlocks != 2 {
		t.Errorf("IgnoredPEMBlocks = %d, want 2", identity.Report.IgnoredPEMBlocks)
	}
	if !containsSubstring(identity.Report.Warnings, "ends with non-self-signed") {
		t.Errorf("warnings = %v, want missing-root note", identity.Report.Warnings)
	}
	if len(identity.Report.Bypassed) != 0 {
		t.Errorf("Bypassed = %v, want none", identity.Report.Bypassed)
	}
	if !strings.Contains(identity.Report.ServerKeyFormat, "ECDSA") {
		t.Errorf("ServerKeyFormat = %q, want ECDSA", identity.Report.ServerKeyFormat)
	}

	withRootPEM := pemForCerts(leaf, intermediate, root)
	identity, err = LoadServerIdentityPEM(withRootPEM, keyPEM, IdentityOptions{Now: time.Now()})
	if err != nil {
		t.Fatalf("LoadServerIdentityPEM() with root error = %v", err)
	}
	if len(identity.Chain) != 3 {
		t.Errorf("chain length with root = %d, want 3", len(identity.Chain))
	}
	if containsSubstring(identity.Report.Warnings, "ends with non-self-signed") {
		t.Errorf("warnings = %v, want no missing-root note when root is included", identity.Report.Warnings)
	}
}

func TestLoadServerIdentityChainErrors(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	root := newTestCA(t, "Root CA", notBefore, notAfter)
	intermediate := newTestCert(t, testCertOptions{
		CommonName: "Intermediate CA", NotBefore: notBefore, NotAfter: notAfter, IsCA: true,
	}, root)
	leaf := newTestServerLeaf(t, "localhost", intermediate, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	keyPEM := pkcs8PEM(t, leaf.Key)
	otherCA := newTestCA(t, "Other CA", notBefore, notAfter)

	tests := []struct {
		name  string
		certs []*testCert
		want  string
	}{
		{"reversed chain", []*testCert{intermediate, leaf}, "leaf certificate must be first"},
		{"missing intermediate", []*testCert{leaf, root}, "issuer"},
		{"unrelated issuer", []*testCert{leaf, otherCA}, "issuer"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadServerIdentityPEM(pemForCerts(test.certs...), keyPEM, IdentityOptions{Now: time.Now()})
			requireMaterialKind(t, err, MaterialChain)
			requireErrorContains(t, err, test.want)

			_, err = LoadServerIdentityPEM(pemForCerts(test.certs...), keyPEM, IdentityOptions{
				Now: time.Now(), AllowInvalidServerCert: true,
			})
			requireMaterialKind(t, err, MaterialChain)
		})
	}

	badIntermediate := newTestCert(t, testCertOptions{
		CommonName: "Not a CA", NotBefore: notBefore, NotAfter: notAfter, IsCA: false,
		KeyUsage: x509.KeyUsageDigitalSignature,
	}, root)
	leafUnderBadIntermediate := newTestServerLeaf(t, "localhost", badIntermediate, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	_, err := LoadServerIdentityPEM(pemForCerts(leafUnderBadIntermediate, badIntermediate), pkcs8PEM(t, leafUnderBadIntermediate.Key), IdentityOptions{})
	requireMaterialKind(t, err, MaterialChain)
	requireErrorContains(t, err, "not a CA certificate")

	noCertSign := newTestCert(t, testCertOptions{
		CommonName: "No CertSign CA", NotBefore: notBefore, NotAfter: notAfter, IsCA: true,
		KeyUsage: x509.KeyUsageDigitalSignature,
	}, root)
	leafUnderNoCertSign := newTestServerLeaf(t, "localhost", noCertSign, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	_, err = LoadServerIdentityPEM(pemForCerts(leafUnderNoCertSign, noCertSign), pkcs8PEM(t, leafUnderNoCertSign.Key), IdentityOptions{})
	requireMaterialKind(t, err, MaterialChain)
	requireErrorContains(t, err, "keyCertSign")
}

func TestLoadServerIdentityPEMErrors(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	ca := newTestCA(t, "Root CA", notBefore, notAfter)
	leaf := newTestServerLeaf(t, "localhost", ca, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	validKey := pkcs8PEM(t, leaf.Key)
	validCert := leaf.PEM

	tests := []struct {
		name    string
		certPEM []byte
		keyPEM  []byte
		kind    MaterialKind
		want    string
	}{
		{
			name: "empty certificate file", certPEM: nil, keyPEM: validKey,
			kind: MaterialPEMDecode, want: "empty",
		},
		{
			name: "garbage certificate file", certPEM: []byte("this is not PEM"), keyPEM: validKey,
			kind: MaterialPEMDecode, want: "malformed",
		},
		{
			name: "leading garbage before certificate", certPEM: append([]byte("garbage\n"), validCert...), keyPEM: validKey,
			kind: MaterialPEMDecode, want: "before the first PEM block",
		},
		{
			name: "malformed block before valid certificate", keyPEM: validKey,
			certPEM: append([]byte("-----BEGIN CERTIFICATE-----\n!!!!\n-----END CERTIFICATE-----\n"), validCert...),
			kind:    MaterialPEMDecode, want: "BEGIN marker",
		},
		{
			name: "trailing garbage after certificate", certPEM: append(append([]byte{}, validCert...), []byte("garbage")...), keyPEM: validKey,
			kind: MaterialPEMDecode, want: "trailing",
		},
		{
			name: "invalid certificate DER", certPEM: pemBlock("CERTIFICATE", []byte("not a certificate")), keyPEM: validKey,
			kind: MaterialCertificateParse, want: "cannot parse certificate block",
		},
		{
			name: "certificate file has only a key", certPEM: validKey, keyPEM: validKey,
			kind: MaterialCertificateParse, want: "no CERTIFICATE",
		},
		{
			name: "empty key file", certPEM: validCert, keyPEM: nil,
			kind: MaterialPEMDecode, want: "empty",
		},
		{
			name: "key file has only a certificate", certPEM: validCert, keyPEM: validCert,
			kind: MaterialPEMDecode, want: "no private key block",
		},
		{
			name: "multiple key blocks", certPEM: validCert, keyPEM: append(append([]byte{}, validKey...), validKey...),
			kind: MaterialPEMDecode, want: "exactly one",
		},
		{
			name: "trailing garbage after key", certPEM: validCert, keyPEM: append(append([]byte{}, validKey...), []byte("garbage")...),
			kind: MaterialPEMDecode, want: "trailing",
		},
		{
			name: "invalid PKCS8 key", certPEM: validCert, keyPEM: pemBlock("PRIVATE KEY", []byte("not a key")),
			kind: MaterialPrivateKeyParse, want: "cannot parse server private key",
		},
		{
			name: "OpenSSH key", certPEM: validCert, keyPEM: pemBlock("OPENSSH PRIVATE KEY", []byte("not supported")),
			kind: MaterialUnsupportedKey, want: "unsupported",
		},
		{
			name: "DSA key", certPEM: validCert, keyPEM: pemBlock("DSA PRIVATE KEY", []byte("not supported")),
			kind: MaterialUnsupportedKey, want: "unsupported",
		},
		{
			name: "encrypted key without source", certPEM: validCert,
			keyPEM: encryptedPKCS8PEM(t, leaf.Key, "secret"),
			kind:   MaterialPasswordRequired, want: "no password source",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadServerIdentityPEM(test.certPEM, test.keyPEM, IdentityOptions{Now: time.Now()})
			requireMaterialKind(t, err, test.kind)
			requireErrorContains(t, err, test.want)
		})
	}
}

func TestLoadServerIdentityKeyFormats(t *testing.T) {
	newRSA := func(t *testing.T) crypto.Signer {
		t.Helper()
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate RSA key: %v", err)
		}
		return key
	}
	newEC := func(t *testing.T) crypto.Signer {
		t.Helper()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate ECDSA key: %v", err)
		}
		return key
	}
	newEd25519 := func(t *testing.T) crypto.Signer {
		t.Helper()
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate Ed25519 key: %v", err)
		}
		return key
	}

	tests := []struct {
		name     string
		newKey   func(*testing.T) crypto.Signer
		encode   func(*testing.T, crypto.Signer) []byte
		password string
	}{
		{
			name: "RSA PKCS1 unencrypted", newKey: newRSA,
			encode: func(t *testing.T, key crypto.Signer) []byte {
				return pkcs1PEM(t, key.(*rsa.PrivateKey))
			},
		},
		{
			name: "RSA PKCS8 unencrypted", newKey: newRSA,
			encode: func(t *testing.T, key crypto.Signer) []byte { return pkcs8PEM(t, key) },
		},
		{
			name: "EC SEC1 unencrypted", newKey: newEC,
			encode: func(t *testing.T, key crypto.Signer) []byte {
				return sec1PEM(t, key.(*ecdsa.PrivateKey))
			},
		},
		{
			name: "EC PKCS8 unencrypted", newKey: newEC,
			encode: func(t *testing.T, key crypto.Signer) []byte { return pkcs8PEM(t, key) },
		},
		{
			name: "Ed25519 PKCS8 unencrypted", newKey: newEd25519,
			encode: func(t *testing.T, key crypto.Signer) []byte { return pkcs8PEM(t, key) },
		},
		{
			name: "RSA PKCS1 legacy encrypted", newKey: newRSA, password: "legacy-rsa-secret",
			encode: func(t *testing.T, key crypto.Signer) []byte {
				return legacyEncryptedPEM(t, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key.(*rsa.PrivateKey)), "legacy-rsa-secret")
			},
		},
		{
			name: "EC SEC1 legacy encrypted", newKey: newEC, password: "legacy-ec-secret",
			encode: func(t *testing.T, key crypto.Signer) []byte {
				der, err := x509.MarshalECPrivateKey(key.(*ecdsa.PrivateKey))
				if err != nil {
					t.Fatalf("marshal SEC1 key: %v", err)
				}
				return legacyEncryptedPEM(t, "EC PRIVATE KEY", der, "legacy-ec-secret")
			},
		},
		{
			name: "PKCS8 legacy encrypted", newKey: newEC, password: "legacy-pkcs8-secret",
			encode: func(t *testing.T, key crypto.Signer) []byte {
				return legacyEncryptedPEM(t, "PRIVATE KEY", pkcs8DER(t, key), "legacy-pkcs8-secret")
			},
		},
		{
			name: "EC encrypted PKCS8", newKey: newEC, password: "pkcs8-ec-secret",
			encode: func(t *testing.T, key crypto.Signer) []byte {
				return encryptedPKCS8PEM(t, key, "pkcs8-ec-secret")
			},
		},
		{
			name: "RSA encrypted PKCS8", newKey: newRSA, password: "pkcs8-rsa-secret",
			encode: func(t *testing.T, key crypto.Signer) []byte {
				return encryptedPKCS8PEM(t, key, "pkcs8-rsa-secret")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := test.newKey(t)
			ca := newTestCA(t, "Key Format CA", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
			leaf := newTestCert(t, testCertOptions{
				CommonName: "key-format-server", NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				DNSNames:    []string{"localhost"}, Key: key,
			}, ca)

			var password *KeyPasswordSpec
			if test.password != "" {
				password = NewStaticKeyPassword(test.password)
			}
			identity, err := LoadServerIdentityPEM(leaf.PEM, test.encode(t, key), IdentityOptions{Password: password})
			if err != nil {
				t.Fatalf("LoadServerIdentityPEM() error = %v", err)
			}
			if identity.Certificate.PrivateKey == nil {
				t.Fatal("loaded private key is nil")
			}
			if identity.Certificate.Leaf == nil {
				t.Fatal("loaded leaf is nil")
			}
			if got, want := identity.Report.ServerKeyEncrypted, test.password != ""; got != want {
				t.Errorf("ServerKeyEncrypted = %v, want %v", got, want)
			}
		})
	}
}

func TestLoadServerIdentityPasswordErrors(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	ca := newTestCA(t, "Password CA", notBefore, notAfter)
	leaf := newTestServerLeaf(t, "localhost", ca, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	certPEM := leaf.PEM

	legacyEncrypted := legacyEncryptedPEM(t, "PRIVATE KEY", pkcs8DER(t, leaf.Key), "correct-password")
	pkcs8Encrypted := encryptedPKCS8PEM(t, leaf.Key, "correct-password")
	unencrypted := pkcs8PEM(t, leaf.Key)

	t.Run("missing password", func(t *testing.T) {
		_, err := LoadServerIdentityPEM(certPEM, pkcs8Encrypted, IdentityOptions{})
		requireMaterialKind(t, err, MaterialPasswordRequired)
	})
	t.Run("empty password", func(t *testing.T) {
		_, err := LoadServerIdentityPEM(certPEM, pkcs8Encrypted, IdentityOptions{Password: NewStaticKeyPassword("")})
		requireMaterialKind(t, err, MaterialPasswordRequired)
	})
	for name, keyPEM := range map[string][]byte{
		"legacy wrong password": legacyEncrypted,
		"pkcs8 wrong password":  pkcs8Encrypted,
	} {
		t.Run(name, func(t *testing.T) {
			const wrong = "definitely-wrong-password"
			_, err := LoadServerIdentityPEM(certPEM, keyPEM, IdentityOptions{Password: NewStaticKeyPassword(wrong)})
			requireMaterialKind(t, err, MaterialPasswordIncorrect)
			if strings.Contains(err.Error(), wrong) || strings.Contains(err.Error(), "correct-password") {
				t.Errorf("error leaks a password: %v", err)
			}
			if strings.Contains(err.Error(), "PRIVATE KEY") {
				t.Errorf("error leaks PEM key material: %v", err)
			}
		})
	}
	t.Run("password on unencrypted key", func(t *testing.T) {
		_, err := LoadServerIdentityPEM(certPEM, unencrypted, IdentityOptions{Password: NewStaticKeyPassword("unused")})
		requireMaterialKind(t, err, MaterialPasswordUnused)
		requireErrorContains(t, err, "not encrypted")
	})
	t.Run("unsupported X25519 PKCS8", func(t *testing.T) {
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate X25519 key: %v", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("marshal X25519 key: %v", err)
		}
		_, err = LoadServerIdentityPEM(certPEM, pemBlock("PRIVATE KEY", der), IdentityOptions{})
		requireMaterialKind(t, err, MaterialUnsupportedKey)
		requireErrorContains(t, err, "unsupported")
	})
}

func TestServerCertificateSemanticChecks(t *testing.T) {
	now := time.Now()
	validFrom := now.Add(-time.Hour)
	validTo := now.Add(time.Hour)
	root := newTestCA(t, "Root CA", now.Add(-48*time.Hour), now.Add(48*time.Hour))

	t.Run("expired leaf requires explicit allowance", func(t *testing.T) {
		leaf := newTestServerLeaf(t, "localhost", root, now.Add(-48*time.Hour), now.Add(-time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
		_, err := LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now})
		requireMaterialKind(t, err, MaterialValidity)
		requireErrorContains(t, err, "expired")

		identity, err := LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now, AllowInvalidServerCert: true})
		if err != nil {
			t.Fatalf("LoadServerIdentityPEM() with allowance error = %v", err)
		}
		if !containsSubstring(identity.Report.Bypassed, "validity") || !containsSubstring(identity.Report.Bypassed, "expired") {
			t.Errorf("Bypassed = %v, want validity entry", identity.Report.Bypassed)
		}
	})

	t.Run("not yet valid leaf", func(t *testing.T) {
		leaf := newTestServerLeaf(t, "localhost", root, now.Add(time.Hour), now.Add(2*time.Hour), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
		_, err := LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now})
		requireMaterialKind(t, err, MaterialValidity)
		requireErrorContains(t, err, "not valid before")
	})

	t.Run("wrong EKU", func(t *testing.T) {
		leaf := newTestServerLeaf(t, "localhost", root, validFrom, validTo, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
		_, err := LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now})
		requireMaterialKind(t, err, MaterialUsage)
		requireErrorContains(t, err, "extended key usage")

		identity, err := LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now, AllowInvalidServerCert: true})
		if err != nil {
			t.Fatalf("LoadServerIdentityPEM() with allowance error = %v", err)
		}
		if !containsSubstring(identity.Report.Bypassed, "extended key usage") {
			t.Errorf("Bypassed = %v, want EKU entry", identity.Report.Bypassed)
		}
	})

	t.Run("wrong key usage", func(t *testing.T) {
		leaf := newTestCert(t, testCertOptions{
			CommonName: "localhost", NotBefore: validFrom, NotAfter: validTo,
			KeyUsage: x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			DNSNames: []string{"localhost"},
		}, root)
		_, err := LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now})
		requireMaterialKind(t, err, MaterialUsage)
		requireErrorContains(t, err, "key usage")
	})

	t.Run("CA certificate as leaf", func(t *testing.T) {
		leaf := newTestCert(t, testCertOptions{
			CommonName: "CA as leaf", NotBefore: validFrom, NotAfter: validTo, IsCA: true,
			KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"},
		}, root)
		_, err := LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now})
		requireMaterialKind(t, err, MaterialUsage)
		requireErrorContains(t, err, "CA certificate")
	})

	t.Run("expired intermediate", func(t *testing.T) {
		intermediate := newTestCert(t, testCertOptions{
			CommonName: "Expired Intermediate", NotBefore: now.Add(-48 * time.Hour), NotAfter: now.Add(-time.Hour), IsCA: true,
		}, root)
		leaf := newTestServerLeaf(t, "localhost", intermediate, validFrom, validTo, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
		_, err := LoadServerIdentityPEM(pemForCerts(leaf, intermediate), pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now})
		requireMaterialKind(t, err, MaterialValidity)
		requireErrorContains(t, err, "certificate 2")

		identity, err := LoadServerIdentityPEM(pemForCerts(leaf, intermediate), pkcs8PEM(t, leaf.Key), IdentityOptions{
			Now: now, AllowInvalidServerCert: true,
		})
		if err != nil {
			t.Fatalf("LoadServerIdentityPEM() with allowance error = %v", err)
		}
		if !containsSubstring(identity.Report.Bypassed, "certificate 2 validity") {
			t.Errorf("Bypassed = %v, want intermediate validity entry", identity.Report.Bypassed)
		}
	})

	t.Run("intermediate EKU", func(t *testing.T) {
		intermediate := newTestCert(t, testCertOptions{
			CommonName: "Client-only Intermediate", NotBefore: validFrom, NotAfter: validTo, IsCA: true,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}, root)
		leaf := newTestServerLeaf(t, "localhost", intermediate, validFrom, validTo, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
		_, err := LoadServerIdentityPEM(pemForCerts(leaf, intermediate), pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now})
		requireMaterialKind(t, err, MaterialUsage)
		requireErrorContains(t, err, "certificate 2 extended key usage")
	})

	t.Run("warnings for SAN and self-signed", func(t *testing.T) {
		leaf := newTestCert(t, testCertOptions{
			CommonName: "no-san", NotBefore: validFrom, NotAfter: validTo,
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}, root)
		identity, err := LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, leaf.Key), IdentityOptions{Now: now})
		if err != nil {
			t.Fatalf("LoadServerIdentityPEM() error = %v", err)
		}
		if !containsSubstring(identity.Report.Warnings, "no subject alternative names") {
			t.Errorf("warnings = %v, want SAN note", identity.Report.Warnings)
		}

		selfSigned := newTestServerLeaf(t, "self-signed", nil, validFrom, validTo, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
		identity, err = LoadServerIdentityPEM(selfSigned.PEM, pkcs8PEM(t, selfSigned.Key), IdentityOptions{Now: now})
		if err != nil {
			t.Fatalf("self-signed LoadServerIdentityPEM() error = %v", err)
		}
		if !containsSubstring(identity.Report.Warnings, "self-signed") {
			t.Errorf("warnings = %v, want self-signed note", identity.Report.Warnings)
		}
	})
}

func TestLoadClientTrustStore(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	ca1 := newTestCA(t, "Device CA 1", notBefore, notAfter)
	ca2 := newTestCA(t, "Device CA 2", notBefore, notAfter)

	t.Run("multiple CAs and ignored blocks", func(t *testing.T) {
		pemData := append([]byte{}, ca1.PEM...)
		pemData = append(pemData, pemBlock("PRIVATE KEY", []byte("ignored"))...)
		pemData = append(pemData, ca2.PEM...)
		store, err := LoadClientTrustStorePEM(pemData, "client-ca.pem")
		if err != nil {
			t.Fatalf("LoadClientTrustStorePEM() error = %v", err)
		}
		if len(store.Certificates) != 2 {
			t.Errorf("certificates = %d, want 2", len(store.Certificates))
		}
		if store.IgnoredBlocks != 1 {
			t.Errorf("IgnoredBlocks = %d, want 1", store.IgnoredBlocks)
		}
		if len(store.Pool.Subjects()) != 2 {
			t.Errorf("pool subjects = %d, want 2", len(store.Pool.Subjects()))
		}
		if !containsSubstring(store.Warnings, "ignored 1") {
			t.Errorf("warnings = %v, want ignored-block note", store.Warnings)
		}
	})

	t.Run("multiple certificates in one block", func(t *testing.T) {
		combined := append(append([]byte{}, ca1.Certificate.Raw...), ca2.Certificate.Raw...)
		store, err := LoadClientTrustStorePEM(pemBlock("CERTIFICATE", combined), "client-ca.pem")
		if err != nil {
			t.Fatalf("LoadClientTrustStorePEM() error = %v", err)
		}
		if len(store.Certificates) != 2 {
			t.Errorf("certificates = %d, want 2", len(store.Certificates))
		}
	})

	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name string
			pem  []byte
			want string
		}{
			{"empty", nil, "empty"},
			{"garbage", []byte("not PEM"), "malformed"},
			{"trailing garbage", append(append([]byte{}, ca1.PEM...), []byte("garbage")...), "trailing"},
			{"invalid certificate", pemBlock("CERTIFICATE", []byte("bad")), "cannot parse"},
			{"no certificate block", pemBlock("PRIVATE KEY", []byte("ignored")), "no CERTIFICATE"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				_, err := LoadClientTrustStorePEM(test.pem, "client-ca.pem")
				requireMaterialKind(t, err, MaterialTrustStore)
				requireErrorContains(t, err, test.want)
			})
		}
	})

	t.Run("file read error", func(t *testing.T) {
		_, err := LoadClientTrustStoreFile(filepath.Join(t.TempDir(), "missing-ca.pem"))
		requireMaterialKind(t, err, MaterialFileRead)
	})

	t.Run("warnings", func(t *testing.T) {
		leaf := newTestCert(t, testCertOptions{
			CommonName: "Pinned Leaf", NotBefore: notBefore, NotAfter: notAfter,
			KeyUsage: x509.KeyUsageDigitalSignature, DNSNames: []string{"device"},
		}, nil)
		store, err := LoadClientTrustStorePEM(leaf.PEM, "client-ca.pem")
		if err != nil {
			t.Fatalf("LoadClientTrustStorePEM() error = %v", err)
		}
		if !containsSubstring(store.Warnings, "not marked as a CA") {
			t.Errorf("warnings = %v, want non-CA note", store.Warnings)
		}

		expired := newTestCA(t, "Expired CA", time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour))
		store, err = LoadClientTrustStorePEM(expired.PEM, "client-ca.pem")
		if err != nil {
			t.Fatalf("LoadClientTrustStorePEM() expired error = %v", err)
		}
		if !containsSubstring(store.Warnings, "expired") {
			t.Errorf("warnings = %v, want expired note", store.Warnings)
		}

		noCertSign := newTestCert(t, testCertOptions{
			CommonName: "No CertSign CA", NotBefore: notBefore, NotAfter: notAfter, IsCA: true,
			KeyUsage: x509.KeyUsageDigitalSignature,
		}, nil)
		store, err = LoadClientTrustStorePEM(noCertSign.PEM, "client-ca.pem")
		if err != nil {
			t.Fatalf("LoadClientTrustStorePEM() no-certSign error = %v", err)
		}
		if !containsSubstring(store.Warnings, "keyCertSign") {
			t.Errorf("warnings = %v, want keyCertSign note", store.Warnings)
		}
	})
}

func TestInvalidServerCertificateAllowanceDoesNotBypassMaterialErrors(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	ca := newTestCA(t, "Allowance CA", notBefore, notAfter)
	leaf := newTestServerLeaf(t, "localhost", ca, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}

	_, err = LoadServerIdentityPEM([]byte("not PEM"), pkcs8PEM(t, leaf.Key), IdentityOptions{
		AllowInvalidServerCert: true,
	})
	requireMaterialKind(t, err, MaterialPEMDecode)

	_, err = LoadServerIdentityPEM(leaf.PEM, pkcs8PEM(t, otherKey), IdentityOptions{
		AllowInvalidServerCert: true,
	})
	requireMaterialKind(t, err, MaterialKeyMismatch)
}

func TestServerIdentityAndClientTrustAreSeparate(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	serverCA := newTestCA(t, "Server Identity CA", notBefore, notAfter)
	deviceCA := newTestCA(t, "Device Issuing CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	files := writeTestMaterialFiles(t, "server", serverLeaf.PEM, pkcs8PEM(t, serverLeaf.Key), deviceCA.PEM)

	cfg := &Config{
		Mode:           ModeMTLS,
		ServerCertFile: files.certFile,
		ServerKeyFile:  files.keyFile,
		ClientCAFile:   files.caFile,
	}
	material, err := cfg.BuildTLSMaterial()
	if err != nil {
		t.Fatalf("BuildTLSMaterial() error = %v", err)
	}
	if material.Config.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert", material.Config.ClientAuth)
	}
	if len(material.Config.Certificates) != 1 || material.Config.Certificates[0].Leaf == nil {
		t.Fatal("server certificate chain was not installed")
	}
	if !bytes.Equal(material.Config.Certificates[0].Leaf.Raw, serverLeaf.Certificate.Raw) {
		t.Error("server certificate is not the configured server identity")
	}
	subjects := material.ClientTrust.Pool.Subjects()
	if len(subjects) != 1 {
		t.Fatalf("client trust subjects = %d, want 1", len(subjects))
	}
	if !bytes.Equal(subjects[0], deviceCA.Certificate.RawSubject) {
		t.Error("ClientCAs is not the configured device trust source")
	}
	if bytes.Equal(subjects[0], serverCA.Certificate.RawSubject) {
		t.Error("ClientCAs was populated from the server certificate CA")
	}
	if material.Report.ClientCAs != 1 {
		t.Errorf("report ClientCAs = %d, want 1", material.Report.ClientCAs)
	}

	tlsOnly := &Config{
		Mode:           ModeTLS,
		ServerCertFile: files.certFile,
		ServerKeyFile:  files.keyFile,
	}
	material, err = tlsOnly.BuildTLSMaterial()
	if err != nil {
		t.Fatalf("tls BuildTLSMaterial() error = %v", err)
	}
	if material.Config.ClientCAs != nil {
		t.Error("tls ClientCAs != nil, want nil")
	}
}

func TestLoadServerIdentityFileReadErrors(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.pem")
	if err := os.WriteFile(certPath, []byte("not used"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	missing := filepath.Join(dir, "missing.pem")
	_, err := LoadServerIdentityFiles(missing, missing, IdentityOptions{})
	requireMaterialKind(t, err, MaterialFileRead)

	_, err = LoadServerIdentityFiles(certPath, missing, IdentityOptions{})
	requireMaterialKind(t, err, MaterialFileRead)
}

func TestBuildTLSMaterialPasswordFile(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	ca := newTestCA(t, "Password File CA", notBefore, notAfter)
	leaf := newTestServerLeaf(t, "localhost", ca, notBefore, notAfter, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.pem")
	keyPath := filepath.Join(dir, "server.key")
	passwordPath := filepath.Join(dir, "server.key.password")
	for path, content := range map[string][]byte{
		certPath:     leaf.PEM,
		keyPath:      encryptedPKCS8PEM(t, leaf.Key, "file-secret"),
		passwordPath: []byte("file-secret\n"),
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	cfg := &Config{
		Mode:                  ModeTLS,
		ServerCertFile:        certPath,
		ServerKeyFile:         keyPath,
		ServerKeyPasswordFile: passwordPath,
	}
	material, err := cfg.BuildTLSMaterial()
	if err != nil {
		t.Fatalf("BuildTLSMaterial() error = %v", err)
	}
	if material.Report.ServerKeyPasswordSource != "file" {
		t.Errorf("password source = %q, want file", material.Report.ServerKeyPasswordSource)
	}

	if err := os.WriteFile(passwordPath, []byte("wrong-secret\n"), 0o600); err != nil {
		t.Fatalf("write wrong password: %v", err)
	}
	_, err = cfg.BuildTLSMaterial()
	requireMaterialKind(t, err, MaterialPasswordIncorrect)
	if strings.Contains(err.Error(), "wrong-secret") || strings.Contains(err.Error(), "file-secret") {
		t.Errorf("error leaks a password: %v", err)
	}

	unencrypted := writeTestMaterialFiles(t, "plain-key", leaf.PEM, pkcs8PEM(t, leaf.Key), ca.PEM)
	cfg = &Config{
		Mode:                  ModeTLS,
		ServerCertFile:        unencrypted.certFile,
		ServerKeyFile:         unencrypted.keyFile,
		ServerKeyPasswordFile: passwordPath,
	}
	_, err = cfg.BuildTLSMaterial()
	requireMaterialKind(t, err, MaterialPasswordUnused)

	cfg = &Config{
		Mode:           ModeTLS,
		ServerCertFile: certPath,
		ServerKeyFile:  keyPath,
	}
	_, err = cfg.BuildTLSMaterial()
	requireMaterialKind(t, err, MaterialPasswordRequired)
}

func containsSubstring(items []string, want string) bool {
	for _, item := range items {
		if strings.Contains(item, want) {
			return true
		}
	}
	return false
}
