package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services/matching"
)

func TestScannerRecognizesSupportedEpisodeNames(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"[Fansub] Title [67].mkv",
		"Title - 02.mp4",
		"Title EP03.avi",
		"Title 第4集.webm",
		"Title S01E05.mov",
		"06.m4v",
		"Title-08v2.mkv",
		"ignored.txt",
		"Title [67].mkv:Zone.Identifier",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 7 {
		t.Fatalf("expected 7 episodes, got %d: %#v", len(episodes), episodes)
	}
	want := []int{2, 3, 4, 5, 6, 8, 67}
	for i, episode := range episodes {
		if episode.EpNumber != want[i] {
			t.Fatalf("episode %d: expected number %d, got %d", i, want[i], episode.EpNumber)
		}
	}
}

func TestScannerRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := NewScannerService(root).Scan("../outside")
	if !errors.Is(err, ErrInvalidVideoPath) {
		t.Fatalf("expected ErrInvalidVideoPath, got %v", err)
	}
}

func TestScannerMovieBecomesEpisodeOne(t *testing.T) {
	root := t.TempDir()
	name := "[Subs]某作品 剧场版 [1080p].mkv"
	if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 {
		t.Fatalf("expected EpNumber 1, got %d", episodes[0].EpNumber)
	}
	if episodes[0].FilePath != name {
		t.Fatalf("expected FilePath %q, got %q", name, episodes[0].FilePath)
	}
}

func TestScannerKaguyaYearOnlyReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	name := "[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4"
	if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 0 {
		t.Fatalf("expected 0 episodes for Kaguya-only folder, got %d: %#v", len(episodes), episodes)
	}
}

func TestScannerSkipsVersionOnlyFilename(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "v2.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 0 {
		t.Fatalf("expected 0 episodes for v2.mkv, got %d: %#v", len(episodes), episodes)
	}
}

func TestScannerRealEpisodeOneBeatsMovie(t *testing.T) {
	root := t.TempDir()
	real := "[TSDM][Cosmic Princess Kaguya][01][1080p].mkv"
	movie := "[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4"
	for _, name := range []string{movie, real} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected only real ep1, got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 {
		t.Fatalf("expected EpNumber 1, got %d", episodes[0].EpNumber)
	}
	if episodes[0].FilePath != real {
		t.Fatalf("expected real ep file %q, got %q", real, episodes[0].FilePath)
	}
}

func TestScannerTwoMoviesKeepsFirstByFilename(t *testing.T) {
	root := t.TempDir()
	first := "[Fansub][Alpha Title] 剧场版 [1080p].mkv"
	later := "[Fansub][Zeta Title] 剧场版 [1080p].mkv"
	for _, name := range []string{later, first} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode (second movie silent drop), got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 || episodes[0].FilePath != first {
		t.Fatalf("expected first-by-filename movie as ep1, got %#v", episodes[0])
	}
}

// TestScannerConcurrentSetRootPathAndReads 验证 rootPath 的无锁读写竞态修复：
// 并发 SetRootPath 与 RootPath/ListSubDirs（Dirs 请求路径）交错时不允许
// data race 或读到撕裂的中间值（go test -race 下验证）。
func TestScannerConcurrentSetRootPathAndReads(t *testing.T) {
	base := t.TempDir()
	roots := []string{base, filepath.Join(base, "a"), filepath.Join(base, "b")}
	for _, root := range roots {
		if err := os.MkdirAll(filepath.Join(root, "show"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scanner := NewScannerService(roots[0])

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			scanner.SetRootPath(roots[i%len(roots)])
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			// 读路径可能被 SetRootPath 交错；必须总能看到某个完整 root（不允许
			// 无同步读写 string 引发的撕裂/竞态，ListSubDirs 内部做 Abs+EvalSymlinks）。
			_ = scanner.RootPath()
			dirs, err := scanner.ListSubDirs()
			if err != nil {
				// 根目录被 SetRootPath 切走时目录可能不存在，允许 ErrInvalid/不存在。
				if _, statErr := os.Stat(scanner.RootPath()); statErr != nil {
					continue
				}
				t.Errorf("ListSubDirs unexpected error: %v", err)
				return
			}
			_ = dirs
		}
	}()
	// 跑一小段时间后停止。
	time.Sleep(100 * time.Millisecond)
	close(done)
	wg.Wait()
}

func TestValidateRelativeVideoPathSeparatorsAndTraversal(t *testing.T) {
	bad := []string{
		"..",
		"../outside",
		"..\\outside",
		"a\\..\\..\\outside",
		"a/../b",
		"a\\..",
		"/abs",
		"\\abs",
		"C:\\Windows",
		"c:/videos",
		"Z:",
		"dir\x00name",
	}
	for _, p := range bad {
		if err := ValidateRelativeVideoPath(p); !errors.Is(err, ErrInvalidVideoPath) {
			t.Errorf("ValidateRelativeVideoPath(%q) 应拒绝，got %v", p, err)
		}
	}
	good := []string{"", ".", "show", "a/b", "a\\b", "a\\b/", "foo..bar", "foo..bar/baz..", "第一季/S01", "1:2"}
	for _, p := range good {
		if err := ValidateRelativeVideoPath(p); err != nil {
			t.Errorf("ValidateRelativeVideoPath(%q) 应允许，got %v", p, err)
		}
	}
}

func TestNormalizeRelDir(t *testing.T) {
	cases := map[string]string{
		"":           "",
		".":          "",
		"  ":         "",
		"/":          "",
		"a/b":        "a/b",
		"a\\b/":      "a/b",
		"a\\b\\":     "a/b",
		" a//b/./c ": "a/b/c",
		"./show/":    "show",
		"show":       "show",
	}
	for in, want := range cases {
		if got := NormalizeRelDir(in); got != want {
			t.Errorf("NormalizeRelDir(%q)=%q want %q", in, got, want)
		}
	}
	if NormalizeRelDir("a\\b/") != NormalizeRelDir("a/b") {
		t.Fatal("a\\b/ 与 a/b 应归一为同一目录")
	}
}

func TestResolveDirectoryAcceptsMixedSeparators(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	sc := NewScannerService(root)
	want, err := sc.resolveDirectory("a/b")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a\\b", "a\\b/", "a/b/", " a/b "} {
		got, err := sc.resolveDirectory(p)
		if err != nil {
			t.Fatalf("resolveDirectory(%q): %v", p, err)
		}
		if got != want {
			t.Fatalf("resolveDirectory(%q)=%q want %q", p, got, want)
		}
	}
	if _, err := sc.resolveDirectory("..\\outside"); !errors.Is(err, ErrInvalidVideoPath) {
		t.Fatalf("..\\outside 应被拒绝，got %v", err)
	}
}

// ---- WithTrustedTarget ----

// trustedSubject 名称与文件名完全无关，验证受信任模式不依赖名称门闩。
func putTrustedProvider(f *fakeSessionProvider, id, n int, withChapters bool) matching.Subject {
	sub := matching.Subject{
		ID: id, Name: "完全无关的日文名", NameCn: "完全无关的中文名",
		Media: matching.KindSeries, EpisodeCount: n, Detailed: true,
	}
	f.putSubject(sub)
	if withChapters {
		chs := make([]matching.Chapter, 0, n)
		for i := 1; i <= n; i++ {
			chs = append(chs, matching.Chapter{ID: id*100 + i, Ep: float64(i), Sort: float64(i)})
		}
		f.chapters[id] = chs
	}
	return sub
}

func trustedAnime(id int, epCount int, dir string) models.Anime {
	return models.Anime{ID: 9, Title: "完全无关的日文名", TitleCn: "完全无关的中文名", BangumiID: id, EpCount: epCount, FilePath: dir}
}

// chaptersErrProvider 章节接口返回非 404 上游错误。
type chaptersErrProvider struct {
	*fakeSessionProvider
	err error
}

func (p chaptersErrProvider) Chapters(context.Context, int) ([]matching.Chapter, error) {
	return nil, p.err
}

func withScanProvider(t *testing.T, p matchingProvider) {
	t.Helper()
	prev := scanSessionHook
	scanSessionHook = func(ctx context.Context, rt *MatchingRuntime) *MatchingSession {
		return newMatchingSessionForTest(ctx, rt, p)
	}
	t.Cleanup(func() { scanSessionHook = prev })
}

func TestScanForAnimeTrustedTargetBindsDespiteNameMismatch(t *testing.T) {
	root := t.TempDir()
	dir := "ObscureDir"
	writeScanVideos(t, filepath.Join(root, dir), "CodeX.S01E03.mkv", "CodeX.S01E04.mkv")
	f := newFakeSessionProvider()
	f.exactOnly = true
	putTrustedProvider(f, 12345, 12, true)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := trustedAnime(12345, 12, dir)

	// 非受信任：名称不符，不得自动绑定
	if inv, err := sc.ScanForAnime(anime, nil, nil); err == nil && len(inv.Desired) > 0 {
		t.Fatalf("非受信任模式不应绑定名称无关的文件: %#v", inv.Desired)
	}

	inv, err := sc.ScanForAnime(anime, nil, nil, WithTrustedTarget())
	if err != nil {
		t.Fatal(err)
	}
	got := desiredByPath(inv.Desired)
	if len(inv.Desired) != 2 || got["CodeX.S01E03.mkv"].EpNumber != 3 || got["CodeX.S01E04.mkv"].EpNumber != 4 {
		t.Fatalf("受信任模式应按章节绑定 3/4: %#v", inv.Desired)
	}
	for _, ep := range inv.Desired {
		if ep.ID != 0 || ep.AnimeID != anime.ID {
			t.Fatalf("新剧集应 ID=0 且 AnimeID=目标: %#v", ep)
		}
	}
}

func TestScanForAnimeTrustedTargetNumberUnknownNotSilentlyDropped(t *testing.T) {
	root := t.TempDir()
	dir := "trusted-unknown"
	writeScanVideos(t, filepath.Join(root, dir), "CodeX.S01E03.mkv", "CodeX.Extras.mkv")
	f := newFakeSessionProvider()
	putTrustedProvider(f, 12346, 12, true)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	inv, err := sc.ScanForAnime(trustedAnime(12346, 12, dir), nil, nil, WithTrustedTarget())
	if !errors.Is(err, ErrMatchReviewRequired) {
		t.Fatalf("无集号非电影文件应触发 Review，got err=%v inv=%#v", err, inv)
	}
	if !strings.Contains(err.Error(), "CodeX.Extras.mkv") {
		t.Fatalf("原因应列出未绑定文件: %v", err)
	}
	if len(inv.Desired) != 0 {
		t.Fatalf("出错应零 Desired: %#v", inv.Desired)
	}

	// 只限定有集号文件时可正常入库
	inv, err = sc.ScanForAnime(trustedAnime(12346, 12, dir), nil, nil, WithTrustedTarget(), WithOnlyFiles([]string{"CodeX.S01E03.mkv"}))
	if err != nil || len(inv.Desired) != 1 || inv.Desired[0].EpNumber != 3 {
		t.Fatalf("onlyFiles 限定后应绑定 ep3: err=%v %#v", err, inv.Desired)
	}
}

func TestScanForAnimeTrustedTargetRejectedAndConflict(t *testing.T) {
	cases := []struct {
		name  string
		files []string
	}{
		{name: "越界被拒", files: []string{"CodeX.S01E03.mkv", "CodeX.S01E99.mkv"}},
		{name: "全部越界冲突", files: []string{"CodeX.S01E98.mkv", "CodeX.S01E99.mkv"}},
		{name: "重复集号", files: []string{"CodeX.S01E03.mkv", "CodeX.S01E03.v2.mkv"}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := "trusted-reject"
			writeScanVideos(t, filepath.Join(root, dir), tc.files...)
			id := 12400 + i
			f := newFakeSessionProvider()
			putTrustedProvider(f, id, 12, true)
			withScanSession(t, f)
			withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

			sc := newV2Scanner(t, root)
			inv, err := sc.ScanForAnime(trustedAnime(id, 12, dir), nil, nil, WithTrustedTarget())
			if !errors.Is(err, ErrMatchReviewRequired) {
				t.Fatalf("应 Review，got err=%v inv=%#v", err, inv)
			}
			if len(inv.Desired) != 0 {
				t.Fatalf("出错应零 Desired: %#v", inv.Desired)
			}
			t.Logf("reason: %v", err)
		})
	}
}

func TestScanForAnimeTrustedTargetChapterFallbacks(t *testing.T) {
	file := "CodeX.S01E03.mkv"
	t.Run("章节404有话数按话数拟合", func(t *testing.T) {
		root := t.TempDir()
		writeScanVideos(t, filepath.Join(root, "d"), file)
		f := newFakeSessionProvider()
		putTrustedProvider(f, 12501, 12, false)
		f.chaptersNotFound = map[int]bool{12501: true}
		withScanSession(t, f)
		withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
		inv, err := newV2Scanner(t, root).ScanForAnime(trustedAnime(12501, 12, "d"), nil, nil, WithTrustedTarget())
		if err != nil || len(inv.Desired) != 1 || inv.Desired[0].EpNumber != 3 {
			t.Fatalf("应按话数拟合 ep3: err=%v %#v", err, inv.Desired)
		}
	})
	t.Run("章节404无话数Review", func(t *testing.T) {
		root := t.TempDir()
		writeScanVideos(t, filepath.Join(root, "d"), file)
		f := newFakeSessionProvider()
		putTrustedProvider(f, 12502, 0, false)
		f.chaptersNotFound = map[int]bool{12502: true}
		withScanSession(t, f)
		withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
		_, err := newV2Scanner(t, root).ScanForAnime(trustedAnime(12502, 0, "d"), nil, nil, WithTrustedTarget())
		if !errors.Is(err, ErrMatchReviewRequired) || !strings.Contains(err.Error(), "目标条目无章节且话数未知") {
			t.Fatalf("应 Review 无章节且话数未知，got %v", err)
		}
	})
	t.Run("条目404Review", func(t *testing.T) {
		root := t.TempDir()
		writeScanVideos(t, filepath.Join(root, "d"), file)
		f := newFakeSessionProvider()
		f.detailNotFound = map[int]bool{12503: true}
		withScanSession(t, f)
		withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
		_, err := newV2Scanner(t, root).ScanForAnime(trustedAnime(12503, 12, "d"), nil, nil, WithTrustedTarget())
		if !errors.Is(err, ErrMatchReviewRequired) || !strings.Contains(err.Error(), "目标条目不存在或不可访问") {
			t.Fatalf("应 Review 目标条目不存在，got %v", err)
		}
	})
	t.Run("详情非404错误透传", func(t *testing.T) {
		root := t.TempDir()
		writeScanVideos(t, filepath.Join(root, "d"), file)
		f := newFakeSessionProvider() // 未注册详情 → ErrMatchUpstream
		withScanSession(t, f)
		withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
		_, err := newV2Scanner(t, root).ScanForAnime(trustedAnime(12504, 12, "d"), nil, nil, WithTrustedTarget())
		if err == nil || errors.Is(err, ErrMatchReviewRequired) || !errors.Is(err, ErrMatchUpstream) {
			t.Fatalf("非 404 详情错误应原样返回，got %v", err)
		}
	})
	t.Run("章节非404错误透传", func(t *testing.T) {
		root := t.TempDir()
		writeScanVideos(t, filepath.Join(root, "d"), file)
		f := newFakeSessionProvider()
		putTrustedProvider(f, 12505, 12, true)
		upstream := fmt.Errorf("限流: %w", ErrMatchUpstream)
		withScanProvider(t, chaptersErrProvider{fakeSessionProvider: f, err: upstream})
		withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
		_, err := newV2Scanner(t, root).ScanForAnime(trustedAnime(12505, 12, "d"), nil, nil, WithTrustedTarget())
		if !errors.Is(err, upstream) || errors.Is(err, ErrMatchReviewRequired) {
			t.Fatalf("非 404 章节错误应原样返回，got %v", err)
		}
	})
}

func TestScanForAnimeTrustedTargetEpisodeOffset(t *testing.T) {
	root := t.TempDir()
	dir := "trusted-offset"
	f13 := "CodeX.S02E13.mkv"
	f14 := "CodeX.S02E14.mkv"
	writeScanVideos(t, filepath.Join(root, dir), f13, f14)
	f := newFakeSessionProvider()
	putTrustedProvider(f, 12601, 12, true)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
	sc := newV2Scanner(t, root)

	inv, err := sc.ScanForAnime(trustedAnime(12601, 12, dir), nil, nil, WithTrustedTarget(), WithEpisodeOffset(12))
	if err != nil {
		t.Fatal(err)
	}
	got := desiredByPath(inv.Desired)
	if len(inv.Desired) != 2 || got[f13].EpNumber != 1 || got[f14].EpNumber != 2 {
		t.Fatalf("偏移 12 后应为 1/2: %#v", inv.Desired)
	}

	// 偏移不适用（集号 <= 话数）
	if _, err := sc.ScanForAnime(trustedAnime(12601, 14, dir), nil, nil, WithTrustedTarget(), WithEpisodeOffset(12)); !errors.Is(err, ErrMatchReviewRequired) ||
		!strings.Contains(err.Error(), errEpisodeOffsetNotApplicable) {
		t.Fatalf("偏移不适用应 Review，got %v", err)
	}
	// 话数未知时偏移不适用
	if _, err := sc.ScanForAnime(trustedAnime(12601, 0, dir), nil, nil, WithTrustedTarget(), WithEpisodeOffset(12)); !errors.Is(err, ErrMatchReviewRequired) {
		t.Fatalf("EpCount==0 偏移应 Review，got %v", err)
	}
}

func TestScanForAnimeTrustedTargetKeepsCrossAnimeAndExisting(t *testing.T) {
	root := t.TempDir()
	dir := "trusted-shared"
	owned := "CodeX.S01E01.mkv"
	other := "CodeX.S01E02.mkv"
	fresh := "CodeX.S01E03.mkv"
	writeScanVideos(t, filepath.Join(root, dir), owned, other, fresh)
	f := newFakeSessionProvider()
	putTrustedProvider(f, 12701, 12, true)
	withScanSession(t, f)
	// other 已归属同目录其他番剧：受信任模式也不得抢占
	withFileAssociated(t, func(name, _ string) (bool, error) { return name == other, nil })

	sc := newV2Scanner(t, root)
	anime := trustedAnime(12701, 12, dir)
	existing := []models.Episode{{ID: 71, AnimeID: anime.ID, EpNumber: 1, FilePath: owned, Duration: 1200}}
	inv, err := sc.ScanForAnime(anime, existing, nil, WithTrustedTarget())
	if err != nil {
		t.Fatal(err)
	}
	got := desiredByPath(inv.Desired)
	if ep, ok := got[owned]; !ok || ep.ID != 71 || ep.Duration != 1200 {
		t.Fatalf("既有剧集应原样保留: %#v", inv.Desired)
	}
	if _, ok := got[other]; ok {
		t.Fatalf("已归属他番的文件不得被受信任模式抢占: %#v", inv.Desired)
	}
	if got[fresh].EpNumber != 3 || len(inv.Desired) != 2 {
		t.Fatalf("未关联文件应绑定 ep3: %#v", inv.Desired)
	}
	if len(inv.PresentFiles) != 3 {
		t.Fatalf("PresentFiles 应含全部磁盘文件: %v", inv.PresentFiles)
	}
}

func TestScanForAnimeTrustedTargetDuplicateWithExistingReview(t *testing.T) {
	root := t.TempDir()
	dir := "trusted-dup"
	old := "CodeX.S01E03.mkv"
	neu := "Other.Name.03.mkv"
	writeScanVideos(t, filepath.Join(root, dir), old, neu)
	f := newFakeSessionProvider()
	putTrustedProvider(f, 12801, 12, true)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := trustedAnime(12801, 12, dir)
	existing := []models.Episode{{ID: 81, AnimeID: anime.ID, EpNumber: 3, FilePath: old}}
	inv, err := sc.ScanForAnime(anime, existing, nil, WithTrustedTarget())
	if !errors.Is(err, ErrMatchReviewRequired) || !strings.Contains(err.Error(), "重复集号") {
		t.Fatalf("与既有剧集同号应经 validateDesiredUnique 拒绝，got err=%v inv=%#v", err, inv)
	}
}

// library.go 目录比较统一经 NormalizeRelDir：历史 Windows 数据 "sub\\show" 与扫描目录 "sub/show" 视为同一目录。
func TestLibraryMatchV2BackslashStoredDirIsSameDirectory(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, filepath.Join("sub", "show", "Slash Show - 02.mkv"))

	anime, err := database.CreateAnime(&models.Anime{Title: "Slash Show", BangumiID: 8200, EpCount: 12, FilePath: "sub\\show"})
	if err != nil {
		t.Fatal(err)
	}
	fake := newFakeSessionProvider()
	putSeries(fake, matching.Subject{ID: 8200, Name: "Slash Show", NameCn: "斜杠秀", EpisodeCount: 12}, "Slash Show")

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range res.Unidentified {
		if strings.Contains(u.Reason, "目录不同") {
			t.Fatalf("分隔符差异不应判为目录不同: %#v", res.Unidentified)
		}
	}
	if res.NewAnimes != 0 {
		t.Fatalf("不应新建番剧: %#v", res)
	}
	eps, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].EpNumber != 2 {
		t.Fatalf("应挂到已有番剧: %#v", eps)
	}
}
