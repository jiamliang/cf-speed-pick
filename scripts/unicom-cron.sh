#!/bin/sh
# 联通 VPS 上的 cf-speed-pick cron 封装（unicom 运营商）
#
# 部署位置：/usr/local/bin/unicom-cron.sh
# 加 cron：crontab -e → 0 */6 * * * /usr/local/bin/unicom-cron.sh
#
# 与 qingdao-cron.sh 区别：
#   - 用 OPERATOR=unicom（你这边是北京/上海联通）
#   - 参数稍保守（联通 IDC 一般也够，但避免太激进）
#
# 注意：POSIX shell 兼容，不依赖 bash。
#
# 必填环境变量（在 /etc/cf-speed-pick/env 里配，cron 启动时会自动 source）：
#   WORKER_URL  — https://vless.cf.peeweecap.com
#   PUT_TOKEN   — Worker 的 PUT_TOKEN secret

set -eu

BIN=/usr/local/bin/cf-speed-pick
OUT=/var/lib/cf-speed-pick/out/full
LOG=/var/log/cf-speed-pick.log
UPLOAD=/usr/local/bin/upload-to-kv.sh
ENV_FILE=/etc/cf-speed-pick/env

# source 环境变量（如果存在）
if [ -f "$ENV_FILE" ]; then
    . "$ENV_FILE"
fi

WORKER_URL="${WORKER_URL:?WORKER_URL must be set in $ENV_FILE or env}"
PUT_TOKEN="${PUT_TOKEN:?PUT_TOKEN must be set in $ENV_FILE or env}"

mkdir -p "$OUT" "$(dirname "$LOG")"

TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] ===== [unicom] 开始完整跑 =====" >> "$LOG"

"$BIN" \
    -out "$OUT" \
    -debug \
    -n 100 \
    -t 2 \
    -max-delay 150 \
    -colo-strategy tiered \
    -dt 5s \
    -dn 20 \
    -colo-priority "NRT,ICN,KIX,FUK,TPE,HKG,SIN,LAX,SJC" \
    -output-colos \
    >> "$LOG" 2>&1

RC=$?
TS=$(date +%Y%m%d-%H%M%S)
echo "[$TS] ===== [unicom] 退出码=$RC =====" >> "$LOG"

# 上传 colo 桶到 Worker KV（联通节点）
if [ -x "$UPLOAD" ]; then
    WORKER_URL="$WORKER_URL" \
    PUT_TOKEN="$PUT_TOKEN" \
    OPERATOR=unicom \
    "$UPLOAD" >> "$LOG" 2>&1 \
        || echo "[$TS] upload-to-kv 失败（不影响 cron 退出码）" >> "$LOG"
else
    echo "[$TS] 跳过 upload-to-kv（$UPLOAD 不存在或不可执行）" >> "$LOG"
fi

exit $RC
