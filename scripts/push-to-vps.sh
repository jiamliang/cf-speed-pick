#!/bin/sh
# R7000 / AC86U 路由器 → 中央 VPS 同步脚本
#
# 部署位置：/jffs/cf-speed-pick/bin/push-to-vps.sh
#
# 行为：
#   1. 把本次跑的 03_top.csv（带时间戳）推到 VPS 的 results/ 目录
#   2. 把 colo/ 分桶覆盖式推送到 VPS 的 cache/ 目录（给其他路由器订阅）
#   3. 在 VPS 端重载 nginx（如果开了 cache 层）
#
# VPS 端布局（运行此脚本前需要手动建好）：
#   /var/lib/cf-speed-pick/cache/colo/{NRT.csv,SIN.csv,...}
#   /var/lib/cf-speed-pick/results/{节点名}/{YYYYMMDD-HHMMSS}.csv
#
# 配置：
#   VPS_USER   VPS 上的用户名（默认 root）
#   VPS_HOST   VPS 的 Tailscale IP（推荐，不用走公网）
#   NODE_NAME  本节点标识（路由器的 hostname 或自定义）

set -eu

VPS_USER="${VPS_USER:-root}"
VPS_HOST="${VPS_HOST:-100.100.100.1}"
NODE_NAME="${NODE_NAME:-$(uname -n)}"

OUT=/jffs/cf-speed-pick/out/full
RESULTS_BASE=/var/lib/cf-speed-pick/results
CACHE_BASE=/var/lib/cf-speed-pick/cache

DATE=$(date +%Y%m%d-%H%M%S)
LOG=/jffs/cf-speed-pick/log/push.log

mkdir -p "$(dirname "$LOG")"

log() {
    echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*" >> "$LOG"
}

# 0. 先测试连通性（避免 NFS 挂了还硬推）
if ! ping -c 1 -W 3 "$VPS_HOST" >/dev/null 2>&1; then
    log "ERROR: ping $VPS_HOST 失败，跳过推送"
    exit 1
fi

# 1. 推送完整 Top 结果（按时间归档）
if [ -f "$OUT/03_top.csv" ]; then
    REMOTE_DIR="$RESULTS_BASE/$NODE_NAME"
    log "推送 03_top.csv → $VPS_USER@$VPS_HOST:$REMOTE_DIR/${DATE}.csv"
    ssh "$VPS_USER@$VPS_HOST" "mkdir -p '$REMOTE_DIR'" || {
        log "ERROR: ssh 创建目录失败"
        exit 1
    }
    scp "$OUT/03_top.csv" "$VPS_USER@$VPS_HOST:$REMOTE_DIR/${DATE}.csv" || {
        log "ERROR: scp 03_top.csv 失败"
        exit 1
    }
else
    log "WARN: 找不到 $OUT/03_top.csv，跳过"
fi

# 2. 推送 colo 分桶（覆盖式）
COLO_DIR="$OUT/colo"
if [ -d "$COLO_DIR" ]; then
    log "同步 colo 分桶 → $VPS_USER@$VPS_HOST:$CACHE_BASE/colo/"
    ssh "$VPS_USER@$VPS_HOST" "mkdir -p '$CACHE_BASE/colo'" || true
    # 把所有 *.csv 推到 VPS 的 cache/colo/ 下
    scp "$COLO_DIR"/*.csv "$VPS_USER@$VPS_HOST:$CACHE_BASE/colo/" 2>/dev/null || {
        log "WARN: colo 分桶推送失败（可能本次没有 colo 输出）"
    }
else
    log "WARN: 找不到 $COLO_DIR，跳过"
fi

# 3. 重载 VPS 端 nginx（如果配置了 cache 层需要 reload）
ssh "$VPS_USER@$VPS_HOST" "nginx -s reload 2>/dev/null; true" || true

log "推送完成"
