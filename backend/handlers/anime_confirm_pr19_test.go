package handlers

import (
	"path/filepath"
	"testing"

	"fan-web/database"
	"fan-web/models"
	"fan-web/utils"
)

// PR #19 审核问题 1：确认失败（no_match）时，已有番剧必须零写入——
// 不得插入本批中已能匹配的文件，也不得删除磁盘文件缺失的既有剧集（及其观看进度）。
func TestConfirmInboxNoMatchLeavesExistingAnimeUntouched(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-atomic"
	seed := "[Group] Confirm Show - 01 [1080p].mkv" // 既有剧集，磁盘文件已缺失
	f02 := "[Group] Confirm Show - 02 [1080p].mkv"
	ghost := "[Group] Confirm Show - 03 [1080p].mkv" // 不在磁盘
	writeVideos(t, filepath.Join(root, relDir), f02)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8621))

	anime, err := database.CreateAnime(&models.Anime{
		Title: "Confirm Show", BangumiID: 8621, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{AnimeID: anime.ID, EpNumber: 1, FilePath: seed, Duration: 1400}); err != nil {
		t.Fatal(err)
	}
	before, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("seed: %v %#v", err, before)
	}
	var userID int64
	if err := database.DB.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProgress(userID, before[0].ID, 73, false); err != nil {
		t.Fatal(err)
	}

	resp := postConfirm(t, handler, confirmInboxRequest{BangumiID: 8621, FilePath: relDir, FileNames: []string{f02, ghost}})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonNoMatch {
		t.Fatalf("应 no_match，got code=%d %#v", resp.Code, data)
	}
	if len(data.FileNames) != 1 || data.FileNames[0] != ghost {
		t.Fatalf("缺失项应只有 ghost: %#v", data)
	}
	after, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].FilePath != seed || after[0].EpNumber != 1 || after[0].Duration != 1400 {
		t.Fatalf("确认失败不得改动已有番剧剧集: before=%#v after=%#v", before, after)
	}
	progress, err := database.GetProgress(userID, before[0].ID)
	if err != nil || progress == nil || progress.Position != 73 {
		t.Fatalf("观看进度应保留 73 秒: %v %#v", err, progress)
	}
}
