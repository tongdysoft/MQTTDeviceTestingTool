# MQTT client test tool
This tool can help you test the stability of your device's MQTT connection.

- version: `1.0.0`
- golang version: `1.18.2`

## Install
Download the program from Release. No installation required.

## Usage

Command: mqtt-test-server `< -l .. | -p .. | -c .. | -t .. | -w .. | -m .. | -s .. | -o .. >`

- `-l string`
  - Language ( `en(default) | cn` )
- `-p string`
  - Define listening on IP:Port (default: `127.0.0.1:1883` )
  - To allow all IP addresses: `:1883`
- `-c string`
  - Only allow these client IDs ( `,` separated)
- `-t string`
  - Only allow these topics ( `,` separated)
- `-w string`
  - Only allow these words in message content ( `,` separated)
- `-m path-string`
  - Log message to csv file
- `-s path-string`
  - Log state changes to a csv file
- `-o path-string`
  - Save log to txt/log file
- `-n`
  - Use a monochrome color scheme

## Build

```
go get .     # Need internet
go generate  # Windows only
go build .
```

Build all platforms under Windows: `build.bat`

## Screenshot

![Screenshot](screenshot-en.png)

# MQTT 客户端测试工具
这个工具可以帮助您测试设备的 MQTT 连接的稳定性。

- 版本: `1.0.0`
- golang 版本: `1.18.2`

## 安装
从 Release 下载相应系统的可执行文件即可，无需安装。

## 使用说明

命令行参数: mqtt-test-server `< -l .. | -p .. | -c .. | -t .. | -w .. | -m .. | -s .. | -o .. >`

- `-l 字符串`
  - 语言 ( `en(英语,默认) | cn(简体中文)` )
- `-p string`
  - 指定要监听的地址和端口 (默认值: `127.0.0.1:1883` )
  - 如需允许所有 IP 地址： `:1883`
- `-c 字符串`
  - 只允许客户端 ID 为这些的客户端（使用 `,` 分隔）
- `-t 字符串`
  - 只接收这些主题的信息（使用 `,` 分隔）
- `-w 字符串`
  - 只有消息内容包含这些关键词才会处理（使用 `,` 分隔）
- `-d 文件路径字符串`
  - 将收取到的消息保存到某个 .csv 文件
- `-s 文件路径字符串`
  - 将客户端的连接、断开、订阅、退订等行为保存到某个 .csv 文件
- `-o 文件路径字符串`
  - 将日志输出保存到某个 .txt / .log 文件
- `-n`
  - 使用单色模式输出，避免某些不支持彩色的终端输出乱码

## 编译

```
go get .     # 需要有互联网连接
go generate  # 只有 Windows 需要执行这条
go build .
```

在 Windows 环境下编译所有平台版本: `build.bat`

## 截图

![截图](screenshot-cn.png)
