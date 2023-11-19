![icon](macOS/mqttclienttesttool/AppIcon.xcassets/AppIcon.appiconset/MQTT%20client%20test%20tool%205.png)

# [MQTT Client Test Tool](https://github.com/tongdysoft/mqtt-test-server)

[ English | [中文](#mqtt-客户端测试工具)]

This tool can help you test the stability of your device's MQTT connection.

- version: `1.5.3`
- golang version: `1.21`

## Function

- Display and log data and behavior of MQTT devices.

## Install

Download the program from [Release](releases). No installation required.

| Release files (Archive)  | OS      | >=ver | BC  | Arch                |
| ------------------------ | ------- | ----- | --- | ------------------- |
| `bin/*_Linux32.zip`      | Linux   | 2.6   | 32  | i386 (x86)          |
| `bin/*_Linux64.zip`      | Linux   | 2.6   | 64  | amd64(x86-64)       |
| `bin/*_macOS64.dmg`      | macOS   | 10.13 | 64  | amd64(x86-64)       |
| `bin/*_macOSARM64.dmg`   | macOS   | 11    | 64  | arm64(AppleSilicon) |
| `bin/*_Windows32.cab`    | Windows | 7     | 32  | i386 (x86)          |
| `bin/*_Windows64.cab`    | Windows | 7     | 64  | amd64(x86-64)       |
| `bin/*_WindowsARM64.cab` | Windows | 10    | 64  | arm64(aarch64)      |

Non-Windows systems need to use `chmod +x <executable file name>` to add permission to run.

### Docker

- You can refer to and modify `Dockerfile` and `docker.sh` files to create Docker images and containers.
- Deployment is relative to the compiled file, run on alpine, does not include compilation of source code.

### Linux systemd

1. Copy `mqtttestclient.service` to `/etc/systemd/system/`
2. Modify the path and start user in `/etc/systemd/system/mqtttestclient.service`
3. `sudo systemctl start mqtttestclient.service`

### Linux desktop shortcuts

1. Copy `mqttclienttesttool.desktop` to `~/Desktop`
2. Modify the execution file and icon path in `~/Desktop/mqttclienttesttool.desktop`

## Usage

Command: mqtt-test-server `< -l .. | -p .. | -u .. | -ca .. | -ce .. | ck .. | cp .. | -c .. | -t .. | -w .. | -d .. | -s .. | -o .. | -ts | -n | -v >`

- `-l string`
  - Language ( `en(default) | cn` )
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

### Use in macOS

Before running this `.app` program directly, make sure that `Terminal.app` is in an exited state.

#### Add startup parameters in macOS system

1. Open the `.dmg` file of the corresponding platform in Release, find the `.app` file inside, and copy it to the `Applications` folder.
2. Right click on the `.app` file and select `Show Package Contents`.
3. Edit the `Contents/Resources/run.sh` script file, and add parameters at the comment position.

The `Contents/Resources/MqttClientTestTool_macOS64` file can be extracted separately and run in the terminal.

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

Copyright (c) 2022 [神楽坂雅詩](https://github.com/KagurazakaYashi)@[Tongdy](https://github.com/tongdysoft) MqttClientTestTool is licensed under Mulan PSL v2. You can use this software according to the terms and conditions of the Mulan PSL v2. You may obtain a copy of Mulan PSL v2 at: <http://license.coscl.org.cn/MulanPSL2> THIS SOFTWARE IS PROVIDED ON AN “AS IS” BASIS, WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT, MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE. See the Mulan PSL v2 for more details.

## Third-party

- logrusorgru/aurora ([The Unlicense](https://github.com/logrusorgru/aurora/blob/master/LICENSE))
- mochi-co/mqtt ([MIT License](https://github.com/mochi-co/mqtt/blob/master/LICENSE.md))
- akavel/rsrc ([MIT License](https://github.com/akavel/rsrc/blob/master/LICENSE.txt))
- gorilla/websocket ([BSD 2-Clause "Simplified" License](https://github.com/gorilla/websocket/blob/master/LICENSE))
- josephspurrier/goversioninfo ([MIT License](https://github.com/josephspurrier/goversioninfo/blob/master/LICENSE))
- rs/xid ([MIT License](https://github.com/rs/xid/blob/master/LICENSE))

# [MQTT 客户端测试工具](https://github.com/tongdysoft/mqtt-test-server)

这个工具可以帮助您测试设备的 MQTT 连接的稳定性。

- 版本: `1.5.3`
- golang 版本: `1.21.5`

## 功能

- 显示和记录 MQTT 设备的数据和行为。

## 安装

从 [Release](releases) 下载相应系统的可执行文件即可，无需安装。

| Release 文件（压缩包）   | 系统    | 最低版 | 位  | 体系结构            |
| ------------------------ | ------- | ------ | --- | ------------------- |
| `bin/*_Linux32.zip`      | Linux   | 2.6    | 32  | i386 (x86)          |
| `bin/*_Linux64.zip`      | Linux   | 2.6    | 64  | amd64(x86-64)       |
| `bin/*_macOS64.dmg`      | macOS   | 10.13  | 64  | amd64(x86-64)       |
| `bin/*_macOSARM64.dmg`   | macOS   | 11     | 64  | arm64(AppleSilicon) |
| `bin/*_Windows32.cab`    | Windows | 7      | 32  | i386 (x86)          |
| `bin/*_Windows64.cab`    | Windows | 7      | 64  | amd64(x86-64)       |
| `bin/*_WindowsARM64.cab` | Windows | 10     | 64  | arm64(aarch64)      |

非 Windows 系统需要使用 `chmod +x <解压缩后的可执行文件名>` 来添加运行权限。

### Docker 部署

- 可以参考和修改 `Dockerfile` 和 `docker.sh` 文件，创建 Docker 镜像和容器。
- 部署是相对于编译完成后的文件，在 alpine 上运行，不包含源代码的编译。

### Linux 系统服务 (systemd)

1. 复制 `mqtttestclient.service` 到 `/etc/systemd/system/`
2. 修改 `/etc/systemd/system/mqtttestclient.service` 中的路径和启动用户等
3. `sudo systemctl start mqtttestclient.service`

### Linux 桌面快捷方式

1. 复制 `mqttclienttesttool.desktop` 到 `~/桌面`
2. 修改 `~/桌面/mqttclienttesttool.desktop` 中的执行文件和图标路径

## 使用说明

命令行参数: mqtt-test-server `< -l .. | -p .. | -u .. | -ca .. | -ce .. | ck .. | cp .. | -c .. | -t .. | -w .. | -d .. | -s .. | -o .. | -ts | -n | -v >`

- `-l 字符串`
  - 语言 ( `en(英语,默认) | cn(简体中文)` )
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

### 在 macOS 中使用

在直接运行该 `.app` 程序之前，请确保 `终端.app` 处于退出状态。

#### 添加启动参数

在直接运行该 `.app` 程序之前，请确保 `终端.app` 处于退出状态。

1. 打开 Release 中的相应平台的 `.dmg` 文件，找到里面的 `.app` 文件，将其复制到 `应用程序` 文件夹.
2. 右键点击改 `.app` 文件，选择 `显示包内容` 。
3. 编辑 `Contents/Resources/run.sh` 脚本文件，在里面注释位置处添加参数。

可以将 `Contents/Resources/MqttClientTestTool_macOS64` 文件单独提取出来在终端中运行。

### Windows 系统中使用中文交互模式

可以将 `InteractiveModeCHS.bat` 和 exe 放在一起，双击启动中文交互模式，无需关心命令行参数书写。

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
