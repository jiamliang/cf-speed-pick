#!/bin/sh
# speedtest-and-upload.sh — cf-speed-pick 测速 + 上传到 Worker KV
#
# 通用：服务器 (x86_64) 和路由器 (armv7l/aarch64) 共用
#
# 行为：
#   1. detect uname -m → 选对应架构 binary + 测速参数
#   2. source 脚本同目录的 env（拿 WORKER_URL / PUT_TOKEN / OPERATOR）
#   3. 跑 cf-speed-pick 测速（TCPing → 下载；跳过 Layer 2 HTTPing）
#   4. 把 out/full/03_top.csv 上传到 Worker KV（KV key = ${operator}/all）
#
# crontab 入口（绝对路径，install.sh 注册）：
#   0 */6 * * * /data/service/cloudflare/cf-speed-pick/scripts/speedtest-and-upload.sh
#
# 注意：POSIX shell 兼容，不依赖 bash。

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# === 1. detect 架构 → 选 binary + 参数 ===
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)
        CFSP_BIN="$SCRIPT_DIR/cf-speed-pick-linux-amd64"
        N=80;  T=2; DT=15s; DN=20; MAX_DELAY=150
        ;;
    aarch64|arm64|armv7l)
        CFSP_BIN="$SCRIPT_DIR/cf-speed-pick-linux-arm64"
        N=50;  T=2; DT=15s; DN=10; MAX_DELAY=100
        ;;
    *)
        echo "unsupported arch: $ARCH (need x86_64 or aarch64/armv7l)" >&2
        exit 1
        ;;
esac

if [ ! -x "$CFSP_BIN" ]; then
    echo "binary not found or not executable: $CFSP_BIN" >&2
    exit 1
fi

# === 2. source env ===
ENV_FILE="$SCRIPT_DIR/.env"
if [ ! -f "$ENV_FILE" ]; then
    echo "missing $ENV_FILE — copy .env.example to .env and fill in" >&2
    exit 1
fi
. "$ENV_FILE"

WORKER_URL="${WORKER_URL:?WORKER_URL required in $ENV_FILE}"
PUT_TOKEN="${PUT_TOKEN:?PUT_TOKEN required in $ENV_FILE}"
OPERATOR="${OPERATOR:?OPERATOR required (telecom or unicom)}"

# 校验 OPERATOR
case "$OPERATOR" in
    unicom|telecom) ;;
    *)
        echo "bad OPERATOR: $OPERATOR (must be unicom or telecom)" >&2
        exit 1
        ;;
esac

# === 3. 跑 cf-speed-pick ===
OUT="$SCRIPT_DIR/out"
LOG="$SCRIPT_DIR/speedtest.log"
RANGES="$SCRIPT_DIR/cf-ranges.txt"

# 强制要求 cf-ranges.txt 存在（不用内置 fallback）
if [ ! -f "$RANGES" ]; then
    echo "missing $RANGES — copy it from the deploy bundle" >&2
    exit 1
fi

mkdir -p "$OUT/full" "$(dirname "$LOG")"

TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] [$ARCH] 开始 cf-speed-pick (n=$N t=$T dt=$DT dn=$DN) ranges=$RANGES" >> "$LOG"

"$CFSP_BIN" \
    -out "$OUT/full" \
    -debug \
    -ranges "$RANGES" \
    -n "$N" -t "$T" -max-delay "$MAX_DELAY" \
    -dt "$DT" -dn "$DN" \
    -min-speed 0 \
    >> "$LOG" 2>&1

RC=$?
TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] [$ARCH] cf-speed-pick 退出码=$RC" >> "$LOG"

# === 4. 上传单文件 03_top.csv 到 Worker KV（KV key = ${operator}/all） ===
SRC_FILE="$OUT/full/03_top.csv"

if [ ! -s "$SRC_FILE" ]; then
    echo "[$TS] missing or empty: $SRC_FILE — skip upload" >> "$LOG"
    exit $RC
fi

LINES=$(wc -l < "$SRC_FILE")
if [ "$LINES" -le 1 ]; then
    echo "[$TS] header-only: $SRC_FILE ($LINES lines) — skip upload" >> "$LOG"
    exit $RC
fi

COUNT=0
FAIL=0
TMP_RESP=$(mktemp)
trap 'rm -f "$TMP_RESP"' EXIT

echo "[$TS] ↑ ${OPERATOR}/all ($LINES lines)" >> "$LOG"
HTTP_CODE=$(curl -s -X POST \
    "${WORKER_URL}/api/put?operator=${OPERATOR}" \
    -H "Authorization: Bearer ${PUT_TOKEN}" \
    -H "Content-Type: text/csv" \
    --data-binary "@${SRC_FILE}" \
    -w "%{http_code}" -o "$TMP_RESP" 2>/dev/null || echo "000")

case "$HTTP_CODE" in
    200)
        COUNT=$((COUNT + 1))
        BODY=$(cat "$TMP_RESP" 2>/dev/null || echo "")
        echo "[$TS]   ok: $BODY" >> "$LOG"
        ;;
    *)
        FAIL=$((FAIL + 1))
        echo "[$TS]   FAIL: HTTP $HTTP_CODE" >> "$LOG"
        cat "$TMP_RESP" >> "$LOG" 2>/dev/null || true
        echo >> "$LOG"
        ;;
esac

TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] uploaded to KV (operator=${OPERATOR}, count=${COUNT}, failed=${FAIL})" >> "$LOG"

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
exit 0
