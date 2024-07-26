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

Common procedures for device connectivity, one-way TLS, mTLS support and server-certificate validation are in [Common procedures (best practices)](#common-procedures-best-practices) below.

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

File and output path protection:

- All input materials (`-server-cert`, `-server-key`, `-server-key-password-file`, `-client-ca`, `-users`) and all outputs (`-log`, `-log-status`, `-log-data`, `-results/events.jsonl`, `-results/summary.json`) are compared before any file is opened, truncated or created.
- One file reached through relative/absolute paths, `.`/`..`, symlinks, hard links or a symlinked parent directory is rejected with exit code `2`; the error names both parameters, and any existing content is left byte-for-byte unchanged.
- Two input parameters may share one file; an input may never be an output, and two outputs may never resolve to the same file. The results directory is not created when a conflict is detected.
- The check and the later open are separate operations, so a concurrent replacement in between is not fully preventable. Do not point outputs at a directory another process rewrites at the same time; the tool does not claim otherwise.

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
  - Maximum duration of the TLS handshake (default `10s`). It is one absolute deadline that covers both reading and writing, so a peer that sends a ClientHello and then stops reading cannot hold the handshake open through blocked server writes. The deadline is cleared only after a successful handshake, before the MQTT CONNECT phase. On timeout, on failure or when the server is shutting down, the connection is closed and the handshake slot is released; shutdown cancels a running handshake immediately instead of waiting for the timeout.
- `-connect-timeout duration`
  - Maximum time from accept (or from a completed TLS handshake) until a complete MQTT CONNECT packet is received (default `30s`). This bounds silent TCP/TLS connections that never speak MQTT. If the deadline cannot be applied, the connection is closed instead of being left unbounded.
- `-max-handshakes n`
  - Maximum number of concurrent TLS handshakes (default `64`, range `1`-`4096`). Additional connections wait in the listen backlog; the limit covers the handshake phase only, not established MQTT sessions.
- `-shutdown-timeout duration`
  - Single absolute shutdown budget (default `10s`). The same deadline covers MQTT DISCONNECT notifications, closing the underlying connections, waiting for connection handlers and MQTT server cleanup, plus a fixed small local grace that does not grow with the number of connections. A peer that stops reading cannot extend the budget through a blocked notification write. If cleanup still exceeds the budget, the error is reported, the summary records an incomplete shutdown and the process exits with code `1`; the tool never waits forever. Synchronous file/terminal I/O cannot be interrupted and is only reported after the fact.
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

The optional certificate-script integration tests are enabled explicitly (bash and OpenSSL are required; without them the tests skip with a reason, and the default suite does not fail):

```sh
MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScript -v
```

This runs the material generation test, the wildcard/hostname-match test and the invalid-SAN rejection test.

Cross-compilation only proves that the target builds; it is not a run-time verification on Windows or macOS:

```sh
GOOS=windows GOARCH=amd64 go build .
GOOS=darwin  GOARCH=arm64 go build .
```

## Screenshot

![Screenshot](screenshot-en.png)

## Common procedures (best practices)

The steps below go from plain connectivity to one-way TLS, mTLS and server-certificate validation. Change one condition per step. After switching certificates, modes or negative cases, restart the device with a new Client ID and Clean Session/Start so session resumption cannot hide the result. Use a new `-results` directory for each run; the tool refuses to overwrite existing files.

### 0. Generate materials

```sh
bash scripts/gen-test-certs.sh -o certs/mtls-check \
    --san-dns "mqtt.example.com,localhost" \
    --san-ip "192.168.1.10,127.0.0.1" \
    --device-id dev-001
```

Set the SAN values to the exact hostname/IP the device connects to.

`--san-dns` also accepts a standard wildcard, but only as the complete leftmost label: `*.example.com` is valid, while `*example.com`, `foo.*.example.com`, multiple asterisks and empty labels are rejected with exit code `2`. Quote the value so the shell cannot expand it, and connect the device to a name the wildcard actually covers:

```sh
bash scripts/gen-test-certs.sh -o certs/wildcard \
    --san-dns '*.example.com' --san-ip ''
```

The wildcard is written into the SAN literally (never as a file name), and the script's own check uses concrete names: `mqtt.example.com` must verify, while `example.com` and `a.b.example.com` must fail. Verify it yourself:

```sh
openssl x509 -in certs/wildcard/server/server.crt -noout -ext subjectAltName
openssl verify -verify_hostname mqtt.example.com \
    -CAfile certs/wildcard/device/ca-a.crt certs/wildcard/server/server.crt
```

`--san-ip ''` generates a certificate without IP SANs; omit it to keep the default `127.0.0.1`. A wildcard is a matching pattern, not a hostname: the device must connect to a name below it and still import the server trust anchor.

Material roles:

| Role | Files | Used by |
| --- | --- | --- |
| Server certificate chain / key | `server/server.crt`, `server/server.key` | Tool `-server-cert` / `-server-key` |
| Trust anchor for device certificates | `server/ca-a.crt` | Tool `-client-ca` |
| Device certificate / key | `device/device.crt`, `device/device.key` | Device |
| Server trust anchor for the device | `device/ca-a.crt` | Device |
| Negative device certificates | `device/negative/` | Device |
| Negative server certificates | `server/negative/` | Tool |

For a device that only accepts traditional key files, add `--leaf-key-format traditional`; to match a device's key capability, use `--key-alg ec-p256`, `rsa2048`, etc. The script writes separate PEM files. Do not concatenate them into a "single certificate" or "triple" package until the vendor documents the field order, separators and encoding. Never import a CA private key into the device.

### 1. Verify the server without the device

```sh
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert certs/mtls-check/server/server.crt \
    -server-key  certs/mtls-check/server/server.key \
    -client-ca   certs/mtls-check/server/ca-a.crt
```

On another terminal or machine:

```sh
# Expected: handshake failure (no client certificate)
openssl s_client -connect 127.0.0.1:8883 -servername localhost \
    -CAfile certs/mtls-check/device/ca-a.crt </dev/null

# Expected: Verify return code: 0 (ok)
openssl s_client -connect 127.0.0.1:8883 -servername localhost \
    -CAfile certs/mtls-check/device/ca-a.crt \
    -cert certs/mtls-check/device/device.crt \
    -key  certs/mtls-check/device/device.key </dev/null
```

Use a `-servername` and destination address covered by the certificate SAN. If the SAN does not include them, regenerate the materials or connect to a covered address. Stop this server before the device runs.

### 2. Test whether the device can connect (plain MQTT)

```sh
./MQTTDeviceTestingTool -mode plain -listen 0.0.0.0:1883 \
    -results results/00-plain -run-id device-plain \
    -device-model "YOUR-MODEL" -firmware "YOUR-FIRMWARE" \
    -test-case plain-baseline \
    -expect-connect accepted
```

On the device: disable TLS and connect to TCP 1883 on the test machine. If `-users` is used, enter the configured username and password. The tool does not change firewall rules.

Expected: `mqtt_auth=accepted` in `summary.json`. Without `-users`, all connections are allowed by default; if the connection is rejected, check the `auth` section of `-users` first. If the device is TLS-only, this step is expected to fail and you can proceed to step 3. Otherwise, fix networking, port, firewall, Client ID and MQTT authentication first.

### 3. Test whether the device can complete one-way TLS

```sh
./MQTTDeviceTestingTool -mode tls -listen 0.0.0.0:8883 \
    -server-cert certs/mtls-check/server/server.crt \
    -server-key  certs/mtls-check/server/server.key \
    -results results/01-tls -run-id device-tls-baseline \
    -device-model "YOUR-MODEL" -firmware "YOUR-FIRMWARE" \
    -test-case tls-baseline \
    -expect-tls success -expect-connect accepted
```

On the device, import `device/ca-a.crt` as the server trust anchor (or the CA that signed the server certificate), connect to an address covered by the certificate SAN, and do not configure a client certificate for this run. Make sure the device can reach the test machine on TCP 8883; the tool does not change firewall rules. Use a new `-results` directory and a new `-run-id` for every repeated run; the tool refuses to overwrite existing result files.

Expected: the device connects; `summary.json` shows `tls=success`, `mqtt_auth=accepted` and `client_certificate=absent` (the server did not ask for one). MQTT authentication is controlled by `-users`; without it, the default policy allows all connections. If this fails, fix the trust anchor, SAN, TLS version, device clock, port and firewall first; a failure here does not yet say anything about mTLS. Do not add `-expect-client-cert` in this mode: the tool correctly reports it as `undetermined`, because one-way TLS never asks for a device certificate.

### 4. Test whether the device supports mTLS

A one-way TLS server never requests a device certificate, so a device with a certificate and one without behave identically. Determining mTLS support requires two `-mode mtls` connections.

#### 4.1 Device without a client certificate

Start the strict mTLS server, keeping the device configuration from the baseline (no client certificate):

```sh
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert certs/mtls-check/server/server.crt \
    -server-key  certs/mtls-check/server/server.key \
    -client-ca   certs/mtls-check/server/ca-a.crt \
    -results results/02-mtls-no-device-cert -run-id device-mtls-no-cert \
    -device-model "YOUR-MODEL" -firmware "YOUR-FIRMWARE" \
    -test-case mtls-without-device-cert
```

Restart the device and use a new Client ID with Clean Session/Start so an old TLS/MQTT session cannot mask the result.

Expected: no accepted MQTT CONNACK. The tool usually records `tls/handshake/failure` with `client_certificate_missing` (heuristic) or an alert such as `certificate_required`; TLS 1.3 may surface the failure only when MQTT data is exchanged, so read the whole connection rather than just the TLS line. `timeout` or `peer_closed` alone does not prove that the device sent no certificate. The connection verdict may be `undetermined` here because no expectation was specified; that is normal.

If the device connects anyway and the summary shows `client_certificate=present`, the device already has a certificate configured or a TLS-terminating proxy is in the path. Record the fingerprint, make sure `did_resume=false`, and rule out the proxy before concluding.

#### 4.2 Device with a client certificate

Import `device/device.crt` and `device/device.key` into the device as its client certificate and private key. Import intermediate certificates only if the vendor requires them. Keep `device/ca-a.crt` as the server trust anchor, restart the device, use a new Client ID and Clean Session/Start, and never import a CA private key.

```sh
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert certs/mtls-check/server/server.crt \
    -server-key  certs/mtls-check/server/server.key \
    -client-ca   certs/mtls-check/server/ca-a.crt \
    -results results/03-mtls-with-device-cert -run-id device-mtls-with-cert \
    -device-model "YOUR-MODEL" -firmware "YOUR-FIRMWARE" \
    -test-case mtls-with-device-cert \
    -expect-tls success -expect-connect accepted -expect-client-cert present
```

Expected: `verdict=pass`, `tls=success`, `client_certificate=present`, and the certificate fingerprint in `summary.json` matches `device/device.crt`:

```sh
openssl x509 -in certs/mtls-check/device/device.crt -noout -fingerprint -sha256
```

`did_resume=true` means the TLS session was resumed; restart the device or clear its session cache and repeat. A TLS-terminating proxy would make the tool see the proxy's certificate instead of the device's.

- If the device refuses to import the files or reports a format/field/algorithm error, record this as an import failure. Try `--leaf-key-format traditional`, another `--key-alg`, or the vendor's required package format. It is not a server rejection.
- If the handshake fails with `certificate_verification`, check the clientAuth EKU, validity period, device clock, missing intermediate certificates, and whether the device certificate is signed by the CA in `-client-ca`.
- If the firmware has no way to import a client certificate and key, the firmware cannot be configured for mTLS; ask the vendor whether the hardware/firmware supports it.

#### Verdict

| One-way TLS baseline | mTLS without device certificate | mTLS with device certificate | Conclusion |
| --- | --- | --- | --- |
| pass | no accepted CONNACK; certificate missing | pass; `client_certificate=present`; fingerprint matches | Device supports mTLS. |
| pass | no accepted CONNACK; certificate missing | import impossible or handshake fails with known-good material | Device uses one-way TLS in this firmware/configuration; mTLS is not usable. Confirm with the vendor. |
| pass | connects; `client_certificate=present` | — | A certificate was already configured, or a TLS proxy is in the path. Inspect the fingerprint and rule out the proxy. |
| fails | — | — | No conclusion; fix server-auth TLS (trust anchor, SAN, clock, TLS version) first. |
| pass | not tested | not tested | Cannot tell; one-way TLS never requests a client certificate. |

A failed handshake alone does not prove that mTLS is unsupported: wrong key format, algorithm, chain, clock or EKU can all cause it. Only report "unsupported" after the material is known-good (for example, the same material is accepted by `openssl s_client`) and the vendor confirms there is no way to configure it.

Optional negative cases: repeat with the CA B-signed, expired, not-yet-valid and wrong-EKU device certificates under `device/negative/`; all should fail the handshake. If the device cannot import an invalid certificate, record that as a device import failure, not as a server rejection. Some devices perform certificate-based authentication at the MQTT application layer (for example, a certificate or signature in the CONNECT payload); these steps test TLS client certificates only.

### 5. Optional: does the device validate the server certificate?

This separates "TLS works" from "the device actually verifies the server". Keep the device trusting CA A and start one-way TLS with a certificate signed by CA B:

```sh
./MQTTDeviceTestingTool -mode tls -listen 0.0.0.0:8883 \
    -server-cert certs/mtls-check/server/negative/server-by-ca-b.crt \
    -server-key  certs/mtls-check/server/negative/server-by-ca-b.key
```

The device should reject the connection at the TLS stage. The other negative cases under `server/negative/` cover a hostname mismatch and an expired certificate; start the tool with `-allow-invalid-server-cert` only for the expired case. A device that accepts an untrusted server certificate does not provide meaningful server authentication, even if it can connect.

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

常用的设备连接、单向 TLS、mTLS 与服务器证书校验步骤见文末“常用实操步骤（最佳实践）”。

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

文件与输出路径保护：

- 所有输入材料（`-server-cert`、`-server-key`、`-server-key-password-file`、`-client-ca`、`-users`）与所有输出（`-log`、`-log-status`、`-log-data`、`-results/events.jsonl`、`-results/summary.json`）在打开、截断或创建之前统一比较。
- 同一文件通过相对/绝对路径、`.`/`..`、符号链接、硬链接或父目录符号链接指向时，启动直接失败（退出码 `2`），错误会同时写出两个参数名，已有文件内容一个字节也不会被改动。
- 两个输入参数可以共用同一文件；输入不能作为输出，两个输出也不能解析为同一文件。检测到冲突时不会创建结果目录。
- 路径检查与实际打开是两次操作，两者之间的并发替换无法完全防御；不要把他人的实时重写目录作为输出目录，工具也不作此承诺。

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
  - TLS 握手的绝对期限（默认 `10s`），同时覆盖读与写：对端发送 ClientHello 后停止读取时，服务端阻塞的写入也会在期限到点后失败，无法长期占住握手槽。握手成功后、进入 MQTT CONNECT 阶段前清除该期限；超时、失败或服务端关闭都会关闭连接并释放槽位，关闭流程会立即取消正在进行的握手，而不是等期限自然到点。
- `-connect-timeout 时长`
  - 从接入（或 TLS 握手完成）到收到完整 MQTT CONNECT 的最长等待时间（默认 `30s`），用于关闭始终不发送 MQTT 报文的静默连接。如果期限无法设置，工具会直接关闭连接，而不是放任无限等待。
- `-max-handshakes 数量`
  - 最大并发 TLS 握手数（默认 `64`，范围 `1`-`4096`）。超出的连接在监听 backlog 中排队；该限制只覆盖握手阶段，不限制已建立的 MQTT 会话。
- `-shutdown-timeout 时长`
  - 关闭时使用的一次性绝对预算（默认 `10s`）。同一期限覆盖向客户端写 MQTT DISCONNECT、关闭底层连接、等待各连接处理与 MQTT 服务端收尾，另加一个不随连接数增长的固定本地收尾余量。对端不读取时，通知写会被同一期限打断，不会把预算延长或按连接重复累计。即使如此仍未完成时，工具会报告错误、在 `summary.json` 记录关闭未完成，并以退出码 `1` 结束，不会无限等待。同步文件/终端 I/O 无法中断，只能在超时后如实报告。
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
MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScript -v
```

该命令覆盖普通材料、通配符/主机名匹配与非法 SAN 拒绝；只运行单项时可加子测试名，例如 `-run TestOpenSSLScriptMaterial`。

### 错误排查

先看启动摘要确认实际模式、TLS 版本范围和设备证书策略，再看 `events.jsonl` 的阶段与 `certainty`，不要只根据终端最后一行判断。

| 现象 | 优先检查 |
| --- | --- |
| 启动退出码 `2` | 参数冲突、未知模式、TLS 版本、数值范围、文件不存在；错误写在 stderr |
| 启动退出码 `2` 且提示 `file path conflict` | 输入材料与 `-log`/`-log-status`/`-log-data`/`-results` 输出指向同一文件（含符号链接、硬链接、`./` 等别名）；文件未被打开或截断，改用不同路径 |
| 启动退出码 `1` | 证书/私钥读取或解析、私钥密码、私钥不配对、链结构、监听端口或结果目录 |
| 关闭耗时接近 `-shutdown-timeout` 或退出码 `1` | 对端不读取导致 DISCONNECT 写阻塞、连接处理未退出；期限为一次性绝对预算，不按连接累加，查看 `summary.json` 的关闭清理错误 |
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

可选证书脚本集成测试（需要 bash 与 OpenSSL 1.1.1+；缺少时明确 Skip，不影响基础测试）：

```sh
MQTT_TEST_OPENSSL_SCRIPT=1 go test -run TestOpenSSLScript -v
```

该命令同时运行材料生成、通配符/主机名匹配和非法 SAN 拒绝三组测试。

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

## 常用实操步骤（最佳实践）

以下按“普通连接 → 单向 TLS → mTLS → 服务器证书校验”的顺序操作。每一步只改变一个条件；切换证书、模式或重测反例时，重启设备并使用新的 Client ID 与 Clean Session/Start，避免 TLS/MQTT 会话恢复掩盖结果。每次运行使用新的 `-results` 目录，工具不会覆盖已有文件。

### 0. 准备材料

```sh
bash scripts/gen-test-certs.sh -o "certs/设备测试-01" \
    --san-dns "mqtt.example.com,localhost" \
    --san-ip "192.168.1.10,127.0.0.1" \
    --device-id dev-001
```

`--san-dns`/`--san-ip` 必须包含设备实际连接使用的主机名或 IP。材料角色：

| 角色 | 文件 | 使用方 |
| --- | --- | --- |
| 服务器证书链 / 私钥 | `server/server.crt`、`server/server.key` | 工具 `-server-cert` / `-server-key` |
| 验证设备证书的信任锚 | `server/ca-a.crt` | 工具 `-client-ca` |
| 设备证书 / 私钥 | `device/device.crt`、`device/device.key` | 设备 |
| 设备验证服务器的信任锚 | `device/ca-a.crt` | 设备 |
| 反例设备证书 | `device/negative/` | 设备 |
| 反例服务器证书 | `server/negative/` | 工具 |

设备只接受传统私钥编码时加 `--leaf-key-format traditional`；只支持特定算法时加 `--key-alg ec-p256` 等。脚本只输出独立 PEM 文件；在厂商确认“单一证书/三元组”的字段顺序、拼接方式和编码前，不要自行拼接。任何 CA 私钥都不要导入设备。

需要匹配任意单层子域名时，`--san-dns` 支持标准通配符，但星号必须覆盖完整的最左侧标签：`*.example.com` 合法；`*example.com`、`foo.*.example.com`、多个星号与空标签会被拒绝（退出码 `2`）。用单引号防止 shell 展开，并让设备连接到通配符实际覆盖的主机名：

```sh
bash scripts/gen-test-certs.sh -o "certs/通配符测试" \
    --san-dns '*.example.com' --san-ip ''
```

通配符会原样写入 SAN（不会变成文件名或被当成 CN），脚本自检使用具体主机名：`mqtt.example.com` 必须通过，`example.com` 与 `a.b.example.com` 必须被拒绝。可自行核对：

```sh
openssl x509 -in "certs/通配符测试/server/server.crt" -noout -ext subjectAltName
openssl verify -verify_hostname mqtt.example.com \
    -CAfile "certs/通配符测试/device/ca-a.crt" \
    "certs/通配符测试/server/server.crt"
```

`--san-ip ''` 表示不生成 IP SAN；省略该参数则保留默认 `127.0.0.1`。通配符是匹配模式而不是主机名：设备必须连接到其覆盖的主机名，并仍将 `device/ca-a.crt` 导入为服务器信任锚。

### 1. 先用软件客户端确认服务端

```sh
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert "certs/设备测试-01/server/server.crt" \
    -server-key  "certs/设备测试-01/server/server.key" \
    -client-ca   "certs/设备测试-01/server/ca-a.crt"
```

在另一个终端或另一台电脑上验证：

```sh
# 预期：握手失败（没有客户端证书）
openssl s_client -connect 127.0.0.1:8883 -servername localhost \
    -CAfile "certs/设备测试-01/device/ca-a.crt" </dev/null

# 预期：Verify return code: 0 (ok)
openssl s_client -connect 127.0.0.1:8883 -servername localhost \
    -CAfile "certs/设备测试-01/device/ca-a.crt" \
    -cert "certs/设备测试-01/device/device.crt" \
    -key  "certs/设备测试-01/device/device.key" </dev/null
```

`-servername` 和连接地址必须在证书 SAN 范围内；如果不匹配，请重新生成材料或改用被覆盖的地址。完成本步后停止这个服务，再开始设备测试。

### 2. 测试设备能否连接（普通 MQTT）

```sh
./MQTTDeviceTestingTool -mode plain -listen 0.0.0.0:1883 \
    -results "results/00-普通连接" -run-id device-plain \
    -device-model "设备型号" -firmware "固件版本" \
    -test-case plain-baseline \
    -expect-connect accepted
```

设备侧：关闭 TLS，连接测试电脑的 TCP 1883。如果使用 `-users`，按配置填写用户名和密码。工具不会修改防火墙规则。

预期：`summary.json` 中 `mqtt_auth=accepted`。未提供 `-users` 时默认允许所有连接；如果被拒绝，先检查 `-users` 的 `auth` 配置。如果设备只支持 TLS，本步失败是预期的，可直接进入第 3 步；如果设备本应支持普通 MQTT，先解决网络、端口、防火墙、Client ID 和 MQTT 认证。

### 3. 测试设备能否完成单向 TLS

```sh
./MQTTDeviceTestingTool -mode tls -listen 0.0.0.0:8883 \
    -server-cert "certs/设备测试-01/server/server.crt" \
    -server-key  "certs/设备测试-01/server/server.key" \
    -results "results/01-单向TLS" -run-id device-tls-baseline \
    -device-model "设备型号" -firmware "固件版本" \
    -test-case tls-baseline \
    -expect-tls success -expect-connect accepted
```

设备侧：导入 `device/ca-a.crt` 作为服务器信任锚（或签发服务器证书的 CA），连接地址必须与服务器证书 SAN 匹配，本轮不要配置客户端证书。确保设备能访问测试电脑的 TCP 8883 端口；本工具不会修改防火墙规则。每次重测都使用新的 `-results` 目录和新的 `-run-id`，工具不会覆盖已有结果文件。

预期：设备连接成功，`summary.json` 中 `tls=success`、`mqtt_auth=accepted`、`client_certificate=absent`（服务器没有索取）。MQTT 认证由 `-users` 控制；未提供时默认全部允许。如果这一步失败，先检查信任锚、SAN、TLS 版本、设备时钟、端口与防火墙；此时还不能对 mTLS 下结论。不要在本模式加 `-expect-client-cert`，工具会正确判为 `undetermined`，因为单向 TLS 从不请求设备证书。

### 4. 测试设备是否支持 mTLS

单向 TLS 服务器从不请求设备证书，因此“设备有证书”和“设备没证书”在单向 TLS 下表现相同。要判断 mTLS，需要在 `-mode mtls` 下做两次连接。

#### 4.1 设备不配置客户端证书

保持设备与第 3 步相同的配置（未配置客户端证书），改用强制 mTLS：

```sh
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert "certs/设备测试-01/server/server.crt" \
    -server-key  "certs/设备测试-01/server/server.key" \
    -client-ca   "certs/设备测试-01/server/ca-a.crt" \
    -results "results/02-mTLS-无设备证书" -run-id device-mtls-no-cert \
    -device-model "设备型号" -firmware "固件版本" \
    -test-case mtls-without-device-cert
```

重启设备，并使用新的 Client ID 与 Clean Session/Start，避免旧的 TLS/MQTT 会话掩盖结果。

预期：没有成功的 CONNACK。工具侧通常记录 `tls/handshake/failure`，原因为 `client_certificate_missing`（启发式）或 alert `certificate_required`；TLS 1.3 可能到 MQTT 数据交换阶段才显现失败，因此要看整个连接，而不能只看 TLS 那一行。仅出现 `timeout` 或 `peer_closed` 不能证明设备没有发送证书。本步没有指定 `-expect-*`，汇总判定为 `undetermined` 属正常，应阅读观测事实。

如果设备仍然连上，且汇总中 `client_certificate=present`：设备原本就配置了证书，或链路上存在终止 TLS 的代理。记录证书指纹、确认 `did_resume=false`，排除代理和会话恢复后再下结论。

#### 4.2 设备配置客户端证书

按厂商说明把 `device/device.crt` 与 `device/device.key` 导入设备，作为客户端证书和私钥；只有厂商明确要求时才附带中间证书。保持 `device/ca-a.crt` 为服务器信任锚，重启设备，使用新的 Client ID 与 Clean Session/Start，不要导入任何 CA 私钥。

```sh
./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
    -server-cert "certs/设备测试-01/server/server.crt" \
    -server-key  "certs/设备测试-01/server/server.key" \
    -client-ca   "certs/设备测试-01/server/ca-a.crt" \
    -results "results/03-mTLS-有设备证书" -run-id device-mtls-with-cert \
    -device-model "设备型号" -firmware "固件版本" \
    -test-case mtls-with-device-cert \
    -expect-tls success -expect-connect accepted -expect-client-cert present
```

预期：`verdict=pass`、`tls=success`、`client_certificate=present`，且 `summary.json` 中的证书指纹与 `device/device.crt` 一致：

```sh
openssl x509 -in "certs/设备测试-01/device/device.crt" -noout -fingerprint -sha256
```

`did_resume=true` 表示 TLS 会话恢复，应重启设备或清除其会话缓存后重测；链路上有终止 TLS 的代理时，工具看到的是代理的证书。

- 设备拒绝导入、提示格式/字段/算法错误时，记录为“导入失败”。可尝试 `--leaf-key-format traditional`、其他 `--key-alg` 或厂商要求的打包格式；这不是服务器拒绝了证书。
- 握手失败且原因为 `certificate_verification` 时，检查 clientAuth 用途、有效期、设备时钟、是否缺少中间证书，以及设备证书是否由 `-client-ca` 指定的 CA 签发。
- 固件没有导入客户端证书和私钥的入口时，该固件无法配置 mTLS；硬件是否支持需向厂商确认。

#### 结论

| 单向 TLS 基线 | mTLS + 设备无证书 | mTLS + 设备有证书 | 结论 |
| --- | --- | --- | --- |
| 通过 | 无成功 CONNACK；证书缺失 | 通过；`client_certificate=present`；指纹一致 | 设备支持 mTLS。 |
| 通过 | 无成功 CONNACK；证书缺失 | 无法导入，或使用已知正确材料仍握手失败 | 该固件/配置只使用单向 TLS；mTLS 不可用，需向厂商确认。 |
| 通过 | 仍能连接；`client_certificate=present` | — | 设备原本已有证书，或链路中有 TLS 代理；检查指纹并排除代理。 |
| 失败 | — | — | 暂无结论；先修复服务器认证 TLS（信任锚、SAN、时钟、TLS 版本）。 |
| 通过 | 未测试 | 未测试 | 无法判断；单向 TLS 从不请求设备证书。 |

握手失败本身不能证明“不支持 mTLS”：私钥格式、算法、证书链、设备时钟或 EKU 错误都会导致失败。只有在材料已确认可用（例如同一套材料能被 `openssl s_client` 接受）且厂商确认设备没有配置入口后，才应报告“不支持”。

可选反例：使用 `device/negative/` 下的 CA B 签发、过期、未生效、错误 EKU 证书分别重测，预期均为握手失败；设备无法导入无效证书时，记录为“设备导入失败”，不要记作“服务器拒绝了该证书”。有些设备把证书认证放在 MQTT 应用层（例如 CONNECT 负载中的证书或签名），本组步骤只验证 TLS 客户端证书。

### 5. 测试设备是否校验服务器证书（可选）

这一步区分“能建立 TLS”和“真的验证服务器身份”。保持设备信任 CA A，改用 CA B 签发的服务器证书：

```sh
./MQTTDeviceTestingTool -mode tls -listen 0.0.0.0:8883 \
    -server-cert "certs/设备测试-01/server/negative/server-by-ca-b.crt" \
    -server-key  "certs/设备测试-01/server/negative/server-by-ca-b.key"
```

设备应在 TLS 阶段拒绝连接。`server/negative/` 下还有主机名不匹配和已过期的服务器证书反例；使用过期证书启动工具时需加 `-allow-invalid-server-cert`。如果设备接受不受信任的服务器证书，即使能连接，也不具备有意义的服务器身份验证。

## 许可

Copyright (c) 2022 [神楽坂雅詩](https://github.com/kagurazakayashi)@[Tongdy](https://github.com/tongdysoft) MQTT 客户端测试工具。
您对“软件”的复制、使用、修改及分发受木兰宽松许可证，第 2 版的条款的约束。
