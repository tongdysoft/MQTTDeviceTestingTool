package main

// 通过 go test 显式启用的 OpenSSL 脚本集成测试。
//
// 默认跳过，不影响基础测试，也不要求开发机安装 OpenSSL：
//   MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScriptMaterial -v
//
// 本测试会在临时目录中实际执行 scripts/gen-test-certs.sh，核对输出结构、
// 权限、清单，并用项目真实的 StartServer 与生成的 mTLS 材料完成一次
// 回环 TLS 握手。脚本内部还会用 OpenSSL 复核链、用途、SAN 与时间条件。

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const opensslScriptEnableEnv = "MQTT_TEST_OPENSSL_SCRIPT"

func TestOpenSSLScriptMaterial(t *testing.T) {
	if os.Getenv(opensslScriptEnableEnv) != "1" {
		t.Skipf("未启用 OpenSSL 脚本集成测试；设置 %s=1 后运行（需要 bash 与 OpenSSL >= 1.1.1）", opensslScriptEnableEnv)
	}

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("未找到 bash，跳过 OpenSSL 脚本集成测试：%v", err)
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skipf("未找到 openssl，跳过 OpenSSL 脚本集成测试：%v", err)
	}

	scriptPath := filepath.Join("scripts", "gen-test-certs.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("缺少证书生成脚本 %s：%v", scriptPath, err)
	}

	// 路径刻意包含空格与中文，验证脚本的引用与 openssl 参数传递。
	outDir := filepath.Join(t.TempDir(), "设备 测试 证书")
	args := []string{
		scriptPath,
		"-o", outDir,
		"--server-name", "mqtt-server",
		"--san-dns", "localhost,mqtt.test",
		"--san-ip", "127.0.0.1",
		"--device-id", "test-device",
		"--validity-days", "30",
	}
	output := runOpenSSLScript(t, bashPath, args...)
	t.Logf("证书脚本输出：\n%s", output)

	mustExist := []string{
		"manifest.txt",
		"说明.txt",
		"ca/ca-a/ca-a.crt", "ca/ca-a/ca-a.key",
		"ca/ca-b/ca-b.crt", "ca/ca-b/ca-b.key",
		"server/server.crt", "server/server.key", "server/ca-a.crt",
		"server/negative/server-expired.crt",
		"server/negative/server-wrong-host.crt",
		"server/negative/server-by-ca-b.crt",
		"device/device.crt", "device/device.key", "device/ca-a.crt",
		"device/negative/device-expired.crt",
		"device/negative/device-notyet.crt",
		"device/negative/device-wrong-eku.crt",
		"device/negative/device-by-ca-b.crt",
		"device/negative/ca-b.crt",
	}
	for _, rel := range mustExist {
		path := filepath.Join(outDir, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("预期输出缺失 %s：%v", rel, err)
		}
		if info.IsDir() || info.Size() == 0 {
			t.Fatalf("输出文件无效 %s（目录或空文件）", rel)
		}
	}

	// 私钥权限：Unix 下必须是 0600；Windows 的模拟权限不作断言。
	if runtime.GOOS != "windows" {
		for _, rel := range []string{
			"ca/ca-a/ca-a.key", "ca/ca-b/ca-b.key",
			"server/server.key", "device/device.key",
		} {
			info, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("%s 权限为 %04o，预期 0600", rel, perm)
			}
		}
	}

	// 设备导入目录绝不能包含 CA 私钥或服务器私钥。
	assertNoForeignPrivateKeys(t, outDir, []string{
		"ca/ca-a/ca-a.key",
		"ca/ca-b/ca-b.key",
		"server/server.key",
	})

	// 清单只包含证书指纹与路径，不得出现私钥内容。
	manifest, err := os.ReadFile(filepath.Join(outDir, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"BEGIN PRIVATE KEY", "BEGIN RSA PRIVATE KEY", "BEGIN EC PRIVATE KEY"} {
		if bytes.Contains(manifest, []byte(forbidden)) {
			t.Errorf("manifest.txt 含私钥内容标记 %q", forbidden)
		}
	}
	for _, required := range []string{
		"CA A", "CA B", "校验结果", "服务器证书", "设备证书", "反例",
	} {
		if !bytes.Contains(manifest, []byte(required)) {
			t.Errorf("manifest.txt 缺少预期内容：%q", required)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "openssl-commands.log")); !os.IsNotExist(err) {
		t.Errorf("成功输出中不应保留 OpenSSL 命令日志（err=%v）", err)
	}

	// 生成的服务器材料必须能被工具自身的加载器接受。
	identity, err := LoadServerIdentityFiles(
		filepath.Join(outDir, "server", "server.crt"),
		filepath.Join(outDir, "server", "server.key"),
		IdentityOptions{},
	)
	if err != nil {
		t.Fatalf("用项目加载器读取生成的服务器材料失败：%v", err)
	}
	leaf := identity.Chain[0]
	if leaf.Subject.CommonName != "mqtt-server" {
		t.Errorf("服务器证书 CN = %q，预期 mqtt-server", leaf.Subject.CommonName)
	}
	if len(leaf.DNSNames) == 0 || leaf.DNSNames[0] != "localhost" {
		t.Errorf("服务器证书 DNS SAN = %v，预期包含 localhost", leaf.DNSNames)
	}
	if len(leaf.IPAddresses) == 0 || !leaf.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Errorf("服务器证书 IP SAN = %v，预期包含 127.0.0.1", leaf.IPAddresses)
	}
	if !hasEKU(leaf, x509.ExtKeyUsageServerAuth) {
		t.Error("服务器证书缺少 serverAuth 用途")
	}

	if _, err := LoadClientTrustStoreFile(filepath.Join(outDir, "device", "ca-a.crt")); err != nil {
		t.Fatalf("用项目加载器读取生成的设备信任 CA 失败：%v", err)
	}

	deviceCert := loadCertificateForTest(t, filepath.Join(outDir, "device", "device.crt"))
	if deviceCert.Subject.CommonName != "test-device" {
		t.Errorf("设备证书 CN = %q，预期 test-device", deviceCert.Subject.CommonName)
	}
	if !hasEKU(deviceCert, x509.ExtKeyUsageClientAuth) {
		t.Error("设备证书缺少 clientAuth 用途")
	}

	// 用真实监听路径验证 OpenSSL 生成的材料：正确设备证书可完成 mTLS，
	// CA B 签发的设备证书必须被拒绝。固定 TLS 1.2，使拒绝发生在握手阶段。
	verifyTLSMaterialWithServer(t, outDir)

	// 默认不覆盖：再次写入同一目录必须失败，且不得留下 partial 目录或残缺材料。
	second := exec.CommandContext(testContext(t), bashPath, scriptPath, "-o", outDir)
	secondOut, secondErr := second.CombinedOutput()
	if secondErr == nil {
		t.Fatal("输出目录已存在时脚本仍然成功，违反“不覆盖”策略")
	}
	if !bytes.Contains(secondOut, []byte("已存在")) {
		t.Errorf("再次运行时未给出明确的目录已存在错误：\n%s", secondOut)
	}
	partials, err := filepath.Glob(outDir + ".partial-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(partials) != 0 {
		t.Errorf("失败后遗留临时目录：%v", partials)
	}
}

func runOpenSSLScript(t *testing.T, bashPath string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(testContext(t), bashPath, args...)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("证书脚本执行失败：%v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	return stdout.String() + stderr.String()
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// assertNoForeignPrivateKeys 确认设备目录中的每个私钥都不是 CA 私钥或服务器私钥。
func assertNoForeignPrivateKeys(t *testing.T, outDir string, forbiddenRel []string) {
	t.Helper()
	forbidden := make(map[string]bool, len(forbiddenRel))
	for _, rel := range forbiddenRel {
		data, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		forbidden[string(data)] = true
	}

	deviceDir := filepath.Join(outDir, "device")
	err := filepath.WalkDir(deviceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".key") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if forbidden[string(data)] {
			t.Errorf("设备导入目录包含禁止的私钥：%s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历设备目录失败：%v", err)
	}
}

func loadCertificateForTest(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("%s 不是有效的证书 PEM", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("解析证书 %s 失败：%v", path, err)
	}
	return cert
}

func hasEKU(cert *x509.Certificate, want x509.ExtKeyUsage) bool {
	for _, eku := range cert.ExtKeyUsage {
		if eku == want {
			return true
		}
	}
	return false
}

// verifyTLSMaterialWithServer 启动真实 MQTT 服务，用生成的设备证书完成 mTLS。
func verifyTLSMaterialWithServer(t *testing.T, outDir string) {
	t.Helper()
	cfg := &Config{
		Listen:                 "127.0.0.1:0",
		Mode:                   ModeMTLS,
		ServerCertFile:         filepath.Join(outDir, "server", "server.crt"),
		ServerKeyFile:          filepath.Join(outDir, "server", "server.key"),
		ClientCAFile:           filepath.Join(outDir, "server", "ca-a.crt"),
		TLSMinVersion:          "1.2",
		TLSMaxVersion:          "1.2",
		AllowInvalidServerCert: false,
	}
	server, err := StartServer(StartOptions{Config: cfg, LogWriter: io.Discard})
	if err != nil {
		t.Fatalf("用生成的证书启动 mTLS 服务失败：%v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("关闭测试服务失败：%v", err)
		}
	})

	caPEM, err := os.ReadFile(filepath.Join(outDir, "device", "ca-a.crt"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("无法把生成的 CA A 加入信任库")
	}

	dial := func(certFile, keyFile string) error {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			t.Fatalf("加载设备证书失败：%v", err)
		}
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		conn, err := tls.DialWithDialer(dialer, "tcp", server.Addr(), &tls.Config{
			RootCAs:      roots,
			ServerName:   "localhost",
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
			MaxVersion:   tls.VersionTLS12,
		})
		if err != nil {
			return err
		}
		return conn.Close()
	}

	if err := dial(
		filepath.Join(outDir, "device", "device.crt"),
		filepath.Join(outDir, "device", "device.key"),
	); err != nil {
		t.Fatalf("CA A 签发的设备证书应能完成 mTLS 握手，实际失败：%v", err)
	}

	if err := dial(
		filepath.Join(outDir, "device", "negative", "device-by-ca-b.crt"),
		filepath.Join(outDir, "device", "negative", "device-by-ca-b.key"),
	); err == nil {
		t.Fatal("CA B 签发的设备证书不应被信任 CA A 的服务器接受")
	}
}
