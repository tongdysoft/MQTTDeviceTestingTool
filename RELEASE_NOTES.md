# MQTTDeviceTestingTool Release 介绍（待发布）

- 版本状态：待发布（当前源码版本仍为 `1.5.5`，新版本号未确定；未创建 tag，未发布）
- 适用代码：本轮 TLS/mTLS 修复、证书处理、诊断结果与自动化测试改造
- 文档语言：简体中文

## 一、概述

本版本重整了 MQTT 服务端的安全模式与启动配置，修复了 TLS/mTLS 可能被静默降级或丢失客户端证书验证的问题，补充了证书/私钥加载校验、连接生命周期控制、结构化诊断事件与测试结果导出，并提供面向真实设备的 OpenSSL 证书生成脚本。

三种模式必须显式选择：

| 模式 | 含义 | 必需材料 | 客户端证书策略 |
| --- | --- | --- | --- |
| `plain` | 普通 MQTT，无 TLS | 无；传入任何 TLS 材料都会报错 | 不适用 |
| `tls` | 单向 TLS | `-server-cert`、`-server-key` | `tls.NoClientCert` |
| `mtls` | 强制双向 TLS | 再加 `-client-ca` | `tls.RequireAndVerifyClientCert` |

TLS/mTLS 启动失败、证书错误或配置冲突会以非零退出码结束，不会自动降级为明文，也不会放宽设备证书验证。

## 二、Bug 修复

- 修复服务器证书加载后重新构造 `tls.Config` 导致 `ClientAuth` 丢失的问题；mTLS 现在固定为要求并验证设备证书。
- 修复仅提供服务器证书时仍以明文监听的问题；模式必须显式选择，`plain` 模式拒绝 TLS 材料，`tls`/`mtls` 缺少必需材料直接失败。
- 修复旧客户端证书验证策略默认值与文档/实际模式不一致的问题，删除可任意放宽验证的 `-cv` 参数。
- 修复加密私钥密码被忽略或错误地按 PKCS#1 DER 解析的问题；密码来源、解密失败与“密码多余”现在都有明确错误。
- 修复证书/私钥解析错误时进程仍以退出码 0 结束的问题；启动失败统一返回非零退出码。
- 修复服务器证书链与设备证书信任库可能混用的问题；`-client-ca` 只作为设备证书的信任锚。
- 修复客户端断开、会话接管等内部清理被误记为 MQTT 取消订阅事件的问题；只有带非零 PacketID 与非空过滤器的真实 UNSUBSCRIBE 报文才会生成取消订阅事件。
- 修复 `-h`/`-help` 被当作参数错误并返回退出码 2 的问题；现在打印完整参数列表并以退出码 0 结束。

## 三、新功能

### 证书与启动

- 新增 `plain|tls|mtls` 三种安全模式，启动摘要打印实际生效的模式、监听地址、TLS 版本范围与设备证书策略。
- 服务器证书链按文件顺序加载，叶证书必须在前；支持中间证书，根证书可省略；非证书区块忽略并计数。
- 私钥支持未加密 PKCS#1、SEC1、PKCS#8（RSA/ECDSA/Ed25519），旧式 RFC 1423 加密 PEM，以及 PBES2 加密 PKCS#8；不支持的格式明确报错。
- 私钥密码来源支持 `-server-key-password-file`、`MQTT_SERVER_KEY_PASSWORD` 与交互式终端隐藏输入；命令行不再接受明文密码。
- 默认检查服务器证书有效期、`serverAuth` EKU、KeyUsage、CA-as-leaf 与链中证书有效期；`-allow-invalid-server-cert` 仅供反例测试放行这些语义检查。
- `-client-ca` 支持包含多个 CA 的信任库；非 CA、过期锚点等给出提示但不静默改变验证结果。

### 诊断与结果

- TCP 接入、TLS 握手、MQTT CONNECT、CONNACK、订阅、发布、授权、断开与传输关闭分别记录为结构化事件。
- `-results DIR` 输出 `events.jsonl`（逐行 JSON）与 `summary.json`（运行元数据、实际配置、事件计数、逐连接观测、证据与判定）。
- 新增 `-run-id`、`-device-model`、`-firmware`、`-test-case`、`-note`，用于把工具结果与真实设备测试记录关联起来。
- 新增 `-expect-tls`、`-expect-connect`、`-expect-client-cert` 自动判定；证据不完整时返回 `undetermined`，不把“未观察到”推断为设备能力。
- 新增 `-log-payload`（默认关闭），负载最多保留 4096 字节；已知密码与 PEM 私钥块会被脱敏。
- 错误分类区分 TLS alert、证书验证、超时、对端关闭、非 TLS 记录与传输错误；对端关闭或超时不会被强行解释为特定证书错误。

### 连接生命周期与运维

- 新增 `-handshake-timeout`、`-connect-timeout`、`-max-handshakes`、`-shutdown-timeout`。
- 握手并发限制只覆盖握手阶段，已建立的 MQTT 会话不占用握手槽位。
- 关闭时先停止监听，再断开现有连接，并在超时范围内等待清理与结果刷盘。
- pprof 默认关闭；只有显式 `-pprof-listen` 才启动，非回环地址会告警，退出时关闭。
- 文本日志、状态 CSV、数据 CSV 与诊断事件使用并发安全写入；显式日志文件无法打开时启动失败。
- `-record-only-client-id`、`-record-only-topic`、`-record-only-payload` 明确为记录过滤器，不影响连接、认证、订阅或发布；访问控制由 `-users` 的 `auth`/`acl` 负责。

### 证书脚本与测试设施

- 新增 `scripts/gen-test-certs.sh` 与 `scripts/gen-test-certs.bat`，生成 CA A/CA B、服务器材料、设备导入材料、正向与反例证书、`manifest.txt` 与中文 `说明.txt`。
- 脚本要求 bash 4+ 与 OpenSSL 1.1.1+，拒绝 LibreSSL；生成后复核链、用途、SAN、时间与公私钥匹配；输出目录已存在时不覆盖。
- 新增动态证书工厂、真实回环 `StartServer` 测试设施、最小 MQTT 3.1.1 测试客户端与认证策略反例矩阵；基础 `go test` 不依赖 OpenSSL、真实设备、公共 MQTT 服务或互联网。
- 新增可选的脚本集成测试：`MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScriptMaterial -v`。

## 四、参数与配置的破坏性变化

旧参数全部移除，不保留别名或兼容层。迁移时请直接替换：

| 旧参数 | 新参数 |
| --- | --- |
| `-p` | `-listen` |
| `-ce` | `-server-cert` |
| `-ck` | `-server-key` |
| `-cp` | `-server-key-password-file`（命令行明文密码不再支持） |
| `-ca` | `-client-ca`（含义收窄为验证设备证书的 CA，仅 `mtls` 可用） |
| `-u` | `-users` |
| `-c` / `-t` / `-w` | `-record-only-client-id` / `-record-only-topic` / `-record-only-payload` |
| `-m` / `-s` / `-o` | `-log-data` / `-log-status` / `-log` |
| `-ts` | `-log-timestamp` |
| `-n` | `-monochrome` |
| `-l` | `-lang` |
| `-v` | `-version` |

其他行为变化：

- 必须显式选择 `-mode`；默认 `plain`，但 `plain` 下传入 TLS 材料会报错。
- `-cv` 彻底删除；`mtls` 固定使用 `tls.RequireAndVerifyClientCert`，不再提供任意验证策略。
- `-only-*` 改名为 `-record-only-*`，语义明确为“只影响记录”，不再被误用为访问控制。
- `-log*` 指定的文件在启动时截断；无法打开时启动失败，而不是静默丢弃日志。
- pprof 不再默认监听固定端口；需要时显式传入 `-pprof-listen`。
- 服务器证书语义检查默认严格；`-allow-invalid-server-cert` 只放行语义检查，不改变设备证书验证。

## 五、删除项

- 删除旧参数 `-p`、`-ce`、`-ck`、`-cp`、`-ca`、`-u`、`-c`、`-t`、`-w`、`-m`、`-s`、`-o`、`-ts`、`-n`、`-l`、`-v`、`-cv` 及 `-only-*`。
- 删除任意客户端证书验证策略与宽松默认值。
- 删除未使用的旧用户加载代码 `userloader.go`。
- 删除启动时无条件监听 `:9999` 的 pprof 行为。
- 删除命令行明文私钥密码参数。

## 六、测试证据

本轮最终验证环境：Linux x64、2 个逻辑处理器、Go 1.27.2（模块声明 `go 1.22.3`）、OpenSSL 3.5.7、bash 5.2.37。

| 项目 | 命令 | 结果 |
| --- | --- | --- |
| 静态检查 | `go vet ./...` | 通过 |
| 完整测试 | `GOMAXPROCS=1 go test -count=1 -p 1 -parallel 1 ./...` | 通过（约 3 秒） |
| 竞态检测 | `GOMAXPROCS=1 go test -race -count=1 -p 1 -parallel 1 ./...` | 通过（约 6 秒），未发现数据竞争 |
| 脚本集成测试 | `MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScriptMaterial -v` | 通过，路径含空格与中文 |
| 交叉编译 | `GOOS=windows GOARCH=amd64`、`GOOS=darwin GOARCH=arm64`、`GOOS=linux GOARCH=arm64` | 通过（仅编译，未在目标平台运行） |

本机回环手工验收（真实二进制 + 脚本生成的 RSA-2048 PKCS#8 材料 + Python TLS 客户端）：

- mTLS 正确设备证书：TLS 1.3 握手成功，`client_certificate=present`，CONNACK `0x00`，预期判定 `pass`。
- mTLS 无客户端证书：TLS 失败，`failure_kind=client_certificate_missing`（启发式），客户端收到 `certificate_required` alert，预期判定 `pass`。
- 单向 TLS：TLS 1.3 握手成功，MQTT CONNACK `0x00`，预期判定 `pass`。
- 结果文件可解析，`events.jsonl` 行序连续，`summary.json` 的 `output.status=complete`。

基础测试与上述自动化测试均为**软件模拟验证**，不能替代真实设备的 TLS 栈、证书文件格式、算法支持与固件行为验证。

## 七、已知限制

- 真实设备尚未实测；设备“单一证书”和“三元组证书”的字段含义、拼接顺序、私钥格式与算法支持仍需用户确认。本工具只输出独立 PEM 文件，不猜测设备专用格式。
- 交叉编译通过不等于目标平台运行通过；Windows/macOS 的证书脚本路径、`date` 与权限行为未实测。
- 未实现 CRL/OCSP、证书轮换、设备身份与 MQTT Client ID 绑定。
- 未提供禁用 TLS 会话恢复的开关；测试“移除客户端证书”反例时应重启工具或设备，并使用新的 Client ID 与 Clean Session，避免会话恢复掩盖结果。
- 加密 PKCS#8 仅支持 `youmark/pkcs8` 覆盖的 PBES2 算法；旧式 RFC 1423 加密依赖标准库已弃用的解密接口，仅为兼容保留并提示。
- 设备证书信任锚过期、非 CA 锚点等目前仅提示，不阻止启动；实际验证仍可能失败。
- 未覆盖全部 TLS 1.2/1.3 密码套件、重协商、ALPN 与压力场景。
- `-expect-*` 的判定结果不改变进程退出码；自动化脚本需读取 `summary.json` 的 `verdict`。
- 内存事件上限为 20000 条；终端显示队列溢出只影响显示，不影响结构化文件。
- 已建立连接数仍由 mochi MQTT 服务端默认上限控制，未新增最大客户端数参数。
- 自动化测试通过不代表真实设备支持 mTLS；结论仅适用于实际验证过的配置、算法与固件。

## 八、升级与验证命令

```sh
go mod download
go build .

# 单向 TLS
./MQTTDeviceTestingTool -mode tls -listen 0.0.0.0:8883 \
    -server-cert server.crt -server-key server.key

# 强制 mTLS
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert server.crt -server-key server.key -client-ca device-ca.crt

# 结构化结果与自动判定
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert server.crt -server-key server.key -client-ca device-ca.crt \
    -results results/mtls-valid -run-id mtls-valid \
    -expect-tls success -expect-connect accepted -expect-client-cert present
```

结果位置：`results/mtls-valid/events.jsonl`、`results/mtls-valid/summary.json`。每次运行使用新目录，工具不会覆盖已有结果文件。
