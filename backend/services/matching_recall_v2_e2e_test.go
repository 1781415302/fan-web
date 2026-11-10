package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fan-web/database"
	"fan-web/models"
)

// Plan v1 §8 S8：识别补洞 + 点击入库原子化 的联调回归（e2e）。
// 夹具：matching/testdata/quality_cases.json 中 id 以 "rv2-" 开头的用例（与内核 TestMatchingQuality 同源），
// provider 回放：testdata/matching/provider_replay.json（Re0/孤独摇滚/芙莉莲/BLEACH/CLANNAD 取自 Bangumi API 快照）。
// 本文件走真实临时文件 + SQLite + LibraryService.Scan / ScannerService.ScanForAnime 全链路；不放宽任何阈值。

const recallV2CasePrefix = "rv2-"

// recallV2SessionOverrides：内核 TestMatchingQuality 直接调 InterpretChapters（零值 ChapterOptions、无季锚点），
// 而全链路走 MatchingSession（interpretWithAnchor + 会话级排序/证据补全）。下列用例在内核层保守进 Review，
// 在全链路上有确定的正确条目，按真实链路断言（仍逐文件比对条目与 stored_number，不是放宽）。
var recallV2SessionOverrides = map[string]scanCaseExpect{
	// S04E19 无"总第"：同季兄弟 丧失篇(sort 67..77)/夺还篇(sort 78..85) → 锚点 67 → sort 85 = 夺还篇 ep8（season_sort）。
	"rv2-re0-s04e19-no-abs": {SubjectID: 633836, StoredNumber: 8, Mode: "accepted"},
	// 目录 葬送的芙莉莲/Season 2 + 05.mkv：内核层因"候选无法解释查询季号"进 Review（基线既有行为）；
	// 会话链路得到第二季 515759 ep5（Bangumi ep5 = sort 33，basis ep）。
	"rv2-season-dir-frieren-s2": {SubjectID: 515759, StoredNumber: 5, Mode: "accepted"},
}

func applyRecallV2SessionOverride(tc scanCase) scanCase {
	ov, ok := recallV2SessionOverrides[tc.ID]
	if !ok {
		return tc
	}
	out := tc
	out.Expected = make([]scanCaseExpect, len(tc.Expected))
	for i, e := range tc.Expected {
		e.SubjectID, e.StoredNumber, e.Mode = ov.SubjectID, ov.StoredNumber, ov.Mode
		out.Expected[i] = e
	}
	return out
}

func loadRecallV2Cases(t *testing.T) []scanCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("matching", "testdata", "quality_cases.json"))
	if err != nil {
		t.Fatalf("read quality_cases.json: %v", err)
	}
	var doc scanCasesFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse quality_cases.json: %v", err)
	}
	var out []scanCase
	for _, tc := range doc.Cases {
		if strings.HasPrefix(tc.ID, recallV2CasePrefix) {
			out = append(out, tc)
		}
	}
	if len(out) == 0 {
		t.Fatal("quality_cases.json 缺少 rv2- 联调用例")
	}
	return out
}

func findRecallV2Case(t *testing.T, id string) scanCase {
	t.Helper()
	for _, tc := range loadRecallV2Cases(t) {
		if tc.ID == id {
			return tc
		}
	}
	t.Fatalf("missing rv2 case %q", id)
	return scanCase{}
}

// sceneARe0Files 现场 A：[晚街与灯] Re0 第四季 4th - 12..19（可选 总第78..85）；夺还篇 633836 = ep1..8 / sort 78..85。
func sceneARe0Files(withAbs bool) []string {
	names := make([]string, 0, 8)
	for ep := 12; ep <= 19; ep++ {
		if withAbs {
			names = append(names, fmt.Sprintf("[晚街与灯][Re Zero kara Hajimeru Isekai Seikatsu][4th - %02d][总第%d][WebRip][1080P_AVC_AAC][简日双语内嵌].mp4", ep, ep+66))
		} else {
			names = append(names, fmt.Sprintf("[晚街与灯][Re Zero kara Hajimeru Isekai Seikatsu][4th - %02d][WebRip][1080P_AVC_AAC][简日双语内嵌].mp4", ep))
		}
	}
	return names
}

type episodeSnapshot struct {
	ID       int64
	EpNumber int
}

func snapshotAnimeEpisodes(t *testing.T, animeID int64) map[string]episodeSnapshot {
	t.Helper()
	eps, err := database.ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatalf("ListEpisodesByAnimeID: %v", err)
	}
	out := make(map[string]episodeSnapshot, len(eps))
	for _, ep := range eps {
		out[ep.FilePath] = episodeSnapshot{ID: ep.ID, EpNumber: ep.EpNumber}
	}
	return out
}

func assertSnapshotsEqual(t *testing.T, label string, before, after map[string]episodeSnapshot) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("%s: 剧集数变化 before=%v after=%v", label, before, after)
	}
	for name, b := range before {
		a, ok := after[name]
		if !ok {
			t.Fatalf("%s: 文件 %q 重扫后丢失", label, name)
		}
		if a != b {
			t.Fatalf("%s: 文件 %q 重扫后变化 before=%+v after=%+v", label, name, b, a)
		}
	}
}

// createConfirmTarget 模拟确认接口"不存在才建档"一步（只写 animes 行；EpCount 取条目话数）。
func createConfirmTarget(t *testing.T, fake *fixtureProvider, bangumiID int, dir string) models.Anime {
	t.Helper()
	sub, ok := fake.subjects[bangumiID]
	if !ok {
		t.Fatalf("fixture missing subject %d", bangumiID)
	}
	created, err := database.CreateAnime(&models.Anime{
		Title: sub.Name, TitleCn: sub.NameCn, BangumiID: bangumiID, EpCount: sub.EpisodeCount, FilePath: dir,
	})
	if err != nil || created == nil {
		t.Fatalf("CreateAnime: %v", err)
	}
	return *created
}

// scanTargetAndReconcile 与 handlers.runScanV2 同序：先读 existing → ScanForAnime(opts...) → ReconcileEpisodes(Expected=existing, RemoveMissing)。
func scanTargetAndReconcile(t *testing.T, root string, fake *fixtureProvider, anime models.Anime, opts ...ScanOption) ([]models.Episode, error) {
	t.Helper()
	prev := scanSessionHook
	scanSessionHook = func(ctx context.Context, r *MatchingRuntime) *MatchingSession {
		return newMatchingSessionForTest(ctx, r, fake)
	}
	defer func() { scanSessionHook = prev }()
	existing, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatalf("existing: %v", err)
	}
	inv, err := newV2Scanner(t, root).ScanForAnime(anime, existing, nil, opts...)
	if err != nil {
		return nil, err
	}
	if err := database.ReconcileEpisodes(anime.ID, database.EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      existing,
		Desired:       inv.Desired,
		PresentFiles:  inv.PresentFiles,
		Renames:       inv.Renames,
		RemoveMissing: true,
	}); err != nil {
		return nil, err
	}
	return database.ListEpisodesByAnimeID(anime.ID)
}

// TestRecallV2E2ELibraryScan 现场 A/B 全库扫描：quality_cases.json 的 rv2- 用例逐文件比较映射，
// 并对每个已接受条目跑两次 handler 风格单番重扫（同一文件扫两次结果不变）。
func TestRecallV2E2ELibraryScan(t *testing.T) {
	replay := loadProviderReplayDoc(t)
	for _, tc := range loadRecallV2Cases(t) {
		tc := applyRecallV2SessionOverride(tc)
		t.Run(tc.ID, func(t *testing.T) {
			setupLibraryDB(t)
			root := t.TempDir()
			writeScanCaseFiles(t, root, tc)
			fake := newFixtureProvider(t, replay)
			res, err := newV2Library(t, root, fake).Scan()
			if err != nil {
				t.Fatalf("LibraryService.Scan: %v", err)
			}
			t.Logf("scan: total=%d new_animes=%d new_eps=%d unidentified=%d", res.TotalFiles, res.NewAnimes, res.NewEpisodes, len(res.Unidentified))
			got := collectAllFileOutcomes(t, tc.Expected)
			assertPerFileMapping(t, tc, got)
		})
	}
}

// TestRecallV2E2ESameFileScannedTwiceStable Plan §6：abs_sort/season_sort 存条目内集号后，
// 同一文件再扫两次（单番重扫 ×2 + 全库重扫）episode ID 与 ep_number 均不变（EpCount>0）。
func TestRecallV2E2ESameFileScannedTwiceStable(t *testing.T) {
	replay := loadProviderReplayDoc(t)
	for _, id := range []string{"rv2-re0-4th-abs", "rv2-bocchi-split", "rv2-qianxia-bracket-ep", "rv2-season-dir-bare-number", "rv2-ep01-batch-dir"} {
		id := id
		t.Run(id, func(t *testing.T) {
			tc := findRecallV2Case(t, id)
			setupLibraryDB(t)
			root := t.TempDir()
			writeScanCaseFiles(t, root, tc)
			fake := newFixtureProvider(t, replay)
			svc := newV2Library(t, root, fake)
			if _, err := svc.Scan(); err != nil {
				t.Fatalf("Scan #1: %v", err)
			}
			assertPerFileMapping(t, tc, collectAllFileOutcomes(t, tc.Expected))

			subjectID := tc.Expected[0].SubjectID
			anime, err := database.GetAnimeByBangumiID(subjectID)
			if err != nil || anime == nil {
				t.Fatalf("anime %d: %v", subjectID, err)
			}
			if anime.EpCount <= 0 {
				t.Fatalf("前提：EpCount>0，got %d", anime.EpCount)
			}
			before := snapshotAnimeEpisodes(t, anime.ID)
			if len(before) != len(tc.Expected) {
				t.Fatalf("入库数 %d want %d: %v", len(before), len(tc.Expected), before)
			}
			for round := 1; round <= 2; round++ {
				runHandlerStyleRescan(t, root, fake, *anime)
				assertSnapshotsEqual(t, fmt.Sprintf("单番重扫#%d", round), before, snapshotAnimeEpisodes(t, anime.ID))
			}
			res, err := svc.Scan()
			if err != nil {
				t.Fatalf("Scan #2: %v", err)
			}
			if res.NewAnimes != 0 || res.NewEpisodes != 0 {
				t.Fatalf("全库重扫不应新增: %#v", res)
			}
			assertSnapshotsEqual(t, "全库重扫", before, snapshotAnimeEpisodes(t, anime.ID))
		})
	}
}

// TestRecallV2E2EInboxConfirmSceneA 现场 A 点击确认（服务层等价）：建档夺还篇 → ScanForAnime(WithOnlyFiles) →
// Reconcile；文件号 12..19 + 总第78..85 经 abs_sort 存 ep 1..8；随后不带选项重扫两次结果不变。
func TestRecallV2E2EInboxConfirmSceneA(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := filepath.Join("Re0", "Season4")
	names := sceneARe0Files(true)
	writeScanVideos(t, filepath.Join(root, dir), names...)
	fake := newFixtureProvider(t, loadProviderReplayDoc(t))
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	anime := createConfirmTarget(t, fake, 633836, dir)
	eps, err := scanTargetAndReconcile(t, root, fake, anime, WithOnlyFiles(names))
	if err != nil {
		t.Fatalf("confirm scan: %v", err)
	}
	if len(eps) != len(names) {
		t.Fatalf("应入库 %d 个文件，got %d: %#v", len(names), len(eps), eps)
	}
	got := snapshotAnimeEpisodes(t, anime.ID)
	for i, name := range names {
		if got[name].EpNumber != i+1 {
			t.Fatalf("%s: ep_number=%d want %d", name, got[name].EpNumber, i+1)
		}
	}
	for round := 1; round <= 2; round++ {
		if _, err := scanTargetAndReconcile(t, root, fake, anime); err != nil {
			t.Fatalf("rescan #%d: %v", round, err)
		}
		assertSnapshotsEqual(t, fmt.Sprintf("确认后重扫#%d", round), got, snapshotAnimeEpisodes(t, anime.ID))
	}
}

// TestRecallV2E2EInboxConfirmOnlyFilesSubset WithOnlyFiles 只入库所选文件，其余留在磁盘不入库。
func TestRecallV2E2EInboxConfirmOnlyFilesSubset(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := filepath.Join("Re0", "Season4")
	names := sceneARe0Files(true)
	writeScanVideos(t, filepath.Join(root, dir), names...)
	fake := newFixtureProvider(t, loadProviderReplayDoc(t))
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	anime := createConfirmTarget(t, fake, 633836, dir)
	pick := []string{names[0], names[7]}
	if _, err := scanTargetAndReconcile(t, root, fake, anime, WithOnlyFiles(pick)); err != nil {
		t.Fatalf("confirm scan: %v", err)
	}
	got := snapshotAnimeEpisodes(t, anime.ID)
	if len(got) != 2 || got[names[0]].EpNumber != 1 || got[names[7]].EpNumber != 8 {
		t.Fatalf("only-files 应只入库 ep1/ep8: %v", got)
	}
}

// TestRecallV2E2ESceneANoAbs 现场 A 去掉"总第"：全库扫描时 ranked 含同季兄弟（丧失篇/夺还篇），
// season_sort 锚点 = 67 → 4th - 12..19 落夺还篇 ep1..8；之后重扫两次不抖动。
func TestRecallV2E2ESceneANoAbs(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := filepath.Join("Re0", "Season4")
	names := sceneARe0Files(false)
	writeScanVideos(t, filepath.Join(root, dir), names...)
	fake := newFixtureProvider(t, loadProviderReplayDoc(t))

	svc := newV2Library(t, root, fake)
	res, err := svc.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	t.Logf("no-abs library scan: new_animes=%d new_eps=%d unidentified=%d", res.NewAnimes, res.NewEpisodes, len(res.Unidentified))
	for _, u := range res.Unidentified {
		t.Logf("  inbox %s reason=%q candidates=%d", u.FileName, u.Reason, len(u.Candidates))
	}
	anime, err := database.GetAnimeByBangumiID(633836)
	if err != nil || anime == nil {
		t.Fatalf("夺还篇应入库: %v unidentified=%s", err, summarizeUnidentified(res))
	}
	got := snapshotAnimeEpisodes(t, anime.ID)
	for i, name := range names {
		if got[name].EpNumber != i+1 {
			t.Fatalf("%s: ep_number=%d want %d (got=%v)", name, got[name].EpNumber, i+1, got)
		}
	}
	if other, _ := database.GetAnimeByBangumiID(547888); other != nil {
		t.Fatalf("丧失篇不应入库: %#v", other)
	}
	for round := 1; round <= 2; round++ {
		runHandlerStyleRescan(t, root, fake, *anime)
		assertSnapshotsEqual(t, fmt.Sprintf("单番重扫#%d", round), got, snapshotAnimeEpisodes(t, anime.ID))
	}
}

// TestRecallV2E2EInboxConfirmNoAbs 手选夺还篇（无"总第"）走确认路径：ResolveForTarget 的召回里含同季兄弟（丧失篇），
// season_sort 锚点 67 → 无需偏移即入库 ep1..8；不带选项重扫两次不抖动。
func TestRecallV2E2EInboxConfirmNoAbs(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := filepath.Join("Re0", "Season4")
	names := sceneARe0Files(false)
	writeScanVideos(t, filepath.Join(root, dir), names...)
	fake := newFixtureProvider(t, loadProviderReplayDoc(t))
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	anime := createConfirmTarget(t, fake, 633836, dir)
	if _, err := scanTargetAndReconcile(t, root, fake, anime, WithOnlyFiles(names)); err != nil {
		t.Fatalf("confirm scan: %v", err)
	}
	got := snapshotAnimeEpisodes(t, anime.ID)
	for i, name := range names {
		if got[name].EpNumber != i+1 {
			t.Fatalf("%s: ep_number=%d want %d (got=%v)", name, got[name].EpNumber, i+1, got)
		}
	}
	for round := 1; round <= 2; round++ {
		if _, err := scanTargetAndReconcile(t, root, fake, anime); err != nil {
			t.Fatalf("rescan #%d: %v", round, err)
		}
		assertSnapshotsEqual(t, fmt.Sprintf("确认后重扫#%d", round), got, snapshotAnimeEpisodes(t, anime.ID))
	}
}

// TestRecallV2E2EInboxConfirmNoSiblingNeedsOffset Plan §4.1 预期降级：召回里没有同季兄弟时锚点只由目标自身得出
// （78+12-1=89 无命中）→ ErrMatchReviewRequired 且零写入；用户填集号偏移 11 后入库 ep1..8，
// 之后不带偏移重扫两次不抖动（EpCount=8>0，原始号 12..19 > EpCount 受 correctExistingEpNumber 保护）。
func TestRecallV2E2EInboxConfirmNoSiblingNeedsOffset(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := filepath.Join("Re0", "Season4")
	names := sceneARe0Files(false)
	writeScanVideos(t, filepath.Join(root, dir), names...)
	fake := newFixtureProvider(t, loadProviderReplayDoc(t))
	// 只让检索返回目标本身：模拟 ranked 中无同季兄弟（如兄弟条目未被检索到）。
	for key, entry := range fake.search {
		for _, id := range entry.IDs {
			if id == 633836 {
				entry.IDs = []int{633836}
				fake.search[key] = entry
				break
			}
		}
	}
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	anime := createConfirmTarget(t, fake, 633836, dir)
	_, err := scanTargetAndReconcile(t, root, fake, anime, WithOnlyFiles(names))
	if !errors.Is(err, ErrMatchReviewRequired) {
		t.Fatalf("无兄弟、无偏移应为 ErrMatchReviewRequired，got %v", err)
	}
	if left := snapshotAnimeEpisodes(t, anime.ID); len(left) != 0 {
		t.Fatalf("失败不得写入剧集: %v", left)
	}

	if _, err := scanTargetAndReconcile(t, root, fake, anime, WithOnlyFiles(names), WithEpisodeOffset(11)); err != nil {
		t.Fatalf("offset 11: %v", err)
	}
	got := snapshotAnimeEpisodes(t, anime.ID)
	for i, name := range names {
		if got[name].EpNumber != i+1 {
			t.Fatalf("%s: ep_number=%d want %d (got=%v)", name, got[name].EpNumber, i+1, got)
		}
	}
	for round := 1; round <= 2; round++ {
		if _, err := scanTargetAndReconcile(t, root, fake, anime); err != nil {
			t.Fatalf("rescan #%d: %v", round, err)
		}
		assertSnapshotsEqual(t, fmt.Sprintf("偏移后重扫#%d", round), got, snapshotAnimeEpisodes(t, anime.ID))
	}
}
