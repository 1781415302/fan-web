package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services"
	"fan-web/services/matching"
	"fan-web/utils"
)

type confirmResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type confirmSuccessData struct {
	Anime    models.Anime     `json:"anime"`
	Created  bool             `json:"created"`
	Scanned  int              `json:"scanned"`
	Episodes []models.Episode `json:"episodes"`
}

func postConfirmRaw(t *testing.T, handler *AnimeHandler, body []byte) (int, confirmResponse) {
	t.Helper()
	router := gin.New()
	router.POST("/api/library/inbox/confirm", handler.Confirm)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/library/inbox/confirm", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	var resp confirmResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return recorder.Code, resp
}

func postConfirm(t *testing.T, handler *AnimeHandler, req confirmInboxRequest) confirmResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	status, resp := postConfirmRaw(t, handler, body)
	if status != http.StatusOK {
		t.Fatalf("HTTP 状态应恒为 200，got %d", status)
	}
	return resp
}

func decodeConfirmError(t *testing.T, resp confirmResponse) confirmErrorData {
	t.Helper()
	var data confirmErrorData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("decode error data %s: %v", resp.Data, err)
	}
	if data.Message != resp.Message {
		t.Fatalf("data.message 应与 message 相同: %q vs %q", data.Message, resp.Message)
	}
	return data
}

func decodeConfirmSuccess(t *testing.T, resp confirmResponse) confirmSuccessData {
	t.Helper()
	if resp.Code != utils.CodeSuccess {
		t.Fatalf("应成功，got code=%d message=%q data=%s", resp.Code, resp.Message, resp.Data)
	}
	var data confirmSuccessData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("decode success data %s: %v", resp.Data, err)
	}
	return data
}

func confirmShowSeed(id int) bangumiSeed {
	return bangumiSeed{
		Subject: matching.Subject{
			ID: id, Name: "Confirm Show", NameCn: "确认番",
			Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		},
		SearchKeys: []string{"Confirm Show", "确认番"},
		Chapters:   seriesChapters(id, 12),
	}
}

func countAnimesByBangumiID(t *testing.T, bangumiID int) int {
	t.Helper()
	var n int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM animes WHERE bangumi_id = ?", bangumiID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestConfirmInboxCreatesAnimeAndClearsInbox(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-new"
	f01 := "[Group] Confirm Show - 01 [1080p].mkv"
	f02 := "[Group] Confirm Show - 02 [1080p].mkv"
	f03 := "[Group] Confirm Show - 03 [1080p].mkv"
	writeVideos(t, filepath.Join(root, relDir), f01, f02, f03)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8601))

	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: relDir, FileName: f01, Reason: "分差不足", Candidates: []models.MatchCandidate{{ID: 8601, Name: "Confirm Show", Score: 0.8}}},
		{FilePath: relDir, FileName: f02, Reason: "分差不足", Candidates: []models.MatchCandidate{{ID: 8601, Name: "Confirm Show", Score: 0.8}}},
		{FilePath: relDir, FileName: f03, Reason: "分差不足", Candidates: []models.MatchCandidate{}},
		{FilePath: "other", FileName: "keep.mkv", Reason: "其它目录", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}

	data := decodeConfirmSuccess(t, postConfirm(t, handler, confirmInboxRequest{
		BangumiID: 8601, FilePath: relDir, FileNames: []string{f01, f02},
	}))
	if !data.Created || data.Scanned != 2 || len(data.Episodes) != 2 {
		t.Fatalf("应新建并入库 2 集: %#v", data)
	}
	if data.Anime.BangumiID != 8601 || data.Anime.FilePath != relDir || data.Anime.ID <= 0 || data.Anime.EpCount != 12 {
		t.Fatalf("anime 字段不符: %#v", data.Anime)
	}
	stored, err := database.GetAnimeByBangumiID(8601)
	if err != nil || stored == nil || stored.ID != data.Anime.ID {
		t.Fatalf("番剧应落库: %v %#v", err, stored)
	}
	eps, err := database.ListEpisodesByAnimeID(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]int{}
	for _, ep := range eps {
		byPath[ep.FilePath] = ep.EpNumber
	}
	if len(eps) != 2 || byPath[f01] != 1 || byPath[f02] != 2 {
		t.Fatalf("only-files 应只入库 01/02: %#v", eps)
	}
	items, total, err := database.ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("应只精确清除已入库文件，got %#v", items)
	}
	for _, it := range items {
		if it.FileName == f01 || it.FileName == f02 {
			t.Fatalf("已入库文件仍在收件箱: %#v", items)
		}
	}
}

func TestConfirmInboxFailureRollsBackNewAnime(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-dup"
	a := "[Group] Confirm Show - 01 [A].mkv"
	b := "[Group] Confirm Show - 01 [B].mkv"
	writeVideos(t, filepath.Join(root, relDir), a, b)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8602))

	resp := postConfirm(t, handler, confirmInboxRequest{BangumiID: 8602, FilePath: relDir, FileNames: []string{a, b}})
	if resp.Code != utils.CodeInternal {
		t.Fatalf("应失败 CodeInternal，got %d %q", resp.Code, resp.Message)
	}
	data := decodeConfirmError(t, resp)
	if data.Reason != confirmReasonReviewRequired || !strings.Contains(data.Message, "未写入") {
		t.Fatalf("应为 review_required，got %#v", data)
	}
	if n := countAnimesByBangumiID(t, 8602); n != 0 {
		t.Fatalf("新建番剧应被回滚，got %d 条", n)
	}
}

func TestConfirmInboxFailureKeepsExistingAnime(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-keep"
	a := "[Group] Confirm Show - 02 [A].mkv"
	b := "[Group] Confirm Show - 02 [B].mkv"
	seed := "[Group] Confirm Show - 01 [1080p].mkv"
	writeVideos(t, filepath.Join(root, relDir), seed, a, b)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8603))

	anime, err := database.CreateAnime(&models.Anime{
		Title: "Confirm Show", BangumiID: 8603, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{AnimeID: anime.ID, EpNumber: 1, FilePath: seed, Duration: 1400}); err != nil {
		t.Fatal(err)
	}
	before, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}

	resp := postConfirm(t, handler, confirmInboxRequest{BangumiID: 8603, FilePath: relDir, FileNames: []string{a, b}})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonReviewRequired {
		t.Fatalf("应 review_required，got code=%d %#v", resp.Code, data)
	}
	got, err := database.GetAnimeByID(anime.ID)
	if err != nil || got == nil {
		t.Fatalf("created=false 不得删除番剧: %v", err)
	}
	after, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || after[0].ID != before[0].ID || after[0].Duration != 1400 {
		t.Fatalf("created=false 失败不得改动剧集: before=%#v after=%#v", before, after)
	}
}

func TestConfirmInboxExistingSameDirOnlyFilesKeepsOtherEpisodes(t *testing.T) {
	root := t.TempDir()
	relDir := "nest/s1"
	seed := "[Group] Confirm Show - 01 [1080p].mkv"
	f02 := "[Group] Confirm Show - 02 [1080p].mkv"
	f03 := "[Group] Confirm Show - 03 [1080p].mkv"
	writeVideos(t, filepath.Join(root, filepath.FromSlash(relDir)), seed, f02, f03)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8604))

	anime, err := database.CreateAnime(&models.Anime{
		Title: "Confirm Show", BangumiID: 8604, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{AnimeID: anime.ID, EpNumber: 1, FilePath: seed, Duration: 1500}); err != nil {
		t.Fatal(err)
	}
	before, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 反斜杠写法的同一目录不算 dir_mismatch
	data := decodeConfirmSuccess(t, postConfirm(t, handler, confirmInboxRequest{
		BangumiID: 8604, FilePath: `nest\s1`, FileNames: []string{f02},
	}))
	if data.Created || data.Anime.ID != anime.ID {
		t.Fatalf("已有同目录番剧应 created=false: %#v", data)
	}
	eps, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]models.Episode{}
	for _, ep := range eps {
		byPath[ep.FilePath] = ep
	}
	if ep, ok := byPath[seed]; !ok || ep.ID != before[0].ID || ep.Duration != 1500 || ep.EpNumber != 1 {
		t.Fatalf("已有剧集应保留: %#v", eps)
	}
	if ep, ok := byPath[f02]; !ok || ep.EpNumber != 2 {
		t.Fatalf("限定文件应入库: %#v", eps)
	}
	if _, ok := byPath[f03]; ok {
		t.Fatalf("未限定文件不应入库: %#v", eps)
	}
}

func TestConfirmInboxDirMismatch(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "dir-b"), "[Group] Confirm Show - 01 [1080p].mkv")
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8605))
	anime, err := database.CreateAnime(&models.Anime{Title: "Confirm Show", BangumiID: 8605, EpCount: 12, FilePath: "dir-a"})
	if err != nil {
		t.Fatal(err)
	}

	resp := postConfirm(t, handler, confirmInboxRequest{
		BangumiID: 8605, FilePath: "dir-b", FileNames: []string{"[Group] Confirm Show - 01 [1080p].mkv"},
	})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonDirMismatch || data.Message != "番剧已存在但目录不同" {
		t.Fatalf("应 dir_mismatch，got code=%d %#v", resp.Code, data)
	}
	got, err := database.GetAnimeByID(anime.ID)
	if err != nil || got.FilePath != "dir-a" {
		t.Fatalf("已有番剧不得改动: %v %#v", err, got)
	}
	if n := countAnimesByBangumiID(t, 8605); n != 1 {
		t.Fatalf("不应新建，got %d", n)
	}
}

func TestConfirmInboxNoMatchMissingFileRollsBack(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-missing"
	f01 := "[Group] Confirm Show - 01 [1080p].mkv"
	ghost := "[Group] Confirm Show - 02 [1080p].mkv" // 不在磁盘
	writeVideos(t, filepath.Join(root, relDir), f01)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8606))
	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: relDir, FileName: f01, Reason: "分差不足", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}

	resp := postConfirm(t, handler, confirmInboxRequest{BangumiID: 8606, FilePath: relDir, FileNames: []string{f01, ghost}})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonNoMatch {
		t.Fatalf("应 no_match，got code=%d %#v", resp.Code, data)
	}
	if data.Message != "这些文件未能归入该条目："+ghost || len(data.FileNames) != 1 || data.FileNames[0] != ghost {
		t.Fatalf("no_match 文案/缺失项不符: %#v", data)
	}
	if n := countAnimesByBangumiID(t, 8606); n != 0 {
		t.Fatalf("no_match 时新建番剧应回滚，got %d", n)
	}
	var epCount int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM episodes").Scan(&epCount); err != nil {
		t.Fatal(err)
	}
	if epCount != 0 {
		t.Fatalf("回滚后不应残留剧集，got %d", epCount)
	}
	_, total, err := database.ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("no_match 不得清收件箱，got total=%d", total)
	}
}

func TestConfirmInboxNoMatchOtherWorkRollsBack(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-other"
	f01 := "[Group] Confirm Show - 01 [1080p].mkv"
	other := "[Group] Different Work - 05 [1080p].mkv"
	writeVideos(t, filepath.Join(root, relDir), f01, other)
	handler := setupScanHandlerV2(t, root,
		confirmShowSeed(8607),
		bangumiSeed{
			Subject: matching.Subject{
				ID: 8608, Name: "Different Work", NameCn: "别的作品",
				Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
			},
			SearchKeys: []string{"Different Work", "别的作品"},
			Chapters:   seriesChapters(8608, 12),
		},
	)

	resp := postConfirm(t, handler, confirmInboxRequest{BangumiID: 8607, FilePath: relDir, FileNames: []string{f01, other}})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonNoMatch {
		t.Fatalf("其它作品文件应 no_match，got code=%d %#v", resp.Code, data)
	}
	if len(data.FileNames) != 1 || data.FileNames[0] != other {
		t.Fatalf("缺失项应为其它作品文件: %#v", data)
	}
	if n := countAnimesByBangumiID(t, 8607); n != 0 {
		t.Fatalf("新建番剧应回滚，got %d", n)
	}
}

func TestConfirmInboxEpisodeOffset(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-offset"
	f13 := "[Group] Confirm Show - 13 [1080p].mkv"
	f14 := "[Group] Confirm Show - 14 [1080p].mkv"
	writeVideos(t, filepath.Join(root, relDir), f13, f14)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8609))

	// 不带偏移：13/14 超出 12 话，全新目标没有可关联文件 → review_required 且回滚
	resp := postConfirm(t, handler, confirmInboxRequest{BangumiID: 8609, FilePath: relDir, FileNames: []string{f13, f14}})
	data := decodeConfirmError(t, resp)
	if data.Reason != confirmReasonReviewRequired {
		t.Fatalf("无偏移时越界应 review_required，got %#v", data)
	}
	if n := countAnimesByBangumiID(t, 8609); n != 0 {
		t.Fatalf("失败应回滚，got %d", n)
	}

	ok := decodeConfirmSuccess(t, postConfirm(t, handler, confirmInboxRequest{
		BangumiID: 8609, FilePath: relDir, FileNames: []string{f13, f14}, EpisodeOffset: 12,
	}))
	byPath := map[string]int{}
	for _, ep := range ok.Episodes {
		byPath[ep.FilePath] = ep.EpNumber
	}
	if !ok.Created || byPath[f13] != 1 || byPath[f14] != 2 {
		t.Fatalf("偏移 12 后应为 ep1/2: %#v", ok)
	}
}

func TestConfirmInboxEpisodeOffsetNotApplicable(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-offset-bad"
	f05 := "[Group] Confirm Show - 05 [1080p].mkv"
	writeVideos(t, filepath.Join(root, relDir), f05)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8610))

	resp := postConfirm(t, handler, confirmInboxRequest{BangumiID: 8610, FilePath: relDir, FileNames: []string{f05}, EpisodeOffset: 2})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonReviewRequired ||
		!strings.Contains(data.Message, "集号偏移仅适用于累计编号（集号需大于该条目话数）") {
		t.Fatalf("n<=EpCount 应 review_required 且文案明确，got code=%d %#v", resp.Code, data)
	}
	if n := countAnimesByBangumiID(t, 8610); n != 0 {
		t.Fatalf("失败应回滚，got %d", n)
	}
}

func TestConfirmInboxInvalidParams(t *testing.T) {
	root := t.TempDir()
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8611))
	good := "[Group] Confirm Show - 01 [1080p].mkv"
	cases := []struct {
		name string
		req  confirmInboxRequest
	}{
		{"bangumi_id=0", confirmInboxRequest{BangumiID: 0, FilePath: "d", FileNames: []string{good}}},
		{"bangumi_id<0", confirmInboxRequest{BangumiID: -1, FilePath: "d", FileNames: []string{good}}},
		{"绝对路径", confirmInboxRequest{BangumiID: 8611, FilePath: "/abs/d", FileNames: []string{good}}},
		{"目录穿越", confirmInboxRequest{BangumiID: 8611, FilePath: "../d", FileNames: []string{good}}},
		{"目录含 NUL", confirmInboxRequest{BangumiID: 8611, FilePath: "d\x00x", FileNames: []string{good}}},
		{"file_names 为空", confirmInboxRequest{BangumiID: 8611, FilePath: "d", FileNames: []string{}}},
		{"file_names 缺失", confirmInboxRequest{BangumiID: 8611, FilePath: "d"}},
		{"文件名空串", confirmInboxRequest{BangumiID: 8611, FilePath: "d", FileNames: []string{good, "  "}}},
		{"文件名含 /", confirmInboxRequest{BangumiID: 8611, FilePath: "d", FileNames: []string{"x/" + good}}},
		{"文件名含 \\", confirmInboxRequest{BangumiID: 8611, FilePath: "d", FileNames: []string{`..\` + good}}},
		{"文件名含 NUL", confirmInboxRequest{BangumiID: 8611, FilePath: "d", FileNames: []string{"a\x00.mkv"}}},
		{"episode_offset<0", confirmInboxRequest{BangumiID: 8611, FilePath: "d", FileNames: []string{good}, EpisodeOffset: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postConfirm(t, handler, tc.req)
			data := decodeConfirmError(t, resp)
			if resp.Code != utils.CodeInvalidParams || data.Reason != confirmReasonInvalid || data.Message == "" {
				t.Fatalf("应 invalid，got code=%d %#v", resp.Code, data)
			}
		})
	}
	t.Run("非法 JSON", func(t *testing.T) {
		status, resp := postConfirmRaw(t, handler, []byte(`{"bangumi_id":`))
		if status != http.StatusOK || resp.Code != utils.CodeInvalidParams {
			t.Fatalf("非法 JSON 应 invalid，got status=%d code=%d", status, resp.Code)
		}
		if data := decodeConfirmError(t, resp); data.Reason != confirmReasonInvalid {
			t.Fatalf("reason=%q", data.Reason)
		}
	})
	if n := countAnimesByBangumiID(t, 8611); n != 0 {
		t.Fatalf("参数非法不得落库，got %d", n)
	}
}

func TestConfirmInboxLegacyEngineUnsupported(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-legacy"
	f01 := "[Group] Confirm Show - 01 [1080p].mkv"
	writeVideos(t, filepath.Join(root, relDir), f01)
	v2 := setupScanHandlerV2(t, root, confirmShowSeed(8612))
	legacy := NewAnimeHandler(v2.bangumi, services.NewScannerService(root))
	if legacy.scanner.MatchEngine() != matching.EngineLegacy {
		t.Fatalf("前置：应为 legacy 引擎")
	}

	resp := postConfirm(t, legacy, confirmInboxRequest{BangumiID: 8612, FilePath: relDir, FileNames: []string{f01}})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonUnsupportedEngine ||
		data.Message != "当前匹配引擎不支持收件箱确认，请使用扫描" {
		t.Fatalf("应 unsupported_engine，got code=%d %#v", resp.Code, data)
	}
	if n := countAnimesByBangumiID(t, 8612); n != 0 {
		t.Fatalf("legacy 不得落库，got %d", n)
	}
}

func TestConfirmInboxConcurrentSameBangumiCreatesOnce(t *testing.T) {
	root := t.TempDir()
	relDir := "confirm-concurrent"
	f01 := "[Group] Confirm Show - 01 [1080p].mkv"
	f02 := "[Group] Confirm Show - 02 [1080p].mkv"
	writeVideos(t, filepath.Join(root, relDir), f01, f02)
	handler := setupScanHandlerV2(t, root, confirmShowSeed(8613))

	const workers = 6
	body, err := json.Marshal(confirmInboxRequest{BangumiID: 8613, FilePath: relDir, FileNames: []string{f01, f02}})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/api/library/inbox/confirm", handler.Confirm)
	results := make([]confirmResponse, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/library/inbox/confirm", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			_ = json.Unmarshal(recorder.Body.Bytes(), &results[i])
		}(i)
	}
	close(start)
	wg.Wait()

	createdCount := 0
	for i, resp := range results {
		data := decodeConfirmSuccess(t, resp)
		if data.Created {
			createdCount++
		}
		if data.Scanned != 2 {
			t.Fatalf("请求 %d scanned=%d", i, data.Scanned)
		}
	}
	if createdCount != 1 {
		t.Fatalf("并发同 bangumi_id 只应有一个 created=true，got %d", createdCount)
	}
	if n := countAnimesByBangumiID(t, 8613); n != 1 {
		t.Fatalf("应只建一条番剧，got %d", n)
	}
	anime, err := database.GetAnimeByBangumiID(8613)
	if err != nil || anime == nil {
		t.Fatal(err)
	}
	eps, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil || len(eps) != 2 {
		t.Fatalf("剧集应为 2 条: %v %#v", err, eps)
	}
}

func TestConfirmScanErrorReasonMapping(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		reason  string
		message string
	}{
		{"review_required", fmt.Errorf("%w: 全新目标没有可关联的文件", services.ErrMatchReviewRequired), confirmReasonReviewRequired, ""},
		{"offset 不适用", fmt.Errorf("%w: 集号偏移仅适用于累计编号（集号需大于该条目话数）", services.ErrMatchReviewRequired), confirmReasonReviewRequired, ""},
		{"upstream", fmt.Errorf("wrap: %w", services.ErrMatchUpstream), confirmReasonUpstream, "匹配上游不可用，未写入任何变更"},
		{"conflict", fmt.Errorf("wrap: %w", database.ErrEpisodeSnapshotChanged), confirmReasonConflict, "番剧或剧集快照已变更，请重试（未写入任何变更）"},
		{"identity 冲突→其它", database.ErrEpisodeIdentityConflict, confirmReasonUpstream, "剧集文件身份冲突，未写入任何变更"},
		{"其它错误", errors.New("boom"), confirmReasonUpstream, "扫描番剧目录失败"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := confirmScanErrorReason(tc.err); got != tc.reason {
				t.Fatalf("reason=%q want %q", got, tc.reason)
			}
			msg := scanV2ErrorMessage(tc.err)
			if tc.message != "" && msg != tc.message {
				t.Fatalf("message=%q want %q", msg, tc.message)
			}
			if tc.reason == confirmReasonReviewRequired && !strings.HasSuffix(msg, "（未写入任何变更）") {
				t.Fatalf("review 文案=%q", msg)
			}
		})
	}
}
