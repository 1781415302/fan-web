package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services/matching"
)

// ---- ResolveAssistInputPath ----

func TestResolveAssistInputPathDirectoryFiltersAssociated(t *testing.T) {
	root := t.TempDir()
	writeScanVideos(t, filepath.Join(root, "a", "b"), "01.mkv", "02.mkv", "03.mkv", "note.txt")
	withFileAssociated(t, func(name, dir string) (bool, error) {
		if dir != "a/b" {
			t.Errorf("关联检查应使用归一化目录 a/b，got %q", dir)
		}
		return name == "02.mkv", nil
	})
	sc := NewScannerService(root)
	for _, raw := range []string{"a/b", "a\\b/", "a\\b"} {
		got, err := sc.ResolveAssistInputPath(raw, nil)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		want := AssistInputResolved{
			RelDir:       "a/b",
			PresentFiles: []string{"01.mkv", "02.mkv", "03.mkv"},
			FreeFiles:    []string{"01.mkv", "03.mkv"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: got %#v want %#v", raw, got, want)
		}
	}
}

func TestResolveAssistInputPathAllAssociatedKeepsPresent(t *testing.T) {
	root := t.TempDir()
	writeScanVideos(t, filepath.Join(root, "d"), "01.mkv")
	withFileAssociated(t, func(string, string) (bool, error) { return true, nil })
	got, err := NewScannerService(root).ResolveAssistInputPath("d", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PresentFiles) != 1 || len(got.FreeFiles) != 0 || got.FreeFiles == nil {
		t.Fatalf("全已关联目录应保留 PresentFiles 且 FreeFiles 为空: %#v", got)
	}

	empty := t.TempDir()
	writeScanVideos(t, filepath.Join(empty, "e"), "readme.txt")
	got, err = NewScannerService(empty).ResolveAssistInputPath("e", nil)
	if err != nil || len(got.PresentFiles) != 0 {
		t.Fatalf("空目录应 PresentFiles 为空: %#v err=%v", got, err)
	}
}

func TestResolveAssistInputPathExplicitFileNames(t *testing.T) {
	root := t.TempDir()
	writeScanVideos(t, root, "CodeX.S01E03.mkv", "CodeX.S01E04.mkv")
	// 显式 file_names 不过滤已关联文件（交给 InspectAssistFilesOwnership）
	withFileAssociated(t, func(string, string) (bool, error) { return true, nil })
	sc := NewScannerService(root)

	// 空 file_path 表示视频根目录
	got, err := sc.ResolveAssistInputPath("", []string{"CodeX.S01E04.mkv", "CodeX.S01E03.mkv", "CodeX.S01E04.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if got.RelDir != "" || got.IsSingleFilePath || !reflect.DeepEqual(got.FreeFiles, []string{"CodeX.S01E04.mkv", "CodeX.S01E03.mkv"}) {
		t.Fatalf("显式 file_names 应去重保序: %#v", got)
	}
	if len(got.PresentFiles) != 2 {
		t.Fatalf("PresentFiles=%v", got.PresentFiles)
	}

	bad := [][]string{
		{"missing.mkv"},
		{"../CodeX.S01E03.mkv"},
		{"sub/CodeX.S01E03.mkv"},
		{"sub\\CodeX.S01E03.mkv"},
		{""},
		{"   "},
		{"CodeX\x00.mkv"},
		{"CodeX..S01E03.mkv"},
	}
	for _, names := range bad {
		if _, err := sc.ResolveAssistInputPath("", names); !errors.Is(err, ErrAssistInvalidInput) {
			t.Errorf("file_names=%q 应参数错误，got %v", names, err)
		}
	}
}

func TestResolveAssistInputPathSingleFile(t *testing.T) {
	root := t.TempDir()
	dir := "XianWangS5"
	name := "The.Daily.Life.of.the.Immortal.King.S05E12.mkv"
	writeScanVideos(t, filepath.Join(root, dir), name, "other.mkv")
	writeScanVideos(t, root, "root.mkv")
	withFileAssociated(t, func(string, string) (bool, error) { return true, nil })
	sc := NewScannerService(root)

	for _, raw := range []string{dir + "/" + name, dir + "\\" + name} {
		got, err := sc.ResolveAssistInputPath(raw, nil)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		want := AssistInputResolved{RelDir: dir, PresentFiles: []string{name}, FreeFiles: []string{name}, IsSingleFilePath: true}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: got %#v want %#v", raw, got, want)
		}
	}
	got, err := sc.ResolveAssistInputPath(dir+"/"+name, []string{name})
	if err != nil || !got.IsSingleFilePath {
		t.Fatalf("一致的 file_names 应允许: %#v %v", got, err)
	}
	if _, err := sc.ResolveAssistInputPath(dir+"/"+name, []string{"other.mkv"}); !errors.Is(err, ErrAssistInvalidInput) {
		t.Fatalf("不一致 file_names 应参数错误，got %v", err)
	}
	if _, err := sc.ResolveAssistInputPath(dir+"/"+name, []string{name, "other.mkv"}); !errors.Is(err, ErrAssistInvalidInput) {
		t.Fatalf("多余 file_names 应参数错误，got %v", err)
	}
	got, err = sc.ResolveAssistInputPath("root.mkv", nil)
	if err != nil || got.RelDir != "" || !got.IsSingleFilePath {
		t.Fatalf("根目录单文件 RelDir 应为空: %#v %v", got, err)
	}
}

func TestResolveAssistInputPathRejectsUnsafe(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeScanVideos(t, outside, "secret.mkv")
	writeScanVideos(t, filepath.Join(root, "d"), "ok.mkv", "readme.txt")
	if err := os.WriteFile(filepath.Join(root, "d", "ok.mkv:Zone.Identifier"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "d", "ok.mkv"), filepath.Join(root, "d", "link.mkv")); err != nil {
		t.Skipf("symlink 不可用: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.mkv"), filepath.Join(root, "d", "escape.mkv")); err != nil {
		t.Fatal(err)
	}
	sc := NewScannerService(root)

	for _, raw := range []string{"d/link.mkv", "d/escape.mkv", "d/ok.mkv:Zone.Identifier", "d/readme.txt"} {
		if _, err := sc.ResolveAssistInputPath(raw, nil); !errors.Is(err, ErrAssistInvalidInput) {
			t.Errorf("%q 应参数错误，got %v", raw, err)
		}
	}
	for _, raw := range []string{"..\\outside", "../outside", "/etc", "C:\\x", "d\\..\\..\\x"} {
		if _, err := sc.ResolveAssistInputPath(raw, nil); !errors.Is(err, ErrInvalidVideoPath) {
			t.Errorf("%q 应路径非法，got %v", raw, err)
		}
	}
	// symlink 文件在目录列举中同样被忽略
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
	got, err := sc.ResolveAssistInputPath("d", nil)
	if err != nil || !reflect.DeepEqual(got.PresentFiles, []string{"ok.mkv"}) {
		t.Fatalf("目录列举应只含 ok.mkv: %#v %v", got, err)
	}
	if _, err := sc.ResolveAssistInputPath("d", []string{"link.mkv"}); !errors.Is(err, ErrAssistInvalidInput) {
		t.Fatalf("symlink 不在 present 中，应参数错误，got %v", err)
	}
	if _, err := sc.ResolveAssistInputPath("nope", nil); err == nil {
		t.Fatal("不存在的路径应报错")
	}
}

// ---- InspectAssistFilesOwnership ----

func createAnimeWithFiles(t *testing.T, title string, bangumiID int, dir string, files ...string) *models.Anime {
	t.Helper()
	a, err := database.CreateAnime(&models.Anime{Title: title, BangumiID: bangumiID, FilePath: dir})
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range files {
		if err := database.CreateEpisode(&models.Episode{AnimeID: a.ID, EpNumber: i + 1, FilePath: f}); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestInspectAssistFilesOwnership(t *testing.T) {
	setupLibraryDB(t)
	sc := NewScannerService(t.TempDir())
	a := createAnimeWithFiles(t, "A", 501, "shared\\dir", "a1.mkv", "a2.mkv")
	b := createAnimeWithFiles(t, "B", 502, "shared/dir", "b1.mkv", "both.mkv")
	if err := database.CreateEpisode(&models.Episode{AnimeID: a.ID, EpNumber: 9, FilePath: "both.mkv"}); err != nil {
		t.Fatal(err)
	}
	_ = b

	t.Run("全归同一番剧", func(t *testing.T) {
		st, err := sc.InspectAssistFilesOwnership("shared/dir", []string{"a2.mkv", "a1.mkv"})
		if err != nil {
			t.Fatal(err)
		}
		if st.OwnedAnime == nil || st.OwnedAnime.ID != a.ID || len(st.Unassociated) != 0 || len(st.ConflictFiles) != 0 {
			t.Fatalf("应全归 A: %#v", st)
		}
		if len(st.OwnedEpisodes) != 2 || st.OwnedEpisodes[0].FilePath != "a1.mkv" {
			t.Fatalf("OwnedEpisodes=%#v", st.OwnedEpisodes)
		}
	})
	t.Run("部分未关联", func(t *testing.T) {
		st, err := sc.InspectAssistFilesOwnership("shared\\dir", []string{"a1.mkv", "new.mkv"})
		if err != nil {
			t.Fatal(err)
		}
		if st.OwnedAnime == nil || st.OwnedAnime.ID != a.ID || !reflect.DeepEqual(st.Unassociated, []string{"new.mkv"}) || len(st.ConflictFiles) != 0 {
			t.Fatalf("应部分归 A: %#v", st)
		}
	})
	t.Run("全未关联", func(t *testing.T) {
		st, err := sc.InspectAssistFilesOwnership("shared/dir", []string{"x.mkv", "y.mkv", "x.mkv"})
		if err != nil {
			t.Fatal(err)
		}
		if st.OwnedAnime != nil || !reflect.DeepEqual(st.Unassociated, []string{"x.mkv", "y.mkv"}) || len(st.ConflictFiles) != 0 {
			t.Fatalf("应全未关联: %#v", st)
		}
	})
	t.Run("分属不同番剧", func(t *testing.T) {
		st, err := sc.InspectAssistFilesOwnership("shared/dir", []string{"a1.mkv", "b1.mkv"})
		if err != nil {
			t.Fatal(err)
		}
		if st.OwnedAnime != nil || len(st.ConflictFiles) != 2 || st.ConflictReason == "" {
			t.Fatalf("应冲突: %#v", st)
		}
	})
	t.Run("多番认领同一文件", func(t *testing.T) {
		st, err := sc.InspectAssistFilesOwnership("shared/dir", []string{"both.mkv", "new.mkv"})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(st.ConflictFiles, []string{"both.mkv"}) || st.ConflictReason == "" || !reflect.DeepEqual(st.Unassociated, []string{"new.mkv"}) {
			t.Fatalf("应报告多番认领冲突: %#v", st)
		}
	})
	t.Run("其他目录不计", func(t *testing.T) {
		st, err := sc.InspectAssistFilesOwnership("other", []string{"a1.mkv"})
		if err != nil {
			t.Fatal(err)
		}
		if st.OwnedAnime != nil || !reflect.DeepEqual(st.Unassociated, []string{"a1.mkv"}) {
			t.Fatalf("其他目录不应命中: %#v", st)
		}
	})
}

// ---- 零写库辅助 ----

func assistDBCounts(t *testing.T) [3]int {
	t.Helper()
	var out [3]int
	for i, table := range []string{"animes", "episodes", "unidentified_files"} {
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// ---- PreviewAssistByBangumiID ----

func TestPreviewAssistByBangumiIDAccepted(t *testing.T) {
	setupLibraryDB(t)
	f := newFakeSessionProvider()
	putTrustedProvider(f, 12345, 12, true)
	withScanSession(t, f)
	sc := newV2Scanner(t, t.TempDir())
	before := assistDBCounts(t)

	res, err := sc.PreviewAssistByBangumiID(context.Background(), "ObscureDir", []string{"CodeX.S01E04.mkv", "CodeX.S01E03.mkv"}, 12345, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Code != matching.Accepted || res.Decision.WinnerID != 12345 || res.Subject == nil || res.Subject.ID != 12345 {
		t.Fatalf("应 Accepted: %#v", res.Decision)
	}
	if res.Candidate == nil || res.Candidate.ID != 12345 || res.Candidate.Score != 1.0 || res.Candidate.EpCount != 12 {
		t.Fatalf("Candidate=%#v", res.Candidate)
	}
	want := []models.AssistMatchBinding{{FileName: "CodeX.S01E03.mkv", EpNumber: 3}, {FileName: "CodeX.S01E04.mkv", EpNumber: 4}}
	if !reflect.DeepEqual(res.Bindings, want) {
		t.Fatalf("Bindings=%#v", res.Bindings)
	}
	if after := assistDBCounts(t); after != before {
		t.Fatalf("预览必须零写库: before=%v after=%v", before, after)
	}
}

func TestPreviewAssistByBangumiIDReviewBranches(t *testing.T) {
	setupLibraryDB(t)
	cases := []struct {
		name   string
		setup  func(f *fakeSessionProvider)
		files  []string
		offset int
		reason string
	}{
		{name: "无集号非电影文件", setup: func(f *fakeSessionProvider) { putTrustedProvider(f, 1, 12, true) },
			files: []string{"CodeX.S01E03.mkv", "CodeX.Extras.mkv"}, reason: "CodeX.Extras.mkv"},
		{name: "越界被拒", setup: func(f *fakeSessionProvider) { putTrustedProvider(f, 1, 12, true) },
			files: []string{"CodeX.S01E03.mkv", "CodeX.S01E99.mkv"}, reason: "CodeX.S01E99.mkv"},
		{name: "条目404", setup: func(f *fakeSessionProvider) { f.detailNotFound = map[int]bool{1: true} },
			files: []string{"CodeX.S01E03.mkv"}, reason: "目标条目不存在或不可访问"},
		{name: "章节404无话数", setup: func(f *fakeSessionProvider) {
			putTrustedProvider(f, 1, 0, false)
			f.chaptersNotFound = map[int]bool{1: true}
		}, files: []string{"CodeX.S01E03.mkv"}, reason: "目标条目无章节且话数未知"},
		{name: "偏移不小于集号", setup: func(f *fakeSessionProvider) { putTrustedProvider(f, 1, 12, true) },
			files: []string{"CodeX.S01E03.mkv", "CodeX.S01E13.mkv"}, offset: 3, reason: "episode_offset 不能大于或等于文件集号"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSessionProvider()
			tc.setup(f)
			withScanSession(t, f)
			sc := newV2Scanner(t, t.TempDir())
			before := assistDBCounts(t)
			res, err := sc.PreviewAssistByBangumiID(context.Background(), "d", tc.files, 1, tc.offset)
			if err != nil {
				t.Fatalf("业务复核不应返回 error: %v", err)
			}
			if res.Decision.Code != matching.Review || !strings.Contains(res.Decision.Reason, tc.reason) {
				t.Fatalf("应 Review 且原因含 %q: %#v", tc.reason, res.Decision)
			}
			if res.Candidate != nil || len(res.Bindings) != 0 {
				t.Fatalf("Review 不应有 Candidate/Bindings: %#v", res)
			}
			if after := assistDBCounts(t); after != before {
				t.Fatalf("预览必须零写库: before=%v after=%v", before, after)
			}
		})
	}
}

func TestPreviewAssistByBangumiIDOffsetAndErrors(t *testing.T) {
	f := newFakeSessionProvider()
	putTrustedProvider(f, 2, 12, true)
	withScanSession(t, f)
	sc := newV2Scanner(t, t.TempDir())
	files := []string{"CodeX.S02E13.mkv", "CodeX.S02E14.mkv"}
	res, err := sc.PreviewAssistByBangumiID(context.Background(), "d", files, 2, 12)
	if err != nil {
		t.Fatal(err)
	}
	want := []models.AssistMatchBinding{{FileName: files[0], EpNumber: 1}, {FileName: files[1], EpNumber: 2}}
	if res.Decision.Code != matching.Accepted || !reflect.DeepEqual(res.Bindings, want) {
		t.Fatalf("偏移后应绑定 1/2: %#v %#v", res.Decision, res.Bindings)
	}

	// 非 404 上游错误原样返回
	upstream := errors.New("限流")
	withScanProvider(t, chaptersErrProvider{fakeSessionProvider: f, err: upstream})
	if _, err := sc.PreviewAssistByBangumiID(context.Background(), "d", files[:1], 2, 0); !errors.Is(err, upstream) {
		t.Fatalf("上游错误应透传，got %v", err)
	}
	if _, err := sc.PreviewAssistByBangumiID(context.Background(), "d", nil, 2, 0); !errors.Is(err, ErrAssistInvalidInput) {
		t.Fatalf("空文件应参数错误，got %v", err)
	}
	if _, err := NewScannerService(t.TempDir()).PreviewAssistByBangumiID(context.Background(), "d", files, 2, 0); err == nil {
		t.Fatal("非 v2 引擎应报错")
	}
}

// ---- ResolveAssistByHint：分组构造 ----

func TestBuildAssistHintGroupBranchANoHint(t *testing.T) {
	g, reason := buildAssistHintGroup(AssistHintInput{
		RelDir:    "d",
		FileNames: []string{"CodeX.S04E13.mkv", "CodeX.S04E14.mkv"},
	})
	if reason != "" || len(g.Files) != 2 || g.Query.Ordinal.Season != 4 || !reflect.DeepEqual(g.Query.Episodes, []int{13, 14}) {
		t.Fatalf("分支 A 基础分组: reason=%q %#v", reason, g.Query)
	}

	_, reason = buildAssistHintGroup(AssistHintInput{RelDir: "d", FileNames: []string{"CodeX.S01E03.mkv", "Other.Show.S01E05.mkv"}})
	if reason != "目标文件属于多个不同标题分组" {
		t.Fatalf("多组应 Review，got %q", reason)
	}

	// 偏移 → 再覆写 SeasonHint（覆写在 Shift 之后，不被重建 Query 冲掉）
	g, reason = buildAssistHintGroup(AssistHintInput{
		RelDir: "d", FileNames: []string{"CodeX.S04E13.mkv", "CodeX.S04E14.mkv"},
		EpisodeOffset: 12, SeasonHint: 5,
	})
	if reason != "" || g.Query.Ordinal.Season != 5 || !reflect.DeepEqual(g.Query.Episodes, []int{1, 2}) {
		t.Fatalf("偏移+季号覆写: reason=%q %#v", reason, g.Query)
	}
	for _, c := range g.Query.Conflicts {
		if strings.Contains(c, "季号") {
			t.Fatalf("SeasonHint 应剔除季号冲突: %v", g.Query.Conflicts)
		}
	}

	_, reason = buildAssistHintGroup(AssistHintInput{RelDir: "d", FileNames: []string{"CodeX.S04E13.mkv", "CodeX.S04E03.mkv"}, EpisodeOffset: 3})
	if reason != "episode_offset 不能大于或等于文件集号" {
		t.Fatalf("偏移不适用应 Review，got %q", reason)
	}
}

func TestBuildAssistHintGroupBranchASeasonHintDropsDirSeasonConflict(t *testing.T) {
	// 目录 S2 与文件 S4 冲突：无 SeasonHint 时保留冲突，有 SeasonHint 时剔除
	in := AssistHintInput{RelDir: "CodeX 第二季", FileNames: []string{"CodeX.S04E03.mkv"}}
	g, _ := buildAssistHintGroup(in)
	hasSeason := false
	for _, c := range g.Query.Conflicts {
		if strings.Contains(c, "季号") {
			hasSeason = true
		}
	}
	if !hasSeason {
		t.Fatalf("目录第二季与文件 S04 应产生季号冲突: %#v", g.Query.Conflicts)
	}
	in.SeasonHint = 5
	g, _ = buildAssistHintGroup(in)
	if g.Query.Ordinal.Season != 5 {
		t.Fatalf("SeasonHint 应覆写: %#v", g.Query)
	}
	for _, c := range g.Query.Conflicts {
		if strings.Contains(c, "季号") {
			t.Fatalf("SeasonHint 应剔除季号冲突: %v", g.Query.Conflicts)
		}
	}
}

func TestBuildAssistHintGroupBranchBHint(t *testing.T) {
	files := []string{"XianWang S04E13.mkv", "The.Daily.Life.of.the.Immortal.King.S04E14.mkv", "CodeX.Extras.mkv"}
	g, reason := buildAssistHintGroup(AssistHintInput{
		RelDir: "XianWangS5", TitleHint: "  仙王的日常生活  ", FileNames: files,
		EpisodeOffset: 12, SeasonHint: 5,
	})
	if reason != "" {
		t.Fatalf("reason=%q", reason)
	}
	if g.Key != "assist:XianWangS5" || g.Dir != "XianWangS5" || len(g.Files) != 3 {
		t.Fatalf("应合成单组且不因标题不同拆组: %#v", g)
	}
	if !reflect.DeepEqual(g.Query.Titles, []matching.Title{{Text: "仙王的日常生活", Source: matching.SourceFile}}) || !g.Query.HasFileTitle {
		t.Fatalf("Query 标题应只取 hint: %#v", g.Query.Titles)
	}
	if !reflect.DeepEqual(g.Query.Episodes, []int{1, 2}) {
		t.Fatalf("Episodes 应为平移后集号: %v", g.Query.Episodes)
	}
	if g.Files[0].Episode.First != 1 || g.Files[1].Episode.Last != 2 || g.Files[2].Episode.Kind != matching.NumberUnknown {
		t.Fatalf("文件集号应原地平移: %#v", g.Files)
	}
	if g.Query.Ordinal.Season != 5 {
		t.Fatalf("SeasonHint 优先: %#v", g.Query.Ordinal)
	}
	for _, c := range g.Query.Conflicts {
		if strings.Contains(c, "季号") {
			t.Fatalf("SeasonHint>0 不应带入文件季号冲突: %v", g.Query.Conflicts)
		}
	}
}

func TestBuildAssistHintGroupBranchBSeasonPriority(t *testing.T) {
	// hint 季号优先于文件
	g, _ := buildAssistHintGroup(AssistHintInput{RelDir: "d", TitleHint: "仙王的日常生活 第五季", FileNames: []string{"XianWang S04E12.mkv"}})
	if g.Query.Ordinal.Season != 5 {
		t.Fatalf("hint 季号应优先: %#v", g.Query.Ordinal)
	}
	// 无 hint 季号：文件一致季号补齐
	g, _ = buildAssistHintGroup(AssistHintInput{RelDir: "d", TitleHint: "仙王的日常生活", FileNames: []string{"XianWang S04E12.mkv", "Other S04E13.mkv"}})
	if g.Query.Ordinal.Season != 4 {
		t.Fatalf("文件一致季号应补齐: %#v", g.Query.Ordinal)
	}
	// 文件季号不一致且无 hint/SeasonHint：组内季号冲突
	g, _ = buildAssistHintGroup(AssistHintInput{RelDir: "d", TitleHint: "仙王的日常生活", FileNames: []string{"XianWang S04E12.mkv", "XianWang S05E01.mkv"}})
	found := false
	for _, c := range g.Query.Conflicts {
		if strings.Contains(c, "组内季号冲突") {
			found = true
		}
	}
	if g.Query.Ordinal.Season != 0 || !found {
		t.Fatalf("应追加组内季号冲突: %#v", g.Query)
	}
	// SeasonHint 覆盖不一致文件季号且不追加冲突
	g, _ = buildAssistHintGroup(AssistHintInput{RelDir: "d", TitleHint: "仙王的日常生活", SeasonHint: 5, FileNames: []string{"XianWang S04E12.mkv", "XianWang S05E01.mkv"}})
	if g.Query.Ordinal.Season != 5 || len(g.Query.Conflicts) != 0 {
		t.Fatalf("SeasonHint 应覆盖且无冲突: %#v", g.Query)
	}
	// Year / Media 优先取 hint
	g, _ = buildAssistHintGroup(AssistHintInput{RelDir: "d", TitleHint: "Fate 剧场版 (2011)", FileNames: []string{"Fate.S01E01.mkv"}})
	if g.Query.Media != matching.KindMovie || g.Query.Year != 2011 {
		t.Fatalf("Year/Media 应取 hint: %#v", g.Query)
	}
	// SeasonHint==0 时文件/目录季号冲突照常带入；SeasonHint>0 时不带入
	g, _ = buildAssistHintGroup(AssistHintInput{RelDir: "CodeX 第二季", TitleHint: "CodeX", FileNames: []string{"CodeX.S04E03.mkv"}})
	if !reflect.DeepEqual(g.Query.Conflicts, []string{"目录季号2与文件季号4冲突"}) {
		t.Fatalf("无 SeasonHint 应保留文件季号冲突: %#v", g.Query.Conflicts)
	}
	g, _ = buildAssistHintGroup(AssistHintInput{RelDir: "CodeX 第二季", TitleHint: "CodeX", SeasonHint: 2, FileNames: []string{"CodeX.S04E03.mkv"}})
	if len(g.Query.Conflicts) != 0 || g.Query.Ordinal.Season != 2 {
		t.Fatalf("SeasonHint>0 不应带入文件季号冲突: %#v", g.Query)
	}
}

func TestBuildAssistHintGroupBranchBOffsetNotApplicable(t *testing.T) {
	_, reason := buildAssistHintGroup(AssistHintInput{RelDir: "d", TitleHint: "X", FileNames: []string{"CodeX.S01E03.mkv"}, EpisodeOffset: 3})
	if reason != "episode_offset 不能大于或等于文件集号" {
		t.Fatalf("got %q", reason)
	}
}

// ---- ResolveAssistByHint：端到端（假 Provider，不联网） ----

func putHintSeries(f *fakeSessionProvider, id int, name string, n int, keys ...string) matching.Subject {
	sub := matching.Subject{ID: id, Name: name, NameCn: name, Media: matching.KindSeries, EpisodeCount: n, Detailed: true}
	f.putSubject(sub, keys...)
	chs := make([]matching.Chapter, 0, n)
	for i := 1; i <= n; i++ {
		chs = append(chs, matching.Chapter{ID: id*100 + i, Ep: float64(i), Sort: float64(i)})
	}
	f.chapters[id] = chs
	return sub
}

func TestResolveAssistByHintAcceptedWithHint(t *testing.T) {
	setupLibraryDB(t)
	f := newFakeSessionProvider()
	putHintSeries(f, 4242, "Hint Target Show", 12, "Hint Target Show")
	withScanSession(t, f)
	sc := newV2Scanner(t, t.TempDir())
	before := assistDBCounts(t)

	files := []string{"Random.Name.S01E02.mkv", "Totally.Else.S01E01.mkv"}
	res, err := sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "d", TitleHint: "Hint Target Show", FileNames: files})
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Code != matching.Accepted || res.Subject == nil || res.Subject.ID != 4242 {
		t.Fatalf("应 Accepted: %#v", res.Decision)
	}
	if res.Candidate == nil || res.Candidate.ID != 4242 || res.Candidate.Score <= 0 {
		t.Fatalf("Candidate=%#v", res.Candidate)
	}
	for _, c := range res.Decision.Candidates {
		if c.Subject.ID == 4242 && c.Score != res.Candidate.Score {
			t.Fatalf("Candidate 分值应取 WinnerID 候选分值: %v vs %v", c.Score, res.Candidate.Score)
		}
	}
	want := []models.AssistMatchBinding{{FileName: files[1], EpNumber: 1}, {FileName: files[0], EpNumber: 2}}
	if !reflect.DeepEqual(res.Bindings, want) {
		t.Fatalf("Bindings 应按集号有序: %#v", res.Bindings)
	}
	if after := assistDBCounts(t); after != before {
		t.Fatalf("ResolveAssistByHint 必须零写库: %v → %v", before, after)
	}
}

func TestResolveAssistByHintAcceptedWithoutHint(t *testing.T) {
	f := newFakeSessionProvider()
	putHintSeries(f, 4343, "Opt Show", 12, "Opt Show")
	withScanSession(t, f)
	sc := newV2Scanner(t, t.TempDir())
	files := []string{"[Group] Opt Show - 13 [1080p].mkv", "[Group] Opt Show - 14 [1080p].mkv"}
	res, err := sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "d", FileNames: files, EpisodeOffset: 12})
	if err != nil {
		t.Fatal(err)
	}
	want := []models.AssistMatchBinding{{FileName: files[0], EpNumber: 1}, {FileName: files[1], EpNumber: 2}}
	if res.Decision.Code != matching.Accepted || !reflect.DeepEqual(res.Bindings, want) {
		t.Fatalf("无 hint 偏移后应 Accepted 1/2: %#v %#v", res.Decision, res.Bindings)
	}
}

func TestResolveAssistByHintAmbiguousMaxThreeCandidates(t *testing.T) {
	setupLibraryDB(t)
	f := newFakeSessionProvider()
	for i := 0; i < 5; i++ {
		putHintSeries(f, 5100+i, "Fate", 12, "Fate")
	}
	withScanSession(t, f)
	sc := newV2Scanner(t, t.TempDir())
	before := assistDBCounts(t)
	res, err := sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "Dir", TitleHint: "Fate", FileNames: []string{"Fate.S01E01.mkv"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Code == matching.Accepted {
		t.Fatalf("同名多候选不应 Accepted: %#v", res.Decision)
	}
	if len(res.Candidates) == 0 || len(res.Candidates) > 3 {
		t.Fatalf("候选应 1..3 个: %#v", res.Candidates)
	}
	if res.Candidate != nil || len(res.Bindings) != 0 {
		t.Fatalf("未接受不应有 Candidate/Bindings: %#v", res)
	}
	if after := assistDBCounts(t); after != before {
		t.Fatalf("必须零写库: %v → %v", before, after)
	}
}

func TestResolveAssistByHintReviewAndValidation(t *testing.T) {
	f := newFakeSessionProvider()
	withScanSession(t, f)
	sc := newV2Scanner(t, t.TempDir())

	res, err := sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "d", FileNames: []string{"CodeX.S01E03.mkv", "Other.Show.S01E05.mkv"}})
	if err != nil || res.Decision.Code != matching.Review || res.Decision.Reason != "目标文件属于多个不同标题分组" {
		t.Fatalf("多组应 Review: %#v %v", res.Decision, err)
	}
	res, err = sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "d", TitleHint: "X", FileNames: []string{"CodeX.S01E03.mkv"}, EpisodeOffset: 5})
	if err != nil || res.Decision.Code != matching.Review || res.Decision.Reason != "episode_offset 不能大于或等于文件集号" {
		t.Fatalf("偏移不适用应 Review: %#v %v", res.Decision, err)
	}
	res, err = sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "d", TitleHint: "Nothing Here", FileNames: []string{"CodeX.S01E03.mkv"}})
	if err != nil || res.Decision.Code == matching.Accepted || len(res.Candidates) != 0 {
		t.Fatalf("无候选应非 Accepted 且候选为空: %#v %v", res, err)
	}
	if _, err := sc.ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "d", TitleHint: "X"}); !errors.Is(err, ErrAssistInvalidInput) {
		t.Fatalf("空 FileNames 应参数错误，got %v", err)
	}
	if _, err := NewScannerService(t.TempDir()).ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "d", FileNames: []string{"a.mkv"}}); err == nil {
		t.Fatal("非 v2 引擎应报错")
	}
	rtLegacy, err := NewMatchingRuntime(nil, matching.EngineLegacy, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewScannerService(t.TempDir(), WithScannerMatching(rtLegacy)).ResolveAssistByHint(context.Background(), AssistHintInput{RelDir: "d", FileNames: []string{"a.mkv"}}); err == nil {
		t.Fatal("legacy 引擎应报错")
	}
}
