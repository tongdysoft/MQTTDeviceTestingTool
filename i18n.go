package main

var langs map[string][]string = map[string][]string{
	"TITLE":           {"MQTT Device Testing Tool", "MQTT 客户端测试工具"},
	"HELP":            {"For help, visit README.md . Web:", "如需帮助，请参阅 README.md 。在线指南:"},
	"BOOTING":         {"Starting MQTT server...", "正在启动 MQTT 服务器..."},
	"START":           {"service start", "服务启动"},
	"SERVERFAIL":      {"Failed to start MQTT server !", "启动 MQTT 服务器失败！"},
	"BOOTOK":          {"MQTT server started.", "MQTT 服务器已启动。"},
	"NEEDSTOP":        {"Received stop request, stopping...", "收到停止请求，正在停止..."},
	"END":             {"Program run ends.", "程序运行结束。"},
	"MODE":            {"Mode", "模式"},
	"MODE_PLAIN":      {"plain (no TLS)", "普通 MQTT（无 TLS）"},
	"MODE_TLS":        {"tls (server certificate only)", "单向 TLS（仅服务器证书）"},
	"MODE_MTLS":       {"mtls (device certificate required and verified)", "强制 mTLS（要求并验证设备证书）"},
	"LISTENADDR":      {"Listening address", "监听地址"},
	"TLSVERSIONS":     {"TLS versions", "TLS 版本"},
	"TLSDISABLED":     {"disabled", "未启用"},
	"CLIENTAUTH":      {"Device certificate", "设备证书"},
	"CLIENTAUTH_NA":   {"not applicable", "不适用"},
	"CLIENTAUTH_NONE": {"not requested or verified", "不请求、不验证"},
	"CLIENTAUTH_REQUIRED": {
		"required and verified (RequireAndVerifyClientCert)",
		"强制要求并验证（RequireAndVerifyClientCert）",
	},
	"INVALIDCERTWARN": {
		"WARNING: -allow-invalid-server-cert is enabled; only semantic certificate checks are relaxed",
		"警告：已启用 -allow-invalid-server-cert，仅放宽服务器证书的语义检查",
	},
	"CERTCHAIN":  {"Certificate chain", "证书链"},
	"CLIENTCAS":  {"Client trust anchors", "设备证书信任锚数量"},
	"CERTWARN":   {"Certificate note: ", "证书提示："},
	"CERTBYPASS": {"Allowed invalid server certificate: ", "已放行的服务器证书问题："},
	"KEYPWDSOURCE": {
		"Key password source",
		"私钥密码来源",
	},
	"STARTFAIL":    {"Failed to start: ", "启动失败："},
	"EXPORTFAIL":   {"Failed to write result files", "结果文件写入失败"},
	"RUNID":        {"Run ID", "运行编号"},
	"RESULTS":      {"Results directory", "结果目录"},
	"CLIENT":       {"Client", "客户端"},
	"CONNECT":      {"Connected", "已连接"},
	"DISCONNECT":   {"Disconnected", "断开连接"},
	"SUBSCRIBED":   {"Subscribed", "开始订阅主题"},
	"UNSUBSCRIBED": {"Unsubscribed", "取消订阅主题"},
	"MESSAGE":      {"Received message, From", "收到消息，发件人"},
	"RECORDONLY":   {"Only record", "仅记录"},
	"RECORDONLYNOTE": {
		"recording filter only; connections and permissions are controlled by -users auth/acl",
		"仅影响记录，不阻止连接或收发；接入与权限由 -users 的 auth/acl 控制",
	},
	"HANDSHAKETIMEOUT": {"TLS handshake timeout", "TLS 握手超时"},
	"CONNECTTIMEOUT":   {"MQTT CONNECT wait timeout", "MQTT CONNECT 等待超时"},
	"MAXHANDSHAKES":    {"Max concurrent handshakes", "最大并发握手数"},
	"SHUTDOWNTIMEOUT":  {"Shutdown timeout", "关闭等待超时"},
	"DEBUGHTTP":        {"pprof HTTP listening", "pprof HTTP 监听"},
	"DEBUGHTTPFAIL":    {"pprof HTTP server stopped", "pprof HTTP 服务停止"},
	"DEBUGHTTPPUBLIC": {
		"WARNING: pprof HTTP is bound to a non-loopback address",
		"警告：pprof HTTP 绑定在非回环地址",
	},
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
	"VERIFYCERT":      {"Client certificate verification mode", "客户端证书验证模式"},
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
	case "chs":
		return langs[title][1]
	}
	return langs[title][0]
}
