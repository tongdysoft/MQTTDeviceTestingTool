package main

var langs map[string][]string = map[string][]string{
	"TITLE":           {"MQTT Server test program", "MQTT 服务器测试程序"},
	"BOOTING":         {"Starting MQTT server, listen ", "正在启动 MQTT 服务器，监听 "},
	"START":           {"service start", "服务启动"},
	"SERVERFAIL":      {"Failed to start MQTT server !", "启动 MQTT 服务器失败！"},
	"BOOTOK":          {"MQTT server started.", "MQTT 服务器已启动。"},
	"NEEDSTOP":        {"Received stop request, stopping...", "收到停止请求，正在停止..."},
	"END":             {"Program run ends.", "程序运行结束。"},
	"CLIENT":          {"Client", "客户端"},
	"CONNECT":         {"Connected", "已连接"},
	"DISCONNECT":      {"Disconnected", "断开连接"},
	"SUBSCRIBED":      {"Subscribed", "开始订阅主题"},
	"UNSUBSCRIBED":    {"Unsubscribed", "取消订阅主题"},
	"MESSAGE":         {"Received message, From", "收到消息，发件人"},
	"ONLY":            {"Only allowed", "只允许这些"},
	"TOPIC":           {"topic", "主题"},
	"PAYLOAD":         {"payload", "内容"},
	"WORD":            {"word", "关键词"},
	"LOG":             {"log file", "日志文件"},
	"LOGDATA":         {"data log file", "数据记录文件"},
	"LOGSTAT":         {"status log file", "状态记录文件"},
	"LOGFAIL":         {"Unable to write to", "无法写入"},
	"CACERT":          {"CA certificate", "CA 证书"},
	"SERVERCERT":      {"server certificate", "服务器证书"},
	"SERVERKEY":       {"server key", "服务器私钥"},
	"SERVERKEYPWD":    {"server key password", "服务器私钥的密码"},
	"NOTEMPTY":        {"can not be empty", "不能为空"},
	"FAIL":            {" failed", "失败"},
	"ERROR":           {" error", "错误"},
	"DECRYPT":         {"Decrypt", "解密"},
	"ParsePrivateKey": {"Failed to parse the private key, the password is incorrect? ", "解析私钥失败，密码不正确？ "},
	"READFAIL":        {"Failed to read", "未能读取"},
	"USERDATABASE":    {"user database", "用户数据库"},
	"LOADED":          {"Loaded", "已加载"},
	"USERDB":          {"user data", "用户数据库"},
	"PERMDB":          {"permissions data", "权限数据库"},
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
