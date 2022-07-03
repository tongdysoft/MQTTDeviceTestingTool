package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/mochi-co/mqtt/server"
	"github.com/mochi-co/mqtt/server/listeners"
	"github.com/mochi-co/mqtt/server/listeners/auth"
)

func main() {
	logPrint("i", "MQTT 服务器测试程序 v0.0.1")
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()
	logPrint("i", "正在启动 MQTT 服务器... (端口 TCP/1883 )")
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
			logPrint("X", "启动 MQTT 服务器失败：")
			log.Fatal(err)
		}
	}()

	logPrint("i", "MQTT 服务器已启动。")

	<-done
	logPrint("X", "收到停止请求，正在停止...")
	server.Close()
	logPrint("i", "运行结束。")
}

func logPrint(iconChar string, text string) {
	var currentTime time.Time = time.Now()
	var timeStr string = currentTime.Format("2006-01-02 15:04:05")
	fmt.Printf("[%s][%s] %s\n", iconChar, timeStr, text)
}
