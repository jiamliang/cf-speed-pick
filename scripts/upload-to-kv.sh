#!/bin/sh
# 把 cf-speed-pick colo 桶上传到 Cloudflare Worker KV
#
# 部署位置：
#   VPS:    /usr/local/bin/upload-to-kv.sh
#   R7000:  /jffs/cf-speed-pick/bin/upload-to-kv.sh
#
# 必填环境变量：
#   WORKER_URL  — Worker 完整 URL（e.g. https://vless.cf.peeweecap.com）
#   PUT_TOKEN   — Worker 的 PUT_TOKEN secret
#   OPERATOR    — unicom 或 telecom
#
# 可选环境变量：
#   SRC_BASE    — colo CSV 目录（默认见下）
#
# 调用：
#   WORKER_URL=... PUT_TOKEN=... OPERATOR=telecom ./upload-to-kv.sh
#
# Cron 集成：qingdao-cron.sh / r7000-cron.sh 末尾自动调用。
#
# 注意：POSIX shell 兼容，不依赖 bash。

set -eu

WORKER_URL="${WORKER_URL:?WORKER_URL env var required (e.g. https://vless.cf.peeweecap.com)}"
PUT_TOKEN="${PUT_TOKEN:?PUT_TOKEN env var required (32-byte hex)}"
OPERATOR="${OPERATOR:?OPERATOR env var required (unicom or telecom)}"
SRC_BASE="${SRC_BASE:-/var/lib/cf-speed-pick/out/full/colo}"

# 去掉 OPERATOR 可能的尾部斜杠/空格
OPERATOR=$(echo "$OPERATOR" | tr -d ' /')

if [ ! -d "$SRC_BASE" ]; then
    echo "no colo dir: $SRC_BASE" >&2
    exit 1
fi

# 校验 OPERATOR
case "$OPERATOR" in
    unicom|telecom) ;;
    *)
        echo "bad OPERATOR: $OPERATOR (must be unicom or telecom)" >&2
        exit 1
        ;;
esac

COUNT=0
SKIP=0
FAIL=0
TMP_RESP=$(mktemp)
trap 'rm -f "$TMP_RESP"' EXIT

# POSIX sh 遍历 *.csv
for f in "$SRC_BASE"/*.csv; do
    [ -f "$f" ] || continue

    name=$(basename "$f" .csv)

    # 跳过空文件
    if [ ! -s "$f" ]; then
        echo "  skip empty: $name" >&2
        SKIP=$((SKIP + 1))
        continue
    fi

    # 跳过只含 header 的 CSV
    LINES=$(wc -l < "$f")
    if [ "$LINES" -le 1 ]; then
        echo "  skip header-only: $name ($LINES lines)" >&2
        SKIP=$((SKIP + 1))
        continue
    fi

    echo "  ↑ ${OPERATOR}/${name} ($LINES lines)" >&2
    HTTP_CODE=$(curl -s -X POST \
        "${WORKER_URL}/api/put?operator=${OPERATOR}&colo=${name}" \
        -H "Authorization: Bearer ${PUT_TOKEN}" \
        -H "Content-Type: text/csv" \
        --data-binary "@${f}" \
        -w "%{http_code}" -o "$TMP_RESP" 2>/dev/null || echo "000")

    case "$HTTP_CODE" in
        200)
            COUNT=$((COUNT + 1))
            BODY=$(cat "$TMP_RESP" 2>/dev/null || echo "")
            echo "    ok: $BODY" >&2
            ;;
        *)
            FAIL=$((FAIL + 1))
            echo "    FAIL: $name (HTTP $HTTP_CODE)" >&2
            cat "$TMP_RESP" >&2 || true
            echo >&2
            ;;
    esac
done

echo "uploaded ${COUNT} colo buckets to KV (operator=${OPERATOR}, skipped=${SKIP}, failed=${FAIL})" >&2

# 全部失败才返回非零（部分成功继续）
if [ "$COUNT" -eq 0 ] && [ "$FAIL" -gt 0 ]; then
    exit 1
fi
exit 0
