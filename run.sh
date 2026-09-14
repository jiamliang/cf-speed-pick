#!/bin/bash
# cf-speed-pick cron 脚本
#
# 部署步骤：
#   1. 把 cf-speed-pick 二进制放到 /usr/local/bin/（VPS）或 /jffs/（路由器）
#   2. crontab -e 加一行：
#        0 */2 * * * /usr/local/bin/cf-speed-pick/run.sh
#
# 说明：
#   - 每 2 小时跑一次（CF IP 路由变化没那么频繁）
#   - 输出 CSV 到 /var/lib/cf-speed-pick/out/
#   - 日志追加到 /var/log/cf-speed-pick.log
#
# 路由器低配参数（AC86U）：
#   /jffs/cf-speed-pick -n 50 -t 2 -dn 5 -dt 5s -out /jffs/cf-speed-pick/out
#
# SPDX-License-Identifier: GPL-3.0-or-later

set -e

# === 配置区 ===
# 二进制路径（按部署环境二选一）
BIN_VPS="/usr/local/bin/cf-speed-pick"
BIN_ROUTER="/jffs/cf-speed-pick"

# 输出目录
OUT_DIR="/var/lib/cf-speed-pick/out"

# 日志文件
LOG_FILE="/var/log/cf-speed-pick.log"

# 默认参数（VPS）
PARAMS="-n 200 -t 4 -dt 10s -dn 10"

# 检测环境
if [[ -x "$BIN_VPS" ]]; then
    BIN="$BIN_VPS"
elif [[ -x "$BIN_ROUTER" ]]; then
    # 路由器自动切低配
    BIN="$BIN_ROUTER"
    PARAMS="-n 50 -t 2 -dt 5s -dn 5"
else
    echo "$(date -Iseconds) ERROR: 找不到 cf-speed-pick 二进制" >> "$LOG_FILE"
    exit 1
fi

# === 执行 ===
echo "========================================" >> "$LOG_FILE"
echo "$(date -Iseconds) 开始 cf-speed-pick 测速" >> "$LOG_FILE"
echo "BIN=$BIN PARAMS=$PARAMS OUT=$OUT_DIR" >> "$LOG_FILE"

"$BIN" $PARAMS -out "$OUT_DIR" >> "$LOG_FILE" 2>&1
RC=$?

echo "$(date -Iseconds) 结束 rc=$RC" >> "$LOG_FILE"

# === 保留最近 7 天的日志 ===
if [[ -f "$LOG_FILE" ]]; then
    # 简单截断：保留末尾 5MB
    SIZE=$(stat -c%s "$LOG_FILE" 2>/dev/null || stat -f%z "$LOG_FILE")
    if [[ $SIZE -gt 5242880 ]]; then
        tail -c 1048576 "$LOG_FILE" > "$LOG_FILE.tmp"
        mv "$LOG_FILE.tmp" "$LOG_FILE"
    fi
fi

exit $RC