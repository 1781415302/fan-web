package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/models"
	"fan-web/services"
	"fan-web/services/matching"
	"fan-web/utils"
)

// ---- 夹具 ----

const (
	assistXianWangID = 400005
	assistCodeXID    = 12345
)

func assistXianWangSeed() bangumiSeed {
	return bangumiSeed{
		Subject: matching.Subject{
			ID: assistXianWangID, Name: "仙王的日常生活 第五季", NameCn: "仙王的日常生活 第五季",
			Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		},
		SearchKeys: []string{"仙王的日常生活"},
		Chapters:   seriesChapters(assistXianWangID, 12),
	}
}

// assistCodeXSeed 日文主名：英文文件名与之毫无字面相似度，只能靠 bangumi_id 直绑。
func assistCodeXSeed() bangumiSeed {
	return bangumiSeed{
		Subject: matching.Subject{
			ID: assistCodeXID, Name: "コードエックス", NameCn: "代号X",
			Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		},
		Chapters: seriesChapters(assistCodeXID, 12),
	}
}

func assistFateSeeds() []bangumiSeed {
	seeds := make([]bangumiSeed, 0, 5)
	for i := 0; i < 5; i++ {
		id := 5100 + i
		seeds = append(seeds, bangumiSeed{
			Subject: matching.Subject{
				ID: id, Name: "Fate", NameCn: "Fate",
				Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
			},
			SearchKeys: []string{"Fate"},
			Chapters:   seriesChapters(id, 12),
		})
	}
	return seeds
}

func boolPtr(v bool) *bool { return &v }

func postAssistRaw(t *testing.T, handler *AnimeHandler, body []byte) (int, confirmResponse) {
	t.Helper()
	router := gin.New()
	router.POST("/api/library/assist-match", handler.AssistMatch)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/library/assist-match", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	var resp confirmResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return recorder.Code, resp
}

func postAssist(t *testing.T, handler *AnimeHandler, req models.AssistMatchRequest) confirmResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	status, resp := postAssistRaw(t, handler, body)
	if status != http.StatusOK {
		t.Fatalf("HTTP 状态应恒为 200，got %d", status)
	}
	return resp
}

func decodeAssistSuccess(t *testing.T, resp confirmResponse) models.AssistMatchResponse {
	t.Helper()
	if resp.Code != utils.CodeSuccess {
		t.Fatalf("业务结论应 code=0，got code=%d message=%q data=%s", resp.Code, resp.Message, resp.Data)
	}
	var data models.AssistMatchResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("decode assist data %s: %v", resp.Data, err)
	}
	return data
}

type assistTableCounts struct{ animes, episodes, unidentified int }

func countAssistTables(t *testing.T) assistTableCounts {
	t.Helper()
	var out assistTableCounts
	for _, q := range []struct {
		sql string
		dst *int
	}{
		{"SELECT COUNT(*) FROM animes", &out.animes},
		{"SELECT COUNT(*) FROM episodes", &out.episodes},
		{"SELECT COUNT(*) FROM unidentified_files", &out.unidentified},
	} {
		if err := database.DB.QueryRow(q.sql).Scan(q.dst); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// ---- §4.5 示例表 6 行 ----

// 示例表第 1 行：Scene 文件无 hint 时内核解析出标题/季/集（零副作用）。
func TestAssistExampleSceneParseNoHint(t *testing.T) {
	name := "The.Daily.Life.of.the.Immortal.King.S05E12.Siblings.Alliance.1080p.CR.WEB-DL.DUAL.AAC2.0.H.264-VARYG.mkv"
	ev := matching.ParseFile(matching.FileInput{Name: name, Dir: "XianWangS5"})
	if len(ev.Titles) == 0 || ev.Titles[0].Text != "The Daily Life of the Immortal King" {
		t.Fatalf("Titles=%#v", ev.Titles)
	}
	if ev.Ordinal.Season != 5 || ev.Episode.Kind != matching.NumberSingle || ev.Episode.First != 12 {
		t.Fatalf("Season=%d Episode=%#v", ev.Ordinal.Season, ev.Episode)
	}
}

// 示例表第 2 行：title_hint + season_hint 命中 → committed、建番绑定目录、写剧集、清收件箱。
func TestAssistExampleHintCommitsCreatesAnime(t *testing.T) {
	root := t.TempDir()
	relDir := "XianWangS5"
	name := "The.Daily.Life.of.the.Immortal.King.S05E12.Siblings.Alliance.1080p.CR.WEB-DL.DUAL.AAC2.0.H.264-VARYG.mkv"
	writeVideos(t, filepath.Join(root, relDir), name)
	handler := setupScanHandlerV2(t, root, assistXianWangSeed())
	if err := database.UpsertUnidentifiedFiles([]models.UnidentifiedFile{{FilePath: relDir, FileName: name, Reason: "旧"}}); err != nil {
		t.Fatal(err)
	}

	data := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{
		FilePath: relDir + "/" + name, TitleHint: "仙王的日常生活", SeasonHint: 5,
	}))
	if data.Status != "committed" || !data.Created || data.Scanned != 1 || data.FilePath != relDir {
		t.Fatalf("应 committed/created/scanned=1/file_path=%s: %#v", relDir, data)
	}
	if len(data.Episodes) != 1 || data.Episodes[0].EpNumber != 12 || data.Episodes[0].FilePath != name {
		t.Fatalf("episodes=%#v", data.Episodes)
	}
	if data.Anime == nil || data.Anime.BangumiID != assistXianWangID || data.Anime.FilePath != relDir {
		t.Fatalf("anime=%#v", data.Anime)
	}
	if n := countAssistTables(t).unidentified; n != 0 {
		t.Fatalf("入库后应清除收件箱对应行，剩 %d", n)
	}
}

// 示例表第 3 行：bangumi_id 直绑（WithTrustedTarget）无视英/日命名差异入库。
func TestAssistExampleBangumiIDTrustedCommit(t *testing.T) {
	root := t.TempDir()
	relDir := "ObscureDir"
	name := "CodeX.S01E03.mkv"
	writeVideos(t, filepath.Join(root, relDir), name, "Unrelated.Other.S02E07.mkv")
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())

	data := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{
		FilePath: relDir, FileNames: []string{name}, BangumiID: assistCodeXID,
	}))
	if data.Status != "committed" || data.Anime == nil || data.Anime.BangumiID != assistCodeXID || data.Scanned != 1 {
		t.Fatalf("应 committed bangumi_id=%d scanned=1: %#v", assistCodeXID, data)
	}
	if len(data.Episodes) != 1 || data.Episodes[0].EpNumber != 3 || data.Episodes[0].FilePath != name {
		t.Fatalf("episodes=%#v", data.Episodes)
	}
}

// 示例表第 4 行：多候选歧义 + auto_commit=true → review_required，最多 3 个候选，写入收件箱。
func TestAssistExampleAmbiguousAutoCommitWritesInbox(t *testing.T) {
	root := t.TempDir()
	relDir := "Dir"
	name := "Fate.S01E01.mkv"
	writeVideos(t, filepath.Join(root, relDir), name)
	handler := setupScanHandlerV2(t, root, assistFateSeeds()...)

	data := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{
		FilePath: relDir, TitleHint: "Fate", AutoCommit: boolPtr(true),
	}))
	if data.Status != "review_required" || data.Reason == "" {
		t.Fatalf("应 review_required 且有 reason: %#v", data)
	}
	if len(data.Candidates) == 0 || len(data.Candidates) > 3 {
		t.Fatalf("候选应 1..3 个: %#v", data.Candidates)
	}
	rows, total, err := database.ListUnidentified(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].FileName != name || rows[0].FilePath != relDir || len(rows[0].Candidates) != len(data.Candidates) {
		t.Fatalf("应写入 1 条收件箱（含候选）: total=%d %#v", total, rows)
	}
	if c := countAssistTables(t); c.animes != 0 || c.episodes != 0 {
		t.Fatalf("复核态不得建番/写剧集: %#v", c)
	}
}

// 示例表第 5 行：auto_commit=false 无论命中还是歧义均严格零写入三表。
func TestAssistExamplePreviewZeroWrite(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "XianWangS5"), "The.Daily.Life.of.the.Immortal.King.S05E12.mkv")
	writeVideos(t, filepath.Join(root, "Dir"), "Fate.S01E01.mkv")
	writeVideos(t, filepath.Join(root, "ObscureDir"), "CodeX.S01E03.mkv")
	seeds := append([]bangumiSeed{assistXianWangSeed(), assistCodeXSeed()}, assistFateSeeds()...)
	handler := setupScanHandlerV2(t, root, seeds...)
	before := countAssistTables(t)

	// 命中（title_hint）→ matched + candidate + bindings
	hit := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{
		FilePath: "XianWangS5", TitleHint: "仙王的日常生活", SeasonHint: 5, AutoCommit: boolPtr(false),
	}))
	if hit.Status != "matched" || hit.Candidate == nil || hit.Candidate.ID != assistXianWangID ||
		len(hit.Bindings) != 1 || hit.Bindings[0].EpNumber != 12 {
		t.Fatalf("hint 命中预览应 matched: %#v", hit)
	}
	// 命中（bangumi_id 直绑预览）→ matched
	direct := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{
		FilePath: "ObscureDir", BangumiID: assistCodeXID, AutoCommit: boolPtr(false),
	}))
	if direct.Status != "matched" || direct.Candidate == nil || direct.Candidate.ID != assistCodeXID ||
		len(direct.Bindings) != 1 || direct.Bindings[0].EpNumber != 3 {
		t.Fatalf("bangumi_id 预览应 matched: %#v", direct)
	}
	// 歧义 → review_required
	amb := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{
		FilePath: "Dir", TitleHint: "Fate", AutoCommit: boolPtr(false),
	}))
	if amb.Status != "review_required" || len(amb.Candidates) == 0 || len(amb.Candidates) > 3 {
		t.Fatalf("歧义预览应 review_required: %#v", amb)
	}
	if after := countAssistTables(t); after != before {
		t.Fatalf("auto_commit=false 必须严格零写入: %#v → %#v", before, after)
	}
}

// 示例表第 6 行：重复调用已归属同一目标番剧的文件 → committed, created=false，无重复插入。
func TestAssistExampleIdempotentRecall(t *testing.T) {
	root := t.TempDir()
	relDir := "ObscureDir"
	name := "CodeX.S01E03.mkv"
	writeVideos(t, filepath.Join(root, relDir), name)
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())

	req := models.AssistMatchRequest{FilePath: relDir, FileNames: []string{name}, BangumiID: assistCodeXID}
	first := decodeAssistSuccess(t, postAssist(t, handler, req))
	if first.Status != "committed" || !first.Created {
		t.Fatalf("首次应 committed/created: %#v", first)
	}
	before := countAssistTables(t)
	for _, r := range []models.AssistMatchRequest{
		req, // 显式 file_names
		{FilePath: relDir, BangumiID: assistCodeXID},               // 目录模式且全已关联
		{FilePath: relDir + "/" + name},                            // 单文件模式（无 bangumi_id）
		{FilePath: relDir + "\\" + name, BangumiID: assistCodeXID}, // Windows 分隔符
	} {
		again := decodeAssistSuccess(t, postAssist(t, handler, r))
		if again.Status != "committed" || again.Created || len(again.Episodes) != 1 || again.Episodes[0].EpNumber != 3 ||
			again.Anime == nil || again.Anime.ID != first.Anime.ID {
			t.Fatalf("重复调用应幂等 committed/created=false: req=%#v got=%#v", r, again)
		}
	}
	if after := countAssistTables(t); after != before {
		t.Fatalf("幂等重调不得写库: %#v → %#v", before, after)
	}
}

// ---- 已关联/冲突/目录/CAS/上游/参数 ----

func TestAssistEmptyDirReturns1001(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "Empty"))
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	resp := postAssist(t, handler, models.AssistMatchRequest{FilePath: "Empty", BangumiID: assistCodeXID})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInvalidParams || data.Reason != confirmReasonInvalid || resp.Message != "目录下没有可识别的视频文件" {
		t.Fatalf("空目录应 1001: code=%d %#v", resp.Code, data)
	}
}

func TestAssistInvalidParams(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "ObscureDir"), "CodeX.S01E03.mkv")
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	before := countAssistTables(t)
	cases := []struct {
		name string
		body string
	}{
		{"bad json", `{`},
		{"traversal", `{"file_path":"../outside","bangumi_id":12345}`},
		{"backslash traversal", `{"file_path":"..\\outside","bangumi_id":12345}`},
		{"absolute", `{"file_path":"/etc","bangumi_id":12345}`},
		{"negative offset", `{"file_path":"ObscureDir","bangumi_id":12345,"episode_offset":-1}`},
		{"negative bangumi", `{"file_path":"ObscureDir","bangumi_id":-3}`},
		{"negative season", `{"file_path":"ObscureDir","season_hint":-1}`},
		{"bad file name", `{"file_path":"ObscureDir","file_names":["a/b.mkv"],"bangumi_id":12345}`},
		{"missing file name", `{"file_path":"ObscureDir","file_names":["Nope.S01E01.mkv"],"bangumi_id":12345}`},
		{"missing dir", `{"file_path":"NoSuchDir","bangumi_id":12345}`},
	}
	for _, tc := range cases {
		status, resp := postAssistRaw(t, handler, []byte(tc.body))
		if status != http.StatusOK || resp.Code != utils.CodeInvalidParams {
			t.Fatalf("%s: 应 HTTP 200 + 1001，got %d %d %q", tc.name, status, resp.Code, resp.Message)
		}
		if data := decodeConfirmError(t, resp); data.Reason != confirmReasonInvalid {
			t.Fatalf("%s: reason=%q", tc.name, data.Reason)
		}
	}
	if after := countAssistTables(t); after != before {
		t.Fatalf("参数非法不得写库: %#v → %#v", before, after)
	}
}

func TestAssistLegacyEngineUnsupported(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "ObscureDir"), "CodeX.S01E03.mkv")
	handler := setupScanHandler(t, root) // 无 MatchingRuntime → legacy
	resp := postAssist(t, handler, models.AssistMatchRequest{FilePath: "ObscureDir", BangumiID: assistCodeXID})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonUnsupportedEngine {
		t.Fatalf("legacy 应 9999 unsupported_engine: code=%d %#v", resp.Code, data)
	}
}

// 文件已被他番认领：显式 file_names + 不同 bangumi_id → 9999 conflict，零写入。
func TestAssistOtherAnimeOwnershipConflict(t *testing.T) {
	root := t.TempDir()
	relDir := "ObscureDir"
	name := "CodeX.S01E03.mkv"
	writeVideos(t, filepath.Join(root, relDir), name)
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	other, err := database.CreateAnime(&models.Anime{Title: "Other", BangumiID: 999, EpCount: 12, FilePath: relDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateEpisode(&models.Episode{AnimeID: other.ID, EpNumber: 3, FilePath: name}); err != nil {
		t.Fatal(err)
	}
	before := countAssistTables(t)

	for _, req := range []models.AssistMatchRequest{
		{FilePath: relDir, FileNames: []string{name}, BangumiID: assistCodeXID},
		{FilePath: relDir + "/" + name, BangumiID: assistCodeXID},
		{FilePath: relDir, BangumiID: assistCodeXID}, // 目录模式全已关联但归他番
	} {
		resp := postAssist(t, handler, req)
		data := decodeConfirmError(t, resp)
		if resp.Code != utils.CodeInternal || data.Reason != confirmReasonConflict || len(data.FileNames) != 1 || data.FileNames[0] != name {
			t.Fatalf("他番认领应 9999 conflict: req=%#v code=%d %#v", req, resp.Code, data)
		}
	}
	if after := countAssistTables(t); after != before {
		t.Fatalf("冲突不得写库: %#v → %#v", before, after)
	}
}

// 文件被多部番剧认领 → conflict。
func TestAssistMultiOwnerConflict(t *testing.T) {
	root := t.TempDir()
	relDir := "Shared"
	name := "CodeX.S01E03.mkv"
	writeVideos(t, filepath.Join(root, relDir), name)
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	for _, bid := range []int{901, 902} {
		a, err := database.CreateAnime(&models.Anime{Title: "Owner", BangumiID: bid, EpCount: 12, FilePath: relDir})
		if err != nil {
			t.Fatal(err)
		}
		if err := database.CreateEpisode(&models.Episode{AnimeID: a.ID, EpNumber: 3, FilePath: name}); err != nil {
			t.Fatal(err)
		}
	}
	resp := postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, FileNames: []string{name}})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonConflict {
		t.Fatalf("多番认领应 conflict: code=%d %#v", resp.Code, data)
	}
}

// 部分已关联：只处理 Unassociated。
func TestAssistPartialAssociatedOnlyCommitsUnassociated(t *testing.T) {
	root := t.TempDir()
	relDir := "ObscureDir"
	f03 := "CodeX.S01E03.mkv"
	f04 := "CodeX.S01E04.mkv"
	writeVideos(t, filepath.Join(root, relDir), f03, f04)
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	first := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, FileNames: []string{f03}, BangumiID: assistCodeXID}))
	if first.Status != "committed" || !first.Created {
		t.Fatalf("first=%#v", first)
	}
	second := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, FileNames: []string{f03, f04}, BangumiID: assistCodeXID}))
	if second.Status != "committed" || second.Created || len(second.FileNames) != 1 || second.FileNames[0] != f04 || len(second.Episodes) != 2 {
		t.Fatalf("应只处理未关联的 %s 并返回全部剧集: %#v", f04, second)
	}
}

// dir_mismatch：同 bangumi_id 已绑定其它目录 → 9999 dir_mismatch，且绝不删除原番剧。
func TestAssistDirMismatchKeepsExistingAnime(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "ObscureDir"), "CodeX.S01E03.mkv")
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	existing, err := database.CreateAnime(&models.Anime{Title: "コードエックス", BangumiID: assistCodeXID, EpCount: 12, FilePath: "OtherDir"})
	if err != nil {
		t.Fatal(err)
	}
	resp := postAssist(t, handler, models.AssistMatchRequest{FilePath: "ObscureDir", BangumiID: assistCodeXID})
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonDirMismatch {
		t.Fatalf("应 9999 dir_mismatch: code=%d %#v", resp.Code, data)
	}
	got, err := database.GetAnimeByID(existing.ID)
	if err != nil || got == nil || got.FilePath != "OtherDir" {
		t.Fatalf("原番剧不得被删除或改动: %v %#v", err, got)
	}
	if c := countAssistTables(t); c.animes != 1 || c.episodes != 0 {
		t.Fatalf("dir_mismatch 不得写库: %#v", c)
	}
}

// bangumi_id>0 + auto_commit=true 但章节不可解释（无集号非电影文件）→ 先回滚新建空番，再 code 0 review_required。
func TestAssistTrustedReviewRollsBackNewAnime(t *testing.T) {
	root := t.TempDir()
	relDir := "ObscureDir"
	writeVideos(t, filepath.Join(root, relDir), "CodeX.Extras.mkv")
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	data := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, BangumiID: assistCodeXID}))
	if data.Status != "review_required" || data.Reason == "" || data.Anime != nil {
		t.Fatalf("应 review_required: %#v", data)
	}
	if c := countAssistTables(t); c.animes != 0 || c.episodes != 0 {
		t.Fatalf("新建空番剧应被回滚: %#v", c)
	}
}

// 目标条目 404：预览返回 review_required（零写库），入库同样 review_required 且回滚（建档失败不落库）。
func TestAssistMissingSubjectPreviewReview(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "ObscureDir"), "CodeX.S01E03.mkv")
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	data := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{FilePath: "ObscureDir", BangumiID: 777777, AutoCommit: boolPtr(false)}))
	if data.Status != "review_required" {
		t.Fatalf("条目 404 预览应 review_required: %#v", data)
	}
	if c := countAssistTables(t); c != (assistTableCounts{}) {
		t.Fatalf("预览零写库: %#v", c)
	}
}

// 无候选 → no_match；auto_commit=true 时写入收件箱。
func TestAssistNoCandidatesNoMatch(t *testing.T) {
	root := t.TempDir()
	relDir := "Unknown"
	name := "Zzqx.Nothing.S01E01.mkv"
	writeVideos(t, filepath.Join(root, relDir), name)
	handler := setupScanHandlerV2(t, root)

	preview := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, TitleHint: "Zzqx", AutoCommit: boolPtr(false)}))
	if preview.Status != "no_match" {
		t.Fatalf("无候选应 no_match: %#v", preview)
	}
	if c := countAssistTables(t); c != (assistTableCounts{}) {
		t.Fatalf("预览零写入: %#v", c)
	}
	committed := decodeAssistSuccess(t, postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, TitleHint: "Zzqx"}))
	if committed.Status != "no_match" {
		t.Fatalf("无候选应 no_match: %#v", committed)
	}
	if c := countAssistTables(t); c.unidentified != 1 || c.animes != 0 {
		t.Fatalf("auto_commit 默认 true 应写入收件箱 1 条: %#v", c)
	}
}

// 上游故障：Bangumi 返回 5xx → 9999 upstream，零写入。
func TestAssistUpstreamFailure(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "ObscureDir"), "CodeX.S01E03.mkv")
	handler, fix := setupScanHandlerV2WithFixture(t, root, assistCodeXSeed())
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)
	fix.bangumi.SetBaseURL(broken.URL)

	for _, req := range []models.AssistMatchRequest{
		{FilePath: "ObscureDir", BangumiID: assistCodeXID, AutoCommit: boolPtr(false)},
		{FilePath: "ObscureDir", BangumiID: assistCodeXID},
		{FilePath: "ObscureDir", TitleHint: "コードエックス", AutoCommit: boolPtr(false)},
	} {
		resp := postAssist(t, handler, req)
		data := decodeConfirmError(t, resp)
		if resp.Code != utils.CodeInternal || data.Reason != confirmReasonUpstream {
			t.Fatalf("上游故障应 9999 upstream: req=%#v code=%d %#v", req, resp.Code, data)
		}
	}
	if c := countAssistTables(t); c != (assistTableCounts{}) {
		t.Fatalf("上游故障零写入: %#v", c)
	}
}

// CAS 冲突：prepare 与 commit 之间番剧快照被改 → 9999 conflict，且回滚本请求新建的番剧。
func TestAssistCASConflictRollsBackCreated(t *testing.T) {
	root := t.TempDir()
	relDir := "ObscureDir"
	writeVideos(t, filepath.Join(root, relDir), "CodeX.S01E03.mkv")
	handler, fix := setupScanHandlerV2WithFixture(t, root, assistCodeXSeed())

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v0/episodes" {
			first := false
			once.Do(func() { first = true })
			if first {
				close(entered)
				<-release
			}
		}
		fix.serve(w, r)
	}))
	t.Cleanup(proxy.Close)
	fix.bangumi.SetBaseURL(proxy.URL)

	done := make(chan confirmResponse, 1)
	go func() {
		done <- postAssist(t, handler, models.AssistMatchRequest{FilePath: relDir, BangumiID: assistCodeXID})
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("章节请求未发生")
	}
	if _, err := database.DB.Exec("UPDATE animes SET ep_count = ? WHERE bangumi_id = ?", 24, assistCodeXID); err != nil {
		t.Fatal(err)
	}
	close(release)
	resp := <-done
	data := decodeConfirmError(t, resp)
	if resp.Code != utils.CodeInternal || data.Reason != confirmReasonConflict {
		t.Fatalf("快照变更应 9999 conflict: code=%d %#v", resp.Code, data)
	}
	if c := countAssistTables(t); c.animes != 0 || c.episodes != 0 {
		t.Fatalf("本请求新建的番剧应被回滚: %#v", c)
	}
}

// 并发：同 bangumi_id 两请求经 confirmLock 串行化，只建一条番剧、一条剧集。
func TestAssistConcurrentSameBangumiSerialized(t *testing.T) {
	root := t.TempDir()
	relDir := "ObscureDir"
	name := "CodeX.S01E03.mkv"
	writeVideos(t, filepath.Join(root, relDir), name)
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	body, err := json.Marshal(models.AssistMatchRequest{FilePath: relDir, FileNames: []string{name}, BangumiID: assistCodeXID})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/api/library/assist-match", handler.AssistMatch)
	const workers = 2
	results := make([]confirmResponse, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/library/assist-match", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			_ = json.Unmarshal(recorder.Body.Bytes(), &results[i])
		}(i)
	}
	close(start)
	wg.Wait()
	created := 0
	for i, resp := range results {
		data := decodeAssistSuccess(t, resp)
		if data.Status != "committed" || len(data.Episodes) != 1 {
			t.Fatalf("请求 %d: %#v", i, data)
		}
		if data.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("只应有一个 created=true，got %d", created)
	}
	if c := countAssistTables(t); c.animes != 1 || c.episodes != 1 {
		t.Fatalf("应只建 1 番 1 集: %#v", c)
	}
}

// ---- 路由权限（与 main.go 三路分组一致：catalogRead / manager / protected+admin） ----

func setupAssistRouter(t *testing.T, root string) (*gin.Engine, string, string, string) {
	t.Helper()
	handler := setupScanHandlerV2(t, root, assistCodeXSeed())
	admin, err := database.CreateUser("assist-admin", "password", true)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := database.CreateUser("assist-ordinary", "password", false)
	if err != nil {
		t.Fatal(err)
	}
	auth := services.NewAuthService("assist-secret", 24*time.Hour)
	adminToken, _, err := auth.IssueToken(*admin)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryToken, _, err := auth.IssueToken(*ordinary)
	if err != nil {
		t.Fatal(err)
	}
	raw := "fw_" + strings.Repeat("ab", 32)
	sum := sha256.Sum256([]byte(raw))
	if _, err := database.CreateAPIKey("hermes", hex.EncodeToString(sum[:]), raw[:11]+"..."+raw[len(raw)-4:], admin.ID); err != nil {
		t.Fatal(err)
	}

	stub := func(c *gin.Context) { utils.Success(c, nil) }
	apiKeyHandler := NewAPIKeyHandler()
	router := gin.New()
	api := router.Group("/api")
	protected := api.Group("")
	protected.Use(middleware.JWTAuth(auth))
	protected.GET("/auth/me", stub)
	protected.GET("/progress/continue", stub)
	protected.GET("/me/bangumi", stub)
	protected.POST("/episodes/:id/media-token", stub)
	catalogRead := api.Group("")
	catalogRead.Use(middleware.JWTOrAPIKeyAuth(auth))
	catalogRead.GET("/animes", handler.List)
	catalogRead.GET("/animes/:id", handler.Get)
	catalogRead.GET("/animes/:id/episodes", handler.Episodes)
	manager := api.Group("")
	manager.Use(middleware.JWTOrAPIKeyAuth(auth), middleware.RequireAdmin)
	manager.POST("/animes", handler.Create)
	manager.POST("/library/inbox/confirm", handler.Confirm)
	manager.POST("/library/assist-match", handler.AssistMatch)
	adminGroup := protected.Group("/admin")
	adminGroup.Use(middleware.RequireAdmin)
	adminGroup.GET("/users", stub)
	adminGroup.GET("/api-keys", apiKeyHandler.List)
	return router, raw, adminToken, ordinaryToken
}

func doAssistRouteRequest(t *testing.T, router *gin.Engine, headers map[string]string, method, path string, body []byte) confirmResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	router.ServeHTTP(recorder, request)
	var resp confirmResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return resp
}

func TestAssistRouteAPIKeyScope(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "ObscureDir"), "CodeX.S01E03.mkv")
	router, raw, _, _ := setupAssistRouter(t, root)
	body := []byte(`{"file_path":"ObscureDir","bangumi_id":12345}`)

	for _, headers := range []map[string]string{
		{middleware.APIKeyHeader: raw},
		{"Authorization": "Bearer " + raw},
	} {
		if resp := doAssistRouteRequest(t, router, headers, http.MethodGet, "/api/animes", nil); resp.Code != utils.CodeSuccess {
			t.Fatalf("API Key 应可读 GET /api/animes: %#v", resp)
		}
		for _, path := range []string{"/api/admin/users", "/api/admin/api-keys", "/api/progress/continue", "/api/me/bangumi", "/api/auth/me"} {
			if resp := doAssistRouteRequest(t, router, headers, http.MethodGet, path, nil); resp.Code != utils.CodeUnauthenticated {
				t.Fatalf("API Key 访问 %s 应 2001，got %d", path, resp.Code)
			}
		}
		if resp := doAssistRouteRequest(t, router, headers, http.MethodPost, "/api/episodes/1/media-token", nil); resp.Code != utils.CodeUnauthenticated {
			t.Fatalf("API Key 申请 media-token 应 2001，got %d", resp.Code)
		}
	}
	// API Key 可调用 assist-match 并真正入库
	resp := doAssistRouteRequest(t, router, map[string]string{middleware.APIKeyHeader: raw}, http.MethodPost, "/api/library/assist-match", body)
	data := decodeAssistSuccess(t, resp)
	if data.Status != "committed" || data.Anime == nil || data.Anime.BangumiID != assistCodeXID {
		t.Fatalf("API Key 调用 assist-match 应 committed: %#v", data)
	}
	if got := doAssistRouteRequest(t, router, map[string]string{middleware.APIKeyHeader: raw}, http.MethodGet, "/api/animes/"+trimmedID(data.Anime.ID)+"/episodes", nil); got.Code != utils.CodeSuccess {
		t.Fatalf("API Key 应可读 GET /api/animes/:id/episodes: %#v", got)
	}
	// 无效 API Key
	if resp := doAssistRouteRequest(t, router, map[string]string{middleware.APIKeyHeader: "fw_" + strings.Repeat("0", 64)}, http.MethodPost, "/api/library/assist-match", body); resp.Code != utils.CodeUnauthenticated {
		t.Fatalf("无效 API Key 应 2001，got %d", resp.Code)
	}
	// 无凭据
	if resp := doAssistRouteRequest(t, router, nil, http.MethodPost, "/api/library/assist-match", body); resp.Code != utils.CodeUnauthenticated {
		t.Fatalf("无凭据应 2001，got %d", resp.Code)
	}
}

func TestAssistRouteJWTScope(t *testing.T) {
	root := t.TempDir()
	writeVideos(t, filepath.Join(root, "ObscureDir"), "CodeX.S01E03.mkv")
	router, _, adminToken, ordinaryToken := setupAssistRouter(t, root)
	ordinary := map[string]string{"Authorization": "Bearer " + ordinaryToken}
	admin := map[string]string{"Authorization": "Bearer " + adminToken}
	body := []byte(`{"file_path":"ObscureDir","bangumi_id":12345,"auto_commit":false}`)

	// 普通用户 JWT：仍可读 GET /api/animes 与个人接口，写接口与 assist-match 被 RequireAdmin 拒绝（2002）。
	if resp := doAssistRouteRequest(t, router, ordinary, http.MethodGet, "/api/animes", nil); resp.Code != utils.CodeSuccess {
		t.Fatalf("普通 JWT 应可读 GET /api/animes: %#v", resp)
	}
	if resp := doAssistRouteRequest(t, router, ordinary, http.MethodGet, "/api/progress/continue", nil); resp.Code != utils.CodeSuccess {
		t.Fatalf("普通 JWT 应可访问个人进度: %#v", resp)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/library/assist-match"},
		{http.MethodPost, "/api/animes"},
		{http.MethodPost, "/api/library/inbox/confirm"},
		{http.MethodGet, "/api/admin/api-keys"},
	} {
		if resp := doAssistRouteRequest(t, router, ordinary, tc.method, tc.path, body); resp.Code != utils.CodeForbidden {
			t.Fatalf("普通 JWT %s %s 应 2002，got %d", tc.method, tc.path, resp.Code)
		}
	}
	// 管理员 JWT：assist-match 与 /admin/api-keys 均可访问。
	data := decodeAssistSuccess(t, doAssistRouteRequest(t, router, admin, http.MethodPost, "/api/library/assist-match", body))
	if data.Status != "matched" {
		t.Fatalf("管理员 JWT 预览应 matched: %#v", data)
	}
	if resp := doAssistRouteRequest(t, router, admin, http.MethodGet, "/api/admin/api-keys", nil); resp.Code != utils.CodeSuccess {
		t.Fatalf("管理员 JWT 应可访问 /admin/api-keys: %#v", resp)
	}
}

// 计划者 review #1：同一输入下 auto_commit=false 预览与 auto_commit=true 提交对 episode_offset 的结论一致。
func TestAssistEpisodeOffsetPreviewAndCommitConsistent(t *testing.T) {
	xianWang := "The.Daily.Life.of.the.Immortal.King.S05E10.1080p.CR.WEB-DL.AAC2.0.H.264-VARYG.mkv"
	cases := []struct {
		name   string
		seed   bangumiSeed
		file   string
		req    models.AssistMatchRequest
		reason string
	}{
		{name: "bangumi_id 集号未超过话数", seed: assistCodeXSeed(), file: "CodeX.S02E10.mkv",
			req:    models.AssistMatchRequest{BangumiID: assistCodeXID, EpisodeOffset: 5},
			reason: "集号偏移仅适用于累计编号（集号需大于该条目话数）"},
		{name: "bangumi_id 偏移不小于集号", seed: assistCodeXSeed(), file: "CodeX.S02E13.mkv",
			req:    models.AssistMatchRequest{BangumiID: assistCodeXID, EpisodeOffset: 20},
			reason: "episode_offset 不能大于或等于文件集号"},
		{name: "title_hint 集号未超过话数", seed: assistXianWangSeed(), file: xianWang,
			req:    models.AssistMatchRequest{TitleHint: "仙王的日常生活", SeasonHint: 5, EpisodeOffset: 5},
			reason: "集号偏移仅适用于累计编号（集号需大于该条目话数）"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			relDir := "OffsetDir"
			writeVideos(t, filepath.Join(root, relDir), tc.file)
			handler := setupScanHandlerV2(t, root, tc.seed)
			for _, auto := range []bool{false, true} {
				req := tc.req
				req.FilePath = relDir
				req.FileNames = []string{tc.file}
				req.AutoCommit = boolPtr(auto)
				data := decodeAssistSuccess(t, postAssist(t, handler, req))
				// 提交路径经 commitInboxFiles 包装（前缀「匹配需要人工确认: 」、后缀「（未写入任何变更）」），核心原因须一致
				if data.Status != "review_required" || !strings.Contains(data.Reason, tc.reason) {
					t.Fatalf("auto_commit=%v 应 review_required %q: %#v", auto, tc.reason, data)
				}
				if c := countAssistTables(t); c.animes != 0 || c.episodes != 0 {
					t.Fatalf("auto_commit=%v 不得建番/写剧集: %#v", auto, c)
				}
			}
		})
	}
}

