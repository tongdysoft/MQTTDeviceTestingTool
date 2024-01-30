package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

type MQTTHookOptions struct {
	Server *mqtt.Server
}
type MQTTHook struct {
	mqtt.HookBase
	config *MQTTHookOptions
}

func (h *MQTTHook) ID() string {
	return "events"
}

func (h *MQTTHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnConnect,
		mqtt.OnDisconnect,
		mqtt.OnSubscribed,
		mqtt.OnUnsubscribed,
		mqtt.OnPublished,
		mqtt.OnPublish,
	}, []byte{b})
}

func (h *MQTTHook) Init(config any) error {
	h.Log.Info("initialised")
	if _, ok := config.(*MQTTHookOptions); !ok && config != nil {
		return mqtt.ErrInvalidConfigType
	}

	h.config = config.(*MQTTHookOptions)
	if h.config.Server == nil {
		return mqtt.ErrInvalidConfigType
	}
	// h.config.Server.Subscribe("#", 1, h.subscribeCallback)
	return nil
}

// subscribe 回调处理订阅主题的消息
// func (h *MQTTHook) subscribeCallback(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {}

// 有设备连接到服务器
func (h *MQTTHook) OnConnect(cl *mqtt.Client, pk packets.Packet) error {
	pkJsonB, err := json.Marshal(pk)
	if err != nil {
		pkJsonB = []byte("")
	}
	clJsonB, err := json.Marshal(cl)
	if err != nil {
		clJsonB = []byte("")
	}
	var infoJson string = fmt.Sprintf("{\"Client\":%s,\"Packet\":%s}", string(clJsonB), string(pkJsonB))
	logFileStr(true, strCL(cl), lang("CONNECT"), strings.ReplaceAll(infoJson, "\"", "'"))
	logPrint("L", infoJson, strCL(cl), lang("CONNECT"))
	return nil
}

// 设备断开连接
func (h *MQTTHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
	logFileStr(true, strCL(cl), lang("DISCONNECT"), strings.ReplaceAll(fmt.Sprint(err), "\n", " "))
	logPrint("D", fmt.Sprintf("%v", err), strCL(cl), lang("DISCONNECT"))
}

// 收到订阅请求
func (h *MQTTHook) OnSubscribed(cl *mqtt.Client, pk packets.Packet, reasonCodes []byte) {
	logFileStr(true, strCL(cl), lang("SUBSCRIBED"), fmt.Sprintf("%s (QOS%d)", pkFilters(pk.Filters), pk.FixedHeader.Qos))
	logPrint("S", fmt.Sprintf("%s (QOS:%v)", lang("SUBSCRIBED"), pk.FixedHeader.Qos), strCL(cl), pkFilters(pk.Filters))
}

// 收到取消订阅请求
func (h *MQTTHook) OnUnsubscribed(cl *mqtt.Client, pk packets.Packet) {
	logFileStr(true, strCL(cl), lang("UNSUBSCRIBED"), pkFilters(pk.Filters))
	logPrint("U", lang("UNSUBSCRIBED"), strCL(cl), pkFilters(pk.Filters))
}

// 客户端发送消息时
func (h *MQTTHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	var clID *string = &cl.ID
	if onlyIdE && !in(&onlyIdS, clID) {
		return pk, nil
	}
	var topic *string = &pk.TopicName
	if onlyTopicE && !in(&onlyTopicS, topic) {
		return pk, nil
	}
	var payload string = string(pk.Payload)
	if onlyPayloadE {
		var inWord bool = false
		for _, word := range onlyPayloadS {
			if strings.Contains(payload, word) {
				inWord = true
				break
			}
		}
		if !inWord {
			return pk, nil
		}
	}
	logFileStr(false, strCL(cl), *topic, payload)
	logPrint("M", payload, strCL(cl), *topic)
	return pk, nil
}
