package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services"
	"fan-web/services/matching"
	"fan-web/utils"
)

func setupScanHandlerV2(t *testing.T, rootPath string) *AnimeHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "scan-v2.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	if _, err := database.DB.Exec(
		"INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)", "scan-v2-user", "x", 0,
	); err != nil {
		t.Fatal(err)
	}
	rt, err := services.NewMatchingRuntime(services.NewBangumiService(), matching.EngineV2, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	sc := services.NewScannerService(rootPath, services.WithScannerMatching(rt))
	return NewAnimeHandler(services.NewBangumiService(), sc)
}

func writeVideos(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func seriesChapters(subjectID, n int) []matching.Chapter {
	chs := make([]matching.Chapter, 0, n)
	for i := 1; i <= n; i++ {
		chs = append(chs, matching.Chapter{ID: subjectID*100 + i, Ep: float64(i), Sort: float64(i)})
	}
	return chs
}

func TestAnimeScanV2RanmaKeepsProgressAndOutbox(t *testing.T) {
	root := t.TempDir()
	relDir := "ranma"
	f01 := "Ranma1-2 (2024) [01].mkv"
	f02 := "Ranma1-2 (2024) [02].mkv"
	writeVideos(t, filepath.Join(root, relDir), f01, f02)

	handler := setupScanHandlerV2(t, root)
	restore := services.InstallFakeScanSession(services.FakeMatchSeed{
		Subject: matching.Subject{
			ID: 425018, Name: "らんま1/2 (2024)", NameCn: "乱马1/2 (2024)",
			Year: 2024, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
			Aliases: []string{"Ranma 1/2", "Ranma1-2"},
		},
		SearchKeys: []string{"らんま1/2", "乱马1/2", "Ranma1-2", "Ranma 1/2"},
		Chapters:   seriesChapters(425018, 12),
	})
	t.Cleanup(restore)

	anime, err := database.CreateAnime(&models.Anime{
		Title: "らんま1/2 (2024)", TitleCn: "乱马1/2 (2024)",
		BangumiID: 425018, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{
		AnimeID: anime.ID, EpNumber: 2, Title: "旧编号", FilePath: f01, Duration: 1300,
	}); err != nil {
		t.Fatal(err)
	}
	eps, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("seed eps: %v %#v", err, eps)
	}
	epID := eps[0].ID

	user, err := database.GetUserByUsername("scan-v2-user")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProgress(user.ID, epID, 73, false); err != nil {
		t.Fatal(err)
	}
	if err := database.EnqueueBangumiOutbox(user.ID, epID); err != nil {
		t.Fatal(err)
	}

	code, body := postAnimeScan(t, handler, anime.ID)
	if code != utils.CodeSuccess {
		t.Fatalf("scan want success, got code=%d body=%s", code, body)
	}

	after, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	var a01, a02 *models.Episode
	for i := range after {
		switch after[i].FilePath {
		case f01:
			a01 = &after[i]
		case f02:
			a02 = &after[i]
		}
	}
	if a01 == nil || a01.ID != epID || a01.EpNumber != 1 {
		t.Fatalf("01 应保留 ID=%d 且 ep=1, got %#v", epID, a01)
	}
	if a02 == nil || a02.ID == 0 || a02.EpNumber != 2 {
		t.Fatalf("02 应为新行 ep=2, got %#v", a02)
	}
	progress, err := database.GetProgress(user.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 73 {
		t.Fatalf("进度应保留 73, got %d", progress.Position)
	}
	outbox, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox) != 1 || outbox[0].EpisodeID != epID {
		t.Fatalf("outbox 应仍引用 %d, got %#v", epID, outbox)
	}
}

func TestAnimeScanV2CreateThenScanConfirmsTarget(t *testing.T) {
	root := t.TempDir()
	relDir := "inbox-show"
	fname := "Target Show - 01.mkv"
	writeVideos(t, filepath.Join(root, relDir), fname)

	handler := setupScanHandlerV2(t, root)
	sub := matching.Subject{
		ID: 8101, Name: "Target Show", NameCn: "目标番",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	restore := services.InstallFakeScanSession(services.FakeMatchSeed{
		Subject:    sub,
		SearchKeys: []string{"Target Show", "目标番"},
		Chapters:   seriesChapters(8101, 12),
	})
	t.Cleanup(restore)

	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: relDir, FileName: fname, Reason: "分差不足", Candidates: []models.MatchCandidate{
			{ID: 8101, Name: "Target Show", NameCn: "目标番", Score: 0.72},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	// 模拟用户点击候选后 Create（直接入库，聚焦 Scan 确认链）
	anime, err := database.CreateAnime(&models.Anime{
		Title: sub.Name, TitleCn: sub.NameCn, BangumiID: sub.ID, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	code, body := postAnimeScan(t, handler, anime.ID)
	if code != utils.CodeSuccess {
		t.Fatalf("Create→Scan 应成功, got code=%d body=%s", code, body)
	}
	eps, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].FilePath != fname || eps[0].EpNumber != 1 {
		t.Fatalf("应确认目标文件入集, got %#v", eps)
	}
	_, total, err := database.ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("确认后应收件箱清空该文件, got total=%d", total)
	}
}

func TestAnimeScanV2MixedDirClearsOnlyTargetFiles(t *testing.T) {
	root := t.TempDir()
	relDir := "mixed"
	tvFile := "Show TV - 01.mkv"
	movieFile := "某作品 剧场版.mkv"
	writeVideos(t, filepath.Join(root, relDir), tvFile, movieFile)

	handler := setupScanHandlerV2(t, root)
	tv := matching.Subject{
		ID: 9201, Name: "Show TV", NameCn: "Show TV",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	mov := matching.Subject{
		ID: 9202, Name: "某作品", NameCn: "剧场版",
		Media: matching.KindMovie, EpisodeCount: 1, Detailed: true,
	}
	restore := services.InstallFakeScanSession(
		services.FakeMatchSeed{Subject: tv, SearchKeys: []string{"Show TV", "Show"}, Chapters: seriesChapters(9201, 12)},
		services.FakeMatchSeed{Subject: mov, SearchKeys: []string{"某作品", "剧场版"}, Chapters: []matching.Chapter{
			{ID: 920201, Ep: 1, Sort: 1},
		}},
	)
	t.Cleanup(restore)

	tvAnime, err := database.CreateAnime(&models.Anime{
		Title: tv.Name, TitleCn: tv.NameCn, BangumiID: tv.ID, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	movAnime, err := database.CreateAnime(&models.Anime{
		Title: mov.Name, TitleCn: mov.NameCn, BangumiID: mov.ID, EpCount: 1, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{
		AnimeID: movAnime.ID, EpNumber: 1, FilePath: movieFile,
	}); err != nil {
		t.Fatal(err)
	}

	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: relDir, FileName: tvFile, Reason: "待确认", Candidates: []models.MatchCandidate{}},
		{FilePath: relDir, FileName: movieFile, Reason: "电影收件箱", Candidates: []models.MatchCandidate{}},
		{FilePath: "other", FileName: "keep.mkv", Reason: "其它目录", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}

	code, body := postAnimeScan(t, handler, tvAnime.ID)
	if code != utils.CodeSuccess {
		t.Fatalf("TV 扫描应成功, got code=%d body=%s", code, body)
	}

	items, total, err := database.ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("应保留电影与其它目录收件箱, got total=%d items=%#v", total, items)
	}
	kept := map[string]bool{}
	for _, it := range items {
		kept[it.FilePath+"/"+it.FileName] = true
	}
	if !kept[relDir+"/"+movieFile] || !kept["other/keep.mkv"] {
		t.Fatalf("电影/其它目录收件箱被误删: %#v", items)
	}
	if kept[relDir+"/"+tvFile] {
		t.Fatalf("TV 目标文件应收件箱清除: %#v", items)
	}

	movEps, err := database.ListEpisodesByAnimeID(movAnime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(movEps) != 1 || movEps[0].FilePath != movieFile {
		t.Fatalf("电影剧集行必须保留, got %#v", movEps)
	}
}

func TestAnimeScanV2ReviewRequiredZeroWrite(t *testing.T) {
	root := t.TempDir()
	relDir := "dup"
	a := "Dup Show - 01 [A].mkv"
	b := "Dup Show - 01 [B].mkv"
	writeVideos(t, filepath.Join(root, relDir), a, b)

	handler := setupScanHandlerV2(t, root)
	restore := services.InstallFakeScanSession(services.FakeMatchSeed{
		Subject: matching.Subject{
			ID: 9301, Name: "Dup Show", NameCn: "Dup Show",
			Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		},
		SearchKeys: []string{"Dup Show"},
		Chapters:   seriesChapters(9301, 12),
	})
	t.Cleanup(restore)

	anime, err := database.CreateAnime(&models.Anime{
		Title: "Dup Show", BangumiID: 9301, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{
		AnimeID: anime.ID, EpNumber: 5, Title: "seed", FilePath: "seed-keep.mkv",
	}); err != nil {
		t.Fatal(err)
	}
	before, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}

	code, body := postAnimeScan(t, handler, anime.ID)
	if code == utils.CodeSuccess {
		t.Fatalf("重复集号应失败零写入, body=%s", body)
	}
	var resp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Message, "未写入") {
		t.Fatalf("错误文案应标明未写入, got %q", resp.Message)
	}

	after, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || after[0].ID != before[0].ID || after[0].EpNumber != before[0].EpNumber {
		t.Fatalf("出错必须零写入, before=%#v after=%#v", before, after)
	}
}

func TestAnimeScanV2SnapshotChangedZeroWrite(t *testing.T) {
	root := t.TempDir()
	relDir := "cas"
	f01 := "CAS Show - 01.mkv"
	f02 := "CAS Show - 02.mkv"
	writeVideos(t, filepath.Join(root, relDir), f01, f02)

	handler := setupScanHandlerV2(t, root)

	anime, err := database.CreateAnime(&models.Anime{
		Title: "CAS Show", BangumiID: 9401, EpCount: 12, FilePath: relDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{
		AnimeID: anime.ID, EpNumber: 1, FilePath: f01,
	}); err != nil {
		t.Fatal(err)
	}
	before, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Scan 已读 ExpectedAnime 后、Reconcile 前：打开 Session（处理 02）时改 ep_count → CAS 失败
	restore := services.InstallFakeScanSessionWithOpenHook(func() {
		if _, err := database.DB.Exec(`UPDATE animes SET ep_count = ep_count + 1 WHERE id = ?`, anime.ID); err != nil {
			t.Errorf("mutate ep_count: %v", err)
		}
	}, services.FakeMatchSeed{
		Subject: matching.Subject{
			ID: 9401, Name: "CAS Show", NameCn: "CAS Show",
			Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		},
		SearchKeys: []string{"CAS Show"},
		Chapters:   seriesChapters(9401, 12),
	})
	t.Cleanup(restore)

	code, body := postAnimeScan(t, handler, anime.ID)
	if code == utils.CodeSuccess {
		t.Fatalf("快照变更应失败, body=%s", body)
	}
	var resp struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	if !strings.Contains(resp.Message, "未写入") && !strings.Contains(resp.Message, "快照") {
		t.Fatalf("应提示快照/未写入, got %q", resp.Message)
	}

	after, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].FilePath != f01 || after[0].EpNumber != 1 {
		t.Fatalf("CAS 失败零写入, before=%#v after=%#v", before, after)
	}
}

func TestAnimeScanV2MatchEngineIsV2(t *testing.T) {
	handler := setupScanHandlerV2(t, t.TempDir())
	if handler.scanner.MatchEngine() != matching.EngineV2 {
		t.Fatalf("handler scanner engine=%s", handler.scanner.MatchEngine())
	}
}
