#!/bin/sh
# build.sh — 编译 cf-speed-pick 的两个架构版本到 deploy/
#
# 用法（在仓库根目录）：
#   ./deploy/build.sh
#   或
#   docker run --rm -v "$(pwd):/src" -w /src golang:1.21-alpine sh /src/deploy/build.sh
#
# 前提：Go 1.21+（系统装或用 Docker golang image）

set -eu

# 找到仓库根
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_ROOT"

if ! command -v go >/dev/null 2>&1; then
    echo "go not found — use docker instead:"
    echo "  docker run --rm -v \"\$(pwd):/src\" -w /src golang:1.21-alpine sh /src/deploy/build.sh"
    exit 1
fi

# 编译参数
LDFLAGS="-s -w"  # 去掉符号表和调试信息，缩小体积
OUT_DIR="deploy"

# amd64 (服务器)
echo "→ building linux/amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$OUT_DIR/cf-speed-pick-linux-amd64" .
ls -lh "$OUT_DIR/cf-speed-pick-linux-amd64"

# arm64 (路由器)
echo "→ building linux/arm64..."
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$OUT_DIR/cf-speed-pick-linux-arm64" .
ls -lh "$OUT_DIR/cf-speed-pick-linux-arm64"

echo ""
echo "✓ done"
echo "  deploy/cf-speed-pick-linux-amd64  (x86_64 服务器)"
echo "  deploy/cf-speed-pick-linux-arm64  (aarch64 路由器)"
