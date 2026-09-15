#!/bin/sh
# R7000 / AC86U 路由器上的 cf-speed-pick cron 封装
#
# 部署位置：/jffs/cf-speed-pick/bin/r7000-cron.sh
# 加 cron：cru a cf-speed-pick-full "0 */6 * * * /jffs/cf-speed-pick/bin/r7000-cron.sh"
#
# 行为：
#   1. 跑完整三层（路由器 CPU 较弱，参数保守）
#   2. 输出到 /jffs/cf-speed-pick/out/full/
#   3. 调用 push-to-vps.sh 把 colo 候选推到 VPS
#
# 注意：梅林 / Koolshare 的默认 shell 是 sh（busybox ash），不能用 bash 特性。

set -eu

BIN=/jffs/cf-speed-pick/bin/cf-speed-pick
OUT=/jffs/cf-speed-pick/out/full
LOG=/jffs/cf-speed-pick/log/run.log
VPS_PUSH=/jffs/cf-speed-pick/bin/push-to-vps.sh
UPLOAD=/jffs/cf-speed-pick/bin/upload-to-kv.sh

mkdir -p "$OUT" "$(dirname "$LOG")"

TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] ===== 开始 cf-speed-pick 完整跑 =====" >> "$LOG"

# 路由器低配参数：
#   -n 50         TCPing 并发 50（默认 200 太重）
#   -t 2          每个 IP 测 2 次
#   -max-delay 100 路由器对延迟更敏感
#   -colo-strategy tiered  按 tier 分层采样
#   -dt 5s        下载测速 5 秒（默认 10）
#   -dn 10        最终 Top 10
#   -output-colos 按 colo 分桶输出（给 VPS 用）
"$BIN" \
    -out "$OUT" \
    -debug \
    -n 50 \
    -t 2 \
    -max-delay 100 \
    -colo-strategy tiered \
    -dt 5s \
    -dn 10 \
    -output-colos \
    >> "$LOG" 2>&1

RC=$?
TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] ===== cf-speed-pick 退出码=$RC =====" >> "$LOG"

# 推到 VPS（nginx 暴露给其他路由器订阅）
if [ -x "$VPS_PUSH" ]; then
    "$VPS_PUSH" >> "$LOG" 2>&1 || echo "[$TS] push-to-vps 失败" >> "$LOG"
fi

# 上传 colo 桶到 Worker KV（R7000 是北京联通）
if [ -x "$UPLOAD" ] && [ -n "${WORKER_URL:-}" ] && [ -n "${PUT_TOKEN:-}" ]; then
    WORKER_URL="$WORKER_URL" \
    PUT_TOKEN="$PUT_TOKEN" \
    OPERATOR=unicom \
    "$UPLOAD" >> "$LOG" 2>&1 \
        || echo "[$TS] upload-to-kv 失败（不影响 cron 退出码）" >> "$LOG"
else
    echo "[$TS] 跳过 upload-to-kv（UPLOAD=$UPLOAD, WORKER_URL/TOKEN 已配？）" >> "$LOG"
fi

exit $RC
