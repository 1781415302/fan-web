#!/bin/bash
# 一键构建：将前后端编译为单个可执行文件
# 用法:
#   ./build.sh                       # 本地模拟发布：dist/fan-web-server，版本为最近 tag
#   DEV=1 ./build.sh                 # 显式开发构建：版本追加 +dev
#   ./build.sh <输出路径>             # 自定义产物路径（交叉编译时常用，见下）
# 说明: 默认无参数输出固定为 ./dist/fan-web-server（兼容历史/文档/发布流程）；
#   交叉编译时显式传 GOOS/GOARCH 与输出路径（如
#   GOOS=linux GOARCH=arm64 ./build.sh dist/fan-web-server-linux-arm64），
#   或直接使用 .opencode/skills/publish-release 发布流程。
set -euo pipefail

# 使用当前 WSL 的系统 PATH。工具链位置见 AGENTS.md。

ROOT="$(cd "$(dirname "$0")" && pwd)"
[ -n "$ROOT" ] || { echo "错误: 无法确定脚本根目录" >&2; exit 1; }

GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
OUT="${1:-$ROOT/dist/fan-web-server}"
OUT_DIR="$(dirname "$OUT")"

echo "==> [1/3] 构建前端..."
cd "$ROOT/frontend"
npm run build
# 校验前端产物完整性，避免嵌入空/残缺的 dist
test -s "$ROOT/frontend/dist/index.html" || { echo "错误: 前端产物缺失 (frontend/dist/index.html)" >&2; exit 1; }
if [ ! -d "$ROOT/frontend/dist/assets" ] || [ -z "$(ls -A "$ROOT/frontend/dist/assets" 2>/dev/null)" ]; then
  echo "错误: 前端产物不完整 (frontend/dist/assets 为空或缺失)" >&2
  exit 1
fi

echo "==> [2/3] 嵌入前端资源到 Go..."
# 先复制到临时目录再原子替换，降低中断时残留已删除占位文件的风险
TMP_DIST="$(mktemp -d)"
cp -r "$ROOT/frontend/dist" "$TMP_DIST/dist"
rm -rf "$ROOT/backend/web/dist"
mv "$TMP_DIST/dist" "$ROOT/backend/web/dist"

echo "==> [3/3] 编译后端..."
cd "$ROOT/backend"
go vet ./...
go test ./...
mkdir -p "$OUT_DIR"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
# +dev 只用于明确的开发构建（DEV=1）；发布/本地模拟发布保持 VERSION 原样
# （在 tag 提交上 describe 即干净 tag vX.Y.Z），不再因未设 CI 而拼 +dev。
if [ "${DEV:-0}" = "1" ]; then
  case "$VERSION" in
    *+dev|dev) ;;
    *) VERSION="${VERSION}+dev" ;;
  esac
fi
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o "$OUT" .

echo "完成: $OUT"
echo "全新部署：不要预置 config.yaml，直接运行并访问 WebUI 初始化页（自动生成 config.yaml 与管理员）。"
echo "升级部署：保留原有 config.yaml 与 data/fan-web.db，仅覆盖可执行文件即可。"
