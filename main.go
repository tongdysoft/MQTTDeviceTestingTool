//go:generate goversioninfo -icon=ico/icon.ico -manifest main.exe.manifest -arm=true
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/cloudfoundry/jibber_jabber"
)

const (
	version    = "1.5.5"
	timeFormat = "2006-01-02 15:04:05"
	isWindows  = runtime.GOOS == "windows"
)

// language 只在启动最早阶段写入一次，之后由所有 goroutine 只读。
var language string

func main() {
	os.Exit(run())
}

// run 负责参数解析、信号、启动与退出码。进程信号只在这里处理，
// 服务器与测试接口本身不接触 os.Signal 或 os.Exit。
func run() int {
	cfg, err := ParseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		fmt.Fprintln(os.Stderr, "run without arguments to start plain MQTT; see README.md for all options")
		return 2
	}

	language = cfg.Language
	if language == "auto" {
		language = "en"
		syslang, _ := jibber_jabber.DetectIETF()
		if strings.Contains(syslang, "zh") {
			language = "chs"
		}
	}

	if cfg.ShowHelp {
		fmt.Printf("%s v%s for %s\n\n", lang("TITLE"), version, runtime.GOOS)
		fmt.Print(cfg.HelpText)
		fmt.Printf("\n%s https://github.com/tongdysoft/MQTTDeviceTestingTool\n", lang("HELP"))
		return 0
	}

	if cfg.ShowVersion {
		fmt.Printf("%s v%s for %s (KagurazakaYashi@Tongdy, 2024)\n", lang("TITLE"), version, runtime.GOOS)
		fmt.Printf("%s https://github.com/tongdysoft/MQTTDeviceTestingTool\n", lang("HELP"))
		return 0
	}

	if cfg.Mode != ModePlain {
		password, err := ResolveKeyPassword(PasswordSourceOptions{
			File:    cfg.ServerKeyPasswordFile,
			StdinFd: int(os.Stdin.Fd()),
			Prompt:  os.Stderr,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "configuration error:", err)
			return 2
		}
		cfg.KeyPassword = password
	}

	// 文本日志、诊断事件与 mochi 日志共享同一终端写锁。
	terminal := newLockedWriter(os.Stdout)
	eventLog, err := OpenEventLog(EventLogOptions{
		Terminal:      terminal,
		ErrorWriter:   os.Stderr,
		TextPath:      cfg.LogFile,
		StatusPath:    cfg.LogStatusFile,
		DataPath:      cfg.LogDataFile,
		FileTimestamp: cfg.FileTimestamp,
		Monochrome:    cfg.Monochrome,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "log error:", err)
		return 1
	}
	defer func() { _ = eventLog.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	eventLog.Print("I", lang("TITLE")+" v"+version+" for "+runtime.GOOS+" (KagurazakaYashi@Tongdy, 2024)")
	eventLog.Print("I", lang("HELP")+" https://github.com/tongdysoft/MQTTDeviceTestingTool")
	logRecordFilters(cfg, eventLog)

	debug, err := startDebugServer(cfg.DebugHTTPListen, eventLog)
	if err != nil {
		eventLog.Print("X", lang("STARTFAIL")+err.Error())
		_ = eventLog.Close()
		return 1
	}
	defer func() {
		if err := debug.Close(cfg.ShutdownTimeout); err != nil {
			fmt.Fprintln(os.Stderr, "debug listener:", err)
		}
	}()

	eventLog.Print("I", lang("BOOTING"))
	server, err := StartServer(StartOptions{Config: cfg, Context: ctx, EventLog: eventLog, LogWriter: terminal})
	if err != nil {
		eventLog.Print("X", lang("STARTFAIL")+err.Error())
		if closeErr := eventLog.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "log error:", closeErr)
		}
		return 1
	}
	defer func() { _ = server.Close() }()

	logEffectiveConfig(server, eventLog)
	eventLog.Print("C", fmt.Sprintf("%s: %s", lang("RUNID"), server.Diagnostics().RunID()))
	if eventsPath, summaryPath := server.Diagnostics().ResultsPaths(); eventsPath != "" {
		eventLog.Print("C", fmt.Sprintf("%s: %s (%s, %s)", lang("RESULTS"), cfg.ResultsDir, eventsPath, summaryPath))
	}
	eventLog.Status(server.Addr(), lang("START"), lang("TITLE")+" v"+version)
	eventLog.Data(server.Addr(), lang("TITLE")+" v"+version, lang("START"))
	eventLog.Print("I", lang("BOOTOK"))

	<-ctx.Done()
	eventLog.Print("X", fmt.Sprintf("%s: %s", lang("NEEDSTOP"), server.Addr()))
	closeErr := server.Close()
	if closeErr != nil {
		eventLog.Print("X", fmt.Sprintf("%s: %v", lang("EXPORTFAIL"), closeErr))
	}
	eventLog.Print("I", lang("END"))
	if err := eventLog.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "log error:", err)
		return 1
	}
	if closeErr != nil {
		return 1
	}
	return 0
}

func logRecordFilters(cfg *Config, eventLog *EventLog) {
	if len(cfg.RecordOnlyClientIDs) > 0 {
		eventLog.Print("C", fmt.Sprintf("%s%s: %s (%s)", lang("RECORDONLY"), lang("CLIENT"),
			cfg.RecordOnlyClientIDs, lang("RECORDONLYNOTE")))
	}
	if len(cfg.RecordOnlyTopics) > 0 {
		eventLog.Print("C", fmt.Sprintf("%s%s: %s (%s)", lang("RECORDONLY"), lang("TOPIC"),
			cfg.RecordOnlyTopics, lang("RECORDONLYNOTE")))
	}
	if len(cfg.RecordOnlyPayloads) > 0 {
		eventLog.Print("C", fmt.Sprintf("%s%s: %s (%s)", lang("RECORDONLY"), lang("WORD"),
			cfg.RecordOnlyPayloads, lang("RECORDONLYNOTE")))
	}
}

// logEffectiveConfig 显示实际生效的模式、监听地址、TLS 版本、客户端证书策略
// 与生命周期限制。这里不输出私钥、私钥密码或任何密钥长度。
func logEffectiveConfig(server *RunningServer, eventLog *EventLog) {
	cfg := server.Config()
	eventLog.Print("C", fmt.Sprintf("%s: %s", lang("MODE"), modeLabel(cfg.Mode)))
	eventLog.Print("C", fmt.Sprintf("%s: %s", lang("LISTENADDR"), server.Addr()))
	eventLog.Print("C", fmt.Sprintf("%s: %s, %s: %s, %s: %d, %s: %s",
		lang("HANDSHAKETIMEOUT"), cfg.HandshakeTimeout,
		lang("CONNECTTIMEOUT"), cfg.ConnectTimeout,
		lang("MAXHANDSHAKES"), cfg.MaxConcurrentHandshakes,
		lang("SHUTDOWNTIMEOUT"), cfg.ShutdownTimeout,
	))

	tlsConfig := server.TLSConfig()
	if tlsConfig == nil {
		eventLog.Print("C", fmt.Sprintf("%s: %s", lang("TLSVERSIONS"), lang("TLSDISABLED")))
		eventLog.Print("C", fmt.Sprintf("%s: %s", lang("CLIENTAUTH"), lang("CLIENTAUTH_NA")))
		return
	}

	eventLog.Print("C", fmt.Sprintf("%s: %s - %s",
		lang("TLSVERSIONS"),
		tls.VersionName(tlsConfig.MinVersion),
		tls.VersionName(tlsConfig.MaxVersion),
	))
	if tlsConfig.ClientAuth == tls.RequireAndVerifyClientCert {
		eventLog.Print("C", fmt.Sprintf("%s: %s", lang("CLIENTAUTH"), lang("CLIENTAUTH_REQUIRED")))
	} else {
		eventLog.Print("C", fmt.Sprintf("%s: %s", lang("CLIENTAUTH"), lang("CLIENTAUTH_NONE")))
	}

	report := server.CertificateReport()
	if report != nil {
		eventLog.Print("C", fmt.Sprintf("%s: %s", lang("CERTCHAIN"), formatChainSummary(report.ServerChain)))
		eventLog.Print("C", fmt.Sprintf("%s: %s", lang("SERVERKEY"), report.ServerKeyFormat))
		eventLog.Print("C", fmt.Sprintf("%s: %s", lang("KEYPWDSOURCE"), report.ServerKeyPasswordSource))
		if report.ClientCAs > 0 {
			eventLog.Print("C", fmt.Sprintf("%s: %d", lang("CLIENTCAS"), report.ClientCAs))
		}
		for _, warning := range report.Warnings {
			eventLog.Print("C", lang("CERTWARN")+warning)
		}
		for _, bypassed := range report.Bypassed {
			eventLog.Print("X", lang("CERTBYPASS")+bypassed)
		}
	}
	if cfg.AllowInvalidServerCert {
		eventLog.Print("X", lang("INVALIDCERTWARN"))
	}
}

func formatChainSummary(chain []CertificateSummary) string {
	if len(chain) == 0 {
		return "?"
	}
	parts := make([]string, 0, len(chain))
	for _, cert := range chain {
		parts = append(parts, cert.Subject)
	}
	return strings.Join(parts, " -> ")
}

func modeLabel(mode SecurityMode) string {
	switch mode {
	case ModePlain:
		return lang("MODE_PLAIN")
	case ModeTLS:
		return lang("MODE_TLS")
	case ModeMTLS:
		return lang("MODE_MTLS")
	default:
		return string(mode)
	}
}
