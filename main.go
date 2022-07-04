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
	"github.com/mochi-co/mqtt/server/listeners"
	"github.com/mochi-co/mqtt/server/listeners/auth"
)

var language string

func main() {
	flag.StringVar(&language, "l", "en", "Language")
	flag.Parse()
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	logPrint("i", lang("TITLE")+" v0.0.1")
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()
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

	logPrint("i", lang("BOOTOK"))

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
