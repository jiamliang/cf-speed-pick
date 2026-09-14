#!/bin/sh
# VPS 端订阅生成器
#
# 其他节点（AC86U / OpenWrt / 其他 VPS）通过 wget/curl 拉取 VPS 上缓存的 colo 候选。
#
# 用法：
#   subscribe.sh --url https://cf-cache.example.com --colos NRT,ICN,KIX --top 5
#   subscribe.sh --url https://cf-cache.example.com --colos NRT,HKG --top 5 --min-speed 1.0
#
# 输出：CSV 格式的 Top N IP 列表，可以直接喂给 ShellCrash / OpenClash / passwall。
#
# 注意：POSIX shell 兼容。

set -eu

URL=""
COLOS=""
TOP=5
MIN_SPEED=0
OUT_FILE=""

usage() {
    cat <<EOF
用法: subscribe.sh --url URL --colos NRT,ICN,KIX [--top N] [--min-speed MBps]

参数:
  --url URL          VPS 上 cf-speed-pick cache 的 HTTP 根
  --colos LIST       逗号分隔的 colo 列表（按优先级）
  --top N            每个 colo 取 Top N（默认 5）
  --min-speed MBps   最低速度过滤（默认 0 = 不过滤）
  --out FILE         输出文件路径（默认 stdout）
  -h, --help         显示帮助

示例:
  subscribe.sh --url https://cf.example.com --colos NRT,ICN,KIX
  subscribe.sh --url https://cf.example.com --colos NRT,HKG --min-speed 1.0
EOF
}

# POSIX 参数解析（不支持长选项合并，如 --foo bar）
while [ $# -gt 0 ]; do
    case "$1" in
        --url) URL="$2"; shift 2 ;;
        --colos) COLOS="$2"; shift 2 ;;
        --top) TOP="$2"; shift 2 ;;
        --min-speed) MIN_SPEED="$2"; shift 2 ;;
        --out) OUT_FILE="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "未知参数: $1" >&2; usage; exit 1 ;;
    esac
done

[ -z "$URL" ] && { echo "ERROR: 必须指定 --url" >&2; exit 1; }
[ -z "$COLOS" ] && { echo "ERROR: 必须指定 --colos" >&2; exit 1; }

# 拉取并合并
MERGED=$(mktemp)
trap 'rm -f "$MERGED"' EXIT

# 表头
echo "ip,download_speed_MBps,colo,tier" > "$MERGED"

# 逗号分割 COLOS（POSIX 无 IFS 数组，用 tr + while 循环）
echo "$COLOS" | tr ',' '\n' | while read -r COLO; do
    COLO=$(echo "$COLO" | tr -d ' ')
    [ -z "$COLO" ] && continue
    CSV_URL="$URL/cache/colo/${COLO}.csv"

    TMP=$(mktemp)
    if ! curl -sf --max-time 10 "$CSV_URL" -o "$TMP"; then
        echo "WARN: $CSV_URL 拉取失败，跳过 $COLO" >&2
        rm -f "$TMP"
        continue
    fi

    # 跳过表头，附加数据行
    tail -n +2 "$TMP" | head -n "$TOP" >> "$MERGED"
    rm -f "$TMP"
done

# 速度过滤（awk POSIX 通用）
if [ "$MIN_SPEED" != "0" ]; then
    FILTERED=$(mktemp)
    head -n 1 "$MERGED" > "$FILTERED"
    awk -F',' -v min="$MIN_SPEED" 'NR>1 && $2+0 >= min' "$MERGED" >> "$FILTERED"
    mv "$FILTERED" "$MERGED"
fi

# 输出
if [ -n "$OUT_FILE" ]; then
    cp "$MERGED" "$OUT_FILE"
    echo "已写入 $OUT_FILE" >&2
else
    cat "$MERGED"
fi
