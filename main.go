//go:generate goversioninfo -icon=ico/icon.ico -manifest=main.exe.manifest -arm=true
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/cloudfoundry/jibber_jabber"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

var (
	language      string
	timeFormat    string = "2006-01-02 15:04:05"
	logFile       string
	userFile      string
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
	go http.ListenAndServe(":9999", nil)
	var (
		version        string = "1.5.5"
		versionView    bool   = false
		listen         string
		onlyID         string
		onlyTopic      string
		onlyPayload    string
		certCA         string
		certCert       string
		certKey        string
		certPassword   string
		useTLS         bool = false
		certClientAuth int  = 0
	)
	// 初始化启动参数
	flag.BoolVar(&versionView, "v", false, "Print version info")
	flag.StringVar(&language, "l", "auto", "Language ( en | chs )")
	flag.StringVar(&listen, "p", "0.0.0.0:1883", "Define listening on IP:Port (default: 0.0.0.0:1883 )")
	flag.StringVar(&onlyID, "c", "", "Only allow these client IDs (comma separated)")
	flag.StringVar(&onlyTopic, "t", "", "Only allow these topics (comma separated)")
	flag.StringVar(&onlyPayload, "w", "", "Only allow these words in message content (comma separated)")
	flag.StringVar(&logData, "m", "", "Log message to csv file")
	flag.StringVar(&logStatus, "s", "", "Log state changes to a csv file")
	flag.StringVar(&logFile, "o", "", "Save log to txt/log file")
	flag.BoolVar(&fileTimestamp, "ts", false, "Use timestamps in logged files")
	flag.BoolVar(&monochrome, "n", false, "Use a monochrome color scheme (When an abnormal character appears in Windows cmd.exe)")
	flag.StringVar(&userFile, "u", "", "Users and permissions file (.json, visit README.md) path")
	flag.StringVar(&certCA, "ca", "", "CA certificate file path")
	flag.StringVar(&certCert, "ce", "", "Server certificate file path")
	flag.StringVar(&certKey, "ck", "", "Server key file path")
	flag.StringVar(&certPassword, "cp", "", "Server key file password")
	flag.IntVar(&certClientAuth, "cv", -1, "Policy the server will follow for TLS Client Authentication")
	flag.Parse()
	if language == "auto" {
		language = "en"
		syslang, _ := jibber_jabber.DetectIETF()
		if len(syslang) > 0 {
			if strings.Contains(syslang, "zh") {
				language = "chs"
			}
		}
	}
	logPrint("I", lang("TITLE")+" v"+version+" for "+runtime.GOOS+" (KagurazakaYashi@Tongdy, 2024)")
	logPrint("I", lang("HELP")+" https://github.com/tongdysoft/mqtt-test-server")
	// 初始化设置
	if versionView {
		return
	}
	if len(onlyID) > 0 {
		onlyIdS = strings.Split(onlyID, ",")
		logPrint("C", fmt.Sprintf("%s%s: %s", lang("ONLY"), lang("CLIENT"), onlyIdS))
		onlyIdE = true
	}
	if len(onlyTopic) > 0 {
		onlyTopicS = strings.Split(onlyTopic, ",")
		logPrint("C", fmt.Sprintf("%s%s: %s", lang("ONLY"), lang("TOPIC"), onlyTopicS))
		onlyTopicE = true
	}
	if len(onlyPayload) > 0 {
		onlyPayloadS = strings.Split(onlyPayload, ",")
		logPrint("C", fmt.Sprintf("%s%s: %s", lang("ONLY"), lang("WORD"), onlyPayloadS))
		onlyPayloadE = true
	}
	logInit(listen, lang("TITLE")+" v"+version)
	// 监听结束信号
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
		close(sigs)
	}()
	// 加载 SSL 证书
	var clientAuth tls.ClientAuthType = clientAuthDefault(certClientAuth, certCA, certCert)
	tlsConfig := &tls.Config{ClientAuth: clientAuth}
	certPool := x509.NewCertPool()
	if len(certCA) > 0 {
		contentC, err := os.ReadFile(certCA)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s%s: %s: %s)", lang("CACERT"), lang("ERROR"), certCA, err.Error()))
			return
		}
		var isok bool = certPool.AppendCertsFromPEM(contentC)
		if !isok {
			logPrint("X", fmt.Sprintf("%s%s: %s (%d)", lang("CACERT"), lang("ERROR"), certCA, len(contentC)))
			return
		}
		tlsConfig = &tls.Config{
			ClientCAs:  certPool,
			ClientAuth: clientAuth,
		}
		useTLS = true
		logPrint("C", fmt.Sprintf("%s: %s (%d)", lang("CACERT"), certCA, len(contentC)))
	}
	if len(certCert) > 0 && len(certKey) == 0 {
		logPrint("X", fmt.Sprintf("%s%s: %s", lang("SERVERKEY"), lang("ERROR"), lang("NOTEMPTY")))
		return
	}
	if len(certCert) == 0 && len(certKey) > 0 {
		logPrint("X", fmt.Sprintf("%s%s: %s", lang("SERVERCERT"), lang("ERROR"), lang("NOTEMPTY")))
		return
	}
	if len(certCert) > 0 && len(certKey) > 0 {
		contentC, err := os.ReadFile(certCert)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s%s: %s: %s", lang("SERVERCERT"), lang("ERROR"), certCert, err.Error()))
			return
		}
		logPrint("C", fmt.Sprintf("%s: %s (%d)", lang("SERVERCERT"), certCert, len(contentC)))
		contentK, err := os.ReadFile(certKey)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s%s: %s: %s", lang("SERVERKEY"), lang("ERROR"), certKey, err.Error()))
			return
		}
		logPrint("C", fmt.Sprintf("%s: %s (%d)", lang("SERVERKEY"), certKey, len(contentK)))
		var cert tls.Certificate = LoadCert(contentC, contentK, certPassword)
		if cert.Certificate == nil {
			return
		}
		if len(certCA) > 0 {
			tlsConfig = &tls.Config{
				ClientCAs:    certPool,
				Certificates: []tls.Certificate{cert},
			}
		} else {
			tlsConfig = &tls.Config{
				Certificates: []tls.Certificate{cert},
			}
		}
	}
	if len(certPassword) > 0 {
		logPrint("C", fmt.Sprintf("%s: (%d)", lang("SERVERKEYPWD"), len(certPassword)))
	}
	// 初始化 MQTT 服务器
	logPrint("I", lang("BOOTING"), listen)
	var server *mqtt.Server = mqtt.New(nil)

	level := new(slog.LevelVar)
	server.Log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	level.Set(slog.LevelError)

	var userDataDefault string = `
auth:
    - allow: true
acl:
    - filters:
        '#': 3
`
	var err error = nil
	var userData []byte = []byte(userDataDefault)
	if len(userFile) > 0 {
		userData, err = os.ReadFile(userFile)
		if err != nil {
			log.Fatal(err)
		}
	}
	err = server.AddHook(new(auth.Hook), &auth.Options{
		Data: userData, // 从字节数组（文件二进制）读取 yaml 或 json
	})
	if err != nil {
		log.Fatal(err)
	}
	var tcp *listeners.TCP
	if useTLS {
		tcp = listeners.NewTCP(listeners.Config{
			Address:   listen,
			TLSConfig: tlsConfig,
		})
	} else {
		tcp = listeners.NewTCP(listeners.Config{
			Address: listen,
		})
	}
	err = server.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}
	// err = server.AddListener(tcp, &conf)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	err = server.AddHook(new(MQTTHook), &MQTTHookOptions{
		Server: server,
	})
	if err != nil {
		log.Fatal(err)
	}

	// 启动 MQTT 服务器
	go func() {
		err := server.Serve()
		if err != nil {
			logPrint("X", lang("SERVERFAIL"), listen)
			log.Fatal(err)
		}
	}()
	// server.Events.OnProcessMessage = func(cl events.Client, pk events.Packet) (pkx events.Packet, err error) {
	// 	return pkx, err
	// }
	// 设备连接出错
	// server.Events.OnError = func(cl events.Client, err error) {
	// 	logPrint("D", fmt.Sprintf("%v", err), strCL(cl))
	// }
	// 有设备连接到服务器
	// server.Events.OnConnect = func(cl events.Client, pk events.Packet) {}
	// 设备断开连接
	// server.Events.OnDisconnect = func(cl events.Client, err error) {}
	// 收到订阅请求
	// server.Events.OnSubscribe = func(filter string, cl events.Client, qos byte) {}
	// 收到取消订阅请求
	// server.Events.OnUnsubscribe = func(filter string, cl events.Client) {}
	// 收到消息
	// server.Events.OnMessage = func(cl events.Client, pk events.Packet) (pkx events.Packet, err error) {}
	// 启动完毕
	logPrint("I", lang("BOOTOK"), listen)
	// 处理结束信号
	<-done
	close(done)
	logPrint("X", lang("NEEDSTOP"), listen)
	server.Close()
	if logFileE {
		logFileF.Close()
	}
	if logDataE {
		logDataF.Close()
	}
	if logStatusE {
		logStatusF.Close()
	}
	logPrint("I", lang("END"))
	os.Exit(0)
}
