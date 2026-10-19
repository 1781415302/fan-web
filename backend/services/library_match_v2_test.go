package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func TestLibraryMatchV2NumberAnomalyIsolatesFiles(t *testing.T) {
	cases := []struct {
		name      string
		extra     string
		wantEps   int
		rejectSub string
	}{
		{"fraction", "Review Show - 06.5.mkv", 12, "06.5"},
		{"outrange", "Review Show - 13.mkv", 12, "13"},
		{"dupver", "Review Show - 05v2.mkv", 11, "05"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupLibraryDB(t)
			root := t.TempDir()
			dir := "review-" + tc.name
			for i := 1; i <= 12; i++ {
				writeLibFile(t, root, filepath.Join(dir, fmt.Sprintf("Review Show - %02d.mkv", i)))
			}
			writeLibFile(t, root, filepath.Join(dir, tc.extra))

			fake := newFakeSessionProvider()
			fake.exactOnly = true
			sub := matching.Subject{
				ID: 880001, Name: "Review Show", NameCn: "Review Show",
				Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
			}
			putSeries(fake, sub, "Review Show")

			res, err := newLibraryV2Service(t, root, fake).Scan()
			if err != nil {
				t.Fatal(err)
			}
			if res.NewEpisodes != tc.wantEps {
				t.Fatalf("NewEpisodes=%d want %d unid=%s res=%#v", res.NewEpisodes, tc.wantEps, summarizeUnidentified(res), res)
			}
			anime, err := database.GetAnimeByBangumiID(880001)
			if err != nil || anime == nil {
				t.Fatalf("anime missing: %v", err)
			}
			eps, err := database.ListEpisodesByAnimeID(anime.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(eps) != tc.wantEps {
				t.Fatalf("db episodes=%d want %d", len(eps), tc.wantEps)
			}
			foundReject := false
			for _, u := range res.Unidentified {
				if strings.Contains(u.FileName, tc.rejectSub) {
					foundReject = true
				}
			}
			if !foundReject {
				t.Fatalf("expected unidentified containing %q, got %s", tc.rejectSub, summarizeUnidentified(res))
			}
		})
	}
}

func TestLibraryMatchV2SequelDigitNotFirstSeason(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, filepath.Join("ef", "[北宇治字幕组] 悠久之翼2 [01].mkv"))
	writeLibFile(t, root, filepath.Join("bm", "爆漫王。3 [01].mkv"))

	fake := newFakeSessionProvider()
	fake.exactOnly = true
	s1 := matching.Subject{ID: 1028, Name: "悠久之翼", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	s2 := matching.Subject{ID: 1029, Name: "悠久之翼2", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	b1 := matching.Subject{ID: 3428, Name: "爆漫王。", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	b3 := matching.Subject{ID: 33352, Name: "爆漫王。3", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	putSeries(fake, s1, "悠久之翼")
	putSeries(fake, s2, "悠久之翼2", "悠久之翼")
	putSeries(fake, b1, "爆漫王。")
	putSeries(fake, b3, "爆漫王。3", "爆漫王。")

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if res.NewEpisodes < 2 {
		t.Fatalf("want both sequels accepted, got %#v unid=%s", res, summarizeUnidentified(res))
	}
	for _, id := range []int{1029, 33352} {
		a, err := database.GetAnimeByBangumiID(id)
		if err != nil || a == nil {
			t.Fatalf("missing sequel anime %d (res=%#v unid=%s)", id, res, summarizeUnidentified(res))
		}
	}
	for _, id := range []int{1028, 3428} {
		a, _ := database.GetAnimeByBangumiID(id)
		if a != nil {
			t.Fatalf("must not create first season %d", id)
		}
	}
}

func TestLibraryMatchV2Subject404DoesNotStarveLaterDir(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, filepath.Join("A", "Broken Show - 01.mkv"))
	writeLibFile(t, root, filepath.Join("B", "Healthy Show - 01.mkv"))

	fake := newFakeSessionProvider()
	fake.exactOnly = true
	fake.detailNotFound = map[int]bool{770001: true}

	broken := matching.Subject{
		ID: 770001, Name: "Broken Show", Media: matching.KindSeries, EpisodeCount: 12, Detailed: false,
	}
	healthy := matching.Subject{
		ID: 770002, Name: "Healthy Show", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	// search returns stub; detail 404 for broken
	fake.putSubject(broken, "Broken Show")
	putSeries(fake, healthy, "Healthy Show")
	fake.detailNotFound[770001] = true
	delete(fake.detail, 770001) // force Detail path to hit notFound

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatalf("404 must not abort scan: %v", err)
	}
	a, err := database.GetAnimeByBangumiID(770002)
	if err != nil || a == nil {
		t.Fatalf("healthy B must be ingested: err=%v res=%#v unid=%s", err, res, summarizeUnidentified(res))
	}
	eps, err := database.ListEpisodesByAnimeID(a.ID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("healthy eps=%v err=%v", eps, err)
	}
	brokenAnime, _ := database.GetAnimeByBangumiID(770001)
	if brokenAnime != nil {
		t.Fatal("must not create anime from unverified 404 subject")
	}
	foundA := false
	for _, u := range res.Unidentified {
		if strings.Contains(u.FilePath, "A") || strings.Contains(u.FileName, "Broken") {
			foundA = true
		}
	}
	if !foundA {
		t.Fatalf("A should be unidentified, got %s", summarizeUnidentified(res))
	}
	// same subject should not be re-requested endlessly
	n770 := 0
	for _, id := range fake.detailCalls {
		if id == 770001 {
			n770++
		}
	}
	if n770 > 3 {
		t.Fatalf("detail 404 should be session-deduped, calls=%d %#v", n770, fake.detailCalls)
	}
}

func TestLibraryMatchV2Subject404ViaHTTPDoesNotStarve(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, filepath.Join("A", "Broken HTTP - 01.mkv"))
	writeLibFile(t, root, filepath.Join("B", "Healthy HTTP - 01.mkv"))

	var detailHits sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v0/search/subjects":
			body, _ := io.ReadAll(r.Body)
			kw := string(body)
			var data []map[string]interface{}
			if strings.Contains(kw, "Broken") {
				data = append(data, map[string]interface{}{
					"id": 660001, "name": "Broken HTTP", "name_cn": "", "date": "2020-01-01", "platform": "TV",
				})
			}
			if strings.Contains(kw, "Healthy") {
				data = append(data, map[string]interface{}{
					"id": 660002, "name": "Healthy HTTP", "name_cn": "", "date": "2020-01-01", "platform": "TV",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
		case strings.HasPrefix(r.URL.Path, "/v0/subjects/"):
			id := strings.TrimPrefix(r.URL.Path, "/v0/subjects/")
			if v, _ := detailHits.LoadOrStore(id, new(int32)); true {
				atomic.AddInt32(v.(*int32), 1)
			}
			if id == "660001" {
				http.Error(w, "gone", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 660002, "name": "Healthy HTTP", "name_cn": "", "date": "2020-01-01",
				"platform": "TV", "eps": 12, "total_episodes": 12, "images": map[string]string{}, "infobox": []interface{}{},
			})
		case r.URL.Path == "/v0/episodes":
			sid := r.URL.Query().Get("subject_id")
			if sid == "660001" {
				http.Error(w, "gone", http.StatusNotFound)
				return
			}
			data := make([]map[string]interface{}, 0, 12)
			for i := 1; i <= 12; i++ {
				data = append(data, map[string]interface{}{"id": 66000200 + i, "ep": i, "sort": i})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
		default:
			http.Error(w, "bad "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)

	bangumi := NewBangumiService()
	bangumi.SetBaseURL(srv.URL)
	rt, err := NewMatchingRuntime(bangumi, matching.EngineV2, "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewLibraryService(bangumi, root, WithLibraryMatching(rt))
	res, err := svc.Scan()
	if err != nil {
		t.Fatalf("HTTP 404 must not abort library scan: %v", err)
	}
	a, err := database.GetAnimeByBangumiID(660002)
	if err != nil || a == nil {
		t.Fatalf("healthy B missing: %v res=%#v unid=%s", err, res, summarizeUnidentified(res))
	}
	if v, ok := detailHits.Load("660001"); ok {
		if atomic.LoadInt32(v.(*int32)) > 3 {
			t.Fatalf("404 subject detail over-fetched: %d", atomic.LoadInt32(v.(*int32)))
		}
	}
}

// TestLibraryMatchV2Subject404KeepsAmbiguityAndContinuesScan：
// A 目录同名两候选之一 Detail 404 → Review/未识别、不写入另一同名；
// B 健康目录仍入库；调换目录名顺序结论一致。
func TestLibraryMatchV2Subject404KeepsAmbiguityAndContinuesScan(t *testing.T) {
	run := func(t *testing.T, dirA, dirB string) {
		t.Helper()
		setupLibraryDB(t)
		root := t.TempDir()
		writeLibFile(t, root, filepath.Join(dirA, "Shared Title - 01.mkv"))
		writeLibFile(t, root, filepath.Join(dirB, "Healthy Show - 01.mkv"))

		fake := newFakeSessionProvider()
		fake.exactOnly = true
		exact := matching.Subject{ID: 881001, Name: "Shared Title", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
		peer := matching.Subject{ID: 881003, Name: "Shared Title", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
		healthy := matching.Subject{ID: 770002, Name: "Healthy Show", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
		putSeries(fake, exact, "Shared Title")
		putSeries(fake, peer, "Shared Title")
		putSeries(fake, healthy, "Healthy Show")
		fake.detailNotFound = map[int]bool{881001: true}
		delete(fake.detail, 881001)

		res, err := newLibraryV2Service(t, root, fake).Scan()
		if err != nil {
			t.Fatalf("scan must not abort on subject 404: %v", err)
		}
		for _, id := range []int{881001, 881003} {
			if a, _ := database.GetAnimeByBangumiID(id); a != nil {
				t.Fatalf("must not create anime for ambiguous/404 pair id=%d", id)
			}
		}
		h, err := database.GetAnimeByBangumiID(770002)
		if err != nil || h == nil {
			t.Fatalf("healthy B must be ingested: err=%v unid=%s", err, summarizeUnidentified(res))
		}
		eps, err := database.ListEpisodesByAnimeID(h.ID)
		if err != nil || len(eps) != 1 {
			t.Fatalf("healthy eps=%v err=%v", eps, err)
		}
		foundA := false
		for _, u := range res.Unidentified {
			if strings.Contains(u.FilePath, dirA) || strings.Contains(u.FileName, "Shared Title") {
				foundA = true
			}
		}
		if !foundA {
			t.Fatalf("A must be unidentified, got %s", summarizeUnidentified(res))
		}
	}

	t.Run("A_then_B", func(t *testing.T) { run(t, "A", "B") })
	t.Run("B_then_A", func(t *testing.T) { run(t, "ZShared", "AHealthy") })
}

// TestLibraryMatchV2Subject404ApproxPeerNotIngested：精确标题 404 后近似作品不得入库。
func TestLibraryMatchV2Subject404ApproxPeerNotIngested(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, filepath.Join("A", "Shared Title - 01.mkv"))
	writeLibFile(t, root, filepath.Join("B", "Healthy Show - 01.mkv"))

	fake := newFakeSessionProvider()
	fake.exactOnly = true
	exact := matching.Subject{ID: 881001, Name: "Shared Title", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	approx := matching.Subject{ID: 881003, Name: "Shared Titlf", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	healthy := matching.Subject{ID: 770002, Name: "Healthy Show", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	putSeries(fake, exact, "Shared Title")
	putSeries(fake, approx, "Shared Title")
	putSeries(fake, healthy, "Healthy Show")
	fake.detailNotFound = map[int]bool{881001: true}
	delete(fake.detail, 881001)

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if a, _ := database.GetAnimeByBangumiID(881003); a != nil {
		t.Fatal("must not ingest approximate peer after exact 404")
	}
	if a, _ := database.GetAnimeByBangumiID(881001); a != nil {
		t.Fatal("must not ingest 404 subject")
	}
	h, err := database.GetAnimeByBangumiID(770002)
	if err != nil || h == nil {
		t.Fatalf("healthy missing: %v unid=%s", err, summarizeUnidentified(res))
	}
}

// TestLibraryMatchV2Chapters404KeepsAmbiguity：章节 404 同样不得消除同名歧义。
func TestLibraryMatchV2Chapters404KeepsAmbiguity(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, filepath.Join("A", "Shared Title - 01.mkv"))
	writeLibFile(t, root, filepath.Join("B", "Healthy Show - 01.mkv"))

	fake := newFakeSessionProvider()
	fake.exactOnly = true
	exact := matching.Subject{ID: 881001, Name: "Shared Title", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	peer := matching.Subject{ID: 881003, Name: "Shared Title", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	healthy := matching.Subject{ID: 770002, Name: "Healthy Show", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	putSeries(fake, exact, "Shared Title")
	putSeries(fake, peer, "Shared Title")
	putSeries(fake, healthy, "Healthy Show")
	fake.chaptersNotFound = map[int]bool{881001: true}
	delete(fake.chapters, 881001)

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, id := range []int{881001, 881003} {
		if a, _ := database.GetAnimeByBangumiID(id); a != nil {
			t.Fatalf("must not create anime id=%d after chapters 404 ambiguity", id)
		}
	}
	if h, err := database.GetAnimeByBangumiID(770002); err != nil || h == nil {
		t.Fatalf("healthy missing: %v unid=%s", err, summarizeUnidentified(res))
	}
}

// TestLibraryMatchV2FateZeroKeepsFullTitle：发布组尾缀不得把 Fate-Zero 剥成 Fate。
func TestLibraryMatchV2FateZeroKeepsFullTitle(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dir := "fate-zero"
	// 同目录不同集号：覆盖 dash / SxxEyy / 中文集号 / 方括号 / 发布尾缀正例
	names := []string{
		"Fate-Zero - 01.mkv",
		"Fate-Zero S01E02.mkv",
		"Fate-Zero 第03话.mkv",
		"Fate-Zero [04].mkv",
		"Fate-Zero - 05-SENPAI.mkv",
	}
	for _, name := range names {
		writeLibFile(t, root, filepath.Join(dir, name))
	}

	fake := newFakeSessionProvider()
	fake.exactOnly = true
	zero := matching.Subject{ID: 991501, Name: "Fate-Zero", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	fate := matching.Subject{ID: 991502, Name: "Fate", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true}
	// 两查询词均显式注册两个候选
	putSeries(fake, zero, "Fate-Zero", "Fate")
	putSeries(fake, fate, "Fate-Zero", "Fate")

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	aZero, _ := database.GetAnimeByBangumiID(991501)
	aFate, _ := database.GetAnimeByBangumiID(991502)
	if aFate != nil {
		t.Fatalf("must not create Fate(991502); zero=%v unid=%s", aZero, summarizeUnidentified(res))
	}
	if aZero == nil {
		t.Fatalf("want Fate-Zero(991501); res=%#v unid=%s", res, summarizeUnidentified(res))
	}
	eps, err := database.ListEpisodesByAnimeID(aZero.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 5 {
		t.Fatalf("want 5 episodes stored, got %d %#v unid=%s", len(eps), eps, summarizeUnidentified(res))
	}
	wantNums := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}
	for _, ep := range eps {
		delete(wantNums, ep.EpNumber)
		base := filepath.Base(ep.FilePath)
		if !strings.Contains(base, "Fate-Zero") {
			t.Fatalf("original filename lost: %#v", ep)
		}
		if strings.HasPrefix(base, "Fate.") || base == "Fate.mkv" {
			t.Fatalf("cleaned title leaked into path: %#v", ep)
		}
	}
	if len(wantNums) != 0 {
		t.Fatalf("missing episode numbers: %v eps=%#v", wantNums, eps)
	}
}

// TestLibraryMatchV2ShortTitleSubtitleZeroWrite：短标题副标题晋升不得误入库。
func TestLibraryMatchV2ShortTitleSubtitleZeroWrite(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	writeLibFile(t, root, filepath.Join("re", "Re S02E01.mkv"))
	writeLibFile(t, root, filepath.Join("monogatari", "物语 S02E01.mkv"))

	fake := newFakeSessionProvider()
	fake.exactOnly = true
	reZero := matching.Subject{
		ID: 991611, Name: "Re:Zero Season 2", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	mono := matching.Subject{
		ID: 991612, Name: "物语 Season 2: Another Story", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	putSeries(fake, reZero, "Re")
	putSeries(fake, mono, "物语")

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, id := range []int{991611, 991612} {
		if a, _ := database.GetAnimeByBangumiID(id); a != nil {
			t.Fatalf("short-title subtitle must not create anime id=%d unid=%s", id, summarizeUnidentified(res))
		}
	}
	if res.NewAnimes != 0 || res.NewEpisodes != 0 {
		t.Fatalf("want zero writes, got %#v unid=%s", res, summarizeUnidentified(res))
	}
}

// TestLibraryMatchV2S01E05v2Duplicate：与普通 S01E05 同组冲突时两份第5集待确认，其余入库。
func TestLibraryMatchV2S01E05v2Duplicate(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	dupDir := "campus-dup"
	for i := 1; i <= 12; i++ {
		writeLibFile(t, root, filepath.Join(dupDir, fmt.Sprintf("Campus Notes S01E%02d.mkv", i)))
	}
	writeLibFile(t, root, filepath.Join(dupDir, "Campus Notes S01E05v2.mkv"))

	fake := newFakeSessionProvider()
	fake.exactOnly = true
	putSeries(fake, matching.Subject{
		ID: 991701, Name: "Campus Notes", NameCn: "校园笔记", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}, "Campus Notes")

	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	a, err := database.GetAnimeByBangumiID(991701)
	if err != nil || a == nil {
		t.Fatalf("anime missing: %v unid=%s", err, summarizeUnidentified(res))
	}
	eps, _ := database.ListEpisodesByAnimeID(a.ID)
	for _, ep := range eps {
		base := filepath.Base(ep.FilePath)
		if strings.Contains(base, "S01E05") {
			t.Fatalf("ep5 must not be stored while duplicate v2 exists: %#v", eps)
		}
	}
	if len(eps) != 11 {
		t.Fatalf("want 11 stored (all but ep5), got %d %#v unid=%s", len(eps), eps, summarizeUnidentified(res))
	}
}

// TestLibraryMatchV2S01E05v2Solo：单独 S01E05v2 无重复第5集时应作为第5集入库。
func TestLibraryMatchV2S01E05v2Solo(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	soloDir := "campus-solo"
	for i := 1; i <= 12; i++ {
		if i == 5 {
			writeLibFile(t, root, filepath.Join(soloDir, "Campus Notes S01E05v2.mkv"))
			continue
		}
		writeLibFile(t, root, filepath.Join(soloDir, fmt.Sprintf("Campus Notes S01E%02d.mkv", i)))
	}
	fake := newFakeSessionProvider()
	fake.exactOnly = true
	putSeries(fake, matching.Subject{
		ID: 991702, Name: "Campus Notes", NameCn: "校园笔记", Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}, "Campus Notes")
	res, err := newLibraryV2Service(t, root, fake).Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	a, err := database.GetAnimeByBangumiID(991702)
	if err != nil || a == nil {
		t.Fatalf("anime missing: %v unid=%s", err, summarizeUnidentified(res))
	}
	eps, _ := database.ListEpisodesByAnimeID(a.ID)
	if len(eps) != 12 {
		t.Fatalf("want 12 eps, got %d %#v unid=%s", len(eps), eps, summarizeUnidentified(res))
	}
	foundV2 := false
	for _, ep := range eps {
		if ep.EpNumber == 5 {
			if !strings.Contains(ep.FilePath, "S01E05v2.mkv") {
				t.Fatalf("ep5 path want S01E05v2: %#v", ep)
			}
			foundV2 = true
		}
	}
	if !foundV2 {
		t.Fatalf("ep5 missing: %#v", eps)
	}
}
