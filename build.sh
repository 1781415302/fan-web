#!/bin/bash
# 一键构建：将前后端编译为单个可执行文件
# 用法: ./build.sh [输出路径，默认 ./dist/fan-web-server]
set -e

# WSL 运行时路径（原生 Linux node/go）
NODE_HOME=/tmp/fan-web-node/bin
GO_HOME=/tmp/fan-web-go/bin
export PATH="$NODE_HOME:$GO_HOME:$PATH"

ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT="${1:-$ROOT/dist/fan-web-server}"
OUT_DIR="$(dirname "$OUT")"

echo "==> [1/3] 构建前端..."
cd "$ROOT/frontend"
npm run build

echo "==> [2/3] 嵌入前端资源到 Go..."
rm -rf "$ROOT/backend/web/dist"
cp -r "$ROOT/frontend/dist" "$ROOT/backend/web/dist"

echo "==> [3/3] 编译后端..."
cd "$ROOT/backend"
mkdir -p "$OUT_DIR"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --abbrev=0 2>/dev/null || echo dev)}"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o "$OUT" .

echo "完成: $OUT"
echo "部署时需在可执行文件旁放置 config.yaml（修改视频目录等配置）。"