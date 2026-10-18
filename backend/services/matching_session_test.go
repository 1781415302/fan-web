package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"fan-web/services/matching"
)

// fakeSessionProvider 包内测试用：不联网。
type fakeSessionProvider struct {
	search   map[string]matchingSearchResult
	detail   map[int]matching.Subject
	chapters map[int][]matching.Chapter

	searchCalls []string
	detailCalls []int
}

func newFakeSessionProvider() *fakeSessionProvider {
	return &fakeSessionProvider{
		search:   map[string]matchingSearchResult{},
		detail:   map[int]matching.Subject{},
		chapters: map[int][]matching.Chapter{},
	}
}

func (f *fakeSessionProvider) Search(_ context.Context, kw string) (matchingSearchResult, error) {
	f.searchCalls = append(f.searchCalls, kw)
	if res, ok := f.search[kw]; ok {
		return cloneSearchResult(res), nil
	}
	// 子串兜底：便于不同查询词命中同一夹具
	for k, res := range f.search {
		if strings.Contains(kw, k) || strings.Contains(k, kw) {
			return cloneSearchResult(res), nil
		}
	}
	return matchingSearchResult{Subjects: []matching.Subject{}, Source: "v0"}, nil
}

func (f *fakeSessionProvider) Detail(_ context.Context, id int) (matching.Subject, error) {
	f.detailCalls = append(f.detailCalls, id)
	sub, ok := f.detail[id]
	if !ok {
		return matching.Subject{}, fmt.Errorf("no detail for %d: %w", id, ErrMatchUpstream)
	}
	if sub.ID != id {
		return matching.Subject{}, fmt.Errorf("detail id mismatch: %w", ErrMatchUpstream)
	}
	out := cloneSubject(sub)
	out.Detailed = true
	return out, nil
}

func (f *fakeSessionProvider) Chapters(_ context.Context, id int) ([]matching.Chapter, error) {
	if ch, ok := f.chapters[id]; ok {
		return cloneChapters(ch), nil
	}
	return []matching.Chapter{}, nil
}

func (f *fakeSessionProvider) putSubject(sub matching.Subject, searchKeys ...string) {
	sub = cloneSubject(sub)
	f.detail[sub.ID] = sub
	stub := cloneSubject(sub)
	stub.Detailed = false
	for _, k := range searchKeys {
		res := f.search[k]
		res.Subjects = append(res.Subjects, stub)
		res.Source = "v0"
		f.search[k] = res
	}
}

func testSessionRuntime(t *testing.T) *MatchingRuntime {
	t.Helper()
	rt, err := NewMatchingRuntime(nil, matching.EngineV2, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	return rt
}

func groupFromFile(t *testing.T, name, dir string) matching.Group {
	t.Helper()
	groups := matching.GroupFiles([]matching.FileInput{{Name: name, Dir: dir}})
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d for %s", len(groups), name)
	}
	return groups[0]
}

func xianWangFixtures(f *fakeSessionProvider) {
	s4 := matching.Subject{
		ID: 405214, Name: "仙王的日常生活 第4季", NameCn: "仙王的日常生活 第4季",
		Year: 2023, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		Aliases: []string{"The Daily Life of the Immortal King Season 4"},
	}
	s3 := matching.Subject{
		ID: 358742, Name: "仙王的日常生活 第3季", NameCn: "仙王的日常生活 第3季",
		Year: 2022, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	s5 := matching.Subject{
		ID: 529875, Name: "仙王的日常生活 第5季", NameCn: "仙王的日常生活 第5季",
		Year: 2024, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
		Aliases: []string{"The Daily Life of the Immortal King"},
	}
	// 干扰项
	other := matching.Subject{
		ID: 999001, Name: "完全无关的别的动画", NameCn: "完全无关的别的动画",
		Year: 2020, Media: matching.KindSeries, EpisodeCount: 24, Detailed: true,
	}

	f.putSubject(s4, "仙王的日常生活", "仙王的日常生活 第4季", "Xian Wang De Ri Chang Sheng Huo")
	f.putSubject(s3, "仙王的日常生活", "仙王的日常生活 第3季", "Xian Wang De Ri Chang Sheng Huo")
	f.putSubject(s5, "仙王的日常生活", "The Daily Life of the Immortal King", "The Daily Life of the Immortal King 5")
	f.putSubject(other, "无关")

	// 章节：按 Ep=集号
	for id, n := range map[int]int{405214: 12, 358742: 12, 529875: 12} {
		chs := make([]matching.Chapter, 0, n)
		for i := 1; i <= n; i++ {
			chs = append(chs, matching.Chapter{ID: id*100 + i, Ep: float64(i), Sort: float64(i)})
		}
		f.chapters[id] = chs
	}
}

func TestMatchingSessionXianWangThreeSeasons(t *testing.T) {
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()
	xianWangFixtures(f)

	cases := []struct {
		name   string
		file   string
		wantID int
	}{
		{
			name:   "S4",
			file:   "[GM-Team][国漫][仙王的日常生活 第4季][Xian Wang De Ri Chang Sheng Huo Ⅳ][2023][12][AVC][GB][1080P].mp4",
			wantID: 405214,
		},
		{
			name:   "S3",
			file:   "[GM-Team][国漫][仙王的日常生活 第3季][Xian Wang De Ri Chang Sheng Huo Ⅲ][2022][09][AVC][GB][1080P].mp4",
			wantID: 358742,
		},
		{
			name:   "S5EN",
			file:   "[Dynamis One] The Daily Life of the Immortal King 5 - 03 (CR 1920x1080 AVC AAC MKV) [C41F9AE1].mkv",
			wantID: 529875,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := newMatchingSessionForTest(context.Background(), rt, f)
			g := groupFromFile(t, tc.file, "xw/"+tc.name)
			res, err := sess.Resolve(g, nil)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if res.Decision.Code != matching.Accepted {
				t.Fatalf("code=%s reason=%s cands=%v", res.Decision.Code, res.Decision.Reason, summarizeCands(res.Decision.Candidates))
			}
			if res.Decision.WinnerID != tc.wantID || res.Subject == nil || res.Subject.ID != tc.wantID {
				t.Fatalf("winner=%d subject=%v want %d", res.Decision.WinnerID, res.Subject, tc.wantID)
			}
			if !res.Subject.Detailed {
				t.Fatal("Accepted 新条目必须 Detailed=true")
			}
			if res.FromExistingBound {
				t.Fatal("搜索路径不应 FromExistingBound")
			}
			if res.Decision.Candidates == nil {
				t.Fatal("Candidates must be non-nil")
			}
		})
	}
}

func TestMatchingSessionRejectsMismatchedDetail(t *testing.T) {
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()

	// 搜索返回看起来像样的候选，但 Detail 结构（季号）与查询不符 → 不能接受
	searchHit := matching.Subject{
		ID: 777, Name: "仙王的日常生活", NameCn: "仙王的日常生活",
		Year: 2023, Media: matching.KindSeries, EpisodeCount: 12, Detailed: false,
	}
	// Detail 补回后变成第1季（无法解释第4季）
	detailHit := matching.Subject{
		ID: 777, Name: "仙王的日常生活", NameCn: "仙王的日常生活",
		Year: 2020, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.search["仙王的日常生活"] = matchingSearchResult{Subjects: []matching.Subject{searchHit}, Source: "v0"}
	f.detail[777] = detailHit
	f.chapters[777] = nil

	sess := newMatchingSessionForTest(context.Background(), rt, f)
	g := groupFromFile(t,
		"[GM-Team][国漫][仙王的日常生活 第4季][Xian Wang De Ri Chang Sheng Huo Ⅳ][2023][12][AVC][GB][1080P].mp4",
		"xw/mismatch")
	res, err := sess.Resolve(g, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Decision.Code == matching.Accepted {
		t.Fatalf("must not accept mismatched detail; got Accepted winner=%d reason=%s",
			res.Decision.WinnerID, res.Decision.Reason)
	}
}

func TestMatchingSessionBoundOVADoesNotBypass(t *testing.T) {
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()

	tvBound := matching.Subject{
		ID: 1001, Name: "示例作品", NameCn: "示例作品",
		Year: 2021, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.detail[1001] = tvBound
	// 同名 OVA 搜索命中（若走到搜索也不应靠旧绑定接受）
	ova := matching.Subject{
		ID: 1002, Name: "示例作品 OVA", NameCn: "示例作品 OVA",
		Year: 2021, Media: matching.KindOVA, EpisodeCount: 1, Detailed: true,
	}
	f.putSubject(ova, "示例作品")

	sess := newMatchingSessionForTest(context.Background(), rt, f)
	// 构造 OVA 组：用 ParseFile 可能抽不出 Media=OVA，直接改 Group.Query
	g := groupFromFile(t, "示例作品 OVA - 01.mkv", "show")
	g.Query.Media = matching.KindOVA
	g.Query.Titles = []matching.Title{{Text: "示例作品", Source: matching.SourceFile}}
	g.Query.HasFileTitle = true
	if len(g.Files) > 0 {
		g.Files[0].Media = matching.KindOVA
		g.Files[0].Titles = g.Query.Titles
	}

	bound := &BoundMatch{Subject: tvBound, UniqueDirectory: true}
	res, err := sess.Resolve(g, bound)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Decision.Code == matching.Accepted && res.Decision.WinnerID == tvBound.ID {
		t.Fatalf("OVA must not append to TV bound via fast path; got %+v", res.Decision)
	}
	if res.FromExistingBound {
		t.Fatal("FromExistingBound must be false when structure conflicts")
	}
}

func TestMatchingSessionOrderIndependent(t *testing.T) {
	rt := testSessionRuntime(t)

	mk := func() *fakeSessionProvider {
		f := newFakeSessionProvider()
		xianWangFixtures(f)
		return f
	}

	file := "[GM-Team][国漫][仙王的日常生活 第4季][Xian Wang De Ri Chang Sheng Huo Ⅳ][2023][12][AVC][GB][1080P].mp4"
	g := groupFromFile(t, file, "xw/order")

	// 顺序 A：正常 Resolve
	f1 := mk()
	s1 := newMatchingSessionForTest(context.Background(), rt, f1)
	r1, err := s1.Resolve(g, nil)
	if err != nil {
		t.Fatalf("A: %v", err)
	}

	// 顺序 B：先用另一查询预热 Session 缓存（不同遍历/缓存顺序）
	f2 := mk()
	s2 := newMatchingSessionForTest(context.Background(), rt, f2)
	// 预热：先搜英文再 Resolve 中文组
	_, _ = f2.Search(context.Background(), "The Daily Life of the Immortal King")
	_, _ = f2.Detail(context.Background(), 405214)
	r2, err := s2.Resolve(g, nil)
	if err != nil {
		t.Fatalf("B: %v", err)
	}

	if r1.Decision.Code != matching.Accepted || r2.Decision.Code != matching.Accepted {
		t.Fatalf("both should accept: A=%s B=%s", r1.Decision.Code, r2.Decision.Code)
	}
	if r1.Decision.WinnerID != r2.Decision.WinnerID {
		t.Fatalf("order-dependent winners: %d vs %d", r1.Decision.WinnerID, r2.Decision.WinnerID)
	}
	if r1.Decision.WinnerID != 405214 {
		t.Fatalf("want 405214, got %d", r1.Decision.WinnerID)
	}
}

func TestMatchingSessionResolveForTargetManual(t *testing.T) {
	rt := testSessionRuntime(t)
	f := newFakeSessionProvider()
	// 低分差场景：两候选接近，自动 Review，显式目标可确认
	a := matching.Subject{
		ID: 201, Name: "目标番剧", NameCn: "目标番剧",
		Year: 2022, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	b := matching.Subject{
		ID: 202, Name: "目标番剧 别裁", NameCn: "目标番剧 别裁",
		Year: 2022, Media: matching.KindSeries, EpisodeCount: 12, Detailed: true,
	}
	f.putSubject(a, "目标番剧")
	f.putSubject(b, "目标番剧")
	f.chapters[201] = []matching.Chapter{{ID: 1, Ep: 1, Sort: 1}}
	f.chapters[202] = []matching.Chapter{{ID: 2, Ep: 1, Sort: 1}}

	sess := newMatchingSessionForTest(context.Background(), rt, f)
	g := groupFromFile(t, "目标番剧 - 01.mkv", "tgt")
	res, err := sess.ResolveForTarget(g, a)
	if err != nil {
		t.Fatalf("ResolveForTarget: %v", err)
	}
	if res.Decision.Code != matching.Accepted || res.Decision.WinnerID != 201 {
		t.Fatalf("want accept 201, got code=%s id=%d reason=%s cands=%v",
			res.Decision.Code, res.Decision.WinnerID, res.Decision.Reason, summarizeCands(res.Decision.Candidates))
	}
	if !res.ManuallyConfirmed {
		t.Fatal("ResolveForTarget Accepted must set ManuallyConfirmed")
	}
}

func summarizeCands(cs []matching.Candidate) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, fmt.Sprintf("{id=%d name=%.2f score=%.2f block=%v miss=%v}",
			c.Subject.ID, c.NameScore, c.Score, c.Blockers, c.Missing))
	}
	return strings.Join(parts, " ")
}
