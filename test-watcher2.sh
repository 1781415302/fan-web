#!/bin/bash
# 阶段五验收脚本 2（临时文件，验收后删除）
set -e
BASE=http://localhost:8080

TOKEN=$(curl -s -X POST $BASE/api/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
AUTH="Authorization: Bearer $TOKEN"

echo "== 忽略 pending #1 =="
curl -s -X POST $BASE/api/watcher/pending/1/ignore -H "$AUTH"; echo

echo "== 触发扫描（ignored 不应重复生成） =="
curl -s -X POST $BASE/api/watcher/scan -H "$AUTH" > /dev/null
sleep 8
curl -s "$BASE/api/watcher/pending?include_ignored=1" -H "$AUTH"; echo

echo "== 移除 pending #1 并删除测试目录 =="
curl -s -X DELETE $BASE/api/watcher/pending/1 -H "$AUTH"; echo
rm -rf "/home/bishe/work/fan-web/anime/验收测试番剧XYZ123"

echo "== 触发扫描（目录已删，pending 不应重建，因为无视频文件） =="
curl -s -X POST $BASE/api/watcher/scan -H "$AUTH" > /dev/null
sleep 8
curl -s "$BASE/api/watcher/pending?include_ignored=1" -H "$AUTH"; echo
curl -s $BASE/api/watcher/status -H "$AUTH"; echo
