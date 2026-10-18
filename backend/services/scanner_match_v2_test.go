package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fan-web/models"
	"fan-web/services/matching"
)

func writeScanVideos(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func withScanSession(t *testing.T, f *fakeSessionProvider) {
	t.Helper()
	prev := scanSessionHook
	scanSessionHook = func(ctx context.Context, rt *MatchingRuntime) *MatchingSession {
		return newMatchingSessionForTest(ctx, rt, f)
	}
	t.Cleanup(func() { scanSessionHook = prev })
}

func withFileAssociated(t *testing.T, fn func(fileName, dirPath string) (bool, error)) {
	t.Helper()
	prev := fileIsAssociated
	fileIsAssociated = fn
	t.Cleanup(func() { fileIsAssociated = prev })
}

func newV2Scanner(t *testing.T, root string) *ScannerService {
	t.Helper()
	rt, err := NewMatchingRuntime(nil, matching.EngineV2, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	return NewScannerService(root, WithScannerMatching(rt))
}

func ranmaSubject() matching.Subject {
	return matching.Subject{
		ID: 425018, Name: "らんま1/2 (2024)", NameCn: "乱马1/2 (2024)",
		Year: 2024, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		Aliases: []string{"Ranma 1/2", "Ranma1-2"},
	}
}

func putRanmaProvider(f *fakeSessionProvider) {
	sub := ranmaSubject()
	f.putSubject(sub, "らんま1/2", "乱马1/2", "Ranma1-2", "Ranma 1/2")
	chs := make([]matching.Chapter, 0, 12)
	for i := 1; i <= 12; i++ {
		chs = append(chs, matching.Chapter{ID: 42501800 + i, Ep: float64(i), Sort: float64(i)})
	}
	f.chapters[sub.ID] = chs
}

func TestScannerScanForAnimePureDirectoryKeepsExisting(t *testing.T) {
	root := t.TempDir()
	dir := "纯目录番"
	writeScanVideos(t, filepath.Join(root, dir), "01.mkv", "02.mkv")

	f := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 7001, Name: "纯目录番", NameCn: "纯目录番",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(sub, "纯目录番")
	chs := make([]matching.Chapter, 0, 12)
	for i := 1; i <= 12; i++ {
		chs = append(chs, matching.Chapter{ID: 700100 + i, Ep: float64(i), Sort: float64(i)})
	}
	f.chapters[7001] = chs
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 1, Title: "纯目录番", TitleCn: "纯目录番", BangumiID: 7001, EpCount: 12, FilePath: dir}
	existing := []models.Episode{
		{ID: 10, AnimeID: 1, EpNumber: 1, FilePath: "01.mkv"},
		{ID: 11, AnimeID: 1, EpNumber: 2, FilePath: "02.mkv"},
	}
	inv, err := sc.ScanForAnime(anime, existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.PresentFiles) != 2 {
		t.Fatalf("PresentFiles=%v", inv.PresentFiles)
	}
	if len(inv.Desired) != 2 {
		t.Fatalf("Desired=%#v", inv.Desired)
	}
	for _, ep := range inv.Desired {
		if ep.ID == 0 {
			t.Fatalf("现存文件应保留原 ID: %#v", ep)
		}
	}
}

func TestScannerScanForAnimeMixedPeersFilter(t *testing.T) {
	root := t.TempDir()
	dir := "混目录"
	tv1 := "Show TV - 01.mkv"
	movie := "[Subs]某作品 剧场版 [1080p].mkv"
	writeScanVideos(t, filepath.Join(root, dir), tv1, movie)

	f := newFakeSessionProvider()
	tv := matching.Subject{
		ID: 8001, Name: "Show TV", NameCn: "Show TV",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	mov := matching.Subject{
		ID: 8002, Name: "某作品 剧场版", NameCn: "某作品 剧场版",
		Media: matching.KindMovie, EpisodeCount: 1, Detailed: true,
	}
	f.putSubject(tv, "Show TV", "Show")
	f.putSubject(mov, "某作品", "剧场版")
	f.chapters[8001] = []matching.Chapter{{ID: 1, Ep: 1, Sort: 1}}
	f.chapters[8002] = []matching.Chapter{{ID: 2, Ep: 1, Sort: 1}}
	withScanSession(t, f)
	withFileAssociated(t, func(name, _ string) (bool, error) {
		return name == movie, nil // 电影已关联到 peer
	})

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 2, Title: "Show TV", BangumiID: 8001, EpCount: 12, FilePath: dir}
	peer := models.Anime{ID: 3, Title: "某作品 剧场版", BangumiID: 8002, EpCount: 1, FilePath: dir}
	existing := []models.Episode{{ID: 20, AnimeID: 2, EpNumber: 1, FilePath: tv1}}

	inv, err := sc.ScanForAnime(anime, existing, []models.Anime{peer})
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range inv.Desired {
		if ep.FilePath == movie {
			t.Fatalf("不得把 peer 电影写入 TV Desired: %#v", inv.Desired)
		}
	}
	if len(inv.PresentFiles) != 2 {
		t.Fatalf("PresentFiles 应含全部视频，got %v", inv.PresentFiles)
	}
	if len(inv.Desired) != 1 || inv.Desired[0].FilePath != tv1 {
		t.Fatalf("Desired=%#v", inv.Desired)
	}
}

func TestScannerScanForAnimeUnknownNewFileRejected(t *testing.T) {
	root := t.TempDir()
	dir := "target-show"
	known := "Target Show - 01.mkv"
	unknown := "Totally Unrelated Noise - 05.mkv"
	writeScanVideos(t, filepath.Join(root, dir), known, unknown)

	f := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 9001, Name: "Target Show", NameCn: "Target Show",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(sub, "Target Show")
	f.chapters[9001] = []matching.Chapter{{ID: 1, Ep: 1, Sort: 1}, {ID: 2, Ep: 2, Sort: 2}}
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 4, Title: "Target Show", BangumiID: 9001, EpCount: 12, FilePath: dir}
	existing := []models.Episode{{ID: 40, AnimeID: 4, EpNumber: 1, FilePath: known}}

	inv, err := sc.ScanForAnime(anime, existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range inv.Desired {
		if ep.FilePath == unknown {
			t.Fatalf("低身份未知文件不得吞入 Desired: %#v", inv.Desired)
		}
	}
	found := false
	for _, n := range inv.PresentFiles {
		if n == unknown {
			found = true
		}
	}
	if !found {
		t.Fatal("未知文件仍应出现在 PresentFiles")
	}
}

func TestScannerScanForAnimeRenameProof(t *testing.T) {
	root := t.TempDir()
	dir := "rename-show"
	oldName := "Show - 01 [OLD].mkv"
	newName := "Show - 01 [NEW].mkv"
	writeScanVideos(t, filepath.Join(root, dir), newName) // 旧名已不在磁盘

	f := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 9101, Name: "Show", NameCn: "Show",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(sub, "Show")
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 5, Title: "Show", BangumiID: 9101, EpCount: 12, FilePath: dir}
	existing := []models.Episode{{ID: 50, AnimeID: 5, EpNumber: 1, Title: "keep", FilePath: oldName, Duration: 1400}}

	inv, err := sc.ScanForAnime(anime, existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Renames) != 1 {
		t.Fatalf("Renames=%#v", inv.Renames)
	}
	r := inv.Renames[0]
	if r.EpisodeID != 50 || r.From != oldName || r.To != newName {
		t.Fatalf("rename=%#v", r)
	}
	if len(inv.Desired) != 1 || inv.Desired[0].ID != 50 || inv.Desired[0].FilePath != newName {
		t.Fatalf("Desired=%#v", inv.Desired)
	}
	if inv.Desired[0].Duration != 1400 {
		t.Fatalf("应保留 duration: %#v", inv.Desired[0])
	}
}

func TestScannerScanForAnimeRanmaRenumber(t *testing.T) {
	root := t.TempDir()
	dir := "ranma"
	// 注：S1 对 "Ranma1-2 (2024) - 01.mkv" 会把 01 吞进标题（1-2 干扰）；
	// 方括号集号形式可正确解析，仍覆盖「同文件更正编号 + 新文件入 Desired」。
	f01 := "Ranma1-2 (2024) [01].mkv"
	f02 := "Ranma1-2 (2024) [02].mkv"
	writeScanVideos(t, filepath.Join(root, dir), f01, f02)

	f := newFakeSessionProvider()
	putRanmaProvider(f)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{
		ID: 6, Title: "らんま1/2 (2024)", TitleCn: "乱马1/2 (2024)",
		BangumiID: 425018, EpCount: 12, FilePath: dir,
	}
	// 旧错误编号：文件 01 被标成 ep=2
	existing := []models.Episode{
		{ID: 60, AnimeID: 6, EpNumber: 2, Title: "旧", FilePath: f01, Duration: 1300},
	}

	inv, err := sc.ScanForAnime(anime, existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	var a, b *models.Episode
	for i := range inv.Desired {
		ep := &inv.Desired[i]
		switch ep.FilePath {
		case f01:
			a = ep
		case f02:
			b = ep
		}
	}
	if a == nil || a.ID != 60 || a.EpNumber != 1 {
		t.Fatalf("01 应保留 ID=60 且更正为 ep=1, got %#v", a)
	}
	if b == nil || b.ID != 0 || b.EpNumber != 2 {
		t.Fatalf("02 应为新行 ep=2, got %#v", b)
	}
	// 幂等：再扫一次
	inv2, err := sc.ScanForAnime(anime, inv.Desired, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv2.Desired) != 2 {
		t.Fatalf("幂等 Desired=%#v", inv2.Desired)
	}
}

func TestScannerScanForAnimeDuplicateNumberReview(t *testing.T) {
	root := t.TempDir()
	dir := "dup"
	a := "Dup Show - 01 [A].mkv"
	b := "Dup Show - 01 [B].mkv"
	writeScanVideos(t, filepath.Join(root, dir), a, b)

	f := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 9201, Name: "Dup Show", NameCn: "Dup Show",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(sub, "Dup Show")
	f.chapters[9201] = []matching.Chapter{{ID: 1, Ep: 1, Sort: 1}}
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 7, Title: "Dup Show", BangumiID: 9201, EpCount: 12, FilePath: dir}

	_, err := sc.ScanForAnime(anime, nil, nil)
	if !errors.Is(err, ErrMatchReviewRequired) {
		t.Fatalf("重复集号应 ReviewRequired, got %v", err)
	}
}

func TestScannerScanForAnimeBrandNewEmptyReview(t *testing.T) {
	root := t.TempDir()
	dir := "empty-new"
	writeScanVideos(t, filepath.Join(root, dir), "Unrelated ZZ - 01.mkv")

	f := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 9301, Name: "Brand New Show", NameCn: "Brand New Show",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(sub, "Brand New Show")
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 8, Title: "Brand New Show", BangumiID: 9301, EpCount: 12, FilePath: dir}
	_, err := sc.ScanForAnime(anime, nil, nil)
	if !errors.Is(err, ErrMatchReviewRequired) {
		t.Fatalf("全新目标无文件应 ReviewRequired, got %v", err)
	}
}

func TestScannerLegacyScanUnchanged(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"[Fansub] Title [67].mkv",
		"Title - 02.mp4",
		"ignored.txt",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 无 Runtime 的 legacy 构造
	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 {
		t.Fatalf("legacy Scan 语义被破坏: %#v", episodes)
	}
	// v2 Runtime 注入后旧 Scan 仍走旧路径
	sc := newV2Scanner(t, root)
	episodes2, err := sc.Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes2) != 2 {
		t.Fatalf("注入 Runtime 后 Scan legacy 语义被破坏: %#v", episodes2)
	}
	if sc.MatchEngine() != matching.EngineV2 {
		t.Fatalf("MatchEngine=%s", sc.MatchEngine())
	}
}

func TestScannerScanForAnimeOtherSubjectIgnored(t *testing.T) {
	root := t.TempDir()
	dir := "peer-dir"
	mine := "My Show - 01.mkv"
	other := "Other Show - 03.mkv"
	writeScanVideos(t, filepath.Join(root, dir), mine, other)

	f := newFakeSessionProvider()
	my := matching.Subject{
		ID: 9401, Name: "My Show", NameCn: "My Show",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	ot := matching.Subject{
		ID: 9402, Name: "Other Show", NameCn: "Other Show",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(my, "My Show")
	f.putSubject(ot, "Other Show")
	f.chapters[9401] = []matching.Chapter{{ID: 1, Ep: 1, Sort: 1}}
	f.chapters[9402] = []matching.Chapter{{ID: 3, Ep: 3, Sort: 3}}
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 9, Title: "My Show", BangumiID: 9401, EpCount: 12, FilePath: dir}
	inv, err := sc.ScanForAnime(anime, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range inv.Desired {
		if strings.Contains(ep.FilePath, "Other") {
			t.Fatalf("不得吞入其他 subject 文件: %#v", inv.Desired)
		}
	}
	if len(inv.Desired) != 1 || inv.Desired[0].FilePath != mine {
		t.Fatalf("Desired=%#v", inv.Desired)
	}
}

func TestScannerScanForAnimeRenameNumberChangeNotInferred(t *testing.T) {
	root := t.TempDir()
	dir := "no-infer"
	// 旧 01 消失，磁盘上是 02——不得推断为 rename
	writeScanVideos(t, filepath.Join(root, dir), "Show - 02.mkv")

	f := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 9501, Name: "Show", NameCn: "Show",
		Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(sub, "Show")
	f.chapters[9501] = []matching.Chapter{
		{ID: 1, Ep: 1, Sort: 1}, {ID: 2, Ep: 2, Sort: 2},
	}
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 10, Title: "Show", BangumiID: 9501, EpCount: 12, FilePath: dir}
	existing := []models.Episode{{ID: 100, AnimeID: 10, EpNumber: 1, FilePath: "Show - 01.mkv"}}

	inv, err := sc.ScanForAnime(anime, existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Renames) != 0 {
		t.Fatalf("改名又改号不得推断 rename: %#v", inv.Renames)
	}
	// 旧行不在 Desired（文件已消失且无 rename）；新 02 可为新行
	for _, ep := range inv.Desired {
		if ep.ID == 100 {
			t.Fatalf("缺失旧行无 rename 时不得保留进 Desired: %#v", inv.Desired)
		}
	}
}

func TestScannerScanForAnimePresentFilesExactNames(t *testing.T) {
	root := t.TempDir()
	dir := "exact"
	// 大小写不同视为不同文件名（身份精确）
	writeScanVideos(t, filepath.Join(root, dir), "Show - 01.mkv", "notes.txt")
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })
	f := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 9601, Name: "Show", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(sub, "Show")
	f.chapters[9601] = []matching.Chapter{{ID: 1, Ep: 1, Sort: 1}}
	withScanSession(t, f)

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 11, Title: "Show", BangumiID: 9601, EpCount: 12, FilePath: dir}
	inv, err := sc.ScanForAnime(anime, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.PresentFiles) != 1 || inv.PresentFiles[0] != "Show - 01.mkv" {
		t.Fatalf("PresentFiles=%v", inv.PresentFiles)
	}
}
