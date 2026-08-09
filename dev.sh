#!/bin/bash
# WSL 运行时路径（原生 Linux node/go，非 Windows 依赖）
# 注意：/tmp/fan-web-node/bin、/tmp/fan-web-go/bin 等旧路径已失效，
# Go/Node 直接使用系统 PATH（同 AGENTS.md 的 WSL 工具链说明）。

# 用法: ./dev.sh backend | frontend
set -e
cd "$(dirname "$0")"

case "$1" in
  backend)
    cd backend
    exec go run .
    ;;
  frontend)
    cd frontend
    exec npm run dev
    ;;
  *)
    echo "用法: ./dev.sh backend | frontend"
    exit 1
    ;;
esac