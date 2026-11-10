package services

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"fan-web/models"
	"fan-web/services/matching"
)

// putOptShowProvider 注册 "Opt Show"（n 话，章节 ep/sort 1..n）。
func putOptShowProvider(f *fakeSessionProvider, id, n int) matching.Subject {
	sub := matching.Subject{
		ID: id, Name: "Opt Show", NameCn: "选项番",
		Media: matching.KindSeries, EpisodeCount: n, Detailed: true,
	}
	f.putSubject(sub, "Opt Show", "选项番")
	chs := make([]matching.Chapter, 0, n)
	for i := 1; i <= n; i++ {
		chs = append(chs, matching.Chapter{ID: id*100 + i, Ep: float64(i), Sort: float64(i)})
	}
	f.chapters[id] = chs
	return sub
}

func desiredByPath(eps []models.Episode) map[string]models.Episode {
	out := make(map[string]models.Episode, len(eps))
	for _, ep := range eps {
		out[ep.FilePath] = ep
	}
	return out
}

func TestScanForAnimeWithOnlyFilesKeepsExistingAndLimitsFree(t *testing.T) {
	root := t.TempDir()
	dir := "opt-only"
	f01 := "[Group] Opt Show - 01 [1080p].mkv"
	f02 := "[Group] Opt Show - 02 [1080p].mkv"
	f03 := "[Group] Opt Show - 03 [1080p].mkv"
	writeScanVideos(t, filepath.Join(root, dir), f01, f02, f03)

	f := newFakeSessionProvider()
	putOptShowProvider(f, 7301, 12)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 3, Title: "Opt Show", TitleCn: "选项番", BangumiID: 7301, EpCount: 12, FilePath: dir}
	existing := []models.Episode{{ID: 31, AnimeID: 3, EpNumber: 1, FilePath: f01, Duration: 1400}}

	inv, err := sc.ScanForAnime(anime, existing, nil, WithOnlyFiles([]string{f02}))
	if err != nil {
		t.Fatal(err)
	}
	got := desiredByPath(inv.Desired)
	if ep, ok := got[f01]; !ok || ep.ID != 31 || ep.EpNumber != 1 || ep.Duration != 1400 {
		t.Fatalf("已有剧集应原样保留: %#v", inv.Desired)
	}
	if ep, ok := got[f02]; !ok || ep.ID != 0 || ep.EpNumber != 2 {
		t.Fatalf("限定文件应入 Desired: %#v", inv.Desired)
	}
	if _, ok := got[f03]; ok {
		t.Fatalf("未限定文件不得入 Desired: %#v", inv.Desired)
	}
	if len(inv.PresentFiles) != 3 {
		t.Fatalf("PresentFiles 应含全部磁盘文件: %v", inv.PresentFiles)
	}

	// 空切片 = 不限定（与不传等价）
	inv2, err := sc.ScanForAnime(anime, existing, nil, WithOnlyFiles(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(inv2.Desired) != 3 {
		t.Fatalf("空 only-files 应不限定: %#v", inv2.Desired)
	}
}

func TestScanForAnimeEpisodeOffsetCumulativeAndStableRescan(t *testing.T) {
	root := t.TempDir()
	dir := "opt-offset"
	f13 := "[Group] Opt Show - 13 [1080p].mkv"
	f14 := "[Group] Opt Show - 14 [1080p].mkv"
	writeScanVideos(t, filepath.Join(root, dir), f13, f14)

	f := newFakeSessionProvider()
	putOptShowProvider(f, 7302, 12)
	withScanSession(t, f)
	withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

	sc := newV2Scanner(t, root)
	anime := models.Anime{ID: 4, Title: "Opt Show", TitleCn: "选项番", BangumiID: 7302, EpCount: 12, FilePath: dir}

	inv, err := sc.ScanForAnime(anime, nil, nil, WithEpisodeOffset(12))
	if err != nil {
		t.Fatal(err)
	}
	got := desiredByPath(inv.Desired)
	if got[f13].EpNumber != 1 || got[f14].EpNumber != 2 || len(inv.Desired) != 2 {
		t.Fatalf("偏移 12 后应为 ep 1/2: %#v", inv.Desired)
	}

	// 下次不带偏移的单番重扫：n>EpCount 由 correctExistingEpNumber 保留，结果不抖动
	stored := make([]models.Episode, 0, len(inv.Desired))
	for i, ep := range inv.Desired {
		ep.ID = int64(40 + i)
		stored = append(stored, ep)
	}
	inv2, err := sc.ScanForAnime(anime, stored, nil)
	if err != nil {
		t.Fatal(err)
	}
	got2 := desiredByPath(inv2.Desired)
	if got2[f13].EpNumber != 1 || got2[f14].EpNumber != 2 || len(inv2.Desired) != 2 {
		t.Fatalf("重扫应稳定: %#v", inv2.Desired)
	}

	// offset=0 与不传等价：13/14 越界，全新目标无可关联文件 → Review
	if _, err := sc.ScanForAnime(anime, nil, nil, WithEpisodeOffset(0)); !errors.Is(err, ErrMatchReviewRequired) {
		t.Fatalf("offset=0 应与不传等价（越界 Review），got %v", err)
	}
}

func TestScanForAnimeEpisodeOffsetApplicability(t *testing.T) {
	cases := []struct {
		name    string
		epCount int
		files   []string
	}{
		{name: "EpCount==0", epCount: 0, files: []string{"[Group] Opt Show - 13 [1080p].mkv"}},
		{name: "集号<=EpCount", epCount: 12, files: []string{"[Group] Opt Show - 12 [1080p].mkv", "[Group] Opt Show - 13 [1080p].mkv"}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := "opt-app"
			writeScanVideos(t, filepath.Join(root, dir), tc.files...)
			id := 7310 + i
			f := newFakeSessionProvider()
			putOptShowProvider(f, id, 12)
			withScanSession(t, f)
			withFileAssociated(t, func(string, string) (bool, error) { return false, nil })

			sc := newV2Scanner(t, root)
			anime := models.Anime{ID: 5, Title: "Opt Show", TitleCn: "选项番", BangumiID: id, EpCount: tc.epCount, FilePath: dir}
			inv, err := sc.ScanForAnime(anime, nil, nil, WithEpisodeOffset(5))
			if !errors.Is(err, ErrMatchReviewRequired) {
				t.Fatalf("应返回 ErrMatchReviewRequired，got %v", err)
			}
			if !strings.Contains(err.Error(), "集号偏移仅适用于累计编号（集号需大于该条目话数）") {
				t.Fatalf("文案不符: %v", err)
			}
			if len(inv.Desired) != 0 {
				t.Fatalf("出错应零 Desired: %#v", inv.Desired)
			}
		})
	}
}

func TestCorrectExistingEpNumberKeepsWhenAbsEpisode(t *testing.T) {
	anime := models.Anime{BangumiID: 1, EpCount: 12, FilePath: "abs"}
	ep := models.Episode{EpNumber: 3, FilePath: "[Group] Abs Show - 05 [1080p].mkv"}
	ev := matching.FileEvidence{
		Name:       ep.FilePath,
		Episode:    matching.EpisodeToken{Kind: matching.NumberSingle, First: 5, Last: 5, Explicit: true},
		AbsEpisode: 30,
	}
	if got := correctExistingEpNumberFromEvidence(anime, ep, ev); got != 3 {
		t.Fatalf("AbsEpisode>0 应保持原集号 3，got %d", got)
	}
	// 对照：无总集数标注时仍按文件名更正（隐式契约 1 不变）
	ev.AbsEpisode = 0
	if got := correctExistingEpNumberFromEvidence(anime, ep, ev); got != 5 {
		t.Fatalf("无 AbsEpisode 应更正为 5，got %d", got)
	}
}

// 走真实 ParseFile 的端到端版本：依赖解析层产出 AbsEpisode（S1 [总第N] 抽取）。
// 解析层尚未产出时只校验不崩溃并跳过，合入 S1 后自动生效。
func TestCorrectExistingEpNumberKeepsWhenAbsEpisodeParsed(t *testing.T) {
	anime := models.Anime{BangumiID: 1, EpCount: 12, FilePath: "abs"}
	ep := models.Episode{EpNumber: 3, FilePath: "[Group] Abs Show - 05 [总第30] [1080p].mkv"}
	ev := matching.ParseFile(matching.FileInput{Name: ep.FilePath, Dir: anime.FilePath})
	got := correctExistingEpNumber(anime, ep)
	if ev.AbsEpisode <= 0 {
		t.Skipf("解析层尚未产出 AbsEpisode（S1 未合入），got=%d", got)
	}
	if got != 3 {
		t.Fatalf("AbsEpisode=%d 时应保持原集号 3，got %d", ev.AbsEpisode, got)
	}
}

func TestMatchCandidatesFromDecisionFillsYearAndEpCount(t *testing.T) {
	d := matching.Decision{Candidates: []matching.Candidate{
		{Subject: matching.Subject{ID: 1, Name: "A", NameCn: "甲", Year: 2016, EpisodeCount: 25}, Score: 0.9},
	}}
	out := matchCandidatesFromDecision(d)
	if len(out) != 1 || out[0].Year != 2016 || out[0].EpCount != 25 || out[0].ID != 1 || out[0].Score != 0.9 {
		t.Fatalf("候选应带 year/ep_count: %#v", out)
	}
}
