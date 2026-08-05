#!/bin/bash
# 阶段五验收脚本 3：自动入库端到端（临时文件，验收后删除）
set -e
BASE=http://localhost:8080

TOKEN=$(curl -s -X POST $BASE/api/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
AUTH="Authorization: Bearer $TOKEN"
SRC="/home/bishe/work/fan-web/anime/[CheeseAni] Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen [67][CR-WebRip HEVC AAC SRT][简繁内封].mkv"
DIR="/home/bishe/work/fan-web/anime/孤独摇滚"

echo "== 1. 建目录 孤独摇滚（应自动匹配 Bangumi 入库） =="
mkdir -p "$DIR"
cp "$SRC" "$DIR/[Test] Bocchi the Rock - 01 [1080p].mkv"
curl -s -X POST $BASE/api/watcher/scan -H "$AUTH" > /dev/null
sleep 15
curl -s "$BASE/api/animes?page_size=5" -H "$AUTH" | head -c 600; echo
ANIME_ID=$(curl -s "$BASE/api/animes?keyword=孤独摇滚" -H "$AUTH" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)
echo "ANIME_ID=$ANIME_ID"
if [ -z "$ANIME_ID" ]; then echo "自动入库失败"; exit 1; fi

echo "== 2. 集数列表（应有第 1 话） =="
curl -s "$BASE/api/animes/$ANIME_ID/episodes" -H "$AUTH"; echo

echo "== 3. 新增第 2 话文件，触发扫描（增量同步） =="
cp "$SRC" "$DIR/[Test] Bocchi the Rock - 02 [1080p].mkv"
curl -s -X POST $BASE/api/watcher/scan -H "$AUTH" > /dev/null
sleep 10
curl -s "$BASE/api/animes/$ANIME_ID/episodes" -H "$AUTH" | grep -o '"ep_number":[0-9]*'; echo
curl -s $BASE/api/watcher/status -H "$AUTH" | grep -o '"message":"[^"]*"'; echo

echo "== 4. 删除目录，触发扫描（应自动删除番剧） =="
rm -rf "$DIR"
curl -s -X POST $BASE/api/watcher/scan -H "$AUTH" > /dev/null
sleep 10
curl -s "$BASE/api/animes?keyword=孤独摇滚" -H "$AUTH" | head -c 300; echo
curl -s $BASE/api/watcher/status -H "$AUTH" | grep -o '"message":"[^"]*"'; echo
echo "== 验收完成 =="
