package main

var langs map[string][]string = map[string][]string{
	"TITLE":        {"MQTT Server test program", "MQTT 服务器测试程序"},
	"BOOTING":      {"Starting MQTT server... ( port TCP/1883 )", "正在启动 MQTT 服务器... (端口 TCP/1883 )"},
	"SERVERFAIL":   {"Failed to start MQTT server !", "启动 MQTT 服务器失败！"},
	"BOOTOK":       {"MQTT server started.", "MQTT 服务器已启动。"},
	"NEEDSTOP":     {"Received stop request, stopping...", "收到停止请求，正在停止..."},
	"END":          {"Program run ends.", "程序运行结束。"},
	"CLIENT":       {"Client", "客户端"},
	"CONNECT":      {"Connected", "已连接"},
	"DISCONNECT":   {"Disconnected", "断开连接"},
	"SUBSCRIBED":   {"Subscribed", "开始订阅主题"},
	"UNSUBSCRIBED": {"Unsubscribed", "取消订阅主题"},
	"MESSAGE":      {"Received message, From", "收到消息，发件人"},
	"ONLY":         {"Only allowed", "只允许这些"},
	"TOPIC":        {"Topic", "主题"},
	"PAYLOAD":      {"Payload", "内容"},
	"WORD":         {"Word", "关键词"},
}

func lang(title string) string {
	var nowLang []string = langs[title]
	if len(nowLang) == 0 {
		return "?" + title + "?"
	}
	switch language {
	case "cn":
		return langs[title][1]
	}
	return langs[title][0]
}
