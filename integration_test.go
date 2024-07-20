package main

// 本文件用真实本地监听、真实 TLS 握手和真实 MQTT 报文验证认证策略是否生效。
// 证书全部由 testcert_test.go 的工厂动态生成，写入 t.TempDir，不依赖 OpenSSL、
// 互联网或真实设备，也不提交任何固定私钥。

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

const (
	integrationReadTimeout  = 3 * time.Second
	integrationNoDataWindow = 500 * time.Millisecond
)

func mustRoundTrip(t *testing.T, client *mqttTestClient, topic, payload string) {
	t.Helper()
	granted, err := client.Subscribe(topic, 1)
	if err != nil {
		t.Fatalf("subscribe %q: %v", topic, err)
	}
	if granted == 0x80 {
		t.Fatalf("subscribe %q was refused by ACL", topic)
	}
	if granted > 1 {
		t.Fatalf("subscribe %q granted unexpected QoS %d", topic, granted)
	}
	if err := client.Publish(topic, []byte(payload), 1); err != nil {
		t.Fatalf("publish %q: %v", topic, err)
	}
	message, err := client.NextPublish(integrationReadTimeout)
	if err != nil {
		t.Fatalf("wait for %q: %v", topic, err)
	}
	if message.Topic != topic {
		t.Fatalf("received topic %q, want %q", message.Topic, topic)
	}
	if string(message.Payload) != payload {
		t.Fatalf("received payload %q, want %q", message.Payload, payload)
	}
}

func requireTLSVersion(t *testing.T, client *mqttTestClient, want uint16) {
	t.Helper()
	if got := client.TLSVersion(); got != want {
		t.Fatalf("negotiated TLS version = %s, want %s", tls.VersionName(got), tls.VersionName(want))
	}
}

func validServerWindow() (time.Time, time.Time) {
	now := time.Now()
	return now.Add(-time.Hour), now.Add(24 * time.Hour)
}

func TestIntegrationPlainMQTTBaseline(t *testing.T) {
	server, addr := startTestServer(t, testServerSpec{Mode: ModePlain})
	if server.TLSConfig() != nil {
		t.Fatal("plain mode must not build a TLS config")
	}

	client := requireConnect(t, addr, mqttClientOptions{})
	if client.TLSVersion() != 0 {
		t.Fatalf("plain mode negotiated TLS version %s", tls.VersionName(client.TLSVersion()))
	}
	mustRoundTrip(t, client, "devices/plain/state", "plain-message")
}

func TestIntegrationTLSWithoutClientCertificate(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Integration Server CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	roots := testCertPool(t, serverCA)

	tests := []struct {
		name     string
		min, max string
		want     uint16
	}{
		{"tls13", "1.3", "1.3", tls.VersionTLS13},
		{"tls12", "1.2", "1.2", tls.VersionTLS12},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, addr := startTestServer(t, testServerSpec{
				Mode:        ModeTLS,
				ServerChain: []*testCert{serverLeaf},
				TLSMin:      test.min,
				TLSMax:      test.max,
			})
			if got := server.TLSConfig().ClientAuth; got != tls.NoClientCert {
				t.Fatalf("TLS mode ClientAuth = %v, want NoClientCert", got)
			}

			client := requireConnect(t, addr, mqttClientOptions{
				TLSConfig: testClientTLSConfig(t, roots, "localhost"),
			})
			requireTLSVersion(t, client, test.want)
			mustRoundTrip(t, client, "devices/tls/state", "tls-message")
		})
	}
}

func TestIntegrationMTLSWithValidClientCertificate(t *testing.T) {
	tests := []struct {
		name    string
		keyType testKeyType
	}{
		{"ecdsa", testKeyECDSA},
		{"rsa", testKeyRSA},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notBefore, notAfter := validServerWindow()
			serverCA := newTestCAWithKey(t, "Server Root CA", notBefore, notAfter, test.keyType)
			deviceCA := newTestCAWithKey(t, "Device Root CA", notBefore, notAfter, test.keyType)
			serverLeaf := newTestServerLeafForNames(t, "localhost", serverCA, notBefore, notAfter,
				[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				[]string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")}, test.keyType)
			deviceLeaf := newTestClientLeaf(t, "device-001", deviceCA, notBefore, notAfter,
				[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, test.keyType)

			versions := []struct {
				name     string
				min, max string
				want     uint16
			}{
				{"tls13", "1.3", "1.3", tls.VersionTLS13},
				{"tls12", "1.2", "1.2", tls.VersionTLS12},
			}
			for _, version := range versions {
				t.Run(version.name, func(t *testing.T) {
					server, addr := startTestServer(t, testServerSpec{
						Mode:        ModeMTLS,
						ServerChain: []*testCert{serverLeaf},
						ClientCAs:   []*testCert{deviceCA},
						TLSMin:      version.min,
						TLSMax:      version.max,
					})
					if got := server.TLSConfig().ClientAuth; got != tls.RequireAndVerifyClientCert {
						t.Fatalf("mTLS ClientAuth = %v, want RequireAndVerifyClientCert", got)
					}

					client := requireConnect(t, addr, mqttClientOptions{
						TLSConfig: testClientTLSConfig(t, testCertPool(t, serverCA), "localhost", deviceLeaf),
					})
					requireTLSVersion(t, client, version.want)
					mustRoundTrip(t, client, "devices/mtls/state", "mtls-message-"+test.name+"-"+version.name)
				})
			}
		})
	}
}

func TestIntegrationMTLSRejectsInvalidClientCertificates(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	now := time.Now()

	serverCA := newTestCA(t, "Server Root CA", notBefore, notAfter)
	deviceCA := newTestCA(t, "Device Root CA", notBefore, notAfter)
	rogueCA := newTestCA(t, "Rogue CA", notBefore, notAfter)
	intermediateCA := newTestIntermediateCA(t, "Device Intermediate CA", deviceCA, notBefore, notAfter, testKeyECDSA)

	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})

	validClient := newTestClientLeaf(t, "device-valid", deviceCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)
	expiredClient := newTestClientLeaf(t, "device-expired", deviceCA,
		now.Add(-48*time.Hour), now.Add(-2*time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)
	futureClient := newTestClientLeaf(t, "device-future", deviceCA,
		now.Add(2*time.Hour), now.Add(48*time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)
	wrongEKUClient := newTestClientLeaf(t, "device-server-eku", deviceCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, testKeyECDSA)
	untrustedClient := newTestClientLeaf(t, "device-untrusted", rogueCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)
	serverCASignedClient := newTestClientLeaf(t, "device-signed-by-server-ca", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)
	intermediateClient := newTestClientLeaf(t, "device-intermediate", intermediateCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)

	tests := []struct {
		name  string
		chain []*testCert
	}{
		{"no-client-certificate", nil},
		{"untrusted-issuer", []*testCert{untrustedClient}},
		{"signed-by-server-ca", []*testCert{serverCASignedClient}},
		{"expired-client-certificate", []*testCert{expiredClient}},
		{"not-yet-valid-client-certificate", []*testCert{futureClient}},
		{"wrong-client-eku", []*testCert{wrongEKUClient}},
		{"missing-intermediate", []*testCert{intermediateClient}},
	}
	versions := []struct {
		name     string
		min, max string
		want     uint16
	}{
		{"tls13", "1.3", "1.3", tls.VersionTLS13},
		{"tls12", "1.2", "1.2", tls.VersionTLS12},
	}

	for _, test := range tests {
		for _, version := range versions {
			t.Run(test.name+"/"+version.name, func(t *testing.T) {
				_, addr := startTestServer(t, testServerSpec{
					Mode:        ModeMTLS,
					ServerChain: []*testCert{serverLeaf},
					ClientCAs:   []*testCert{deviceCA},
					TLSMin:      version.min,
					TLSMax:      version.max,
				})

				ctx, cancel := contextWithTestTimeout()
				defer cancel()
				client, err := dialMQTT(ctx, addr, mqttClientOptions{
					TLSConfig: testClientTLSConfig(t, testCertPool(t, serverCA), "localhost", test.chain...),
				})
				if client != nil {
					_ = client.Close()
				}
				clientErr := requireNoConnack(t, err)
				if version.want == tls.VersionTLS12 && clientErr.Stage != mqttStageTLS {
					t.Fatalf("TLS 1.2 client rejection stage = %s, want %s (error: %v)",
						clientErr.Stage, mqttStageTLS, err)
				}
				if version.want == tls.VersionTLS13 {
					switch clientErr.Stage {
					case mqttStageTLS, mqttStageConnectWrite, mqttStageConnectRead:
					default:
						t.Fatalf("TLS 1.3 client rejection surfaced at unexpected stage %s: %v", clientErr.Stage, err)
					}
				}

				// 同一服务器随后仍能接受合法设备，说明是证书被拒绝而不是服务崩溃。
				healthy := requireConnect(t, addr, mqttClientOptions{
					TLSConfig: testClientTLSConfig(t, testCertPool(t, serverCA), "localhost", validClient),
				})
				requireTLSVersion(t, healthy, version.want)
				granted, err := healthy.Subscribe("devices/health", 1)
				if err != nil || granted == 0x80 {
					t.Fatalf("health check subscribe after rejection: granted=%d err=%v", granted, err)
				}
			})
		}
	}
}

func TestIntegrationClientVerifiesServerCertificate(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	now := time.Now()

	serverCA := newTestCA(t, "Server Root CA", notBefore, notAfter)
	rogueCA := newTestCA(t, "Rogue CA", notBefore, notAfter)
	validLeaf := newTestServerLeafForNames(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		[]string{"localhost"}, nil, testKeyECDSA)
	ipLeaf := newTestServerLeafForNames(t, "ip-only", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		nil, []net.IP{net.ParseIP("127.0.0.1")}, testKeyECDSA)
	mismatchLeaf := newTestServerLeafForNames(t, "mismatch", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		[]string{"mqtt.example.invalid"}, nil, testKeyECDSA)
	untrustedLeaf := newTestServerLeaf(t, "localhost", rogueCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	expiredLeaf := newTestServerLeaf(t, "localhost", serverCA,
		now.Add(-48*time.Hour), now.Add(-2*time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	futureLeaf := newTestServerLeaf(t, "localhost", serverCA,
		now.Add(2*time.Hour), now.Add(48*time.Hour),
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})

	t.Run("dns-san-success", func(t *testing.T) {
		_, addr := startTestServer(t, testServerSpec{
			Mode:        ModeTLS,
			ServerChain: []*testCert{validLeaf},
		})
		client := requireConnect(t, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, testCertPool(t, serverCA), "localhost"),
		})
		mustRoundTrip(t, client, "server-cert/dns", "ok")
	})

	t.Run("ip-san-success", func(t *testing.T) {
		_, addr := startTestServer(t, testServerSpec{
			Mode:        ModeTLS,
			ServerChain: []*testCert{ipLeaf},
		})
		client := requireConnect(t, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, testCertPool(t, serverCA), "127.0.0.1"),
		})
		mustRoundTrip(t, client, "server-cert/ip", "ok")
	})

	t.Run("san-mismatch", func(t *testing.T) {
		_, addr := startTestServer(t, testServerSpec{
			Mode:        ModeTLS,
			ServerChain: []*testCert{mismatchLeaf},
		})
		ctx, cancel := contextWithTestTimeout()
		defer cancel()
		client, err := dialMQTT(ctx, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, testCertPool(t, serverCA), "localhost"),
		})
		if client != nil {
			_ = client.Close()
		}
		requireNoConnack(t, err)
		var hostnameErr x509.HostnameError
		if !errors.As(err, &hostnameErr) {
			t.Fatalf("expected x509.HostnameError, got %T: %v", err, err)
		}
	})

	t.Run("untrusted-server-certificate", func(t *testing.T) {
		_, addr := startTestServer(t, testServerSpec{
			Mode:        ModeTLS,
			ServerChain: []*testCert{untrustedLeaf, rogueCA},
		})
		ctx, cancel := contextWithTestTimeout()
		defer cancel()
		client, err := dialMQTT(ctx, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, testCertPool(t, serverCA), "localhost"),
		})
		if client != nil {
			_ = client.Close()
		}
		requireNoConnack(t, err)
		var unknownErr x509.UnknownAuthorityError
		if !errors.As(err, &unknownErr) {
			t.Fatalf("expected x509.UnknownAuthorityError, got %T: %v", err, err)
		}
	})

	invalidServerTests := []struct {
		name string
		leaf *testCert
	}{
		{"expired-server-certificate", expiredLeaf},
		{"not-yet-valid-server-certificate", futureLeaf},
	}
	for _, test := range invalidServerTests {
		t.Run(test.name, func(t *testing.T) {
			// 默认启动检查会拦截这些反例，使用既定的显式测试通道；
			// 该通道只放行服务器证书语义检查，不修改客户端验证。
			server, addr := startTestServer(t, testServerSpec{
				Mode:                   ModeTLS,
				ServerChain:            []*testCert{test.leaf},
				AllowInvalidServerCert: true,
			})
			if report := server.CertificateReport(); report == nil || len(report.Bypassed) == 0 {
				t.Fatalf("expected bypassed certificate checks, got report=%+v", report)
			}

			ctx, cancel := contextWithTestTimeout()
			defer cancel()
			client, err := dialMQTT(ctx, addr, mqttClientOptions{
				TLSConfig: testClientTLSConfig(t, testCertPool(t, serverCA), "localhost"),
			})
			if client != nil {
				_ = client.Close()
			}
			requireNoConnack(t, err)
			var invalidErr x509.CertificateInvalidError
			if !errors.As(err, &invalidErr) {
				t.Fatalf("expected x509.CertificateInvalidError, got %T: %v", err, err)
			}
			if invalidErr.Reason != x509.Expired {
				t.Fatalf("certificate invalid reason = %v, want Expired: %v", invalidErr.Reason, err)
			}
		})
	}
}

func TestIntegrationIntermediateCAChains(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	rootCA := newTestCA(t, "Root CA", notBefore, notAfter)
	intermediateCA := newTestIntermediateCA(t, "Intermediate CA", rootCA, notBefore, notAfter, testKeyECDSA)

	serverLeaf := newTestServerLeaf(t, "localhost", intermediateCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	deviceLeaf := newTestClientLeaf(t, "device-under-intermediate", intermediateCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)

	t.Run("mtls-complete-client-chain", func(t *testing.T) {
		_, addr := startTestServer(t, testServerSpec{
			Mode:        ModeMTLS,
			ServerChain: []*testCert{serverLeaf, intermediateCA},
			ClientCAs:   []*testCert{rootCA},
		})
		client := requireConnect(t, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, testCertPool(t, rootCA), "localhost", deviceLeaf, intermediateCA),
		})
		mustRoundTrip(t, client, "chain/mtls", "complete")
	})

	t.Run("mtls-missing-client-intermediate", func(t *testing.T) {
		_, addr := startTestServer(t, testServerSpec{
			Mode:        ModeMTLS,
			ServerChain: []*testCert{serverLeaf, intermediateCA},
			ClientCAs:   []*testCert{rootCA},
		})
		ctx, cancel := contextWithTestTimeout()
		defer cancel()
		client, err := dialMQTT(ctx, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, testCertPool(t, rootCA), "localhost", deviceLeaf),
		})
		if client != nil {
			_ = client.Close()
		}
		requireNoConnack(t, err)
	})

	t.Run("tls-complete-server-chain", func(t *testing.T) {
		_, addr := startTestServer(t, testServerSpec{
			Mode:        ModeTLS,
			ServerChain: []*testCert{serverLeaf, intermediateCA},
		})
		client := requireConnect(t, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, testCertPool(t, rootCA), "localhost"),
		})
		if got := len(client.tlsState.PeerCertificates); got != 2 {
			t.Fatalf("server presented %d certificates, want 2", got)
		}
		mustRoundTrip(t, client, "chain/server", "complete")
	})

	t.Run("tls-missing-server-intermediate", func(t *testing.T) {
		server, addr := startTestServer(t, testServerSpec{
			Mode:        ModeTLS,
			ServerChain: []*testCert{serverLeaf},
		})
		report := server.CertificateReport()
		if report == nil {
			t.Fatal("missing certificate report")
		}
		if !containsWarning(report.Warnings, "non-self-signed") {
			t.Fatalf("expected a warning for an incomplete server chain, got %v", report.Warnings)
		}

		ctx, cancel := contextWithTestTimeout()
		defer cancel()
		client, err := dialMQTT(ctx, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, testCertPool(t, rootCA), "localhost"),
		})
		if client != nil {
			_ = client.Close()
		}
		requireNoConnack(t, err)
		var unknownErr x509.UnknownAuthorityError
		if !errors.As(err, &unknownErr) {
			t.Fatalf("expected x509.UnknownAuthorityError for incomplete chain, got %T: %v", err, err)
		}
	})
}

const integrationAuthUsers = `
users:
  alice:
    password: alice-secret
  watcher:
    password: watcher-secret
acl:
  - username: alice
    filters:
      "allowed/#": 3
      "readonly/#": 1
  - username: alice
    filters:
      "#": 0
  - username: watcher
    filters:
      "#": 3
`

func TestIntegrationMQTTAuthenticationAndACL(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Server Root CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	roots := testCertPool(t, serverCA)
	users := []byte(integrationAuthUsers)

	start := func(t *testing.T) string {
		t.Helper()
		_, addr := startTestServer(t, testServerSpec{
			Mode:        ModeTLS,
			ServerChain: []*testCert{serverLeaf},
			Users:       users,
		})
		return addr
	}
	clientFor := func(t *testing.T, addr, user, password string) *mqttTestClient {
		t.Helper()
		return requireConnect(t, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, roots, "localhost"),
			Username:  user,
			Password:  password,
		})
	}

	t.Run("valid-credentials-and-allowed-acl", func(t *testing.T) {
		addr := start(t)
		alice := clientFor(t, addr, "alice", "alice-secret")
		mustRoundTrip(t, alice, "allowed/data", "authorized")

		granted, err := alice.Subscribe("readonly/status", 1)
		if err != nil {
			t.Fatalf("subscribe read-only topic: %v", err)
		}
		if granted == 0x80 {
			t.Fatal("read-only subscription was unexpectedly denied")
		}
	})

	t.Run("wrong-password-is-connack-refusal", func(t *testing.T) {
		addr := start(t)
		ctx, cancel := contextWithTestTimeout()
		defer cancel()
		client, err := dialMQTT(ctx, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, roots, "localhost"),
			Username:  "alice",
			Password:  "wrong-password",
		})
		if client != nil {
			_ = client.Close()
		}
		// 0x05 = not authorized (MQTT 3.1.1 mapping of bad username/password).
		requireConnackRefused(t, err, 0x05)
	})

	t.Run("missing-credentials-is-connack-refusal", func(t *testing.T) {
		addr := start(t)
		ctx, cancel := contextWithTestTimeout()
		defer cancel()
		client, err := dialMQTT(ctx, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, roots, "localhost"),
		})
		if client != nil {
			_ = client.Close()
		}
		requireConnackRefused(t, err, 0x05)
	})

	t.Run("denied-subscribe-returns-failure-code", func(t *testing.T) {
		addr := start(t)
		alice := clientFor(t, addr, "alice", "alice-secret")
		granted, err := alice.Subscribe("forbidden/data", 1)
		if err != nil {
			t.Fatalf("subscribe to denied topic: %v", err)
		}
		if granted != 0x80 {
			t.Fatalf("denied subscribe granted code 0x%02x, want 0x80", granted)
		}
	})

	t.Run("denied-qos1-publish-disconnects", func(t *testing.T) {
		addr := start(t)
		alice := clientFor(t, addr, "alice", "alice-secret")
		err := alice.Publish("forbidden/data", []byte("blocked"), 1)
		if err == nil {
			t.Fatal("expected the server to reject an unauthorized QoS 1 publish")
		}
		var clientErr *mqttClientError
		if !errors.As(err, &clientErr) {
			t.Fatalf("expected mqttClientError, got %T: %v", err, err)
		}
		if clientErr.Stage != mqttStageRead && clientErr.Stage != mqttStageWrite {
			t.Fatalf("unauthorized publish failed at stage %s, want read/write: %v", clientErr.Stage, err)
		}

		watcher := clientFor(t, addr, "watcher", "watcher-secret")
		mustRoundTrip(t, watcher, "forbidden/data", "watcher-still-healthy")
	})

	t.Run("denied-qos0-publish-is-dropped", func(t *testing.T) {
		addr := start(t)
		watcher := clientFor(t, addr, "watcher", "watcher-secret")
		granted, err := watcher.Subscribe("forbidden/#", 1)
		if err != nil || granted == 0x80 {
			t.Fatalf("watcher subscribe: granted=%d err=%v", granted, err)
		}

		alice := clientFor(t, addr, "alice", "alice-secret")
		if err := alice.Publish("forbidden/data", []byte("must-not-arrive"), 0); err != nil {
			t.Fatalf("QoS 0 publish should not tear down the connection: %v", err)
		}
		if _, err := watcher.NextPublish(integrationNoDataWindow); err == nil {
			t.Fatal("unauthorized QoS 0 publish was delivered")
		}
		// 超时后的读取不再复用该连接，避免半包状态影响后续断言。
		_ = watcher.Close()

		// 证明同一主题的订阅本身有效，只是刚才那条消息被 ACL 丢弃。
		authorizedWatcher := clientFor(t, addr, "watcher", "watcher-secret")
		mustRoundTrip(t, authorizedWatcher, "forbidden/data", "watcher-authorized")
	})
}

func TestIntegrationTLSFailureIsDistinctFromMQTTAuthFailure(t *testing.T) {
	notBefore, notAfter := validServerWindow()
	serverCA := newTestCA(t, "Server Root CA", notBefore, notAfter)
	deviceCA := newTestCA(t, "Device Root CA", notBefore, notAfter)
	serverLeaf := newTestServerLeaf(t, "localhost", serverCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	deviceLeaf := newTestClientLeaf(t, "device-001", deviceCA, notBefore, notAfter,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, testKeyECDSA)
	roots := testCertPool(t, serverCA)
	users := []byte(integrationAuthUsers)

	_, addr := startTestServer(t, testServerSpec{
		Mode:        ModeMTLS,
		ServerChain: []*testCert{serverLeaf},
		ClientCAs:   []*testCert{deviceCA},
		Users:       users,
	})

	t.Run("missing-device-certificate-fails-before-connack", func(t *testing.T) {
		ctx, cancel := contextWithTestTimeout()
		defer cancel()
		client, err := dialMQTT(ctx, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, roots, "localhost"),
			Username:  "alice",
			Password:  "alice-secret",
		})
		if client != nil {
			_ = client.Close()
		}
		requireNoConnack(t, err)
	})

	t.Run("valid-device-certificate-with-wrong-password-gets-connack", func(t *testing.T) {
		ctx, cancel := contextWithTestTimeout()
		defer cancel()
		client, err := dialMQTT(ctx, addr, mqttClientOptions{
			TLSConfig: testClientTLSConfig(t, roots, "localhost", deviceLeaf),
			Username:  "alice",
			Password:  "wrong-password",
		})
		if client != nil {
			_ = client.Close()
		}
		requireConnackRefused(t, err, 0x05)
	})
}

func containsWarning(warnings []string, substring string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, substring) {
			return true
		}
	}
	return false
}
