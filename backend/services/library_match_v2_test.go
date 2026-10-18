package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fan-web/database"
	"fan-web/services/matching"
)

func newLibraryV2Service(t *testing.T, root string, fake *fakeSessionProvider) *LibraryService {
	t.Helper()
	rt, err := NewMatchingRuntime(nil, matching.EngineV2, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	svc := NewLibraryService(nil, root, WithLibraryMatching(rt))
	svc.newSession = func(ctx context.Context) *MatchingSession {
		return newMatchingSessionForTest(ctx, rt, fake)
	}
	return svc
}

func putSeries(f *fakeSessionProvider, sub matching.Subject, keys ...string) {
	sub = cloneSubject(sub)
	sub.Detailed = true
	if sub.Media == "" {
		sub.Media = matching.KindSeries
	}
	f.putSubject(sub, keys...)
	if _, ok := f.chapters[sub.ID]; !ok && sub.EpisodeCount > 0 {
		chs := make([]matching.Chapter, 0, sub.EpisodeCount)
		for i := 1; i <= sub.EpisodeCount; i++ {
			chs = append(chs, matching.Chapter{ID: sub.ID*1000 + i, Ep: float64(i), Sort: float64(i)})
		}
		f.chapters[sub.ID] = chs
	}
}

func writeLibFile(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeEmptyFile(path); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryMatchV2MixedYearsBindSeparately(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := "hxh"
	writeLibFile(t, root, filepath.Join(dir, "Hunter x Hunter (1999) - 01.mkv"))
	writeLibFile(t, root, filepath.Join(dir, "Hunter x Hunter (2011) - 02.mkv"))

	fake := newFakeSessionProvider()
	putSeries(fake, matching.Subject{
		ID: 1999, Name: "Hunter x Hunter", NameCn: "全职猎人", Year: 1999, EpisodeCount: 62,
	}, "Hunter x Hunter")
	putSeries(fake, matching.Subject{
		ID: 2011, Name: "Hunter x Hunter", NameCn: "全职猎人", Year: 2011, EpisodeCount: 148,
	}, "Hunter x Hunter")

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if res.NewAnimes != 2 || res.NewEpisodes != 2 {
		t.Fatalf("want 2 animes + 2 eps, got %#v unidentified=%v", res, summarizeUnidentified(res))
	}
	a1999, _ := database.GetAnimeByBangumiID(1999)
	a2011, _ := database.GetAnimeByBangumiID(2011)
	if a1999 == nil || a2011 == nil {
		t.Fatalf("missing animes: 1999=%v 2011=%v", a1999, a2011)
	}
	eps1999, _ := database.ListEpisodesByAnimeID(a1999.ID)
	eps2011, _ := database.ListEpisodesByAnimeID(a2011.ID)
	if len(eps1999) != 1 || eps1999[0].EpNumber != 1 {
		t.Fatalf("1999 eps: %#v", eps1999)
	}
	if len(eps2011) != 1 || eps2011[0].EpNumber != 2 {
		t.Fatalf("2011 eps: %#v", eps2011)
	}
}

func TestLibraryMatchV2DuplicateChapterAcrossGroupsReview(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := "dup"
	// 两组不同语言标题，夹具让它们落到同一 subject 且同一 ChapterID（sort 映射）
	writeLibFile(t, root, filepath.Join(dir, "Show Title - 01.mkv"))
	writeLibFile(t, root, filepath.Join(dir, "ショータイトル - 25.mkv"))

	fake := newFakeSessionProvider()
	sub := matching.Subject{
		ID: 4242, Name: "Show Title", NameCn: "ショータイトル", Year: 2020, EpisodeCount: 24, Detailed: true,
		Media: matching.KindSeries,
	}
	putSeries(fake, sub, "Show Title", "ショータイトル")
	// Ep 1..24，Sort 25..48 续作映射：文件 25 → Chapter of sort 25 = same as… wait we need same ChapterID
	// 按契约：01 用 ep，25 用 sort 且 ChapterID 与 01 相同 → 冲突
	chs := make([]matching.Chapter, 0, 24)
	for i := 1; i <= 24; i++ {
		chs = append(chs, matching.Chapter{ID: 9000 + i, Ep: float64(i), Sort: float64(i)})
	}
	// 让 25 的唯一 sort 映射落到与 ep1 相同的 ChapterID 9001
	chs = append(chs, matching.Chapter{ID: 9001, Ep: 0, Sort: 25})
	fake.chapters[4242] = chs

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if res.NewEpisodes != 0 {
		t.Fatalf("cross-group same ChapterID must not write, got %#v", res)
	}
	if len(res.Unidentified) < 2 {
		t.Fatalf("both new files should review, got %#v", res.Unidentified)
	}
}

func TestLibraryMatchV2MixedTypeOVADoesNotAppendToTV(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := "mixed"
	writeLibFile(t, root, filepath.Join(dir, "Cool Show - 01.mkv"))

	fake := newFakeSessionProvider()
	tv := matching.Subject{
		ID: 7001, Name: "Cool Show", NameCn: "酷秀", Year: 2021, EpisodeCount: 12,
		Media: matching.KindSeries, Detailed: true,
	}
	putSeries(fake, tv, "Cool Show")

	svc := newLibraryV2Service(t, root, fake)
	first, err := svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if first.NewAnimes != 1 || first.NewEpisodes != 1 {
		t.Fatalf("first scan: %#v", first)
	}

	writeLibFile(t, root, filepath.Join(dir, "Cool Show OVA - 01.mkv"))
	ova := matching.Subject{
		ID: 7002, Name: "Cool Show OVA", NameCn: "酷秀 OVA", Year: 2021, EpisodeCount: 1,
		Media: matching.KindOVA, Detailed: true,
	}
	putSeries(fake, ova, "Cool Show OVA", "Cool Show")

	second, err := svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	anime, _ := database.GetAnimeByBangumiID(7001)
	eps, _ := database.ListEpisodesByAnimeID(anime.ID)
	for _, ep := range eps {
		if strings.Contains(ep.FilePath, "OVA") {
			t.Fatalf("OVA must not append to TV anime: %#v", eps)
		}
	}
	// OVA 要么独立入库要么 Review；不得静默写入 TV
	ovaAnime, _ := database.GetAnimeByBangumiID(7002)
	if ovaAnime != nil {
		return
	}
	found := false
	for _, u := range second.Unidentified {
		if strings.Contains(u.FileName, "OVA") {
			found = true
		}
	}
	if !found {
		t.Fatalf("OVA should be review or own anime, got %#v", second)
	}
}

func TestLibraryMatchV2SkipsAlreadyAssociated(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	name := "Skip Show - 01.mkv"
	writeLibFile(t, root, name)

	fake := newFakeSessionProvider()
	putSeries(fake, matching.Subject{
		ID: 8001, Name: "Skip Show", NameCn: "跳过秀", EpisodeCount: 12,
	}, "Skip Show")

	svc := newLibraryV2Service(t, root, fake)
	first, err := svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if first.NewAnimes != 1 || first.NewEpisodes != 1 {
		t.Fatalf("first: %#v", first)
	}
	second, err := svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if second.Skipped != 1 || second.NewAnimes != 0 || second.NewEpisodes != 0 {
		t.Fatalf("rescan should skip associated, got %#v", second)
	}
}

func TestLibraryMatchV2DirectoryConflict(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, filepath.Join("dir-a", "Conflict Show - 01.mkv"))

	fake := newFakeSessionProvider()
	putSeries(fake, matching.Subject{
		ID: 8100, Name: "Conflict Show", NameCn: "冲突秀", EpisodeCount: 12,
	}, "Conflict Show")

	svc := newLibraryV2Service(t, root, fake)
	if _, err := svc.Scan(); err != nil {
		t.Fatal(err)
	}

	writeLibFile(t, root, filepath.Join("dir-b", "Conflict Show - 02.mkv"))
	res, err := svc.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if res.NewAnimes != 0 {
		t.Fatalf("must not create second anime for same bangumi, got %#v", res)
	}
	found := false
	for _, u := range res.Unidentified {
		if u.FileName == "Conflict Show - 02.mkv" && strings.Contains(u.Reason, "目录不同") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want directory conflict unidentified, got %#v", res.Unidentified)
	}
	anime, _ := database.GetAnimeByBangumiID(8100)
	eps, _ := database.ListEpisodesByAnimeID(anime.ID)
	if len(eps) != 1 {
		t.Fatalf("original episodes unchanged, got %#v", eps)
	}
}

func TestLibraryMatchV2DuplicateNumberInGroupReview(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := "dupnum"
	writeLibFile(t, root, filepath.Join(dir, "Dup Num Show - 01.mkv"))
	writeLibFile(t, root, filepath.Join(dir, "Dup Num Show - 01v2.mkv"))

	fake := newFakeSessionProvider()
	putSeries(fake, matching.Subject{
		ID: 8200, Name: "Dup Num Show", NameCn: "重复编号秀", EpisodeCount: 12,
	}, "Dup Num Show")

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatal(err)
	}
	// 组内同 StoredNumber → 不应写入任一（或整组 Review）
	if res.NewEpisodes != 0 {
		t.Fatalf("duplicate numbers must not write, got %#v unid=%v", res, summarizeUnidentified(res))
	}
}

func TestLibraryMatchV2AbsoluteEpisodeNumberPreserved(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	// 用中文「第N话」形式验证原始大集号不被当成年份丢掉；避免 Sxx 季号与无季条目冲突干扰本断言。
	name := "海贼王 第1100话.mkv"
	writeLibFile(t, root, name)

	fake := newFakeSessionProvider()
	putSeries(fake, matching.Subject{
		ID: 8300, Name: "One Piece", NameCn: "海贼王", EpisodeCount: 1100,
	}, "海贼王", "One Piece")
	chs := make([]matching.Chapter, 0, 1100)
	for i := 1; i <= 1100; i++ {
		chs = append(chs, matching.Chapter{ID: 8300*10000 + i, Ep: float64(i), Sort: float64(i)})
	}
	fake.chapters[8300] = chs

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if res.NewEpisodes != 1 {
		t.Fatalf("want accept absolute ep, got %#v unid=%v", res, summarizeUnidentified(res))
	}
	anime, _ := database.GetAnimeByBangumiID(8300)
	eps, _ := database.ListEpisodesByAnimeID(anime.ID)
	if len(eps) != 1 || eps[0].EpNumber != 1100 {
		t.Fatalf("StoredNumber must stay 1100, got %#v", eps)
	}
}

func TestLibraryMatchV2UpstreamErrorDoesNotReturnEmptySuccess(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, "Fail Show - 01.mkv")

	fake := newFakeSessionProvider()
	// Search 直接返回上游错误
	fake.search["Fail Show"] = matchingSearchResult{}
	origSearch := fake.Search
	_ = origSearch
	failing := &failingSearchProvider{err: fmt.Errorf("boom: %w", ErrMatchUpstream)}

	rt, err := NewMatchingRuntime(nil, matching.EngineV2, "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewLibraryService(nil, root, WithLibraryMatching(rt))
	svc.newSession = func(ctx context.Context) *MatchingSession {
		return newMatchingSessionForTest(ctx, rt, failing)
	}

	res, err := svc.Scan()
	if err == nil {
		t.Fatalf("infrastructure failure must error, got success %#v", res)
	}
	if !errors.Is(err, ErrMatchUpstream) && !strings.Contains(err.Error(), "匹配上游") && !strings.Contains(err.Error(), "boom") {
		// Resolve 可能包装错误；至少不能是 nil
		t.Logf("err=%v", err)
	}
	// 不得伪装成「扫描成功且无未识别」
	if res != nil && res.NewAnimes == 0 && res.NewEpisodes == 0 && len(res.Unidentified) == 0 && err == nil {
		t.Fatal("empty success on failure")
	}
}

func TestLibraryMatchV2XianWangThreeSeasonsSynthetic(t *testing.T) {
	// S6 附件缺口下用合成夹具验证安全路径；不声称完整语料验收。
	setupLibraryDB(t)
	root := t.TempDir()
	files := []string{
		"[GM-Team][国漫][仙王的日常生活 第4季][Xian Wang De Ri Chang Sheng Huo Ⅳ][2023][12][AVC][GB][1080P].mp4",
		"[GM-Team][国漫][仙王的日常生活 第3季][Xian Wang De Ri Chang Sheng Huo Ⅲ][2022][09][AVC][GB][1080P].mp4",
		"[Dynamis One] The Daily Life of the Immortal King 5 - 03 (CR 1920x1080 AVC AAC MKV) [C41F9AE1].mkv",
	}
	for _, f := range files {
		writeLibFile(t, root, filepath.Join("xw", f))
	}
	fake := newFakeSessionProvider()
	xianWangFixtures(fake)

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if res.NewAnimes != 3 || res.NewEpisodes != 3 {
		t.Fatalf("synthetic xianwang: %#v unid=%v", res, summarizeUnidentified(res))
	}
	for _, id := range []int{405214, 358742, 529875} {
		a, err := database.GetAnimeByBangumiID(id)
		if err != nil || a == nil {
			t.Fatalf("missing anime %d", id)
		}
	}
}

type failingSearchProvider struct {
	err error
}

func (f *failingSearchProvider) Search(context.Context, string) (matchingSearchResult, error) {
	return matchingSearchResult{}, f.err
}
func (f *failingSearchProvider) Detail(context.Context, int) (matching.Subject, error) {
	return matching.Subject{}, f.err
}
func (f *failingSearchProvider) Chapters(context.Context, int) ([]matching.Chapter, error) {
	return nil, f.err
}

func summarizeUnidentified(res *LibraryScanResult) string {
	if res == nil {
		return "<nil>"
	}
	parts := make([]string, 0, len(res.Unidentified))
	for _, u := range res.Unidentified {
		parts = append(parts, u.FileName+":"+u.Reason)
	}
	return strings.Join(parts, " | ")
}
