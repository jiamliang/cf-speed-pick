#!/bin/sh
# 梅林固件 cron watchdog
#
# 梅林的 cru 命令把 cron 写到 NVRAM。Web UI 改任何设置都会触发 NVRAM 重写，
# 导致自定义 cron 丢失。这个脚本每 30 分钟检查一次，丢失就补回。
#
# 部署：/jffs/cf-speed-pick/bin/ensure-cron.sh
# 加 cron：cru a cf-speed-pick-watchdog "*/30 * * * * /jffs/cf-speed-pick/bin/ensure-cron.sh"

set -eu

WATCHDOG_LOG=/jffs/cf-speed-pick/log/cron-watchdog.log
mkdir -p "$(dirname "$WATCHDOG_LOG")"

# 期望存在的 cron 条目（key → 命令）
EXPECTED_CRON_KEYS="cf-speed-pick-full cf-speed-pick-push"

RECOVERED=0
for KEY in $EXPECTED_CRON_KEYS; do
    if ! cru l | grep -q "^$KEY"; then
        echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] 缺失 $KEY，恢复" >> "$WATCHDOG_LOG"

        case "$KEY" in
            cf-speed-pick-full)
                cru a cf-speed-pick-full "0 */6 * * * /jffs/cf-speed-pick/bin/r7000-cron.sh"
                ;;
            cf-speed-pick-push)
                # 注意：push 已合并进 r7000-cron.sh 末尾，这里只是个 fallback
                cru a cf-speed-pick-push "30 */6 * * * /jffs/cf-speed-pick/bin/push-to-vps.sh"
                ;;
        esac
        RECOVERED=1
    fi
done

if [ "$RECOVERED" = "1" ]; then
    echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] cron 已恢复" >> "$WATCHDOG_LOG"
fi
