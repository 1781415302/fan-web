package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services"
	"fan-web/services/matching"
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

// setupScanHandlerV2WithFixture 同 setupScanHandlerV2，额外返回 Bangumi 夹具以便测试包一层代理。
func setupScanHandlerV2WithFixture(t *testing.T, rootPath string, seeds ...bangumiSeed) (*AnimeHandler, *bangumiFixture) {
	t.Helper()
	_ = setupScanHandlerV2(t, rootPath) // 只借用其临时数据库与用户初始化
	fix := startBangumiFixture(t, seeds...)
	rt, err := services.NewMatchingRuntime(fix.bangumi, matching.EngineV2, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	sc := services.NewScannerService(rootPath, services.WithScannerMatching(rt))
	return NewAnimeHandler(fix.bangumi, sc), fix
}

// PR #19 审核问题 2：Bangumi 详情请求阻塞期间另一写入者先建了同目录同条目，
// CreateAnime 命中唯一索引返回既有行；确认失败时不得把这条“别人建的”番剧回滚删除。
func TestConfirmInboxConcurrentCreatorNotRolledBack(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-race"
	f01 := "[Group] Confirm Show - 01 [1080p].mkv"
	ghost := "[Group] Confirm Show - 02 [1080p].mkv" // 不在磁盘 → no_match
	writeVideos(t, filepath.Join(root, relDir), f01)
	handler, fix := setupScanHandlerV2WithFixture(t, root, confirmShowSeed(8622))

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v0/subjects/") {
			first := false
			once.Do(func() { first = true })
			if first {
				close(entered)
				<-release
			}
		}
		req, err := http.NewRequest(r.Method, fix.server.URL+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		req.Header = r.Header.Clone()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer res.Body.Close()
		for k, vs := range res.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	t.Cleanup(proxy.Close)
	fix.bangumi.SetBaseURL(proxy.URL)

	done := make(chan confirmResponse, 1)
	go func() {
		done <- postConfirm(t, handler, confirmInboxRequest{BangumiID: 8622, FilePath: relDir, FileNames: []string{f01, ghost}})
	}()
	<-entered
	other, err := database.CreateAnime(&models.Anime{Title: "Confirm Show", BangumiID: 8622, EpCount: 12, FilePath: relDir})
	if err != nil || other == nil {
		t.Fatalf("另一写入者建档失败: %v", err)
	}
	close(release)
	resp := <-done

	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonNoMatch {
		t.Fatalf("应 no_match，got code=%d %#v", resp.Code, data)
	}
	got, err := database.GetAnimeByID(other.ID)
	if err != nil || got == nil {
		t.Fatalf("其他流程创建的番剧不得被回滚删除: %v %#v", err, got)
	}
	if n := countAnimesByBangumiID(t, 8622); n != 1 {
		t.Fatalf("应仍只有一条番剧，got %d", n)
	}
}
