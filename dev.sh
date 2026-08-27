#!/bin/bash
# WSL 运行时路径（原生 Linux node/go，非 Windows 依赖）
# 注意：/tmp/fan-web-node/bin、/tmp/fan-web-go/bin 等旧路径已失效，
# Go/Node 直接使用系统 PATH（同 AGENTS.md 的 WSL 工具链说明）。

# 用法: ./dev.sh backend [args...] | frontend [args...]
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"

case "${1:-}" in
  backend)
    cd "$ROOT/backend"
    shift
    exec go run . "$@"
    ;;
  frontend)
    cd "$ROOT/frontend"
    shift
    exec npm run dev -- "$@"
    ;;
  *)
    echo "用法: ./dev.sh backend [args...] | frontend [args...]"
    exit 1
    ;;
esac