//go:generate goversioninfo -icon=rc.ico -manifest=main.exe.manifest
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	mqtt "github.com/mochi-co/mqtt/server"
	"github.com/mochi-co/mqtt/server/events"
	"github.com/mochi-co/mqtt/server/listeners"
	"github.com/mochi-co/mqtt/server/listeners/auth"
)

var (
	language   string
	timeFormat string = "2006-01-02 15:04:05"
	logFile    string
	logData    string
	logStatus  string
	logFileE   bool = false
	logDataE   bool = false
	logStatusE bool = false
	logFileF   *os.File
	logDataF   *os.File
	logStatusF *os.File
	monochrome bool = false
)

func main() {
	go http.ListenAndServe(":9999", nil)
	var (
		version      string = "1.1.0"
		versionView  bool   = false
		listen       string
		onlyID       string
		onlyTopic    string
		onlyPayload  string
		onlyIdE      bool     = false
		onlyTopicE   bool     = false
		onlyPayloadE bool     = false
		onlyIdS      []string = []string{}
		onlyTopicS   []string = []string{}
		onlyPayloadS []string = []string{}
		certCA       string
	)
	// 初始化启动参数
	logPrint("i", lang("TITLE")+" v"+version)
	flag.BoolVar(&versionView, "v", false, "Print version info")
	flag.StringVar(&language, "l", "en", "Language ( en(default) | cn )")
	flag.StringVar(&listen, "p", "127.0.0.1:1883", "Define listening on IP:Port (default: 127.0.0.1:1883 )")
	flag.StringVar(&onlyID, "c", "", "Only allow these client IDs (comma separated)")
	flag.StringVar(&onlyTopic, "t", "", "Only allow these topics (comma separated)")
	flag.StringVar(&onlyPayload, "w", "", "Only allow these words in message content (comma separated)")
	flag.StringVar(&logData, "m", "", "Log message to csv file")
	flag.StringVar(&logStatus, "s", "", "Log state changes to a csv file")
	flag.StringVar(&logFile, "o", "", "Save log to txt/log file")
	flag.BoolVar(&monochrome, "n", false, "Use a monochrome color scheme")
	flag.StringVar(&certCA, "ca", "", "CA certificate file path")
	flag.Parse()
	// 初始化设置
	if versionView {
		logPrint("i", "KagurazakaYashi@Tongdy, 2022")
		logPrint("i", "https://github.com/tongdysoft/mqtt-test-server")
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
	logInit()
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
	tlsConfig := &tls.Config{}
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
			ClientAuth: tls.RequireAndVerifyClientCert,
		}
		logPrint("C", fmt.Sprintf("%s: %s (%d)", lang("CACERT"), certCA, len(contentC)))
	}
	// 初始化 MQTT 服务器
	logPrint("i", lang("BOOTING")+listen+" ...")
	server := mqtt.NewServer(nil)
	tcp := listeners.NewTCP(listen, listen)
	err := error(nil)
	err = server.AddListener(tcp, &listeners.Config{
		Auth:      new(auth.Allow),
		TLSConfig: tlsConfig,
	})
	if err != nil {
		log.Fatal(err)
	}
	// 启动 MQTT 服务器
	go func() {
		err := server.Serve()
		if err != nil {
			logPrint("E", lang("SERVERFAIL"))
			log.Fatal(err)
		}
	}()
	// server.Events.OnProcessMessage = func(cl events.Client, pk events.Packet) (pkx events.Packet, err error) {
	// 	return pkx, err
	// }
	// 设备连接出错
	server.Events.OnError = func(cl events.Client, err error) {
		logPrint("E", fmt.Sprintf("%s %s: %v", lang("CLIENT"), cl.ID, err))
	}
	// 有设备连接到服务器
	server.Events.OnConnect = func(cl events.Client, pk events.Packet) {
		logFileStr(true, lang("CONNECT"), cl.ID, strings.ReplaceAll(fmt.Sprint(pk), "\n", ""))
		logPrint("L", fmt.Sprintf("%s %s %s: %+v", lang("CLIENT"), cl.ID, lang("CONNECT"), pk))
	}
	// 设备断开连接
	server.Events.OnDisconnect = func(cl events.Client, err error) {
		logFileStr(true, lang("DISCONNECT"), cl.ID, strings.ReplaceAll(fmt.Sprint(err), "\n", ""))
		logPrint("D", fmt.Sprintf("%s %s %s: %v", lang("CLIENT"), cl.ID, lang("DISCONNECT"), err))
	}
	// 收到订阅请求
	server.Events.OnSubscribe = func(filter string, cl events.Client, qos byte) {
		logFileStr(true, lang("SUBSCRIBED"), filter, fmt.Sprintf("QOS%d", qos))
		logPrint("S", fmt.Sprintf("%s %s %s %s, (QOS:%v)", lang("CLIENT"), cl.ID, lang("SUBSCRIBED"), filter, qos))
	}
	// 收到取消订阅请求
	server.Events.OnUnsubscribe = func(filter string, cl events.Client) {
		logFileStr(true, lang("SUBSCRIBED"), filter, "")
		logPrint("U", fmt.Sprintf("%s %s %s %s", lang("CLIENT"), cl.ID, lang("UNSUBSCRIBED"), filter))
	}
	// 收到消息
	server.Events.OnMessage = func(cl events.Client, pk events.Packet) (pkx events.Packet, err error) {
		pkx = pk
		var clID *string = &cl.ID
		if onlyIdE && !in(&onlyIdS, clID) {
			return
		}
		var topic *string = &pkx.TopicName
		if onlyTopicE && !in(&onlyTopicS, topic) {
			return
		}
		var payload string = string(pkx.Payload)
		if onlyPayloadE {
			var inWord bool = false
			for _, word := range onlyPayloadS {
				if strings.Contains(payload, word) {
					inWord = true
					break
				}
			}
			if !inWord {
				return
			}
		}
		logFileStr(false, *clID, *topic, payload)
		logPrint("M", fmt.Sprintf("%s: %s, %s: %s, %s: %s", lang("MESSAGE"), *clID, lang("TOPIC"), *topic, lang("PAYLOAD"), payload))
		return pk, nil
	}
	// 启动完毕
	logPrint("i", lang("BOOTOK"))
	// 处理结束信号
	<-done
	close(done)
	logPrint("X", lang("NEEDSTOP"))
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
	logPrint("i", lang("END"))
	os.Exit(0)
}

func in(strArr *[]string, str *string) bool {
	sort.Strings(*strArr)
	index := sort.SearchStrings(*strArr, *str)
	if index < len(*strArr) && (*strArr)[index] == *str {
		return true
	}
	return false
}
