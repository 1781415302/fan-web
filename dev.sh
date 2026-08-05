#!/bin/bash
# WSL 运行时路径（原生 Linux node/go，非 Windows 依赖）
NODE_HOME=/tmp/fan-web-node/bin
GO_HOME=/tmp/fan-web-go/bin

# 用法: ./dev.sh backend | frontend
set -e
cd "$(dirname "$0")"

export PATH="$NODE_HOME:$GO_HOME:$PATH"

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