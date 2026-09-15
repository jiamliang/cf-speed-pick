#!/bin/sh
# VPS 上的 cron watchdog
#
# VPS 没有梅林 cru，直接用 crontab。但要保证每 6 小时 cf-speed-pick 跑一次。
#
# 部署：/usr/local/bin/ensure-cron-vps.sh
# 加到 /etc/crontab 或 root 的 crontab：
#   */30 * * * * /usr/local/bin/ensure-cron-vps.sh
#
# 检测 crontab 里有没有指定命令；没有就补回。

set -eu

WATCHDOG_LOG=/var/log/cf-speed-pick-cron-watchdog.log
EXPECTED_PATTERN="cf-speed-pick|unicom-cron|qingdao-cron"

RECOVERED=0

# 读当前 crontab
CURRENT=$(crontab -l 2>/dev/null || echo "")

if ! echo "$CURRENT" | grep -qE "$EXPECTED_PATTERN"; then
    echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] 缺失 cf-speed-pick cron 条目，恢复" >> "$WATCHDOG_LOG"

    # 追加（保留原有 cron）
    {
        echo "$CURRENT"
        echo "# cf-speed-pick: 每 6 小时跑一次"
        echo "0 */6 * * * /usr/local/bin/unicom-cron.sh >/dev/null 2>&1"
    } | crontab -

    RECOVERED=1
fi

if [ "$RECOVERED" = "1" ]; then
    echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] cron 已恢复" >> "$WATCHDOG_LOG"
fi
