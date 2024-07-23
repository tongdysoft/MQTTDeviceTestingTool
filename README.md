![icon](ico/icon.ico)

# [MQTT Device Testing Tool](https://github.com/tongdysoft/mqtt-test-server)

[ English | [中文](#mqtt-客户端测试工具)]

This tool can help you test the stability of your device's MQTT connection.

- version: `1.5.5`
- golang version: `1.22.3`

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

Command: `mqtt-test-server -mode plain|tls|mtls [options]`

Security modes:

- `plain` (default): ordinary MQTT, no TLS. Any TLS material parameter is rejected instead of being silently ignored.
- `tls`: one-way TLS. Requires `-server-cert` and `-server-key`; the server does not request a device certificate.
- `mtls`: mutual TLS. Requires `-server-cert`, `-server-key` and `-client-ca`, and always uses `tls.RequireAndVerifyClientCert`.

TLS options:

- `-server-cert path-string`
  - Server certificate chain in PEM format (leaf first, then optional intermediates).
- `-server-key path-string`
  - Server private key in PEM format. Unencrypted PKCS#1 (`RSA PRIVATE KEY`), SEC1 (`EC PRIVATE KEY`) and PKCS#8 (`PRIVATE KEY`, RSA/ECDSA/Ed25519) keys are supported. Legacy RFC 1423 encrypted PEM keys and encrypted PKCS#8 (`ENCRYPTED PRIVATE KEY`, PBES2 with PBKDF2/scrypt and AES/3DES) are also supported. OpenSSH, DSA, raw Ed25519 (non-PKCS#8) and multiple key blocks are rejected explicitly.
- `-server-key-password-file path-string`
  - File containing the password for an encrypted key. The trailing newline is stripped. The file should be readable only by the account running the tool (mode `0600`); wider permissions produce a warning. Alternatively set `MQTT_SERVER_KEY_PASSWORD`, or let the tool prompt on an interactive terminal. A plaintext password command-line option is intentionally not provided.
  - If the private key is encrypted and no password file, environment variable or interactive terminal is available, startup fails. Supplying a password source for an unencrypted key is an error; the password is never ignored.
- `-client-ca path-string`
  - PEM bundle of one or more trust anchors used to verify device certificates. Required by `mtls`; rejected by `plain` and `tls`. This is a device-issuing trust source only: it is never replaced by, or mixed with, the server certificate chain, and the server CA private key is not required.
- `-tls-min 1.2|1.3` / `-tls-max 1.2|1.3`
  - TLS version range (default `1.2`-`1.3`). TLS 1.0 and 1.1 are rejected. `-tls-min` must not be higher than `-tls-max`.
- `-allow-invalid-server-cert`
  - For deliberate negative tests, allows semantic-only server certificate problems: validity period, serverAuth EKU, key usage, CA-as-leaf, and expiry of chain certificates. Each bypassed item is printed at startup. It never bypasses file-read/PEM/certificate/private-key parse errors, key pairing errors or chain signature/CA-attribute errors, and it never disables device certificate verification.

Certificate loading behavior:

- The server certificate file may contain extra non-`CERTIFICATE` blocks (for example, a combined certificate/key file); they are ignored and counted in the startup report. All `CERTIFICATE` blocks must be ordered leaf-first; the tool does not reorder a reversed chain. OpenSSL `Bag Attributes` metadata before the first block is accepted; arbitrary leading non-PEM text and malformed or unterminated blocks fail.
- The private key file must contain exactly one supported private key block. Other blocks are ignored and reported. Empty files, multiple key blocks, malformed blocks and trailing non-PEM data fail startup.
- The client CA bundle may contain multiple `CERTIFICATE` blocks and is loaded as a whole. A non-CA trust anchor is accepted with a warning; it may only be usable for explicit leaf pinning in some clients. Empty or malformed bundles fail startup.
- Startup prints the loaded chain, key format, password source (never the password), client trust-anchor count, certificate notes and every check bypassed by `-allow-invalid-server-cert`.

General options:

- `-listen IP:port`
  - Listening address (default `0.0.0.0:1883`). Port `0` selects an ephemeral port; the actual address is printed at startup.
- `-users path-string`
  - [Users and permissions file](#examples-of-user-and-rights-profiles) (.yaml/.json) path. This is the actual authentication/ACL configuration.
- `-record-only-client-id string`
  - Only record message data from these client IDs (comma separated). This is a recording filter: it does **not** block connections and is not an access-control policy.
- `-record-only-topic string`
  - Only record message data on these topics (comma separated). It does not block publish or subscribe.
- `-record-only-payload string`
  - Only record messages containing these words (comma separated). It does not affect delivery.
- `-log-data path-string`
  - Log message data to a CSV file. The file is truncated at startup; unexpected write failures are reported and counted.
- `-log-status path-string`
  - Log state changes to a CSV file.
- `-log path-string`
  - Save the text log to a file.
- `-log-timestamp`
  - Use Unix timestamps in logged CSV files instead of the time string.

CSV columns: status logs contain time, client, event and detail; data logs contain time, client, topic and payload. With `-log-timestamp` the first column is a Unix nanosecond timestamp. These files are written synchronously and are separate from `events.jsonl`/`summary.json`.
- `-monochrome`
  - Use a monochrome color scheme (for terminals such as Windows cmd.exe).
- `-lang auto|en|chs`
  - Display language (default `auto`).
- `-version`
  - Print version info and exit.
- `-h`, `-help`
  - Print all flags with their default values and exit with code `0`.

Diagnostics and result export:

- `-run-id string`
  - Run identifier written to every event and to the summary (default: generated). At most 64 characters; control characters are rejected.
- `-device-model`, `-firmware`, `-test-case`, `-note string`
  - Free-form metadata for the run summary. They are recorded as operator-provided context, not interpreted by the tool.
- `-results DIR`
  - Write `events.jsonl` and `summary.json` into `DIR`. The directory is created if missing. Startup fails instead of overwriting an existing `events.jsonl` or `summary.json`, so use a new directory for each run.
- `-log-payload`
  - Include MQTT application payloads in diagnostic events (default off). Payloads are truncated to 4096 bytes; non-UTF-8 data is base64 encoded. Payload recording is still subject to `-record-only-*`, and known secrets are redacted, but treat result files as sensitive.
- `-expect-tls any|success|failure`, `-expect-connect any|accepted|rejected`, `-expect-client-cert any|present|absent`
  - Optional expectations used for automatic per-connection judgement. `any` disables that part of the judgement. A verdict is only `pass` when every specified condition has complete observed evidence; `fail` requires contrary evidence; otherwise the result is `undetermined`. In one-way TLS mode a client-certificate expectation is always `undetermined`, because the server does not request a certificate. Expectation results do not change the process exit code.

`events.jsonl` contains one JSON object per line with `seq`, RFC3339Nano `time`, `run_id`, `connection_id`, `stage`, `action`, `result`, `certainty`, and optional `remote`, `client_id`, `username`, `reason`, `reason_code`, `tls` and `context`. Stages are `run`, `transport`, `tls`, `mqtt_connect`, `mqtt_auth`, `authorization`, `subscribe`, `unsubscribe`, `publish` and `disconnect`. `certainty` distinguishes `observed` facts from `heuristic`, `uncertain` and `unknown` inferences; do not read an unobserved value as a negative result.

`summary.json` contains the run metadata, effective configuration, expectations, event counts, one entry per connection with observed facts, evidence, failures/undetermined reasons, and the overall verdict. TLS certificate summaries include subject, issuer, serial, validity, SANs and SHA-256 fingerprints, never private keys. `output.status` is `complete`, `failed` or `memory_only` (no `-results`). Password values and PEM private-key blocks are redacted from events and summary text.

Connection lifecycle options:

- `-handshake-timeout duration`
  - Maximum time for a TLS handshake (default `10s`). A client that does not finish the handshake is disconnected and recorded with its observed failure classification.
- `-connect-timeout duration`
  - Maximum time from accept (or from a completed TLS handshake) until an MQTT CONNECT packet is received (default `30s`). This bounds silent TCP/TLS connections that never speak MQTT.
- `-max-handshakes n`
  - Maximum number of concurrent TLS handshakes (default `64`, range `1`-`4096`). Additional connections wait in the listen backlog; the limit covers the handshake phase only, not established MQTT sessions.
- `-shutdown-timeout duration`
  - Maximum time to wait for existing connections to finish and for pending diagnostic output to flush during shutdown (default `10s`). On timeout the shutdown returns an error instead of blocking forever.
- `-pprof-listen address`
  - Disabled by default. When set, starts the pprof HTTP diagnostics listener on this address (for example `127.0.0.1:6060`). Binding a non-loopback address produces a warning; the listener is stopped during shutdown.

TLS, configuration, log-file or debug-listener errors stop startup with a non-zero exit code. The server never downgrades from TLS/mTLS to plain TCP or weakens client certificate verification automatically. The startup summary prints the effective mode, bound address, TLS version range and whether device certificates are required and verified.

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

Requirements: Go 1.22.3 or newer (the module declares `go 1.22.3`; this round was tested with Go 1.27.2). Building does not require OpenSSL, a real device or internet access after module download.

```sh
go mod download   # Need internet
go generate       # Windows only
go build .
```

Build all platforms under Windows: `build.bat`

### Tests

The default suite uses only loopback TCP/TLS, dynamically generated certificates in `t.TempDir`, and a minimal in-process MQTT client. It does not use OpenSSL, a real device, a public broker or the internet.

```sh
go vet ./...
BUDGET=$(( $(nproc) / 2 )); [ "$BUDGET" -lt 1 ] && BUDGET=1
GOMAXPROCS=$BUDGET go test -count=1 -p "$BUDGET" -parallel "$BUDGET" ./...
GOMAXPROCS=$BUDGET go test -race -count=1 -p "$BUDGET" -parallel "$BUDGET" ./...
```

On Windows, replace `$(nproc)` with the number of logical processors and keep the same half-budget rule (minimum 1).

The optional certificate-script integration test is enabled explicitly:

```sh
MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScriptMaterial -v
```

Cross-compilation only proves that the target builds; it is not a run-time verification on Windows or macOS:

```sh
GOOS=windows GOARCH=amd64 go build .
GOOS=darwin  GOARCH=arm64 go build .
```

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
- youmark/pkcs8 ([MIT License](https://github.com/youmark/pkcs8/blob/master/LICENSE)) — required to decrypt PBES2 encrypted PKCS#8 private keys; the Go standard library cannot do this.
- golang.org/x/term ([BSD 3-Clause License](https://github.com/golang/term/blob/master/LICENSE)) — hidden terminal password input.
- golang.org/x/crypto ([BSD 3-Clause License](https://github.com/golang/crypto/blob/master/LICENSE)) — indirect dependency of pkcs8 (PBKDF2 and scrypt).

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

命令行: `mqtt-test-server -mode plain|tls|mtls [选项]`

安全模式：

- `plain`（默认）：普通 MQTT，不启用 TLS。只要传入 TLS 相关参数就会报错，不会被静默忽略。
- `tls`：单向 TLS。必须提供 `-server-cert` 和 `-server-key`，服务器不请求设备证书。
- `mtls`：强制双向 TLS。必须提供 `-server-cert`、`-server-key` 和 `-client-ca`，固定使用 `tls.RequireAndVerifyClientCert` 要求并验证设备证书。

TLS 参数：

- `-server-cert 文件路径`
  - PEM 格式的服务器证书链（叶证书在前，其后可附中间证书）。
- `-server-key 文件路径`
  - PEM 格式的服务器私钥。支持未加密的 PKCS#1（`RSA PRIVATE KEY`）、SEC1（`EC PRIVATE KEY`）和 PKCS#8（`PRIVATE KEY`，RSA/ECDSA/Ed25519）；也支持旧式 RFC 1423 加密 PEM 私钥和加密 PKCS#8（`ENCRYPTED PRIVATE KEY`，PBES2 + PBKDF2/scrypt + AES/3DES）。OpenSSH、DSA、非 PKCS#8 的裸 Ed25519 以及包含多个私钥区块的文件会明确报错。
- `-server-key-password-file 文件路径`
  - 加密私钥的密码文件，读取时会去掉末尾换行。文件应只允许运行本工具的账号读取（`0600`），权限过宽会给出警告。也可以使用环境变量 `MQTT_SERVER_KEY_PASSWORD`，或在交互式终端中由程序隐藏输入提示。命令行不提供明文密码参数。
  - 私钥已加密但既没有密码文件、环境变量，也没有可交互终端时，启动会失败。私钥未加密却提供密码来源会报错，密码不会被静默忽略。
- `-client-ca 文件路径`
  - 用于验证设备证书的 PEM 信任锚集合，可包含多个 CA。`mtls` 模式必需；`plain` 和 `tls` 模式传入会报错。它只表示设备证书的签发信任源，不会被服务器证书链替换或混用；不需要提供服务器 CA 私钥。
- `-tls-min 1.2|1.3` / `-tls-max 1.2|1.3`
  - TLS 版本范围（默认 `1.2`-`1.3`）。拒绝 TLS 1.0 和 1.1；`-tls-min` 不得高于 `-tls-max`。
- `-allow-invalid-server-cert`
  - 仅供反例测试，只放行服务器证书的语义问题：有效期、serverAuth 扩展密钥用途、密钥用途、把 CA 证书当作叶证书，以及链中证书过期。每个放行项都会在启动时输出。它不会放行文件读取、PEM/证书/私钥解析、私钥配对、链签名或 CA 属性错误，也绝不会关闭设备证书验证。

证书加载行为：

- 服务器证书文件可以包含额外的非 `CERTIFICATE` 区块（例如证书与私钥合并文件），这些区块会被忽略并计入启动报告。所有 `CERTIFICATE` 区块必须按叶证书在前的顺序排列，工具不会自动重排倒序链。第一个区块之前的 OpenSSL `Bag Attributes` 元数据可以接受；其他任意前导非 PEM 文本以及损坏或未闭合的区块会导致失败。
- 私钥文件必须且只能包含一个受支持的私钥区块。其他区块会被忽略并报告。空文件、多个私钥区块、损坏区块和尾部非 PEM 数据都会导致启动失败。
- 设备 CA 文件可以包含多个 `CERTIFICATE` 区块并整体载入。非 CA 信任锚会给出警告但仍可载入；它可能只能用于部分客户端的精确叶证书固定。空文件或损坏内容会导致启动失败。
- 启动时会显示实际加载的证书链、私钥格式、密码来源（绝不显示密码）、设备信任锚数量、证书提示以及所有由 `-allow-invalid-server-cert` 放行的检查项。

通用参数：

- `-listen IP:端口`
  - 监听地址（默认 `0.0.0.0:1883`）。端口 `0` 表示由系统分配动态端口，启动时会显示实际地址。
- `-users 文件路径`
  - [用户和主题权限配置文件](#用户和主题权限配置文件示例) (.yaml/.json) 路径。真正的认证与 ACL 访问控制由该文件负责。
- `-record-only-client-id 字符串`
  - 只记录这些客户端 ID 的消息内容（使用 `,` 分隔）。这是记录过滤器，**不会**阻止连接，也不代表访问控制。
- `-record-only-topic 字符串`
  - 只记录这些主题的消息内容（使用 `,` 分隔）。不会阻止发布或订阅。
- `-record-only-payload 字符串`
  - 只有消息内容包含这些关键词才会记录（使用 `,` 分隔）。不影响消息投递。
- `-log-data 文件路径`
  - 将收取到的消息保存到 .csv 文件。启动时会截断旧文件；写入失败会被报告并计数。
- `-log-status 文件路径`
  - 将客户端的连接、断开、订阅、退订等行为保存到 .csv 文件。
- `-log 文件路径`
  - 将日志输出保存到 .txt / .log 文件。
- `-log-timestamp`
  - 在记录的 CSV 文件中使用 Unix 时间戳而不是时间字符串。

CSV 列：状态日志为“时间、客户端、事件、说明”，数据日志为“时间、客户端、主题、负载”；使用 `-log-timestamp` 时首列为 Unix 纳秒时间戳。CSV/文本日志为同步写入，与 `events.jsonl`、`summary.json` 相互独立。
- `-monochrome`
  - 使用单色模式输出，避免某些终端（例如 Windows 的 cmd.exe）显示乱码。
- `-lang auto|en|chs`
  - 显示语言（默认 `auto`）。
- `-version`
  - 显示版本号等信息并退出。
- `-h`、`-help`
  - 打印全部参数及默认值并退出，退出码为 `0`。

诊断与结果导出参数：

- `-run-id 字符串`
  - 运行编号，写入每条事件与汇总（默认自动生成）。最长 64 个字符，不允许控制字符。
- `-device-model`、`-firmware`、`-test-case`、`-note 字符串`
  - 写入汇总的运行上下文。工具只原样记录，不解释其含义。
- `-results 目录`
  - 在该目录写入 `events.jsonl` 与 `summary.json`；目录不存在时会创建。目录中已存在其中任一文件时，启动直接失败而不是覆盖，因此每轮测试应使用新目录。
- `-log-payload`
  - 在诊断事件中包含 MQTT 应用负载（默认关闭）。负载最多保留 4096 字节，非 UTF-8 内容以 Base64 记录；仍受 `-record-only-*` 过滤，已知密码会被替换，但结果文件整体仍应按敏感材料管理。
- `-expect-tls any|success|failure`、`-expect-connect any|accepted|rejected`、`-expect-client-cert any|present|absent`
  - 可选的自动判定预期，`any` 表示该项不参与判定。只有全部已指定条件都取得完整观测证据时，连接判定才是 `pass`；出现相反证据为 `fail`；证据不完整为 `undetermined`。单向 TLS 模式下指定设备证书预期始终为 `undetermined`，因为服务器不会请求证书。预期判定结果**不会改变进程退出码**。

结果格式：

- `events.jsonl` 每行一个 JSON 对象，字段包括 `seq`、RFC3339Nano 格式的 `time`、`run_id`、`connection_id`、`stage`、`action`、`result`、`certainty`，以及可选的 `remote`、`client_id`、`username`、`reason`、`reason_code`、`tls`、`context`。
- 阶段（`stage`）取值为 `run`、`transport`、`tls`、`mqtt_connect`、`mqtt_auth`、`authorization`、`subscribe`、`unsubscribe`、`publish`、`disconnect`。其中 `mqtt_connect` 只表示收到 CONNECT，认证结果以 `mqtt_auth` 阶段的 CONNACK 返回码为准。
- `certainty` 区分 `observed`（直接观察）、`heuristic`（启发式判断）、`uncertain`（不确定）与 `unknown`（未观察）。不要把“未观察到”当成“设备不支持”。
- `summary.json` 包含运行元数据、实际生效配置、预期、事件计数、每个连接的观测事实、证据、失败/未判定原因与总判定；TLS 证书摘要只含主题、签发者、序列号、有效期、SAN 与 SHA-256 指纹，不含私钥。
- `output.status` 为 `complete`、`failed` 或 `memory_only`（未指定 `-results`）。`counts.dropped_terminal_events` 只表示终端显示队列溢出，不影响 JSONL 与内存事件；结构化写入失败会进入 `output.write_errors`。

退出码：

| 退出码 | 含义 |
| --- | --- |
| `0` | 正常运行结束，或 `-version` / `-h` |
| `1` | 启动失败、监听失败、日志/结果文件写入失败、关闭超时 |
| `2` | 参数或配置校验失败（未知模式、参数冲突、无效数值、文件不存在等） |

连接生命周期参数：

- `-handshake-timeout 时长`
  - TLS 握手的最长等待时间（默认 `10s`）。超时的客户端会被断开，并按可观察到的失败类型记录。
- `-connect-timeout 时长`
  - 从接入（或 TLS 握手完成）到收到 MQTT CONNECT 的最长等待时间（默认 `30s`）。用于关闭始终不发送 MQTT 报文的静默连接。
- `-max-handshakes 数量`
  - 最大并发 TLS 握手数（默认 `64`，范围 `1`-`4096`）。超出的连接在监听 backlog 中排队；该限制只覆盖握手阶段，不限制已建立的 MQTT 会话。
- `-shutdown-timeout 时长`
  - 关闭时等待现有连接结束与诊断输出刷盘的最长时间（默认 `10s`）。超时会返回错误，而不是无限阻塞。
- `-pprof-listen 地址`
  - 默认关闭。设置后在指定地址启动 pprof HTTP 诊断监听（例如 `127.0.0.1:6060`）。绑定非回环地址会输出警告；退出时会一并关闭。

TLS、配置、日志文件或调试监听错误会以非零退出码终止启动。程序不会从 TLS/mTLS 自动降级为明文，也不会自动放宽设备证书验证。启动摘要会显示实际生效的模式、监听地址、TLS 版本范围，以及是否强制验证设备证书。

### 真实设备测试证书（OpenSSL 脚本）

需要给真实设备准备长期使用的测试证书时，可在已安装 bash 4+ 与 OpenSSL 1.1.1+ 的机器上运行：

```sh
bash scripts/gen-test-certs.sh -o "certs/设备测试-01" \
    --san-dns "mqtt.example.com,localhost" --san-ip "192.168.1.10,127.0.0.1" \
    --device-id dev-001
```

常用选项：`--validity-days`（叶证书天数，默认 365）、`--ca-validity-days`（CA 天数，默认 3650，且至少比叶证书多 30 天）、`--key-alg`（`rsa2048`/`rsa3072`/`rsa4096`/`ec-p256`/`ec-p384`）、`--ca-key-alg`、`--leaf-key-format pkcs8|traditional`。完整说明可运行 `bash scripts/gen-test-certs.sh --help`。Windows 默认 cmd 可运行 `scripts\gen-test-certs.bat`（内部调用 MSYS2/Git Bash）；参数含中文时建议直接在 MSYS2 终端执行 `.sh`。

输出结构：

```text
<输出目录>/
  ca/ca-a/             CA A 私钥、证书与审计文件（不得导入设备或分发）
  ca/ca-b/             CA B 私钥、证书（用于不受信任签发者反例）
  server/              server.crt、server.key、ca-a.crt（设备信任锚）
  server/negative/     过期、SAN/CN 不匹配、CA B 签发的服务器证书反例
  device/              device.crt、device.key、ca-a.crt（设备导入材料）
  device/negative/     过期、未生效、EKU 错误、CA B 签发的设备证书反例
  manifest.txt         路径与 SHA-256 指纹清单（不含私钥）
  说明.txt             中文说明与每项反例的唯一预期失败原因
```

脚本在生成后用 OpenSSL 复核链、用途、SAN、时间与公私钥匹配，并保证设备目录不含 CA 私钥或服务器私钥。私钥权限为 `0600`，输出目录已存在时脚本直接失败、不覆盖任何材料。生成物默认被 `.gitignore` 中的 `/certs/`、`/test-certs/` 排除。

用生成的材料启动本工具：

```sh
# 单向 TLS：设备只需信任 server/ca-a.crt
./MQTTDeviceTestingTool -mode tls -listen 0.0.0.0:8883 \
    -server-cert "certs/设备测试-01/server/server.crt" \
    -server-key  "certs/设备测试-01/server/server.key"

# 强制 mTLS：服务器用 server/ca-a.crt 验证设备证书
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert "certs/设备测试-01/server/server.crt" \
    -server-key  "certs/设备测试-01/server/server.key" \
    -client-ca   "certs/设备测试-01/server/ca-a.crt"
```

脚本只用于人工测试材料，基础 `go test` 不依赖它。可选的脚本集成测试需显式启用（需要 bash 与 OpenSSL；未安装相关工具时会明确 Skip）：

```sh
MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScriptMaterial -v
```

### 真实设备手工测试流程

以下流程中的“通过”仅代表本次实际观察到的配置、算法与固件组合，不代表设备支持所有客户 PKI、所有证书格式或身份绑定要求。设备“单一证书”与“三元组证书”的字段含义和拼接顺序仍未确认，必须先按设备文档核对，不能根据本工具猜测。

#### 0. 先验证测试环境

1. 用已知支持 mTLS 的软件客户端连接本工具，排除服务端材料、端口、防火墙与主机名问题。示例（需自行安装 OpenSSL 客户端工具）：

   ```sh
   openssl s_client -connect mqtt.example.com:8883 -servername mqtt.example.com \
       -CAfile "certs/设备测试-01/device/ca-a.crt" \
       -cert "certs/设备测试-01/device/device.crt" \
       -key  "certs/设备测试-01/device/device.key"
   ```

   没有 `-cert`/`-key` 时应被拒绝；带正确材料时握手应成功。需要验证 MQTT 层时，可使用 `mosquitto_pub`/`mosquitto_sub` 等客户端，加上 `--cafile`、`--cert`、`--key`，不要使用跳过服务器校验的选项。

2. 记录工具版本（`./MQTTDeviceTestingTool -version`）、实际模式、监听地址、TLS 版本范围与设备证书策略。启动摘要显示的是实际生效值，不是命令行输入。

3. 每次切换证书、CA 或模式后重新启动工具，并在设备侧重新建立连接。设备的 TLS 会话缓存、MQTT 会话恢复和连接复用可能让“移除证书”的反例错误地继续使用旧会话；测试时应使用新的 Client ID、Clean Session/Start，必要时重启工具或设备。

#### 1. 单向 TLS 基线

- 工具使用 `-mode tls` 与正向 `server/server.crt`、`server/server.key`。
- 设备导入 `device/ca-a.crt` 作为服务器信任锚，服务器地址必须匹配证书 SAN（DNS 或 IP）。
- 预期：设备验证服务器成功，MQTT 连接按 `-users` 配置被接受。

#### 2. 强制 mTLS：无证书必须被拒绝

- 工具使用 `-mode mtls` 与 `-client-ca server/ca-a.crt`。
- 设备清除客户端证书（或使用不含证书的配置）后重新连接。
- 预期：TLS 握手失败，工具记录 `tls/handshake/failure`，原因通常是 `client_certificate_missing`（启发式）或设备收到的 `certificate_required` alert；不应出现成功的 CONNACK。若设备仍显示连接成功，优先检查会话恢复、代理终止 TLS 或设备实际仍在发送旧证书。

#### 3. 强制 mTLS：正确三元组成功

- 设备导入 `device/device.crt`、`device/device.key` 与 `device/ca-a.crt`。
- 工具保持 `-client-ca server/ca-a.crt`。
- 预期：TLS 握手成功，`client_certificate=present`，MQTT CONNACK 为接受。用 `-results` 与 `-expect-*` 可让工具自动判定，例如：

  ```sh
  ./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
      -server-cert "certs/设备测试-01/server/server.crt" \
      -server-key  "certs/设备测试-01/server/server.key" \
      -client-ca   "certs/设备测试-01/server/ca-a.crt" \
      -results "results/mtls-valid" -run-id mtls-valid \
      -expect-tls success -expect-connect accepted -expect-client-cert present
  ```

#### 4. 强制 mTLS：不受信任或无效的设备证书必须被拒绝

分别替换设备证书/私钥后重新连接，其他条件保持一致：

- `device/negative/device-by-ca-b.crt`：签发者不在 `-client-ca` 中。
- `device/negative/device-expired.crt`：已过期。
- `device/negative/device-notyet.crt`：尚未生效。
- `device/negative/device-wrong-eku.crt`：缺少 `clientAuth` 用途。

预期均为 TLS 握手失败（`certificate_verification`、alert 或对端关闭），且没有 MQTT 认证成功。若设备无法导入无效证书，应记录为“设备导入失败”，不要记作“服务器拒绝了该证书”。

#### 5. 设备拒绝无效服务器证书

保持设备信任 `device/ca-a.crt`，分别让工具使用以下服务器材料：

- `server/negative/server-by-ca-b.crt`：设备不受信任的签发者。
- `server/negative/server-wrong-host.crt`：名称与设备连接地址不匹配。
- `server/negative/server-expired.crt`：已过期；启动工具时需显式加 `-allow-invalid-server-cert` 才会放行服务器自身的语义检查。

预期：设备在 TLS 握手阶段拒绝，工具侧通常看到失败或对端关闭，且没有 CONNECT/CONNACK。`-allow-invalid-server-cert` 只放行服务器证书语义检查，不会让设备信任该证书，也不会放宽设备证书验证。

#### 6. 材料角色映射与待核对项

| 标准角色 | 脚本材料 | 使用方 |
| --- | --- | --- |
| 服务器证书链 | `server/server.crt` | 工具 `-server-cert` |
| 服务器私钥 | `server/server.key` | 工具 `-server-key` |
| 验证设备证书的信任锚 | `server/ca-a.crt` | 工具 `-client-ca` |
| 设备证书 | `device/device.crt` | 设备 |
| 设备私钥 | `device/device.key` | 设备 |
| 设备验证服务器的信任锚 | `device/ca-a.crt` | 设备 |
| 不受信任的对照 CA | `device/negative/ca-b.crt` | 对照实验 |

真实设备若要求“单一证书”或“三元组证书”，需向设备文档或厂商确认：设备证书是否必须包含中间证书、各字段的顺序与分隔方式、私钥格式（PKCS#1/SEC1/PKCS#8、是否加密）、CA 是否与设备证书放在同一文件，以及设备支持的密钥算法。确认前不要把多个 PEM 文件拼接后直接导入。

#### 7. 结果记录模板

```text
测试日期：
测试人员：
设备型号：
固件版本：
工具版本（-version）：
运行编号（-run-id）：
测试模式（plain/tls/mtls）：
监听地址与端口：
TLS 版本范围：
服务器证书主题 / SHA-256：
设备证书主题 / SHA-256：
设备信任锚 / SHA-256：
设备 CA（工具 -client-ca）/ SHA-256：
其他条件（用户名、Client ID、Clean Session、网络、代理、设备时钟）：

预期：
实际结果（阶段 / 结果 / 原因或 alert / CONNACK 返回码）：
判定（通过 / 失败 / 未判定）：
日志与结果位置（-log、-results 目录）：
备注与后续动作：
```

失败按阶段区分：

- **导入失败**：设备拒绝证书/私钥格式、字段缺失或文件过大，连接尚未建立。
- **握手失败**：TCP 已建立，但 TLS 在证书验证、算法、协议版本或客户端证书要求处失败。
- **MQTT 层失败**：TLS 成功，但 CONNACK 拒绝、订阅被拒或发布未授权；这类结果不能解释为 TLS 失败。


### 错误排查

先看启动摘要确认实际模式、TLS 版本范围和设备证书策略，再看 `events.jsonl` 的阶段与 `certainty`，不要只根据终端最后一行判断。

| 现象 | 优先检查 |
| --- | --- |
| 启动退出码 `2` | 参数冲突、未知模式、TLS 版本、数值范围、文件不存在；错误写在 stderr |
| 启动退出码 `1` | 证书/私钥读取或解析、私钥密码、私钥不配对、链结构、监听端口或结果目录 |
| `tls/handshake/failure` + `certificate_verification` | 设备证书签发者是否在 `-client-ca`、是否过期/未生效、是否含 `clientAuth`、中间证书是否完整 |
| `client_certificate_missing`（`heuristic`） | 设备是否真的发送证书；同时排除 TLS 会话恢复、代理终止 TLS 与旧连接复用 |
| `tls_record` / `not_tls_record` | 客户端把明文发到 TLS 端口、端口错误或代理改写了流量 |
| `timeout` | `-handshake-timeout`、`-connect-timeout`、网络中断或设备未发送 CONNECT；超时不等于证书错误 |
| `peer_closed` | 对端主动关闭；只能说明连接结束，不能据此断定具体证书原因 |
| TLS 成功但 `mqtt_auth/rejected` | 认证由 `-users` 的 `auth` 决定，与 `-record-only-*` 无关；查看 CONNACK 返回码 |
| 订阅或发布被拒 | 检查 `-users` 的 `acl`；`-record-only-*` 只控制记录，不控制权限 |
| 设备拒绝服务器证书 | 检查 SAN/连接主机名、设备信任锚、有效期、设备时钟、TLS 版本与算法能力 |
| `output.status=failed` | 结果文件写入失败；查看 `summary.json` 的 `output.write_errors` 与 stderr |

`-allow-invalid-server-cert` 只放行服务器证书的有效期、EKU、KeyUsage、CA-as-leaf 等语义检查；它不会放行损坏材料、私钥不匹配或链错误，也不会影响设备证书验证。OpenSSL 脚本要求 bash 4+ 与 OpenSSL 1.1.1+，明确拒绝 LibreSSL；Windows 下路径或中文参数问题请改用 MSYS2 终端执行 `.sh`。

### 敏感材料处理

- CA 私钥与服务器私钥只保留在受控目录，权限分别为 `0700`/`0600`；设备的“三元组”或“单一证书”导入包中不应包含 CA 私钥，除非设备文档明确要求。
- 私钥密码优先使用权限 `0600` 的密码文件或环境变量；命令行不提供明文密码参数，密码不会写入日志、事件或汇总。
- `-results` 与 `-log*` 可能包含 Client ID、用户名、主题；启用 `-log-payload` 后还会包含应用负载。结果文件默认权限为 `0644`、结果目录为 `0755`，如需更严格请放在受限目录或运行后调整权限，并排除在版本管理之外（仓库已忽略 `/certs/`、`/test-certs/`、`/results/`、`*.log`、`*.csv`；也可使用仓库外路径）。
- 测试材料不应导入生产信任库；测试完成后按组织要求删除或轮换，不要提交私钥、密码或真实设备日志。
- 工具不修改系统信任库、防火墙或真实设备配置；这些操作需由用户另行执行。

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

运行环境：Go 1.22.3 或更高版本（模块声明 `go 1.22.3`，本轮在 Go 1.27.2 上验证）。首次下载模块需要互联网；编译本身不依赖 OpenSSL、真实设备或公共 MQTT 服务。

```sh
go mod download   # 需要网络
go generate       # 只有 Windows 需要执行这条
go build .
```

### 测试

基础测试只使用回环 TCP/TLS、`t.TempDir` 中的动态证书和进程内的最小 MQTT 客户端，不依赖 OpenSSL、真实设备、公共 MQTT 服务或互联网。并发预算不超过逻辑处理器数的一半（向下取整，最少 1）：

```sh
go vet ./...
BUDGET=$(( $(nproc) / 2 )); [ "$BUDGET" -lt 1 ] && BUDGET=1
GOMAXPROCS=$BUDGET go test -count=1 -p "$BUDGET" -parallel "$BUDGET" ./...
GOMAXPROCS=$BUDGET go test -race -count=1 -p "$BUDGET" -parallel "$BUDGET" ./...
```

Windows 下没有 `nproc` 时，请按逻辑处理器数量手动设置同样的“一半、最少 1”预算。

可选证书脚本集成测试（需要 bash 与 OpenSSL 1.1.1+；缺少时明确 Skip）：

```sh
MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScriptMaterial -v
```

交叉编译只证明目标平台能编译，不代表已在目标平台运行：

```sh
GOOS=windows GOARCH=amd64 go build .
GOOS=darwin  GOARCH=arm64 go build .
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
