![icon](ico/icon.ico)

# [MQTT Device Testing Tool](https://github.com/tongdysoft/mqtt-test-server)

[ English | [中文](#mqtt-客户端测试工具)]

This tool can help you test the stability of your device's MQTT connection.

- version: `1.5.5`
- golang version: `1.21`

## Function

- Display and log data and behavior of MQTT devices.

## Install

Download the program from [Release](releases). No installation required.

Non-Windows systems need to use `chmod +x <executable file name>` to add permission to run.

### Docker

- You can refer to and modify `Dockerfile` and `docker.sh` files to create Docker images and containers.
- Deployment is relative to the compiled file, run on alpine, does not include compilation of source code.

### Linux systemd

1. Copy `MQTTDeviceTestingTool.service` to `/etc/systemd/system/`
2. Modify the path and start user in `/etc/systemd/system/MQTTDeviceTestingTool.service`
3. `sudo systemctl start MQTTDeviceTestingTool.service`

### Linux desktop shortcuts

1. Copy `MQTTDeviceTestingTool.desktop` to `~/Desktop`
2. Modify the execution file and icon path in `~/Desktop/MQTTDeviceTestingTool.desktop`

## Usage

Command: mqtt-test-server `< -l .. | -p .. | -u .. | -ca .. | -ce .. | ck .. | cp .. | cv .. | -c .. | -t .. | -w .. | -d .. | -s .. | -o .. | -ts | -n | -v >`

- `-l string`
  - Language ( `en(default) | chs` )
- `-p string`
  - Define listening on IP:Port (default: `0.0.0.0:1883` )
  - To allow all IP addresses: `:1883`
- `-u path-string`
  - [Users and permissions file](#examples-of-user-and-rights-profiles) (.yaml/.json) path
- `-ca path-string`
  - CA certificate file path
- `-ce path-string`
  - Server certificate file path
- `-ck path-string`
  - Server key file path
- `-cp string`
  - Server key file password
- `-cv number (0-4)`
  - Policy the server will follow for TLS Client Authentication:
    0. NoClientCert: Indicates that no client certificate should be requested during the handshake, and if any certificates are sent they will not be verified.
    1. RequestClientCert: Indicates that a client certificate should be requested during the handshake, but does not require that the client send any certificates.
    2. RequireAnyClientCert: Indicates that a client certificate should be requested during the handshake, and that at least one certificate is required to be sent by the client, but that certificate is not required to be valid.
    3. VerifyClientCertIfGiven: Indicates that a client certificate should be requested during the handshake, but does not require that the client sends a certificate. If the client does send a certificate it is required to be valid.
    4. RequireAndVerifyClientCert: Indicates that a client certificate should be requested during the handshake, and that at least one valid certificate is required to be sent by the client.
  - default value:
    - If no server certificate is given, the default value is `0`
    - If at least a CA certificate is given, the default value is `4`
    - If a server certificate is given, the default value is `3`
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
- `-ts`
  - Use timestamps in logged files (instead of the time string)
- `-n`
  - Use a monochrome color scheme (When an abnormal character appears in Windows cmd.exe)
- `-v`
  - Print version info

### Examples of User and Rights Profiles

```yaml
auth:
    - username: username1
      password: password1
      allow: true
    - username: username2
      password: password2
      allow: false
    - remote: 127.0.0.1:*
      allow: true
    - remote: localhost:*
      allow: true
acl:
# 0 = deny, 1 = read only, 2 = write only, 3 = read and write
    - remote: 127.0.0.1:*
    - username: 用户名1
      filters:
        aaa/#: 3  # Topic:Permission
        bbb/#: 2
    - filters:
        '#': 1
        bbb/#: 0
```

Default configuration (all allowed):

```yaml
auth:
    - allow: true
acl:
    - filters:
        '#': 3
```

## Build

```sh
go get .     # Need internet
go generate  # Windows only
go build .
```

Build all platforms under Windows: `build.bat`

## Screenshot

![Screenshot](screenshot-en.png)

## LICENSE

Copyright (c) 2022 [神楽坂雅詩](https://github.com/KagurazakaYashi)@[Tongdy](https://github.com/tongdysoft) MQTTDeviceTestingTool is licensed under Mulan PSL v2. You can use this software according to the terms and conditions of the Mulan PSL v2. You may obtain a copy of Mulan PSL v2 at: <http://license.coscl.org.cn/MulanPSL2> THIS SOFTWARE IS PROVIDED ON AN “AS IS” BASIS, WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT, MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE. See the Mulan PSL v2 for more details.

## Third-party

- logrusorgru/aurora ([The Unlicense](https://github.com/logrusorgru/aurora/blob/master/LICENSE))
- mochi-co/mqtt ([MIT License](https://github.com/mochi-co/mqtt/blob/master/LICENSE.md))
- akavel/rsrc ([MIT License](https://github.com/akavel/rsrc/blob/master/LICENSE.txt))
- gorilla/websocket ([BSD 2-Clause "Simplified" License](https://github.com/gorilla/websocket/blob/master/LICENSE))
- josephspurrier/goversioninfo ([MIT License](https://github.com/josephspurrier/goversioninfo/blob/master/LICENSE))
- rs/xid ([MIT License](https://github.com/rs/xid/blob/master/LICENSE))

# [MQTT 客户端测试工具](https://github.com/tongdysoft/mqtt-test-server)

这个工具可以帮助您测试设备的 MQTT 连接的稳定性。

- 版本: `1.5.5`
- golang 版本: `1.22.3`

## 功能

- 显示和记录 MQTT 设备的数据和行为。

## 安装

从 [Release](releases) 下载相应系统的可执行文件即可，无需安装。

非 Windows 系统需要使用 `chmod +x <解压缩后的可执行文件名>` 来添加运行权限。

### Docker 部署

- 可以参考和修改 `Dockerfile` 和 `docker.sh` 文件，创建 Docker 镜像和容器。
- 部署是相对于编译完成后的文件，在 alpine 上运行，不包含源代码的编译。

### Linux 系统服务 (systemd)

1. 复制 `MQTTDeviceTestingTool.service` 到 `/etc/systemd/system/`
2. 修改 `/etc/systemd/system/MQTTDeviceTestingTool.service` 中的路径和启动用户等
3. `sudo systemctl start MQTTDeviceTestingTool.service`

### Linux 桌面快捷方式

1. 复制 `MQTTDeviceTestingTool.desktop` 到 `~/桌面`
2. 修改 `~/桌面/MQTTDeviceTestingTool.desktop` 中的执行文件和图标路径

## 使用说明

命令行参数: mqtt-test-server `< -l .. | -p .. | -u .. | -ca .. | -ce .. | ck .. | cp .. | -cv .. | -c .. | -t .. | -w .. | -d .. | -s .. | -o .. | -ts | -n | -v >`

- `-l 字符串`
  - 语言 ( `en(英语,默认) | chs(简体中文)` )
- `-p 字符串`
  - 指定要监听的地址和端口 (默认值: `0.0.0.0:1883` )
  - 如需允许所有 IP 地址： `:1883`
- `-u 文件路径字符串`
  - [用户和主题权限配置文件](#用户和主题权限配置文件示例) (.yaml/.json) 路径
- `-ca 文件路径字符串`
  - CA 证书文件路径
- `-ce 文件路径字符串`
  - 服务器证书文件路径
- `-ck 文件路径字符串`
  - 服务器私钥文件路径
- `-cp 字符串`
  - 服务器私钥文件的密码
- `-cv 数字 (0-4)`
  - TLS 客户端身份验证策略:
    0. 无客户端证书: 表示握手期间不应请求客户端证书，如果发送任何证书，则不会对其进行验证。
    1. 请求客户端证书: 表示在握手期间应请求客户端证书，但不要求客户端发送任何证书。
    2. 需要任何客户端证书: 表示握手期间应请求客户端证书，并且客户端至少需要发送一个证书，但不要求该证书有效。
    3. 验证客户端证书是否已给出: 表示握手期间应请求客户端证书，但不要求客户端发送证书。如果客户端确实发送了证书，则该证书必须有效。
    4. 需要并验证客户端证书: 表示握手时需要请求客户端证书，并且客户端至少需要发送一个有效的证书。
  - 默认值:
    - 如果没有给予服务器证书，那么默认值是 `0`
    - 如果至少给予了CA证书，那么默认值是 `4`
    - 如果给予了服务器证书，那么默认值是 `3`
- `-c 字符串`
  - 只允许客户端 ID 为这些的客户端（使用 `,` 分隔）
- `-t 字符串`
  - 只接收这些主题的信息（使用 `,` 分隔）
- `-w 字符串`
  - 只有消息内容包含这些关键词才会处理（使用 `,` 分隔）
- `-m 文件路径字符串`
  - 将收取到的消息保存到某个 .csv 文件
- `-s 文件路径字符串`
  - 将客户端的连接、断开、订阅、退订等行为保存到某个 .csv 文件
- `-o 文件路径字符串`
  - 将日志输出保存到某个 .txt / .log 文件
- `-ts`
  - 在记录的文件中使用时间戳而不是时间
- `-n`
  - 使用单色模式输出，避免某些不支持彩色的终端输出乱码（例如 Windows 的 cmd.exe）
- `-v`
  - 显示版本号等信息并退出

### 用户和主题权限配置文件示例

```yaml
auth:
    - username: 用户名1
      password: 密码1
      allow: true
    - username: 用户名2
      password: 密码2
      allow: false
    - remote: 127.0.0.1:*
      allow: true
    - remote: localhost:*
      allow: true
acl:
    # 0 = 封禁, 1 = 只读, 2 = 只写, 3 = 读写
    - remote: 127.0.0.1:*
    - username: 用户名1
      filters:
        aaa/#: 3  # 主题名称:权限 , # 是通配符
        bbb/#: 2
    - filters:
        '#': 1
        bbb/#: 0
```

默认配置（全部允许）：

```yaml
auth:
    - allow: true
acl:
    - filters:
        '#': 3
```

## 编译

```sh
go get .     # 需要有互联网连接
go generate  # 只有 Windows 需要执行这条
go build .
```

### 跨平台编译

在 Windows x64 中也可以通过批处理一键生成全平台二进制文件：

```bat
build.bat
```

批处理脚本最后会调用 `MAKECAB` 和 `7z` 命令进行压缩。

## 截图

![截图](screenshot-cn.png)

## 许可

Copyright (c) 2022 [神楽坂雅詩](https://github.com/kagurazakayashi)@[Tongdy](https://github.com/tongdysoft) MQTT 客户端测试工具。
您对“软件”的复制、使用、修改及分发受木兰宽松许可证，第 2 版的条款的约束。
