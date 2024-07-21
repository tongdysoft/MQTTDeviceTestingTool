//go:generate goversioninfo -icon=ico/icon.ico -manifest main.exe.manifest -arm=true
package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/cloudfoundry/jibber_jabber"
)

const version = "1.5.5"

var (
	language      string
	timeFormat    string = "2006-01-02 15:04:05"
	logFile       string
	logData       string
	logStatus     string
	logFileE      bool = false
	logDataE      bool = false
	logStatusE    bool = false
	fileTimestamp bool = false
	logFileF      *os.File
	logDataF      *os.File
	logStatusF    *os.File
	monochrome    bool = false
	isWindows     bool = runtime.GOOS == "windows"

	onlyIdE      bool     = false
	onlyTopicE   bool     = false
	onlyPayloadE bool     = false
	onlyIdS      []string = []string{}
	onlyTopicS   []string = []string{}
	onlyPayloadS []string = []string{}
)

func main() {
	// pprof 仅用于本地诊断，失败不影响主服务。
	go func() {
		_ = http.ListenAndServe(":9999", nil)
	}()

	cfg, err := ParseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		fmt.Fprintln(os.Stderr, "run without arguments to start plain MQTT; see README.md for all options")
		os.Exit(2)
	}

	language = cfg.Language
	if language == "auto" {
		language = "en"
		syslang, _ := jibber_jabber.DetectIETF()
		if strings.Contains(syslang, "zh") {
			language = "chs"
		}
	}

	logPrint("I", lang("TITLE")+" v"+version+" for "+runtime.GOOS+" (KagurazakaYashi@Tongdy, 2024)")
	logPrint("I", lang("HELP")+" https://github.com/tongdysoft/MQTTDeviceTestingTool")
	if cfg.ShowVersion {
		return
	}

	if cfg.Mode != ModePlain {
		password, err := ResolveKeyPassword(PasswordSourceOptions{
			File:    cfg.ServerKeyPasswordFile,
			StdinFd: int(os.Stdin.Fd()),
			Prompt:  os.Stderr,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "configuration error:", err)
			os.Exit(2)
		}
		cfg.KeyPassword = password
	}

	applyLogOptions(cfg)
	applyFilterOptions(cfg)
	logInit(cfg.Listen, lang("TITLE")+" v"+version)

	sigs := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		close(done)
	}()

	logPrint("I", lang("BOOTING"))
	server, err := StartServer(StartOptions{Config: cfg})
	if err != nil {
		logPrint("X", lang("STARTFAIL")+err.Error())
		closeLogFiles()
		os.Exit(1)
	}
	defer func() {
		signal.Stop(sigs)
		_ = server.Close()
	}()

	logEffectiveConfig(server)
	logPrint("C", fmt.Sprintf("%s: %s", lang("RUNID"), server.Diagnostics().RunID()))
	if eventsPath, summaryPath := server.Diagnostics().ResultsPaths(); eventsPath != "" {
		logPrint("C", fmt.Sprintf("%s: %s (%s, %s)", lang("RESULTS"), cfg.ResultsDir, eventsPath, summaryPath))
	}
	logPrint("I", lang("BOOTOK"))

	<-done
	logPrint("X", fmt.Sprintf("%s: %s", lang("NEEDSTOP"), server.Addr()))
	if err := server.Close(); err != nil {
		logPrint("X", fmt.Sprintf("%s: %v", lang("EXPORTFAIL"), err))
		closeLogFiles()
		os.Exit(1)
	}
	closeLogFiles()
	logPrint("I", lang("END"))
}

func applyLogOptions(cfg *Config) {
	logFile = cfg.LogFile
	logData = cfg.LogDataFile
	logStatus = cfg.LogStatusFile
	fileTimestamp = cfg.FileTimestamp
	monochrome = cfg.Monochrome
}

func applyFilterOptions(cfg *Config) {
	onlyIdS = nil
	onlyTopicS = nil
	onlyPayloadS = nil
	onlyIdE = false
	onlyTopicE = false
	onlyPayloadE = false
	if len(cfg.OnlyClientIDs) > 0 {
		onlyIdS = cfg.OnlyClientIDs
		onlyIdE = true
		logPrint("C", fmt.Sprintf("%s%s: %s", lang("ONLY"), lang("CLIENT"), onlyIdS))
	}
	if len(cfg.OnlyTopics) > 0 {
		onlyTopicS = cfg.OnlyTopics
		onlyTopicE = true
		logPrint("C", fmt.Sprintf("%s%s: %s", lang("ONLY"), lang("TOPIC"), onlyTopicS))
	}
	if len(cfg.OnlyPayloads) > 0 {
		onlyPayloadS = cfg.OnlyPayloads
		onlyPayloadE = true
		logPrint("C", fmt.Sprintf("%s%s: %s", lang("ONLY"), lang("WORD"), onlyPayloadS))
	}
}

// logEffectiveConfig 显示实际生效的模式、监听地址、TLS 版本和客户端证书策略。
// 这里不输出私钥、私钥密码或任何密钥长度。
func logEffectiveConfig(server *RunningServer) {
	cfg := server.Config()
	logPrint("C", fmt.Sprintf("%s: %s", lang("MODE"), modeLabel(cfg.Mode)))
	logPrint("C", fmt.Sprintf("%s: %s", lang("LISTENADDR"), server.Addr()))

	tlsConfig := server.TLSConfig()
	if tlsConfig == nil {
		logPrint("C", fmt.Sprintf("%s: %s", lang("TLSVERSIONS"), lang("TLSDISABLED")))
		logPrint("C", fmt.Sprintf("%s: %s", lang("CLIENTAUTH"), lang("CLIENTAUTH_NA")))
		return
	}

	logPrint("C", fmt.Sprintf("%s: %s - %s",
		lang("TLSVERSIONS"),
		tls.VersionName(tlsConfig.MinVersion),
		tls.VersionName(tlsConfig.MaxVersion),
	))
	if tlsConfig.ClientAuth == tls.RequireAndVerifyClientCert {
		logPrint("C", fmt.Sprintf("%s: %s", lang("CLIENTAUTH"), lang("CLIENTAUTH_REQUIRED")))
	} else {
		logPrint("C", fmt.Sprintf("%s: %s", lang("CLIENTAUTH"), lang("CLIENTAUTH_NONE")))
	}

	report := server.CertificateReport()
	if report != nil {
		logPrint("C", fmt.Sprintf("%s: %s", lang("CERTCHAIN"), formatChainSummary(report.ServerChain)))
		logPrint("C", fmt.Sprintf("%s: %s", lang("SERVERKEY"), report.ServerKeyFormat))
		logPrint("C", fmt.Sprintf("%s: %s", lang("KEYPWDSOURCE"), report.ServerKeyPasswordSource))
		if report.ClientCAs > 0 {
			logPrint("C", fmt.Sprintf("%s: %d", lang("CLIENTCAS"), report.ClientCAs))
		}
		for _, warning := range report.Warnings {
			logPrint("C", lang("CERTWARN")+warning)
		}
		for _, bypassed := range report.Bypassed {
			logPrint("X", lang("CERTBYPASS")+bypassed)
		}
	}
	if cfg.AllowInvalidServerCert {
		logPrint("X", lang("INVALIDCERTWARN"))
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

func closeLogFiles() {
	if logFileE && logFileF != nil {
		_ = logFileF.Close()
	}
	if logDataE && logDataF != nil {
		_ = logDataF.Close()
	}
	if logStatusE && logStatusF != nil {
		_ = logStatusF.Close()
	}
	logFileE = false
	logDataE = false
	logStatusE = false
}
