package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/mochi-co/mqtt/server"
	"github.com/mochi-co/mqtt/server/events"
	"github.com/mochi-co/mqtt/server/listeners"
	"github.com/mochi-co/mqtt/server/listeners/auth"
)

var language string

func main() {
	// 初始化启动参数
	flag.StringVar(&language, "l", "en", "Language")

	flag.Parse()
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	// 监听结束信号
	logPrint("i", lang("TITLE")+" v0.0.1")
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()
	// 初始化 MQTT 服务器
	logPrint("i", lang("BOOTING"))
	server := mqtt.NewServer(nil)
	tcp := listeners.NewTCP("t1", ":1883")
	err := server.AddListener(tcp, &listeners.Config{
		Auth: new(auth.Allow),
	})
	if err != nil {
		log.Fatal(err)
	}
	// 启动 MQTT 服务器
	go func() {
		err := server.Serve()
		if err != nil {
			logPrint("X", lang("SERVERFAIL"))
			log.Fatal(err)
		}
	}()
	// 有设备连接到服务器
	server.Events.OnConnect = func(cl events.Client, pk events.Packet) {
		logPrint("+", fmt.Sprintf("%s %s %s: %+v", lang("CLIENT"), cl.ID, lang("CONNECT"), pk))
	}
	// 设备断开连接
	server.Events.OnDisconnect = func(cl events.Client, err error) {
		logPrint("-", fmt.Sprintf("%s %s %s: %v", lang("CLIENT"), cl.ID, lang("DISCONNECT"), err))
	}
	// 收到订阅请求
	server.Events.OnSubscribe = func(filter string, cl events.Client, qos byte) {
		logPrint("+", fmt.Sprintf("%s %s %s %s, (QOS:%v)", lang("CLIENT"), cl.ID, lang("SUBSCRIBED"), filter, qos))
	}
	// 收到消息
	server.Events.OnMessage = func(cl events.Client, pk events.Packet) (pkx events.Packet, err error) {
		pkx = pk
		logPrint("i", fmt.Sprintf("%s: %s, %s: %s, %s: %s", lang("MESSAGE"), cl.ID, lang("TOPIC"), string(pkx.TopicName), lang("PAYLOAD"), string(pkx.Payload)))
		return pk, nil
	}
	// 收到取消订阅请求
	server.Events.OnUnsubscribe = func(filter string, cl events.Client) {
		logPrint("-", fmt.Sprintf("%s %s %s %s)", lang("CLIENT"), cl.ID, lang("UNSUBSCRIBED"), filter))
	}
	// 启动完毕
	logPrint("i", lang("BOOTOK"))
	// 处理结束信号
	<-done
	logPrint("X", lang("NEEDSTOP"))
	server.Close()
	logPrint("i", lang("END"))
}

func logPrint(iconChar string, text string) {
	var currentTime time.Time = time.Now()
	var timeStr string = currentTime.Format("2006-01-02 15:04:05")
	fmt.Printf("[%s][%s] %s\n", iconChar, timeStr, text)
}
