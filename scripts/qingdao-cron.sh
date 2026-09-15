#!/bin/sh
# 青岛/福州 VPS 上的 cf-speed-pick cron 封装
#
# 部署位置：/usr/local/bin/qingdao-cron.sh
# 加 cron：crontab -e → 0 */6 * * * /usr/local/bin/qingdao-cron.sh
#
# VPS 资源充足，跑完整三层 + 给 R7000 推送候选。
# VPS 自己也是测速节点，跑完后也把自己的 Top 推到自己 cache。
#
# 注意：POSIX shell 兼容，不依赖 bash。
#
# 必填环境变量（在 /etc/cf-speed-pick/env 里配）：
#   WORKER_URL  — https://vless.cf.peeweecap.com
#   PUT_TOKEN   — Worker 的 PUT_TOKEN secret

set -eu

BIN=/usr/local/bin/cf-speed-pick
OUT=/var/lib/cf-speed-pick/out/full
LOG=/var/log/cf-speed-pick.log
NODE=$(hostname)

# Worker KV 上传（cf-speed-pick colo 桶 → Cloudflare KV）
UPLOAD=/usr/local/bin/upload-to-kv.sh
ENV_FILE=/etc/cf-speed-pick/env

# source 环境变量
if [ -f "$ENV_FILE" ]; then
    . "$ENV_FILE"
fi

WORKER_URL="${WORKER_URL:-}"
PUT_TOKEN="${PUT_TOKEN:-}"

mkdir -p "$OUT" "$(dirname "$LOG")"

TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] ===== [$NODE] 开始完整跑 =====" >> "$LOG"

# VPS 配置（资源充足，用默认参数或略激进）
# 输出重定向到 LOG（tee 在 POSIX sh 里行为可能不一致）
"$BIN" \
    -out "$OUT" \
    -debug \
    -n 200 \
    -t 4 \
    -max-delay 100 \
    -colo-strategy tiered \
    -dt 10s \
    -dn 15 \
    -output-colos \
    >> "$LOG" 2>&1

RC=$?
TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] ===== [$NODE] 退出码=$RC =====" >> "$LOG"

# VPS 端做中央缓存（nginx 提供给路由器订阅）
CACHE_BASE=/var/lib/cf-speed-pick/cache
mkdir -p "$CACHE_BASE/colo"

# 覆盖式更新 colo 分桶
if [ -d "$OUT/colo" ]; then
    # POSIX sh 复制文件
    for f in "$OUT/colo"/*.csv; do
        [ -f "$f" ] || continue
        cp -f "$f" "$CACHE_BASE/colo/"
    done
    COUNT=$(ls "$OUT/colo"/*.csv 2>/dev/null | wc -l)
    echo "[$TS] colo 缓存已更新 ($COUNT 个 colo)" >> "$LOG"
fi

# 重载 nginx（让新文件立即可访问）
if command -v nginx >/dev/null 2>&1; then
    nginx -s reload 2>/dev/null || true
fi

# 上传 colo 桶到 Worker KV（胶州 VPS 是电信）
if [ -x "$UPLOAD" ] && [ -n "$WORKER_URL" ] && [ -n "$PUT_TOKEN" ]; then
    WORKER_URL="$WORKER_URL" \
    PUT_TOKEN="$PUT_TOKEN" \
    OPERATOR=telecom \
    "$UPLOAD" >> "$LOG" 2>&1 \
        || echo "[$TS] upload-to-kv 失败（不影响 cron 退出码）" >> "$LOG"
else
    echo "[$TS] 跳过 upload-to-kv（UPLOAD=$UPLOAD, WORKER_URL/TOKEN 已配？）" >> "$LOG"
fi

exit $RC
