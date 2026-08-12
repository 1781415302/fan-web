#!/bin/bash
# 一键构建：将前后端编译为单个可执行文件
# 用法: ./build.sh [输出路径，默认 ./dist/fan-web-server]
set -e

# 使用当前 WSL 的系统 PATH。工具链位置见 AGENTS.md。

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
echo "全新部署：不要预置 config.yaml，直接运行并访问 WebUI 初始化页（自动生成 config.yaml 与管理员）。"
echo "升级部署：保留原有 config.yaml 与 data/fan-web.db，仅覆盖可执行文件即可。"
