// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2022 mochi-mqtt, mochi-co
// SPDX-FileContributor: mochi-co

package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

func main() {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println(sigs)
		done <- true
	}()

	// You can also run from top-level server.go folder:
	// go run examples/auth/encoded/main.go --path=examples/auth/encoded/auth.yaml
	// go run examples/auth/encoded/main.go --path=examples/auth/encoded/auth.json
	path := flag.String("path", "auth.yaml", "path to data auth file")
	flag.Parse()

	// Get ledger from yaml file
	data, err := os.ReadFile(*path)
	if err != nil {
		log.Fatal(err)
	}

	server := mqtt.New(&mqtt.Options{
		InlineClient: true,
	})
	err = server.AddHook(new(auth.Hook), &auth.Options{
		Data: data, // build ledger from byte slice, yaml or json
	})
	if err != nil {
		log.Fatal(err)
	}

	tcp := listeners.NewTCP("127.0.0.1", ":1883", nil)
	err = server.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}
	// Add custom hook (ExampleHook) to the server
	err = server.AddHook(new(ExampleHook), &ExampleHookOptions{
		Server: server,
	})
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		err := server.Serve()
		if err != nil {
			log.Fatal(err)
		}
	}()
	// // Demonstration of directly publishing messages to a topic via the
	// // `server.Publish` method. Subscribe to `direct/publish` using your
	// // MQTT client to see the messages.
	// go func() {
	// 	cl := server.NewClient(nil, "local", "inline", true)
	// 	for range time.Tick(time.Second * 1) {
	// 		err := server.InjectPacket(cl, packets.Packet{
	// 			FixedHeader: packets.FixedHeader{
	// 				Type: packets.Publish,
	// 			},
	// 			TopicName: "direct/publish",
	// 			Payload:   []byte("injected scheduled message"),
	// 		})
	// 		if err != nil {
	// 			server.Log.Error("server.InjectPacket", "error", err)
	// 		}
	// 		server.Log.Info("main.go injected packet to direct/publish")
	// 	}
	// }()
	// // There is also a shorthand convenience function, Publish, for easily sending
	// // publish packets if you are not concerned with creating your own packets.
	// go func() {
	// 	for range time.Tick(time.Second * 5) {
	// 		err := server.Publish("direct/publish", []byte("packet scheduled message"), false, 0)
	// 		if err != nil {
	// 			server.Log.Error("server.Publish", "error", err)
	// 		}
	// 		server.Log.Info("main.go issued direct message to direct/publish")
	// 	}
	// }()

	<-done
	server.Log.Warn("caught signal, stopping...")
	_ = server.Close()
	server.Log.Info("main.go finished")
}

// Options contains configuration settings for the hook.
type ExampleHookOptions struct {
	Server *mqtt.Server
}
type ExampleHook struct {
	mqtt.HookBase
	config *ExampleHookOptions
}

func (h *ExampleHook) ID() string {
	return "events-example"
}

func (h *ExampleHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnConnect,
		mqtt.OnDisconnect,
		mqtt.OnSubscribed,
		mqtt.OnUnsubscribed,
		mqtt.OnPublished,
		mqtt.OnPublish,
	}, []byte{b})
}

func (h *ExampleHook) Init(config any) error {
	h.Log.Info("initialised")
	if _, ok := config.(*ExampleHookOptions); !ok && config != nil {
		return mqtt.ErrInvalidConfigType
	}

	h.config = config.(*ExampleHookOptions)
	if h.config.Server == nil {
		return mqtt.ErrInvalidConfigType
	}
	return nil
}

// subscribeCallback handles messages for subscribed topics
func (h *ExampleHook) subscribeCallback(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
	h.Log.Info("hook subscribed message", "client", cl.ID, "topic", pk.TopicName)
}

func (h *ExampleHook) OnConnect(cl *mqtt.Client, pk packets.Packet) error {
	h.Log.Info("client connected", "client", cl.ID)
	fmt.Println(">>>>>>>>>> 连接 <<<<<<<<<<")
	// Example demonstrating how to subscribe to a topic within the hook.
	h.config.Server.Subscribe("/device/sensor", 1, h.subscribeCallback)

	// Example demonstrating how to publish a message within the hook
	err := h.config.Server.Publish("/device/sensor", []byte("packet hook message"), false, 0)
	if err != nil {
		h.Log.Error("hook.publish", "error", err)
	}
	return nil
}

func (h *ExampleHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
	fmt.Println(">>>>>>>>>> 断开 <<<<<<<<<<")
	if err != nil {
		h.Log.Info("client disconnected", "client", cl.ID, "expire", expire, "error", err)
	} else {
		h.Log.Info("client disconnected", "client", cl.ID, "expire", expire)
	}

}

func (h *ExampleHook) OnSubscribed(cl *mqtt.Client, pk packets.Packet, reasonCodes []byte) {
	h.Log.Info(fmt.Sprintf("subscribed qos=%v", reasonCodes), "client", cl.ID, "filters", pk.Filters)
	fmt.Println(">>>>>>>>>> 订阅 <<<<<<<<<<", cl.ID, ">>", pk.Filters)
}

func (h *ExampleHook) OnUnsubscribed(cl *mqtt.Client, pk packets.Packet) {
	h.Log.Info("unsubscribed", "client", cl.ID, "filters", pk.Filters)
	fmt.Println(">>>>>>>>>> 取消订阅 <<<<<<<<<<")
}

func (h *ExampleHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	h.Log.Info("received from client", "client", cl.ID, "payload", string(pk.Payload))
	fmt.Println(">>>>>>>>>> 发布 <<<<<<<<<<", cl.ID, ">>", string(pk.Payload))
	pkx := pk
	if cl.ID == "mqttx_36a55639" {
		pkx.Payload = []byte("hello world")
		h.Log.Info("received modified packet from client", "client", cl.ID, "payload", string(pkx.Payload))
	}

	return pkx, nil
}

func (h *ExampleHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	h.Log.Info("published to client", "client", cl.ID, "payload", string(pk.Payload))
	fmt.Println(">>>>>>>>>> 发布成功 <<<<<<<<<<")
}
