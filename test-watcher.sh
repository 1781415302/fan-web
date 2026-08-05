#!/bin/bash
# 阶段五验收脚本（临时文件，验收后删除）
set -e
BASE=http://localhost:8080

TOKEN=$(curl -s -X POST $BASE/api/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
if [ -z "$TOKEN" ]; then echo "登录失败"; exit 1; fi
echo "== 登录成功 =="
AUTH="Authorization: Bearer $TOKEN"

echo "== watcher/status =="
curl -s $BASE/api/watcher/status -H "$AUTH"; echo

echo "== 构造测试目录（乱码名，必进 pending） =="
mkdir -p /home/bishe/work/fan-web/anime/验收测试番剧XYZ123
cp "/home/bishe/work/fan-web/anime/[CheeseAni] Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen [67][CR-WebRip HEVC AAC SRT][简繁内封].mkv" \
   "/home/bishe/work/fan-web/anime/验收测试番剧XYZ123/[Test] XYZ123 - 01 [1080p].mkv"

echo "== 触发扫描 =="
curl -s -X POST $BASE/api/watcher/scan -H "$AUTH"; echo
sleep 12

echo "== 扫描后状态 =="
curl -s $BASE/api/watcher/status -H "$AUTH"; echo
echo "== 待入库列表 =="
curl -s "$BASE/api/watcher/pending" -H "$AUTH"; echo
