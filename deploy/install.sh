#!/bin/sh
# install.sh — 注册 cron + 写 env（不复制任何文件）
#
# 用法（在仓库根目录运行）：
#   ./scripts/install.sh                          # 交互式（问 OPERATOR）
#   ./scripts/install.sh --operator telecom       # 非交互
#   ./scripts/install.sh --operator unicom --cron "0 */4 * * *"
#
# 行为：
#   1. detect uname -m → 选对应架构 binary（仅验证存在）
#   2. 询问或读 --operator 参数（telecom / unicom）
#   3. 写脚本同目录的 env（如不存在 → 复制 env.example，提示用户填）
#   4. 把 OPERATOR 注入 env（如还没设）
#   5. 写 cf-ranges.txt（如不存在 → 复制 .example）
#   6. 注册 cron（用绝对路径）：
#      - 标准 Linux：crontab -l 去掉旧的加上新的
#      - 梅林固件：cru a cf-speed-pick "..."
#
# 注意：POSIX shell 兼容。
# 注意：本脚本不复制任何 binary / 脚本文件，全部在当前目录操作。

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ARCH=$(uname -m)

# === 1. detect 架构 → 选 binary ===
case "$ARCH" in
    x86_64|amd64)
        CFSP_BIN="$SCRIPT_DIR/cf-speed-pick-linux-amd64"
        PLATFORM="server (x86_64)"
        ;;
    aarch64|arm64|armv7l)
        CFSP_BIN="$SCRIPT_DIR/cf-speed-pick-linux-arm64"
        PLATFORM="router (arm)"
        ;;
    *)
        echo "unsupported arch: $ARCH" >&2
        echo "need x86_64 (server) or aarch64/armv7l (router)" >&2
        exit 1
        ;;
esac

if [ ! -x "$CFSP_BIN" ]; then
    echo "binary not found or not executable: $CFSP_BIN" >&2
    echo "expected at: $CFSP_BIN" >&2
    exit 1
fi

if [ ! -x "$SCRIPT_DIR/speedtest-and-upload.sh" ]; then
    echo "speedtest-and-upload.sh not found or not executable: $SCRIPT_DIR/speedtest-and-upload.sh" >&2
    exit 1
fi

echo "platform: $PLATFORM"
echo "binary:   $CFSP_BIN"

# === 2. parse args ===
OPERATOR=""
CRON_EXPR="0 */6 * * *"
CRON_NAME="cf-speed-pick"

while [ $# -gt 0 ]; do
    case "$1" in
        --operator)
            OPERATOR="$2"
            shift 2
            ;;
        --cron)
            CRON_EXPR="$2"
            shift 2
            ;;
        --name)
            CRON_NAME="$2"
            shift 2
            ;;
        -h|--help)
            echo "usage: $0 [--operator telecom|unicom] [--cron \"0 */6 * * *\"] [--name cf-speed-pick]"
            exit 0
            ;;
        *)
            echo "unknown arg: $1" >&2
            exit 1
            ;;
    esac
done

# === 3. 询问 OPERATOR（如果没传） ===
if [ -z "$OPERATOR" ]; then
    if [ -t 0 ]; then
        printf "operator (telecom/unicom): "
        read -r OPERATOR
    else
        echo "OPERATOR not given and stdin is not a TTY" >&2
        echo "use --operator telecom|unicom" >&2
        exit 1
    fi
fi

case "$OPERATOR" in
    telecom|unicom) ;;
    *)
        echo "bad OPERATOR: $OPERATOR (must be telecom or unicom)" >&2
        exit 1
        ;;
esac

# === 4. env 文件 ===
ENV_FILE="$SCRIPT_DIR/env"

if [ ! -f "$ENV_FILE" ]; then
    if [ ! -f "$SCRIPT_DIR/env.example" ]; then
        echo "missing $SCRIPT_DIR/env.example — cannot bootstrap env" >&2
        exit 1
    fi
    cp "$SCRIPT_DIR/env.example" "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    echo ""
    echo "wrote $ENV_FILE from env.example"
    echo "please edit it (set WORKER_URL + PUT_TOKEN), then re-run:"
    echo "  $0 --operator $OPERATOR --cron \"$CRON_EXPR\""
    exit 0
fi

# chmod 600（重跑 install.sh 时也保证权限）
chmod 600 "$ENV_FILE"

# 注入 OPERATOR（如还没设）
if ! grep -q "^OPERATOR=" "$ENV_FILE"; then
    echo "" >> "$ENV_FILE"
    echo "OPERATOR=$OPERATOR" >> "$ENV_FILE"
    echo "added OPERATOR=$OPERATOR to $ENV_FILE"
fi

# 验证 env 必须有 WORKER_URL 和 PUT_TOKEN
if ! grep -q "^WORKER_URL=" "$ENV_FILE" || ! grep -q "^PUT_TOKEN=" "$ENV_FILE"; then
    echo "WORKER_URL or PUT_TOKEN missing in $ENV_FILE" >&2
    echo "please edit and re-run: $0 --operator $OPERATOR" >&2
    exit 1
fi

# === 5. cf-ranges.txt ===
if [ ! -f "$SCRIPT_DIR/cf-ranges.txt" ]; then
    if [ -f "$SCRIPT_DIR/cf-ranges.txt.example" ]; then
        cp "$SCRIPT_DIR/cf-ranges.txt.example" "$SCRIPT_DIR/cf-ranges.txt"
        echo "wrote $SCRIPT_DIR/cf-ranges.txt from .example"
    else
        echo "WARNING: no cf-ranges.txt and no .example — speedtest will use built-in ranges" >&2
    fi
fi

# === 6. 注册 cron（绝对路径） ===
CRON_CMD="$SCRIPT_DIR/speedtest-and-upload.sh"
CRON_LINE="$CRON_EXPR $CRON_CMD"

# 检测 cron 工具
if command -v cru >/dev/null 2>&1; then
    # 梅林固件
    if cru l | grep -qE "^[0-9]+\s+.*$CRON_NAME"; then
        cru d "$CRON_NAME" 2>/dev/null || true
        echo "removed existing cru: $CRON_NAME"
    fi
    cru a "$CRON_NAME" "$CRON_LINE"
    echo "registered via cru: $CRON_NAME -> $CRON_LINE"
elif command -v crontab >/dev/null 2>&1; then
    # 标准 crontab
    CURRENT=$(crontab -l 2>/dev/null || true)
    # 去掉旧的 speedtest 行
    FILTERED=$(echo "$CURRENT" | grep -v "$CRON_CMD" || true)
    {
        echo "$FILTERED"
        echo "$CRON_LINE # $CRON_NAME"
    } | crontab -
    echo "registered via crontab: $CRON_LINE"
else
    echo "no cru or crontab found — please register manually:" >&2
    echo "  $CRON_LINE" >&2
    exit 1
fi

echo ""
echo "✓ install done"
echo ""
echo "verify:"
echo "  - env file:    $ENV_FILE"
echo "  - ranges file: $SCRIPT_DIR/cf-ranges.txt"
echo "  - binary:      $CFSP_BIN"
echo "  - script:      $CRON_CMD"
echo "  - schedule:    $CRON_EXPR ($PLATFORM)"
echo ""
echo "test now:"
echo "  $CRON_CMD"
