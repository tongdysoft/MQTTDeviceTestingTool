#!/usr/bin/env bash
# =============================================================================
# MQTTDeviceTestingTool —— 真实设备人工测试用 OpenSSL 证书生成脚本
#
# 用途：生成一套长期可用、可重复核对的证书材料，供真实设备连接本工具的
#       tls / mtls 模式时使用。本脚本与 go test 中的动态证书工厂完全独立，
#       基础 go test 不依赖本脚本，也不依赖 OpenSSL。
#
# 支持环境：Linux、macOS、Windows 上的 MSYS2（bash）与 Git Bash 同类环境。
# 依赖：bash >= 4、OpenSSL >= 1.1.1（3.x 已实测）。不支持 LibreSSL。
#
# 主要输出：CA 管理材料、服务器材料、设备导入材料、反例材料、清单与说明。
# 设备导入目录中绝不含 CA 私钥或服务器私钥。
#
# 参数说明见 --help；完整字段说明见生成目录内的“说明.txt”和“manifest.txt”。
# =============================================================================

set -euo pipefail
umask 077

# 先检查 bash 版本：脚本后面的 mapfile 等语法需要 bash 4+，
# 否则会在依赖检查之前就出现难以理解的错误。
if ((BASH_VERSINFO[0] < 4)); then
  printf '错误：需要 bash 4.0 及以上版本，当前为 %s；macOS 可先 brew install bash。\n' "$BASH_VERSION" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# MSYS2 / Cygwin：默认会把以 / 开头的参数（例如 -subj "/CN=..."）改写为
# Windows 路径。证书参数里大量使用 /，必须彻底关闭参数自动转换，
# 否则会出现 subject 被改成 "C:/Program Files/Git/CN=..." 一类的错误。
# 需要传给原生 Windows 版 openssl 的路径由 to_openssl_path 单独转换。
# ---------------------------------------------------------------------------
case "$(uname -s 2>/dev/null || echo unknown)" in
  MINGW* | MSYS* | CYGWIN*)
    export MSYS2_ARG_CONV_EXCL='*'
    export MSYS_NO_PATHCONV=1
    ;;
esac

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)

STEP_NO=1
OPENSSL_LOG=""
WORK_DIR=""
OUT_DIR=""

die() {
  printf '错误：%s\n' "$*" >&2
  exit 1
}

die_usage() {
  printf '参数错误：%s\n\n' "$*" >&2
  usage >&2
  exit 2
}

info() {
  printf '%s\n' "$*"
}

step() {
  printf '\n[%d/8] %s\n' "$STEP_NO" "$*"
  STEP_NO=$((STEP_NO + 1))
}

cleanup() {
  local status=$?
  if ((status != 0)) && [[ -n $WORK_DIR && -d $WORK_DIR ]]; then
    # 失败时保留 OpenSSL 命令日志，便于定位问题；成功时日志不进入最终输出。
    if [[ -n $OPENSSL_LOG && -f $OPENSSL_LOG ]]; then
      local failed_log="${WORK_DIR}.失败日志.log"
      if cp -- "$OPENSSL_LOG" "$failed_log" 2>/dev/null; then
        printf '已保存 OpenSSL 命令日志：%s\n' "$failed_log" >&2
      else
        printf '警告：无法保存 OpenSSL 命令日志到 %s\n' "$failed_log" >&2
      fi
    fi
    rm -rf -- "$WORK_DIR"
    printf '已删除未完成的输出目录：%s\n' "$WORK_DIR" >&2
  fi
  exit "$status"
}
trap cleanup EXIT

usage() {
  cat <<'EOF'
用法：bash scripts/gen-test-certs.sh [选项]

生成供真实设备人工测试使用的证书材料（CA、服务器、设备、反例、清单）。

选项：
  -o, --out DIR             输出目录；必须不存在。默认
                            <仓库>/certs/test-certs-YYYYmmdd-HHMMSS
      --server-name NAME    服务器证书 CN（仅 ASCII），默认 mqtt-server
      --san-dns LIST        服务器证书 DNS SAN，逗号分隔，默认 localhost
      --san-ip LIST         服务器证书 IP SAN，逗号分隔，默认 127.0.0.1
      --device-id NAME      设备证书 CN（设备标识），仅允许 ASCII 字母、数字、
                            . _ -，默认 device-001
      --validity-days N     叶证书有效天数，默认 365（1-3650）
      --ca-validity-days N  CA 有效天数，默认 3650（1-7300），且必须至少比
                            叶证书有效天数多 30 天（覆盖“尚未生效”反例的时间窗）
      --key-alg ALG         叶证书密钥算法：
                            rsa2048（默认）| rsa3072 | rsa4096 | ec-p256 | ec-p384
      --ca-key-alg ALG      CA 密钥算法，取值同上，默认 rsa2048
      --leaf-key-format F   叶证书私钥编码：pkcs8（默认）| traditional
                            traditional 表示 RSA 用 PKCS#1、EC 用 SEC1，
                            便于只接受传统 PEM 类型的设备
  -h, --help                显示本帮助

示例：
  bash scripts/gen-test-certs.sh
  bash scripts/gen-test-certs.sh -o "D:/设备测试/证书 1" \
      --san-dns "mqtt.example.com,localhost" \
      --san-ip "192.168.1.10,127.0.0.1" --device-id dev-001 \
      --validity-days 180 --key-alg ec-p256

注意：
  * 输出目录已存在时脚本直接报错，不会覆盖任何已有密钥或材料。
  * 生成过程失败会删除本次未完成的临时目录，避免残缺材料被误用。
  * 设备“单一证书 / 三元组”的拼接格式尚未确认，本脚本只输出独立 PEM 文件。
EOF
}

# ---------------------------------------------------------------------------
# 参数解析
# ---------------------------------------------------------------------------
SERVER_NAME="mqtt-server"
SAN_DNS="localhost"
SAN_IP="127.0.0.1"
DEVICE_ID="device-001"
VALIDITY_DAYS=365
CA_VALIDITY_DAYS=3650
KEY_ALG="rsa2048"
CA_KEY_ALG="rsa2048"
LEAF_KEY_FORMAT="pkcs8"

while (($# > 0)); do
  case "$1" in
    -o | --out)
      (($# >= 2)) || die_usage "$1 需要一个目录参数"
      OUT_DIR=$2
      shift 2
      ;;
    --server-name)
      (($# >= 2)) || die_usage "$1 需要一个名称"
      SERVER_NAME=$2
      shift 2
      ;;
    --san-dns)
      (($# >= 2)) || die_usage "$1 需要逗号分隔的 DNS 列表"
      SAN_DNS=$2
      shift 2
      ;;
    --san-ip)
      (($# >= 2)) || die_usage "$1 需要逗号分隔的 IP 列表"
      SAN_IP=$2
      shift 2
      ;;
    --device-id)
      (($# >= 2)) || die_usage "$1 需要一个设备标识"
      DEVICE_ID=$2
      shift 2
      ;;
    --validity-days)
      (($# >= 2)) || die_usage "$1 需要天数"
      VALIDITY_DAYS=$2
      shift 2
      ;;
    --ca-validity-days)
      (($# >= 2)) || die_usage "$1 需要天数"
      CA_VALIDITY_DAYS=$2
      shift 2
      ;;
    --key-alg)
      (($# >= 2)) || die_usage "$1 需要算法名"
      KEY_ALG=$2
      shift 2
      ;;
    --ca-key-alg)
      (($# >= 2)) || die_usage "$1 需要算法名"
      CA_KEY_ALG=$2
      shift 2
      ;;
    --leaf-key-format)
      (($# >= 2)) || die_usage "$1 需要 pkcs8 或 traditional"
      LEAF_KEY_FORMAT=$2
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      die_usage "未知参数：$1"
      ;;
  esac
done

# ---------------------------------------------------------------------------
# 参数校验
# ---------------------------------------------------------------------------
validate_uint() {
  local label=$1 value=$2 min=$3 max=$4
  [[ $value =~ ^[0-9]+$ ]] || die_usage "$label 必须是整数：$value"
  # 显式按十进制解释，避免 "08" 被 bash 当成非法八进制。
  local n=$((10#$value))
  ((n >= min && n <= max)) || die_usage "$label 必须在 $min-$max 之间：$value"
}

validate_ascii_name() {
  local label=$1 value=$2
  [[ $value =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] \
    || die_usage "$label 只允许 ASCII 字母、数字、点、下划线和连字符（1-64 字符）：$value"
}

validate_key_alg() {
  local label=$1 value=$2
  case "$value" in
    rsa2048 | rsa3072 | rsa4096 | ec-p256 | ec-p384) ;;
    *) die_usage "$label 不支持：$value（可选 rsa2048、rsa3072、rsa4096、ec-p256、ec-p384）" ;;
  esac
}

validate_uint "有效期天数" "$VALIDITY_DAYS" 1 3650
validate_uint "CA 有效期天数" "$CA_VALIDITY_DAYS" 1 7300
((CA_VALIDITY_DAYS >= VALIDITY_DAYS + 30)) \
  || die_usage "CA 有效期必须至少比叶证书多 30 天（当前 $CA_VALIDITY_DAYS < $((VALIDITY_DAYS + 30))），否则未生效反例的时间窗会超出 CA 有效期"
validate_ascii_name "服务器名称" "$SERVER_NAME"
validate_ascii_name "设备标识" "$DEVICE_ID"
validate_key_alg "叶证书密钥算法" "$KEY_ALG"
validate_key_alg "CA 密钥算法" "$CA_KEY_ALG"
case "$LEAF_KEY_FORMAT" in
  pkcs8 | traditional) ;;
  *) die_usage "叶证书私钥编码只能是 pkcs8 或 traditional：$LEAF_KEY_FORMAT" ;;
esac

# 去掉逗号两侧空白，得到数组；空列表表示不使用该类 SAN。
split_list() {
  local value=$1 item
  local -a result=()
  local IFS=','
  for item in $value; do
    item=${item#"${item%%[![:space:]]*}"}
    item=${item%"${item##*[![:space:]]}"}
    [[ -n $item ]] && result+=("$item")
  done
  # 空列表不输出任何行，避免 mapfile 得到一个空元素。
  if ((${#result[@]} > 0)); then
    printf '%s\n' "${result[@]}"
  fi
}

mapfile -t DNS_LIST < <(split_list "$SAN_DNS")
mapfile -t IP_LIST < <(split_list "$SAN_IP")
((${#DNS_LIST[@]} + ${#IP_LIST[@]} > 0)) || die_usage "至少要提供一个 DNS 或 IP SAN"

for d in "${DNS_LIST[@]+"${DNS_LIST[@]}"}"; do
  [[ $d =~ ^\*?[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]] \
    || die_usage "DNS SAN 不合法（仅支持 ASCII 主机名或前置 * 的通配符）：$d"
done
for ip in "${IP_LIST[@]+"${IP_LIST[@]}"}"; do
  if [[ $ip == *:* ]]; then
    [[ $ip =~ ^[0-9A-Fa-f:]+$ ]] || die_usage "IPv6 SAN 不合法：$ip"
  else
    [[ $ip =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || die_usage "IPv4 SAN 不合法：$ip"
    local_ifs=$IFS
    IFS='.'
    for octet in $ip; do
      ((10#$octet <= 255)) || die_usage "IPv4 SAN 不合法：$ip"
    done
    IFS=$local_ifs
  fi
done

# ---------------------------------------------------------------------------
# 依赖检查
# ---------------------------------------------------------------------------
step "检查 Bash 与 OpenSSL 环境"

OPENSSL_BIN=$(command -v openssl || true)
[[ -n $OPENSSL_BIN ]] || die "未找到 openssl。请安装 OpenSSL 1.1.1 或更高版本并加入 PATH。"
[[ -x $OPENSSL_BIN ]] || die "openssl 不可执行：$OPENSSL_BIN"

OPENSSL_VERSION_TEXT=$("$OPENSSL_BIN" version 2>/dev/null || true)
[[ -n $OPENSSL_VERSION_TEXT ]] || die "无法执行 openssl version：$OPENSSL_BIN"

case "$OPENSSL_VERSION_TEXT" in
  LibreSSL*)
    die "检测到 LibreSSL（$OPENSSL_VERSION_TEXT）。本脚本只用 OpenSSL 1.1.1/3.x 验证过，
      请安装官方 OpenSSL（例如 macOS 上 brew install openssl 并把其 bin 目录放到 PATH 前部）。"
    ;;
esac

OPENSSL_VERSION=$(printf '%s\n' "$OPENSSL_VERSION_TEXT" | awk '{print $2}')
[[ $OPENSSL_VERSION =~ ^[0-9]+\.[0-9]+ ]] \
  || die "无法解析 OpenSSL 版本号：$OPENSSL_VERSION_TEXT"
OPENSSL_MAJOR=${OPENSSL_VERSION%%.*}
OPENSSL_REST=${OPENSSL_VERSION#*.}
OPENSSL_MINOR=${OPENSSL_REST%%.*}
OPENSSL_PATCH=$(printf '%s' "${OPENSSL_REST#*.}" | tr -cd '0-9')
[[ -n $OPENSSL_PATCH ]] || OPENSSL_PATCH=0
OPENSSL_CODE=$((10#$OPENSSL_MAJOR * 10000 + 10#$OPENSSL_MINOR * 100 + 10#$OPENSSL_PATCH))
((OPENSSL_CODE >= 10101)) \
  || die "OpenSSL 版本过低：$OPENSSL_VERSION（需要 >= 1.1.1）"

# 判断 openssl 是否为原生 Windows 程序。MSYS2 自带的 /usr/bin/openssl 使用
# POSIX 路径；mingw64/ucrt64/clang64 中的 openssl.exe 需要 Windows 路径。
NATIVE_OPENSSL=0
UNAME_S=$(uname -s 2>/dev/null || echo unknown)
case "$UNAME_S" in
  MINGW* | MSYS* | CYGWIN*)
    if command -v cygpath >/dev/null 2>&1; then
      case "$OPENSSL_BIN" in
        *.exe | /mingw* | /clang* | /ucrt*) NATIVE_OPENSSL=1 ;;
      esac
    fi
    ;;
esac

to_openssl_path() {
  if ((NATIVE_OPENSSL)); then
    cygpath -m -- "$1"
  else
    printf '%s' "$1"
  fi
}

# 可控签发日期依赖 date 的两种语法；先探测一次，之后不再猜测。
DATE_STYLE=""
if date -u -d "@0" +%Y >/dev/null 2>&1; then
  DATE_STYLE=gnu
elif date -u -r 0 +%Y >/dev/null 2>&1; then
  DATE_STYLE=bsd
else
  die "无法用 date 进行时间换算，脚本不支持当前环境的 date 实现"
fi

epoch_to_asn1() {
  if [[ $DATE_STYLE == gnu ]]; then
    date -u -d "@$1" +%Y%m%d%H%M%SZ
  else
    date -u -r "$1" +%Y%m%d%H%M%SZ
  fi
}

NOW_EPOCH=$(date -u +%s)
DAY_SECONDS=86400

# 时间条件都留出明显余量，避免设备时钟偏差影响判定。
VALID_START=$(epoch_to_asn1 $((NOW_EPOCH - DAY_SECONDS)))
VALID_END=$(epoch_to_asn1 $((NOW_EPOCH + VALIDITY_DAYS * DAY_SECONDS)))
# CA 必须比所有历史反例更早生效，否则“已过期证书只错在时间”无法成立。
CA_START=$(epoch_to_asn1 $((NOW_EPOCH - 825 * DAY_SECONDS)))
CA_END=$(epoch_to_asn1 $((NOW_EPOCH + CA_VALIDITY_DAYS * DAY_SECONDS)))
EXPIRED_START=$(epoch_to_asn1 $((NOW_EPOCH - 800 * DAY_SECONDS)))
EXPIRED_END=$(epoch_to_asn1 $((NOW_EPOCH - 30 * DAY_SECONDS)))
EXPIRED_MID=$((NOW_EPOCH - 400 * DAY_SECONDS))
FUTURE_START=$(epoch_to_asn1 $((NOW_EPOCH + 30 * DAY_SECONDS)))
FUTURE_END=$(epoch_to_asn1 $((NOW_EPOCH + (30 + VALIDITY_DAYS) * DAY_SECONDS)))
# 取未来窗口的中点；不能落在 notAfter 上（openssl 将其视为已过期）。
FUTURE_MID=$((NOW_EPOCH + (30 + VALIDITY_DAYS / 2) * DAY_SECONDS))

# ---------------------------------------------------------------------------
# 输出目录
# ---------------------------------------------------------------------------
if [[ -z $OUT_DIR ]]; then
  OUT_DIR="$REPO_ROOT/certs/test-certs-$(date -u +%Y%m%d-%H%M%S)"
fi

# MSYS2 下允许用户传入 C:\... 或 C:/... 形式的路径。
if [[ $OUT_DIR =~ ^[A-Za-z]:[\\/] ]] && command -v cygpath >/dev/null 2>&1; then
  OUT_DIR=$(cygpath -u -- "$OUT_DIR")
fi

case "$OUT_DIR" in
  /*) ;;
  *) OUT_DIR="$PWD/$OUT_DIR" ;;
esac
# 去掉末尾多余的斜杠，便于 [[ -e ]] 与 mv 判断。
while [[ $OUT_DIR == */ && $OUT_DIR != / ]]; do
  OUT_DIR=${OUT_DIR%/}
 done
[[ -n $OUT_DIR ]] || die_usage "输出目录不能为空"

if [[ -e $OUT_DIR ]]; then
  die "输出目录已存在：$OUT_DIR
不覆盖已有密钥和材料。请使用新的输出目录，或先自行备份并删除旧目录。"
fi

OUT_PARENT=$(dirname -- "$OUT_DIR")
mkdir -p -- "$OUT_PARENT"
WORK_DIR="${OUT_DIR}.partial-$$"
mkdir -p -- "$WORK_DIR"
chmod 700 -- "$WORK_DIR"

OPENSSL_LOG="$WORK_DIR/openssl-commands.log"
: >"$OPENSSL_LOG"

CA_ROOT="$WORK_DIR/ca"
SERVER_DIR="$WORK_DIR/server"
DEVICE_DIR="$WORK_DIR/device"
TMP_DIR="$WORK_DIR/tmp"
mkdir -p -- "$CA_ROOT" "$SERVER_DIR/negative" "$DEVICE_DIR/negative" "$TMP_DIR"

REQ_CONFIG="$TMP_DIR/req.cnf"
cat >"$REQ_CONFIG" <<'EOF'
[ req ]
distinguished_name = req_dn
prompt = no

[ req_dn ]
EOF

info "OpenSSL：$OPENSSL_VERSION_TEXT（$OPENSSL_BIN）"
info "输出目录：$OUT_DIR"
if ((NATIVE_OPENSSL)); then
  info "路径转换：已启用（原生 Windows openssl，cygpath -m）"
else
  info "路径转换：不需要（使用 POSIX 路径）"
fi

# ---------------------------------------------------------------------------
# 通用生成函数
# ---------------------------------------------------------------------------
gen_key() {
  local out=$1 alg=$2
  case "$alg" in
    rsa2048) "$OPENSSL_BIN" genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$(to_openssl_path "$out")" ;;
    rsa3072) "$OPENSSL_BIN" genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$(to_openssl_path "$out")" ;;
    rsa4096) "$OPENSSL_BIN" genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 -out "$(to_openssl_path "$out")" ;;
    ec-p256) "$OPENSSL_BIN" genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$(to_openssl_path "$out")" ;;
    ec-p384) "$OPENSSL_BIN" genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-384 -out "$(to_openssl_path "$out")" ;;
    *) die "内部错误：未知密钥算法 $alg" ;;
  esac >>"$OPENSSL_LOG" 2>&1 || die "生成私钥失败：$out（详见 $OPENSSL_LOG）"
  chmod 600 -- "$out"
}

# 真实设备常只接受传统 PEM 私钥类型，按需转换（不改变密钥本身）。
# OpenSSL 3.x 的 `openssl rsa` 默认输出 PKCS#8，需要 -traditional；
# 1.1.1 默认就是传统格式，且没有该选项。
finalize_leaf_key() {
  local key=$1
  [[ $LEAF_KEY_FORMAT == traditional ]] || return 0
  local expected
  case "$KEY_ALG" in
    rsa*)
      expected="RSA PRIVATE KEY"
      if ((OPENSSL_MAJOR >= 3)); then
        "$OPENSSL_BIN" rsa -traditional \
          -in "$(to_openssl_path "$key")" -out "$(to_openssl_path "$key.tmp")" \
          >>"$OPENSSL_LOG" 2>&1 || die "转换 RSA 私钥为 PKCS#1 失败：$key"
      else
        "$OPENSSL_BIN" rsa \
          -in "$(to_openssl_path "$key")" -out "$(to_openssl_path "$key.tmp")" \
          >>"$OPENSSL_LOG" 2>&1 || die "转换 RSA 私钥为 PKCS#1 失败：$key"
      fi
      ;;
    ec-*)
      expected="EC PRIVATE KEY"
      "$OPENSSL_BIN" ec \
        -in "$(to_openssl_path "$key")" -out "$(to_openssl_path "$key.tmp")" \
        >>"$OPENSSL_LOG" 2>&1 || die "转换 EC 私钥为 SEC1 失败：$key"
      ;;
  esac
  mv -f -- "$key.tmp" "$key"
  chmod 600 -- "$key"
  head -n 1 "$key" | grep -q -- "BEGIN $expected" \
    || die "私钥 $key 转换后不是预期的 $expected 格式"
}

write_ca_config() {
  local name=$1 dir=$2
  {
    printf '[ ca ]\n'
    printf 'default_ca = CA_default\n\n'
    printf '[ CA_default ]\n'
    printf 'dir = %s\n' "$(to_openssl_path "$dir")"
    printf 'database = $dir/db/index.txt\n'
    printf 'new_certs_dir = $dir/db/newcerts\n'
    printf 'certificate = $dir/%s.crt\n' "$name"
    printf 'private_key = $dir/%s.key\n' "$name"
    printf 'serial = $dir/db/serial\n'
    printf 'default_md = sha256\n'
    printf 'policy = policy_any\n'
    printf 'email_in_dn = no\n'
    printf 'unique_subject = no\n'
    printf 'copy_extensions = none\n'
    printf 'default_days = %s\n\n' "$VALIDITY_DAYS"
    printf '[ policy_any ]\n'
    printf 'commonName = supplied\n'
    printf 'countryName = optional\n'
    printf 'organizationName = optional\n'
    printf 'organizationalUnitName = optional\n'
    printf 'stateOrProvinceName = optional\n'
    printf 'localityName = optional\n'
  } >"$dir/openssl.cnf"
}

# 用 openssl ca -selfsign 创建自签 CA，以便显式指定生效/失效时间。
# 不能用 req -x509 -days：它的生效时间只能是“现在”，会让历史反例的链
# 在预期验证时刻处于 CA 生效之前，从而产生第二个失败原因。
create_ca() {
  local name=$1 cn=$2
  local dir="$CA_ROOT/$name"
  local csr="$TMP_DIR/$name-ca.csr"
  local ext="$TMP_DIR/$name-ca.ext.cnf"
  mkdir -p -- "$dir/db/newcerts"
  : >"$dir/db/index.txt"
  printf '1000\n' >"$dir/db/serial"
  gen_key "$dir/$name.key" "$CA_KEY_ALG"
  write_ca_config "$name" "$dir"
  make_csr "$dir/$name.key" "$cn" "$csr"
  {
    printf '[ ca_ext ]\n'
    printf 'basicConstraints = critical,CA:TRUE,pathlen:0\n'
    printf 'keyUsage = critical,keyCertSign,cRLSign\n'
    printf 'subjectKeyIdentifier = hash\n'
  } >"$ext"
  "$OPENSSL_BIN" ca -selfsign -batch -notext \
    -config "$(to_openssl_path "$dir/openssl.cnf")" \
    -keyfile "$(to_openssl_path "$dir/$name.key")" \
    -in "$(to_openssl_path "$csr")" \
    -extfile "$(to_openssl_path "$ext")" -extensions ca_ext \
    -startdate "$CA_START" -enddate "$CA_END" \
    -out "$(to_openssl_path "$dir/$name.crt")" >>"$OPENSSL_LOG" 2>&1 \
    || die "创建 CA 失败：$name（详见 $OPENSSL_LOG）"
  chmod 644 -- "$dir/$name.crt"
  chmod 700 -- "$dir"
}

make_csr() {
  local key=$1 cn=$2 out=$3
  "$OPENSSL_BIN" req -new \
    -key "$(to_openssl_path "$key")" \
    -subj "/CN=$cn" -utf8 \
    -config "$(to_openssl_path "$REQ_CONFIG")" \
    -out "$(to_openssl_path "$out")" >>"$OPENSSL_LOG" 2>&1 \
    || die "生成 CSR 失败：$out（详见 $OPENSSL_LOG）"
}

# write_ext_file <文件> <段名> <用途 server/client> <EKU> [DNS列表] [IP列表]
write_ext_file() {
  local file=$1 section=$2 usage=$3 eku=$4 dns_csv=${5:-} ip_csv=${6:-}
  local key_usage="digitalSignature"
  if [[ $KEY_ALG == rsa* ]]; then
    key_usage="digitalSignature,keyEncipherment"
  fi
  {
    printf '[ %s ]\n' "$section"
    printf 'basicConstraints = critical,CA:FALSE\n'
    printf 'keyUsage = critical,%s\n' "$key_usage"
    printf 'extendedKeyUsage = %s\n' "$eku"
    printf 'subjectKeyIdentifier = hash\n'
    printf 'authorityKeyIdentifier = keyid,issuer\n'
    if [[ $usage == server ]]; then
      printf 'subjectAltName = @%s_alt\n' "$section"
    fi
    printf '\n'
    if [[ $usage == server ]]; then
      printf '[ %s_alt ]\n' "$section"
      local i=1 d
      local IFS=','
      for d in $dns_csv; do
        printf 'DNS.%d = %s\n' "$i" "$d"
        i=$((i + 1))
      done
      i=1
      for d in $ip_csv; do
        printf 'IP.%d = %s\n' "$i" "$d"
        i=$((i + 1))
      done
      printf '\n'
    fi
  } >"$file"
}

# issue_cert <CA名> <CSR> <输出证书> <扩展文件> <段名> <notBefore> <notAfter>
issue_cert() {
  local ca_name=$1 csr=$2 out=$3 ext=$4 section=$5 start=$6 end=$7
  local config="$CA_ROOT/$ca_name/openssl.cnf"
  "$OPENSSL_BIN" ca -batch -notext \
    -config "$(to_openssl_path "$config")" \
    -extfile "$(to_openssl_path "$ext")" \
    -extensions "$section" \
    -startdate "$start" -enddate "$end" \
    -in "$(to_openssl_path "$csr")" \
    -out "$(to_openssl_path "$out")" >>"$OPENSSL_LOG" 2>&1 \
    || die "签发证书失败：$out（详见 $OPENSSL_LOG）"
  chmod 644 -- "$out"
}

# make_leaf <CA名> <名称前缀> <CSR CN> <用途> <EKU> <notBefore> <notAfter> <输出证书> <输出私钥> [DNS] [IP]
make_leaf() {
  local ca_name=$1 prefix=$2 cn=$3 usage=$4 eku=$5 start=$6 end=$7 cert=$8 key=$9
  local dns=${10:-} ip=${11:-}
  local csr="$TMP_DIR/$prefix.csr"
  local ext="$TMP_DIR/$prefix.ext.cnf"
  gen_key "$key" "$KEY_ALG"
  make_csr "$key" "$cn" "$csr"
  write_ext_file "$ext" "${prefix}_ext" "$usage" "$eku" "$dns" "$ip"
  issue_cert "$ca_name" "$csr" "$cert" "$ext" "${prefix}_ext" "$start" "$end"
  finalize_leaf_key "$key"
}

DNS_CSV=$(IFS=,; printf '%s' "${DNS_LIST[*]-}")
IP_CSV=$(IFS=,; printf '%s' "${IP_LIST[*]-}")

# ---------------------------------------------------------------------------
# 生成 CA
# ---------------------------------------------------------------------------
step "创建 CA A 与独立 CA B"

create_ca "ca-a" "MQTT Device Test CA A"
create_ca "ca-b" "MQTT Device Test CA B"

cp -p -- "$CA_ROOT/ca-a/ca-a.crt" "$SERVER_DIR/ca-a.crt"
cp -p -- "$CA_ROOT/ca-a/ca-a.crt" "$DEVICE_DIR/ca-a.crt"
cp -p -- "$CA_ROOT/ca-b/ca-b.crt" "$DEVICE_DIR/negative/ca-b.crt"
chmod 644 -- "$SERVER_DIR/ca-a.crt" "$DEVICE_DIR/ca-a.crt" "$DEVICE_DIR/negative/ca-b.crt"

# ---------------------------------------------------------------------------
# 服务器证书（正向 + 反例）
# ---------------------------------------------------------------------------
step "生成服务器证书与服务器反例"

make_leaf "ca-a" "server" "$SERVER_NAME" server serverAuth \
  "$VALID_START" "$VALID_END" \
  "$SERVER_DIR/server.crt" "$SERVER_DIR/server.key" "$DNS_CSV" "$IP_CSV"

# 反例 1：有效期内但已过期；除时间外与正向服务器证书一致。
make_leaf "ca-a" "server-expired" "$SERVER_NAME" server serverAuth \
  "$EXPIRED_START" "$EXPIRED_END" \
  "$SERVER_DIR/negative/server-expired.crt" "$SERVER_DIR/negative/server-expired.key" \
  "$DNS_CSV" "$IP_CSV"

# 反例 2：CN 与 SAN 都指向错误主机名；时间、用途、签发者均正常。
make_leaf "ca-a" "server-wrong-host" "wrong-host.invalid" server serverAuth \
  "$VALID_START" "$VALID_END" \
  "$SERVER_DIR/negative/server-wrong-host.crt" "$SERVER_DIR/negative/server-wrong-host.key" \
  "wrong-host.invalid" ""

# 反例 3：由独立 CA B 签发；其他条件与正向服务器证书一致。
make_leaf "ca-b" "server-by-ca-b" "$SERVER_NAME" server serverAuth \
  "$VALID_START" "$VALID_END" \
  "$SERVER_DIR/negative/server-by-ca-b.crt" "$SERVER_DIR/negative/server-by-ca-b.key" \
  "$DNS_CSV" "$IP_CSV"

# ---------------------------------------------------------------------------
# 设备证书（正向 + 反例）
# ---------------------------------------------------------------------------
step "生成设备证书与设备反例"

make_leaf "ca-a" "device" "$DEVICE_ID" client clientAuth \
  "$VALID_START" "$VALID_END" \
  "$DEVICE_DIR/device.crt" "$DEVICE_DIR/device.key"

# 反例 1：已过期。
make_leaf "ca-a" "device-expired" "$DEVICE_ID" client clientAuth \
  "$EXPIRED_START" "$EXPIRED_END" \
  "$DEVICE_DIR/negative/device-expired.crt" "$DEVICE_DIR/negative/device-expired.key"

# 反例 2：尚未生效。
make_leaf "ca-a" "device-notyet" "$DEVICE_ID" client clientAuth \
  "$FUTURE_START" "$FUTURE_END" \
  "$DEVICE_DIR/negative/device-notyet.crt" "$DEVICE_DIR/negative/device-notyet.key"

# 反例 3：EKU 只有 serverAuth，缺少 clientAuth。
make_leaf "ca-a" "device-wrong-eku" "$DEVICE_ID" client serverAuth \
  "$VALID_START" "$VALID_END" \
  "$DEVICE_DIR/negative/device-wrong-eku.crt" "$DEVICE_DIR/negative/device-wrong-eku.key"

# 反例 4：由独立 CA B 签发；服务器只信任 CA A 时不受信任。
make_leaf "ca-b" "device-by-ca-b" "$DEVICE_ID" client clientAuth \
  "$VALID_START" "$VALID_END" \
  "$DEVICE_DIR/negative/device-by-ca-b.crt" "$DEVICE_DIR/negative/device-by-ca-b.key"

# ---------------------------------------------------------------------------
# 校验
# ---------------------------------------------------------------------------
step "用 OpenSSL 校验链、用途、SAN、时间与公私钥匹配"

CHECK_LOG="$WORK_DIR/checks.txt"
: >"$CHECK_LOG"

check_ok() {
  printf '  [通过] %s\n' "$*" >>"$CHECK_LOG"
}

# key 与 cert 的公钥必须完全一致。
assert_key_pair() {
  local label=$1 cert=$2 key=$3
  local cert_pub key_pub
  cert_pub=$("$OPENSSL_BIN" x509 -in "$(to_openssl_path "$cert")" -noout -pubkey 2>>"$OPENSSL_LOG") \
    || die "读取证书公钥失败：$cert"
  key_pub=$("$OPENSSL_BIN" pkey -in "$(to_openssl_path "$key")" -pubout 2>>"$OPENSSL_LOG") \
    || die "读取私钥公钥失败：$key"
  [[ $cert_pub == "$key_pub" ]] || die "公私钥不匹配：$label（$cert）"
  check_ok "公私钥匹配：$label"
}

# 预期成功的校验；失败即终止。
verify_ok() {
  local label=$1
  shift
  local out
  if ! out=$("$OPENSSL_BIN" verify "$@" 2>&1); then
    printf '%s\n' "$out" >&2
    die "预期成功但校验失败：$label"
  fi
  check_ok "$label"
}

# 预期失败的校验：必须失败，且失败原因必须命中预期关键字，否则视为脚本错误。
verify_fail() {
  local label=$1 token=$2
  shift 2
  local out
  if out=$("$OPENSSL_BIN" verify "$@" 2>&1); then
    die "预期失败但校验成功：$label"
  fi
  if ! printf '%s\n' "$out" | grep -Eqi -- "$token"; then
    printf '%s\n' "$out" >&2
    die "反例失败原因与预期不符：$label（预期包含：$token）"
  fi
  check_ok "预期失败且原因符合：$label（$token）"
}

CA_A="$CA_ROOT/ca-a/ca-a.crt"
CA_B="$CA_ROOT/ca-b/ca-b.crt"
FIRST_DNS=""
for candidate in "${DNS_LIST[@]+"${DNS_LIST[@]}"}"; do
  # 通配符 SAN 不能直接传给 -verify_hostname，跳过它选择一个具体主机名。
  [[ $candidate == \** ]] && continue
  FIRST_DNS=$candidate
  break
done
FIRST_IP=${IP_LIST[0]:-}
# 负例使用与 wrong-host.invalid 不同的名字，避免恰好匹配反例证书的 SAN。
HOSTNAME_CHECK=${FIRST_DNS:-device-check.invalid}
[[ $HOSTNAME_CHECK == wrong-host.invalid ]] && HOSTNAME_CHECK=device-check.invalid

assert_key_pair "CA A" "$CA_A" "$CA_ROOT/ca-a/ca-a.key"
assert_key_pair "CA B" "$CA_B" "$CA_ROOT/ca-b/ca-b.key"
assert_key_pair "服务器证书" "$SERVER_DIR/server.crt" "$SERVER_DIR/server.key"
assert_key_pair "设备证书" "$DEVICE_DIR/device.crt" "$DEVICE_DIR/device.key"
assert_key_pair "过期服务器证书" "$SERVER_DIR/negative/server-expired.crt" "$SERVER_DIR/negative/server-expired.key"
assert_key_pair "SAN 不匹配服务器证书" "$SERVER_DIR/negative/server-wrong-host.crt" "$SERVER_DIR/negative/server-wrong-host.key"
assert_key_pair "CA B 服务器证书" "$SERVER_DIR/negative/server-by-ca-b.crt" "$SERVER_DIR/negative/server-by-ca-b.key"
assert_key_pair "过期设备证书" "$DEVICE_DIR/negative/device-expired.crt" "$DEVICE_DIR/negative/device-expired.key"
assert_key_pair "未生效设备证书" "$DEVICE_DIR/negative/device-notyet.crt" "$DEVICE_DIR/negative/device-notyet.key"
assert_key_pair "错误 EKU 设备证书" "$DEVICE_DIR/negative/device-wrong-eku.crt" "$DEVICE_DIR/negative/device-wrong-eku.key"
assert_key_pair "CA B 设备证书" "$DEVICE_DIR/negative/device-by-ca-b.crt" "$DEVICE_DIR/negative/device-by-ca-b.key"

# 正向：链、用途、SAN。
verify_ok "服务器证书链（信任 CA A）" -CAfile "$CA_A" "$SERVER_DIR/server.crt"
verify_ok "服务器证书用途 sslserver" -purpose sslserver -CAfile "$CA_A" "$SERVER_DIR/server.crt"
verify_ok "设备证书链（信任 CA A）" -CAfile "$CA_A" "$DEVICE_DIR/device.crt"
verify_ok "设备证书用途 sslclient" -purpose sslclient -CAfile "$CA_A" "$DEVICE_DIR/device.crt"

if [[ -n $FIRST_DNS ]]; then
  verify_ok "服务器证书 DNS SAN 匹配：$FIRST_DNS" \
    -verify_hostname "$FIRST_DNS" -CAfile "$CA_A" "$SERVER_DIR/server.crt"
fi
if [[ -n $FIRST_IP ]]; then
  verify_ok "服务器证书 IP SAN 匹配：$FIRST_IP" \
    -verify_ip "$FIRST_IP" -CAfile "$CA_A" "$SERVER_DIR/server.crt"
fi

# CA B 的正向对照：同一批证书在信任 CA B 时必须有效。
verify_ok "CA B 服务器证书在 CA B 信任下有效" \
  -purpose sslserver -CAfile "$CA_B" "$SERVER_DIR/negative/server-by-ca-b.crt"
verify_ok "CA B 设备证书在 CA B 信任下有效" \
  -purpose sslclient -CAfile "$CA_B" "$DEVICE_DIR/negative/device-by-ca-b.crt"

# 时间反例：先用 -attime 证明“除时间外都正确”，再证明默认校验只因子时间失败。
verify_ok "过期服务器证书在历史有效期内链有效" \
  -attime "$EXPIRED_MID" -purpose sslserver -CAfile "$CA_A" "$SERVER_DIR/negative/server-expired.crt"
verify_fail "服务器证书已过期" "certificate has expired" \
  -purpose sslserver -CAfile "$CA_A" "$SERVER_DIR/negative/server-expired.crt"

verify_ok "过期设备证书在历史有效期内链有效" \
  -attime "$EXPIRED_MID" -purpose sslclient -CAfile "$CA_A" "$DEVICE_DIR/negative/device-expired.crt"
verify_fail "设备证书已过期" "certificate has expired" \
  -purpose sslclient -CAfile "$CA_A" "$DEVICE_DIR/negative/device-expired.crt"

verify_ok "未生效设备证书在未来有效期内链有效" \
  -attime "$FUTURE_MID" -purpose sslclient -CAfile "$CA_A" "$DEVICE_DIR/negative/device-notyet.crt"
verify_fail "设备证书尚未生效" "not yet valid" \
  -purpose sslclient -CAfile "$CA_A" "$DEVICE_DIR/negative/device-notyet.crt"

# 用途与信任反例。
verify_fail "设备证书缺少 clientAuth" "purpose" \
  -purpose sslclient -CAfile "$CA_A" "$DEVICE_DIR/negative/device-wrong-eku.crt"
verify_fail "设备证书签发者为不受信任的 CA B" \
  "unable to get local issuer certificate|unable to verify the first certificate" \
  -purpose sslclient -CAfile "$CA_A" "$DEVICE_DIR/negative/device-by-ca-b.crt"

verify_fail "服务器证书 SAN 与连接主机名不符" "hostname mismatch" \
  -verify_hostname "$HOSTNAME_CHECK" -CAfile "$CA_A" "$SERVER_DIR/negative/server-wrong-host.crt"
verify_fail "服务器证书签发者为不受信任的 CA B" \
  "unable to get local issuer certificate|unable to verify the first certificate" \
  -purpose sslserver -CAfile "$CA_A" "$SERVER_DIR/negative/server-by-ca-b.crt"

# 确认每个反例只改变一个条件，避免其他错误掩盖目标失败原因。
verify_ok "SAN 不匹配证书本身链与用途有效（不做主机名检查）" \
  -purpose sslserver -CAfile "$CA_A" "$SERVER_DIR/negative/server-wrong-host.crt"
verify_ok "错误 EKU 证书链本身有效（不做用途检查）" \
  -CAfile "$CA_A" "$DEVICE_DIR/negative/device-wrong-eku.crt"

WRONG_HOST_SAN=$("$OPENSSL_BIN" x509 -in "$(to_openssl_path "$SERVER_DIR/negative/server-wrong-host.crt")" \
  -noout -ext subjectAltName 2>>"$OPENSSL_LOG") || die "读取 SAN 失败"
[[ $WRONG_HOST_SAN == *wrong-host.invalid* ]] || die "SAN 不匹配反例的 SAN 内容异常"
check_ok "SAN 不匹配反例只包含 wrong-host.invalid"

# ---------------------------------------------------------------------------
# 清单与说明
# ---------------------------------------------------------------------------
step "生成清单与中文说明"

# CSR 与扩展配置只是中间产物，不进入最终材料，也不应出现在清单里。
rm -rf -- "$TMP_DIR"

MANIFEST="$WORK_DIR/manifest.txt"
README="$WORK_DIR/说明.txt"

{
  printf 'MQTTDeviceTestingTool 真实设备测试证书清单\n'
  printf '生成时间（UTC）：%s\n' "$(date -u '+%Y-%m-%d %H:%M:%S')"
  printf '生成脚本：scripts/gen-test-certs.sh\n'
  printf '平台：%s\n' "$(uname -srm 2>/dev/null || echo unknown)"
  printf 'OpenSSL：%s\n' "$OPENSSL_VERSION_TEXT"
  printf '\n'
  printf '生成参数：\n'
  printf '  服务器名称（CN）：%s\n' "$SERVER_NAME"
  printf '  服务器 DNS SAN：%s\n' "${DNS_CSV:-(无)}"
  printf '  服务器 IP SAN：%s\n' "${IP_CSV:-(无)}"
  printf '  设备标识（CN）：%s\n' "$DEVICE_ID"
  printf '  叶证书有效期：%s 天\n' "$VALIDITY_DAYS"
  printf '  CA 有效期：%s 天\n' "$CA_VALIDITY_DAYS"
  printf '  叶证书密钥算法：%s（私钥编码：%s）\n' "$KEY_ALG" "$LEAF_KEY_FORMAT"
  printf '  CA 密钥算法：%s\n' "$CA_KEY_ALG"
  printf '  生效时间（正向材料）：%s\n' "$VALID_START"
  printf '  失效时间（正向材料）：%s\n' "$VALID_END"
  printf '\n'
  printf '目录结构：\n'
  printf '  ca/          CA 管理材料（含 CA 私钥，严禁导入设备或随包分发）\n'
  printf '  server/      服务器材料（含服务器私钥），供本工具 -server-cert/-server-key 使用\n'
  printf '  device/      设备导入材料（设备证书、设备私钥、用于验证服务器的 CA A 证书）\n'
  printf '  manifest.txt 本清单\n'
  printf '  说明.txt     中文使用说明、反例预期失败原因与限制\n'
  printf '\n'
} >>"$MANIFEST"

describe_cert() {
  local title=$1 file=$2
  {
    printf '### %s\n' "$title"
    printf '文件：%s\n' "${file#"$WORK_DIR"/}"
    "$OPENSSL_BIN" x509 -in "$(to_openssl_path "$file")" -noout \
      -subject -issuer -serial -dates -fingerprint -sha256 2>>"$OPENSSL_LOG" \
      | sed 's/^/  /'
    local san eku
    san=$("$OPENSSL_BIN" x509 -in "$(to_openssl_path "$file")" -noout -ext subjectAltName 2>/dev/null \
      | sed -n '2,$p' | tr -d '\r' | sed 's/^[[:space:]]*//')
    [[ -n $san ]] && printf '  SAN：%s\n' "$san"
    eku=$("$OPENSSL_BIN" x509 -in "$(to_openssl_path "$file")" -noout -ext extendedKeyUsage 2>/dev/null \
      | sed -n '2,$p' | tr -d '\r' | sed 's/^[[:space:]]*//')
    [[ -n $eku ]] && printf '  EKU：%s\n' "$eku"
    printf '\n'
  } >>"$MANIFEST"
}

{
  printf '## 一、CA 管理材料（ca/，含私钥，禁止导入设备）\n\n'
  printf 'CA 私钥权限为 0600；db/ 子目录仅用于签发审计，不是运行材料。\n'
  printf 'db/newcerts/ 中的证书副本由 openssl ca 自动生成，与 server/、device/\n'
  printf '下对应证书内容相同；db/*.old 为 OpenSSL 自动备份。\n\n'
} >>"$MANIFEST"
describe_cert "CA A（设备与服务端共同信任的测试根）" "$CA_ROOT/ca-a/ca-a.crt"
describe_cert "CA B（独立的反例根，不应被默认信任）" "$CA_ROOT/ca-b/ca-b.crt"

{
  printf '## 二、服务器材料（server/）\n\n'
  printf '正向服务器证书：server.crt / server.key\n'
  printf '设备验证服务器所需的信任锚：server/ca-a.crt（等同 CA A 证书）\n'
  printf '启动示例见“说明.txt”。\n\n'
} >>"$MANIFEST"
describe_cert "服务器证书（正向）" "$SERVER_DIR/server.crt"
describe_cert "服务器证书（已过期，反例）" "$SERVER_DIR/negative/server-expired.crt"
describe_cert "服务器证书（SAN 不匹配，反例）" "$SERVER_DIR/negative/server-wrong-host.crt"
describe_cert "服务器证书（CA B 签发，反例）" "$SERVER_DIR/negative/server-by-ca-b.crt"

{
  printf '## 三、设备导入材料（device/）\n\n'
  printf 'device.crt / device.key：设备身份证书与私钥（clientAuth）。\n'
  printf 'ca-a.crt：设备用于验证服务器身份的信任锚（只含 CA 公钥）。\n'
  printf '本目录不会出现 CA 私钥或服务器私钥。\n\n'
} >>"$MANIFEST"
describe_cert "设备证书（正向）" "$DEVICE_DIR/device.crt"
describe_cert "设备证书（已过期，反例）" "$DEVICE_DIR/negative/device-expired.crt"
describe_cert "设备证书（尚未生效，反例）" "$DEVICE_DIR/negative/device-notyet.crt"
describe_cert "设备证书（EKU 错误，反例）" "$DEVICE_DIR/negative/device-wrong-eku.crt"
describe_cert "设备证书（CA B 签发，反例）" "$DEVICE_DIR/negative/device-by-ca-b.crt"

{
  printf '## 四、校验结果\n\n'
  cat "$CHECK_LOG"
  printf '\n'
  printf '## 五、文件列表（路径与大小，不含私钥内容）\n\n'
  while IFS= read -r f; do
    rel=${f#"$WORK_DIR"/}
    size=$(wc -c <"$f" | tr -d '[:space:]')
    printf '%8s 字节  %s\n' "$size" "$rel"
  done < <(find "$WORK_DIR" -type f ! -name 'openssl-commands.log' ! -name 'checks.txt' \
    ! -name 'manifest.txt' ! -name '说明.txt' | LC_ALL=C sort) >>"$MANIFEST"
  printf '\n'
  printf '说明：本清单不包含任何私钥内容；指纹为证书 SHA-256 指纹。\n'
} >>"$MANIFEST"

# 构建说明.txt。动态值集中在前半部分，静态说明使用带引号的 heredoc。
{
  printf 'MQTTDeviceTestingTool 真实设备测试证书材料\n'
  printf '================================================\n\n'
  printf '生成时间（UTC）：%s\n' "$(date -u '+%Y-%m-%d %H:%M:%S')"
  printf '输出目录：%s\n' "$OUT_DIR"
  printf '生成环境：%s\n' "$(uname -srm 2>/dev/null || echo unknown)"
  printf 'OpenSSL：%s\n' "$OPENSSL_VERSION_TEXT"
  printf '服务器名称：%s\n' "$SERVER_NAME"
  printf '服务器 DNS SAN：%s\n' "${DNS_CSV:-(无)}"
  printf '服务器 IP SAN：%s\n' "${IP_CSV:-(无)}"
  printf '设备标识：%s\n' "$DEVICE_ID"
  printf '叶证书有效期：%s 天；CA 有效期：%s 天\n' "$VALIDITY_DAYS" "$CA_VALIDITY_DAYS"
  printf '密钥算法：叶证书 %s，CA %s\n' "$KEY_ALG" "$CA_KEY_ALG"
  printf '叶证书私钥编码：%s\n\n' "$LEAF_KEY_FORMAT"
} >"$README"

cat >>"$README" <<'EOF'
一、这些材料用来做什么
------------------------------------------------
本目录是供真实设备人工测试使用的长期材料，与 `go test` 在临时目录内动态生成的
证书完全独立。基础 `go test` 不需要 OpenSSL，也不会读取本目录。

  * ca/       CA 管理材料，含 CA 私钥。只应保存在可信的分析机上，严禁导入设备、
              服务器运行目录或随固件/安装包分发。
  * server/   服务器材料，供本工具的 tls / mtls 模式使用。
  * device/   设备导入材料，只包含设备证书、设备私钥和用于验证服务器的 CA 公钥。

所有证书和私钥均为独立 PEM 文件，未拼接、未编码为 PFX/P12。
设备侧“单一证书 / 三元组”的字段顺序、编码和粘贴格式尚未确认，本脚本不猜测，
也不生成合并文件；请按设备文档核对后再决定是否拼接。

二、目录与文件用途
------------------------------------------------
ca/ca-a/ca-a.crt            测试根 CA A 证书（设备与服务端共同信任）
ca/ca-a/ca-a.key            CA A 私钥（0600，禁止分发）
ca/ca-b/ca-b.crt            独立测试根 CA B 证书（仅用于反例）
ca/ca-b/ca-b.key            CA B 私钥（0600，禁止分发）
ca/ca-a/db/、ca/ca-b/db/    签发数据库（index.txt、serial、newcerts），便于审计，
                            不是运行材料

server/server.crt           服务器证书（正向，serverAuth，含配置的 DNS/IP SAN）
server/server.key           服务器私钥（0600）
server/ca-a.crt             服务器用于验证设备证书的信任锚（即 CA A）

server/negative/server-expired.crt     已过期的服务器证书
server/negative/server-wrong-host.crt  CN 与 SAN 均与连接地址不匹配的服务器证书
server/negative/server-by-ca-b.crt     由 CA B 签发、设备默认不信任的服务器证书

device/device.crt           设备证书（正向，clientAuth）
device/device.key           设备私钥（0600）
device/ca-a.crt             设备用于验证服务器身份的信任锚（CA A 公钥）

device/negative/device-expired.crt     已过期的设备证书
device/negative/device-notyet.crt      尚未生效的设备证书
device/negative/device-wrong-eku.crt   只含 serverAuth、缺少 clientAuth 的设备证书
device/negative/device-by-ca-b.crt     由 CA B 签发的设备证书
device/negative/ca-b.crt               CA B 公钥，用于“信任 CA B”的对照实验

三、服务器启动示例
------------------------------------------------
单向 TLS（服务器只出示证书）：

  ./MQTTDeviceTestingTool -mode tls -listen 0.0.0.0:8883 \
      -server-cert "server/server.crt" \
      -server-key  "server/server.key"

强制 mTLS（要求并校验证书由 CA A 签发）：

  ./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \
      -server-cert "server/server.crt" \
      -server-key  "server/server.key" \
      -client-ca   "server/ca-a.crt"

Windows cmd 中请使用反斜杠路径并为含空格/中文的路径加双引号。参数含义以
`MQTTDeviceTestingTool -h` 和 README 为准。

四、反例与唯一预期失败原因
------------------------------------------------
每个反例只改变一个条件，其余条件与对应正向证书一致：

  1. device/negative/device-expired.crt
     预期失败：证书已过期（Go 报 Expired；OpenSSL 报 certificate has expired）。
  2. device/negative/device-notyet.crt
     预期失败：证书尚未生效（NotYetValid / not yet valid）。
  3. device/negative/device-wrong-eku.crt
     预期失败：缺少 clientAuth 用途（incompatible key usage / unsuitable purpose）。
  4. device/negative/device-by-ca-b.crt（服务器使用 -client-ca server/ca-a.crt）
     预期失败：签发者不受信任（unknown authority）。
     对照：将 -client-ca 换成 device/negative/ca-b.crt 后，同一设备证书应能通过，
     用于证明失败原因是信任链而不是证书本身损坏。
  5. server/negative/server-expired.crt
     预期失败：设备验证服务器证书时判定已过期。
  6. server/negative/server-wrong-host.crt
     预期失败：设备校验主机名/IP 时与 CN、SAN 均不匹配（需设备实际启用了主机名校验）。
  7. server/negative/server-by-ca-b.crt（设备只信任 device/ca-a.crt）
     预期失败：服务器证书签发者不受信任。

除上述单一原因外，脚本会用 OpenSSL 复核这些反例的链、用途、SAN 和时间，
确保没有其他错误掩盖目标失败原因。若某条校验出现非预期错误，脚本会失败并
删除本次未完成的输出目录。

五、证书与私钥不匹配的测试组合
------------------------------------------------
本脚本不伪造“错误证书格式”文件。需要验证工具的配对检查时，直接交叉使用现成的
证书与私钥即可，例如：

  * server/server.key + device/device.crt          → 私钥与证书不匹配
  * device/device.key + server/server.crt          → 私钥与证书不匹配
  * device/device.key + device/negative/device-by-ca-b.crt → 私钥与证书不匹配

预期结果：工具在启动阶段报私钥不匹配并以非零退出码退出，不进入监听。
这些组合只用于人工测试，不应写进设备或服务器的正式配置。

六、权限与覆盖策略
------------------------------------------------
  * 脚本默认 umask 077；私钥 0600，证书 0644，CA 管理目录 0700。
  * Windows/MSYS2 的 chmod 只影响模拟权限，不写入 Windows ACL；请自行确认
    输出目录未被共享、未被版本控制。
  * 输出目录已存在时脚本直接报错，不会覆盖已有密钥或材料。
  * 生成失败时会删除本次的临时目录，不会留下可被误认为完整的残缺证书集。

七、支持范围与跨平台说明
------------------------------------------------
  * 需要 bash 4+ 与 OpenSSL 1.1.1+（3.x 已实测）；不支持 LibreSSL。
  * MSYS2 / Git Bash：脚本关闭了 MSYS 的参数路径自动转换，避免 subject 中的
    “/C=../CN=..” 被改写；若 openssl 是 mingw64/ucrt64 的原生 Windows 程序，
    会自动用 cygpath -m 转换传给 openssl 的文件路径。
  * 通过 .bat 入口传入含中文的参数可能受 cmd 代码页影响；推荐在 MSYS2 终端中
    直接运行 bash 脚本，或在 .bat 中使用纯 ASCII 路径。
  * 过期/未生效证书使用 `openssl ca -startdate/-enddate` 指定绝对签发时间，
    不修改系统时间，也不使用负数有效天数。
  * 脚本在 Linux（OpenSSL 3.5）实测；macOS 与 Windows/MSYS2 的路径与权限行为
    尚未实测，需要在目标机器上先跑一次本脚本核对清单。

八、尚未确认 / 刻意未实现
------------------------------------------------
  * 设备“单一证书 / 三元组”的拼接顺序、字段含义与编码未确认，不生成合并文件。
  * 未实现中间 CA 场景：真实设备需要“根 + 中间 + 叶”链时的 PEM 顺序与缺失链
    行为，已在 Go 集成测试中覆盖；如后续需要设备侧材料，应在此脚本中增加
    intermediate CA 并明确 chain/fullchain 文件区别。
  * 不包含 CRL/OCSP、证书轮换、设备身份与 MQTT Client ID 绑定。
  * 本材料只能证明协议与证书配置行为，不能替代设备 TLS 栈、算法支持、时钟偏差
    和网络环境的真实能力结论。
EOF

# ---------------------------------------------------------------------------
# 收尾
# ---------------------------------------------------------------------------
step "整理目录、权限与输出"

rm -rf -- "$TMP_DIR"
rm -f -- "$CHECK_LOG" "$OPENSSL_LOG"
chmod 700 -- "$CA_ROOT" "$CA_ROOT/ca-a" "$CA_ROOT/ca-b"
chmod 755 -- "$SERVER_DIR" "$SERVER_DIR/negative" "$DEVICE_DIR" "$DEVICE_DIR/negative"
while IFS= read -r -d '' f; do chmod 600 -- "$f"; done < <(find "$WORK_DIR" -type f -name '*.key' -print0)
while IFS= read -r -d '' f; do chmod 644 -- "$f"; done < <(find "$WORK_DIR" -type f ! -name '*.key' -print0)
chmod 700 -- "$WORK_DIR"

mv -- "$WORK_DIR" "$OUT_DIR"

info ""
info "已生成证书材料：$OUT_DIR"
info ""
info "服务器（mTLS）启动示例："
info "  ./MQTTDeviceTestingTool -mode mtls -listen 0.0.0.0:8883 \\"
info "      -server-cert \"$OUT_DIR/server/server.crt\" \\"
info "      -server-key  \"$OUT_DIR/server/server.key\" \\"
info "      -client-ca   \"$OUT_DIR/server/ca-a.crt\""
info ""
info "设备导入：device/device.crt、device/device.key、device/ca-a.crt"
info "完整清单：$OUT_DIR/manifest.txt"
info "使用说明：$OUT_DIR/说明.txt"
info ""
info "提示：ca/ 目录含 CA 私钥，请勿导入设备或随材料分发。"
