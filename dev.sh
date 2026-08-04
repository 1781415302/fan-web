#!/bin/bash
# 用法: ./dev.sh backend | frontend
set -e
cd "$(dirname "$0")"

case "$1" in
  backend)
    cd backend
    exec /home/bishe/go/bin/go run .
    ;;
  frontend)
    cd frontend
    exec /home/bishe/.qoder-server/bin/4ee322f78fe8606b0bcb6dd6991c61463b3112de/node node_modules/vite/bin/vite.js
    ;;
  *)
    echo "用法: ./dev.sh backend | frontend"
    exit 1
    ;;
esac
