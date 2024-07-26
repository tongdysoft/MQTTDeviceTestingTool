package main

// OpenSSL 证书脚本的通配符 SAN 测试，属于显式启用的 OpenSSL 测试路径：
//
//	MQTT_TEST_OPENSSL_SCRIPT=1 go test -run 'TestOpenSSLScript' -v
//
// 覆盖：普通 DNS 与通配符混合、仅通配符且无 IP SAN、非法星号位置与格式。
// 生成的证书同时用 Go x509 交叉验证主机匹配结果，避免只依赖脚本内部自检。

import (
	"crypto/x509"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testWildcardSAN = "*.example.com"

// opensslScriptPrerequisites 统一检查脚本集成测试的启用开关与运行环境。
func opensslScriptPrerequisites(t *testing.T) (bashPath, scriptPath string) {
	t.Helper()
	if os.Getenv(opensslScriptEnableEnv) != "1" {
		t.Skipf("未启用 OpenSSL 脚本集成测试；设置 %s=1 后运行（需要 bash 与 OpenSSL >= 1.1.1）", opensslScriptEnableEnv)
	}

	var err error
	bashPath, err = exec.LookPath("bash")
	if err != nil {
		t.Skipf("未找到 bash，跳过 OpenSSL 脚本集成测试：%v", err)
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skipf("未找到 openssl，跳过 OpenSSL 脚本集成测试：%v", err)
	}

	scriptPath = filepath.Join("scripts", "gen-test-certs.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("缺少证书生成脚本 %s：%v", scriptPath, err)
	}
	return bashPath, scriptPath
}

func TestOpenSSLScriptWildcardSAN(t *testing.T) {
	bashPath, scriptPath := opensslScriptPrerequisites(t)

	t.Run("普通DNS与通配符混合", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "证书 通配符 混合")
		runOpenSSLScript(t, bashPath, scriptPath,
			"-o", outDir,
			"--server-name", "mqtt-server",
			"--san-dns", testWildcardSAN+",localhost",
			"--san-ip", "127.0.0.1",
			"--device-id", "wildcard-device",
			"--validity-days", "30",
		)

		cert := loadCertificateForTest(t, filepath.Join(outDir, "server", "server.crt"))
		// 通配符必须以匹配模式原样进入 SAN，不能被 shell 展开成文件名或丢弃。
		if !containsStringInList(cert.DNSNames, testWildcardSAN) {
			t.Fatalf("服务器证书 DNS SAN = %v，缺少原样保留的 %s", cert.DNSNames, testWildcardSAN)
		}
		if !containsStringInList(cert.DNSNames, "localhost") {
			t.Errorf("服务器证书 DNS SAN = %v，缺少 localhost", cert.DNSNames)
		}
		if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "127.0.0.1" {
			t.Errorf("服务器证书 IP SAN = %v，预期仅 127.0.0.1", cert.IPAddresses)
		}

		// 具体主机名匹配与通配符覆盖边界（Go 标准库独立验证）。
		assertServerHostname(t, outDir, "mqtt.example.com", true)
		assertServerHostname(t, outDir, "other.example.com", true)
		assertServerHostname(t, outDir, "localhost", true)
		assertServerHostname(t, outDir, "example.com", false)
		assertServerHostname(t, outDir, "a.b.example.com", false)

		// 脚本内部自检必须实际覆盖通配符，而不是只修正了参数接受。
		manifest, err := os.ReadFile(filepath.Join(outDir, "manifest.txt"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"通配符 SAN 匹配具体主机名：mqtt.example.com",
			"通配符 SAN 不接受根域：example.com",
			"通配符 SAN 不接受多层子域：a.b.example.com",
			"SAN 与配置的 DNS/IP 列表一致",
		} {
			if !strings.Contains(string(manifest), want) {
				t.Errorf("manifest.txt 缺少通配符自检记录：%q", want)
			}
		}
	})

	t.Run("仅通配符且无IP_SAN", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "证书 仅通配符")
		runOpenSSLScript(t, bashPath, scriptPath,
			"-o", outDir,
			"--san-dns", testWildcardSAN,
			"--san-ip", "",
			"--validity-days", "30",
		)

		cert := loadCertificateForTest(t, filepath.Join(outDir, "server", "server.crt"))
		if len(cert.DNSNames) != 1 || cert.DNSNames[0] != testWildcardSAN {
			t.Fatalf("服务器证书 DNS SAN = %v，预期仅 %s", cert.DNSNames, testWildcardSAN)
		}
		if len(cert.IPAddresses) != 0 {
			t.Errorf("未配置 IP SAN 时证书仍带 IP：%v", cert.IPAddresses)
		}

		assertServerHostname(t, outDir, "mqtt.example.com", true)
		assertServerHostname(t, outDir, "example.com", false)
		assertServerHostname(t, outDir, "a.b.example.com", false)
	})
}

func TestOpenSSLScriptRejectsInvalidDNSSAN(t *testing.T) {
	bashPath, scriptPath := opensslScriptPrerequisites(t)

	cases := []struct {
		name    string
		san     string
		wantMsg string
	}{
		{"星号未覆盖整个标签", "*example.com", "最左侧标签"},
		{"星号不在最左侧标签", "foo.*.example.com", "最左侧标签"},
		{"多个星号", "*.*.example.com", "只允许一个通配符"},
		{"标签中间星号", "mq*.example.com", "最左侧标签"},
		{"只有星号", "*", "最左侧标签"},
		{"空标签", "a..example.com", "空标签"},
		{"尾随点", "example.com.", "空标签"},
		{"标签首尾连字符", "a-.example.com", "连字符"},
		{"混合列表含非法通配符", "localhost,*example.com", "最左侧标签"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outDir := filepath.Join(t.TempDir(), "证书 非法 SAN")
			cmd := exec.CommandContext(testContext(t), bashPath, scriptPath,
				"-o", outDir, "--san-dns", tc.san, "--san-ip", "")
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("非法 DNS SAN %q 仍被接受：\n%s", tc.san, out)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("预期脚本以非零退出码失败，实际错误：%v", err)
			}
			if code := exitErr.ExitCode(); code != 2 {
				t.Errorf("非法 DNS SAN %q 退出码 = %d，预期 2（参数错误）\n%s", tc.san, code, out)
			}
			if !strings.Contains(string(out), tc.wantMsg) {
				t.Errorf("错误信息未说明原因 %q：\n%s", tc.wantMsg, out)
			}
			// 参数被拒绝时不得创建输出目录或残留临时材料。
			if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
				t.Errorf("参数被拒绝后仍创建了输出目录 %s（err=%v）", outDir, statErr)
			}
			partials, globErr := filepath.Glob(outDir + ".partial-*")
			if globErr != nil {
				t.Fatal(globErr)
			}
			if len(partials) != 0 {
				t.Errorf("参数被拒绝后遗留临时目录：%v", partials)
			}
		})
	}
}

func containsStringInList(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// assertServerHostname 用 Go x509 验证服务器证书对具体主机名的接受/拒绝结果。
func assertServerHostname(t *testing.T, outDir, hostname string, wantMatch bool) {
	t.Helper()
	leaf := loadCertificateForTest(t, filepath.Join(outDir, "server", "server.crt"))
	caPEM, err := os.ReadFile(filepath.Join(outDir, "server", "ca-a.crt"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("无法把生成的 CA A 加入验证信任库")
	}

	_, err = leaf.Verify(x509.VerifyOptions{
		DNSName:   hostname,
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if wantMatch && err != nil {
		t.Errorf("主机名 %q 应被服务器证书接受，实际失败：%v（SAN %v）", hostname, err, leaf.DNSNames)
	}
	if !wantMatch && err == nil {
		t.Errorf("主机名 %q 不应被服务器证书接受（SAN %v）", hostname, leaf.DNSNames)
	}
}
