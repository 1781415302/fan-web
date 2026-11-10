package services

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"fan-web/services/matching"
)

// 真实 Bangumi 数据复刻（Re:Zero）：袭击篇 425998 sort51–58；反击篇 510728 sort59–66；
// 丧失篇 547888 ep1–11/sort67–77；夺还篇 633836 ep1–8/sort78–85。
const (
	anchorAttackID   = 425998
	anchorCounterID  = 510728
	anchorLossID     = 547888
	anchorRecoveryID = 633836
)

func anchorChapters(subjectID, eps, firstSort int) []matching.Chapter {
	out := make([]matching.Chapter, 0, eps)
	for i := 1; i <= eps; i++ {
		out = append(out, matching.Chapter{ID: subjectID*100 + i, Ep: float64(i), Sort: float64(firstSort + i - 1)})
	}
	return out
}

func anchorSubjects() map[int]matching.Subject {
	return map[int]matching.Subject{
		anchorAttackID:   {ID: anchorAttackID, Name: "Re:ゼロから始める異世界生活 3rd season 襲撃編", NameCn: "Re：从零开始的异世界生活 第三季 袭击篇", Media: matching.KindSeries, EpisodeCount: 8},
		anchorCounterID:  {ID: anchorCounterID, Name: "Re:ゼロから始める異世界生活 3rd season 反撃編", NameCn: "Re：从零开始的异世界生活 第三季 反击篇", Media: matching.KindSeries, EpisodeCount: 8},
		anchorLossID:     {ID: anchorLossID, Name: "Re:ゼロから始める異世界生活 4th season 喪失編", NameCn: "Re：从零开始的异世界生活 第四季 丧失篇", Media: matching.KindSeries, EpisodeCount: 11},
		anchorRecoveryID: {ID: anchorRecoveryID, Name: "Re:ゼロから始める異世界生活 4th season 奪還編", NameCn: "Re：从零开始的异世界生活 第四季 夺还篇", Media: matching.KindSeries, EpisodeCount: 8},
	}
}

func newAnchorSession(t *testing.T) (*MatchingSession, *fakeSessionProvider) {
	t.Helper()
	f := newFakeSessionProvider()
	for _, sub := range anchorSubjects() {
		f.putSubject(sub)
	}
	f.chapters[anchorAttackID] = anchorChapters(anchorAttackID, 8, 51)
	f.chapters[anchorCounterID] = anchorChapters(anchorCounterID, 8, 59)
	f.chapters[anchorLossID] = anchorChapters(anchorLossID, 11, 67)
	f.chapters[anchorRecoveryID] = anchorChapters(anchorRecoveryID, 8, 78)
	return newMatchingSessionForTest(context.Background(), testSessionRuntime(t), f), f
}

// anchorGroup：Re:Zero 第四季文件 4th - from..to；absFrom>0 时附总集数。
func anchorGroup(season, from, to, absFrom int) matching.Group {
	g := matching.Group{Key: "rezero/s4", Dir: "rezero/s4"}
	var eps []int
	for n := from; n <= to; n++ {
		f := matching.FileEvidence{
			Name:    fmt.Sprintf("[晚街与灯][Re Zero kara Hajimeru Isekai Seikatsu][4th - %02d][WebRip].mp4", n),
			Ordinal: matching.Ordinal{Season: season},
			Media:   matching.KindSeries,
			Episode: matching.EpisodeToken{Kind: matching.NumberSingle, First: n, Last: n, Explicit: true},
		}
		if absFrom > 0 {
			f.AbsEpisode = absFrom + (n - from)
		}
		g.Files = append(g.Files, f)
		eps = append(eps, n)
	}
	g.Query = matching.Query{
		Titles:       []matching.Title{{Text: "Re Zero kara Hajimeru Isekai Seikatsu", Source: matching.SourceFile}},
		Ordinal:      matching.Ordinal{Season: season},
		Media:        matching.KindSeries,
		Episodes:     eps,
		HasFileTitle: true,
	}
	return g
}

func anchorCandidate(id int, nameScore float64, blockers ...string) matching.Candidate {
	return matching.Candidate{Subject: anchorSubjects()[id], NameScore: nameScore, Score: nameScore, Blockers: blockers}
}

// 目标在前；丧失篇同季高身份；第三季两部不同季。
func anchorRanked() []matching.Candidate {
	return []matching.Candidate{
		anchorCandidate(anchorRecoveryID, 1.0),
		anchorCandidate(anchorLossID, 1.0),
		anchorCandidate(anchorAttackID, 0.95),
		anchorCandidate(anchorCounterID, 0.95),
	}
}

func assertRecoveryConfirmed(t *testing.T, d matching.ChapterDecision, wantBasis string) {
	t.Helper()
	if d.State != matching.ChapterConfirmed || len(d.Rejected) != 0 || len(d.Bindings) != 8 {
		t.Fatalf("state=%s bindings=%v rejected=%v", d.State, d.Bindings, d.Rejected)
	}
	for i, b := range d.Bindings {
		if b.Basis != wantBasis || b.StoredNumber != i+1 || b.ChapterID != anchorRecoveryID*100+i+1 {
			t.Fatalf("binding[%d]=%+v want basis=%s stored=%d", i, b, wantBasis, i+1)
		}
	}
	want := "[1 2 3 4 5 6 7 8]"
	if fmt.Sprint(d.Coverage.Required) != want || fmt.Sprint(d.Coverage.Matched) != want || len(d.Coverage.Missing) != 0 {
		t.Fatalf("coverage=%+v", d.Coverage)
	}
}

func chaptersCallCount(f *fakeSessionProvider, id int) int {
	n := 0
	for _, c := range f.chaptersCalls {
		if c == id {
			n++
		}
	}
	return n
}

// ② 经 probeChapters → interpretWithAnchor：兄弟集合={夺还篇, 丧失篇}，锚点 67 → season_sort 1..8。
func TestInterpretWithAnchorSeasonSiblingsRecovery(t *testing.T) {
	s, f := newAnchorSession(t)
	g := anchorGroup(4, 12, 19, 0)
	d, err := s.probeChapters(g, anchorRecoveryID, g.Query, anchorRanked())
	if err != nil {
		t.Fatalf("probeChapters: %v", err)
	}
	assertRecoveryConfirmed(t, d, "season_sort")
	if chaptersCallCount(f, anchorLossID) != 1 {
		t.Fatalf("same-season sibling must be fetched once: %v", f.chaptersCalls)
	}
	if chaptersCallCount(f, anchorAttackID) != 0 || chaptersCallCount(f, anchorCounterID) != 0 {
		t.Fatalf("other-season subjects must not be fetched: %v", f.chaptersCalls)
	}
}

// ① 经会话：带总集数 → abs_sort；原路径（零值）已可解释时不额外拉兄弟章节。
func TestInterpretWithAnchorAbsEpisodeNoSiblingFetch(t *testing.T) {
	s, f := newAnchorSession(t)
	g := anchorGroup(4, 12, 19, 78)
	d, err := s.probeChapters(g, anchorRecoveryID, g.Query, anchorRanked())
	if err != nil {
		t.Fatalf("probeChapters: %v", err)
	}
	assertRecoveryConfirmed(t, d, "abs_sort")
	if chaptersCallCount(f, anchorLossID) != 0 {
		t.Fatalf("no sibling fetch needed when zero-option result has no rejection: %v", f.chaptersCalls)
	}
}

// ④ 经会话：丧失篇 4th - 1..11 走原 ep 路径，与 InterpretChapters 逐字节一致，且不拉兄弟章节。
func TestInterpretWithAnchorOriginalEpPathUnchanged(t *testing.T) {
	s, f := newAnchorSession(t)
	g := anchorGroup(4, 1, 11, 0)
	ranked := []matching.Candidate{anchorCandidate(anchorLossID, 1.0), anchorCandidate(anchorRecoveryID, 1.0)}
	d, err := s.probeChapters(g, anchorLossID, g.Query, ranked)
	if err != nil {
		t.Fatalf("probeChapters: %v", err)
	}
	sub := anchorSubjects()[anchorLossID]
	sub.Detailed = true
	want := matching.InterpretChapters(g.Files, sub, anchorChapters(anchorLossID, 11, 67), true)
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("changed:\n got=%#v\nwant=%#v", d, want)
	}
	for _, b := range d.Bindings {
		if b.Basis != "ep" {
			t.Fatalf("basis=%s want ep", b.Basis)
		}
	}
	if chaptersCallCount(f, anchorRecoveryID) != 0 {
		t.Fatalf("sibling must not be fetched: %v", f.chaptersCalls)
	}
}

// ⑥a 兄弟章节获取失败 → 忽略该兄弟、不抛错；锚点只由目标自身得出（78+12-1=89）→ 无命中 → 与旧行为相同。
func TestInterpretWithAnchorSiblingFetchFailureFallsBack(t *testing.T) {
	s, f := newAnchorSession(t)
	f.chaptersNotFound = map[int]bool{anchorLossID: true}
	g := anchorGroup(4, 12, 19, 0)
	d, err := s.probeChapters(g, anchorRecoveryID, g.Query, anchorRanked())
	if err != nil {
		t.Fatalf("sibling failure must not propagate: %v", err)
	}
	sub := anchorSubjects()[anchorRecoveryID]
	sub.Detailed = true
	old := matching.InterpretChapters(g.Files, sub, anchorChapters(anchorRecoveryID, 8, 78), true)
	if !reflect.DeepEqual(d, old) {
		t.Fatalf("must equal old behavior:\n got=%#v\nwant=%#v", d, old)
	}
	if d.State != matching.ChapterConflict || len(d.Bindings) != 0 {
		t.Fatalf("must fail safely: %s %v", d.State, d.Bindings)
	}
	if s.isUnavailable(anchorLossID) {
		t.Fatal("sibling failure must not mark subject unavailable")
	}
	// 目标章节也为空 + 兄弟全部失败 → anchor=0
	if got := s.seasonSortAnchor(sub, nil, g.Query, anchorRanked()); got != 0 {
		t.Fatalf("all failed anchor=%d want 0", got)
	}
}

// ⑥b 无同季兄弟（ranked 中只有第三季）→ 锚点仅由目标得出，结果与旧行为相同；不拉异季章节。
func TestInterpretWithAnchorNoSameSeasonSiblingFallsBack(t *testing.T) {
	s, f := newAnchorSession(t)
	g := anchorGroup(4, 12, 19, 0)
	ranked := []matching.Candidate{
		anchorCandidate(anchorRecoveryID, 1.0),
		anchorCandidate(anchorAttackID, 0.95),
		anchorCandidate(anchorCounterID, 0.95),
	}
	sub := anchorSubjects()[anchorRecoveryID]
	sub.Detailed = true
	if got := s.seasonSortAnchor(sub, anchorChapters(anchorRecoveryID, 8, 78), g.Query, ranked); got != 78 {
		t.Fatalf("target-only anchor=%d want 78", got)
	}
	d, err := s.probeChapters(g, anchorRecoveryID, g.Query, ranked)
	if err != nil {
		t.Fatalf("probeChapters: %v", err)
	}
	old := matching.InterpretChapters(g.Files, sub, anchorChapters(anchorRecoveryID, 8, 78), true)
	if !reflect.DeepEqual(d, old) {
		t.Fatalf("must equal old behavior:\n got=%#v\nwant=%#v", d, old)
	}
	if chaptersCallCount(f, anchorAttackID) != 0 || chaptersCallCount(f, anchorCounterID) != 0 {
		t.Fatalf("other-season subjects must not be fetched: %v", f.chaptersCalls)
	}
}

// ⑥c 查询无季号 → anchor=0 → 旧行为；不拉任何兄弟章节。
func TestInterpretWithAnchorNoQuerySeasonFallsBack(t *testing.T) {
	s, f := newAnchorSession(t)
	g := anchorGroup(0, 12, 19, 0)
	sub := anchorSubjects()[anchorRecoveryID]
	sub.Detailed = true
	if got := s.seasonSortAnchor(sub, anchorChapters(anchorRecoveryID, 8, 78), g.Query, anchorRanked()); got != 0 {
		t.Fatalf("anchor=%d want 0", got)
	}
	d, err := s.probeChapters(g, anchorRecoveryID, g.Query, anchorRanked())
	if err != nil {
		t.Fatalf("probeChapters: %v", err)
	}
	old := matching.InterpretChapters(g.Files, sub, anchorChapters(anchorRecoveryID, 8, 78), true)
	if !reflect.DeepEqual(d, old) {
		t.Fatalf("must equal old behavior:\n got=%#v\nwant=%#v", d, old)
	}
	if chaptersCallCount(f, anchorLossID) != 0 {
		t.Fatalf("no sibling fetch without query season: %v", f.chaptersCalls)
	}
}

// 兄弟筛选：NameScore<0.88、有 Blockers、非 series 均排除；最多 3 个（含目标，目标优先）。
func TestSeasonSortAnchorSiblingFilterAndCap(t *testing.T) {
	s, f := newAnchorSession(t)
	g := anchorGroup(4, 12, 19, 0)
	recovery := anchorSubjects()[anchorRecoveryID]
	recoveryChs := anchorChapters(anchorRecoveryID, 8, 78)

	lowScore := []matching.Candidate{anchorCandidate(anchorRecoveryID, 1.0), anchorCandidate(anchorLossID, 0.87)}
	if got := s.seasonSortAnchor(recovery, recoveryChs, g.Query, lowScore); got != 78 {
		t.Fatalf("low NameScore sibling must be excluded: anchor=%d", got)
	}
	blocked := []matching.Candidate{anchorCandidate(anchorRecoveryID, 1.0), anchorCandidate(anchorLossID, 1.0, "年份冲突")}
	if got := s.seasonSortAnchor(recovery, recoveryChs, g.Query, blocked); got != 78 {
		t.Fatalf("blocked sibling must be excluded: anchor=%d", got)
	}
	movie := anchorCandidate(anchorLossID, 1.0)
	movie.Subject.Media = matching.KindMovie
	if got := s.seasonSortAnchor(recovery, recoveryChs, g.Query, []matching.Candidate{movie}); got != 78 {
		t.Fatalf("non-series sibling must be excluded: anchor=%d", got)
	}
	if chaptersCallCount(f, anchorLossID) != 0 {
		t.Fatalf("excluded siblings must not be fetched: %v", f.chaptersCalls)
	}

	// 上限：目标 + 2 个同季兄弟；第 3 个同季兄弟（sort 1 起）不被取用
	const extraA, extraB = 900001, 900002
	f.chapters[extraA] = anchorChapters(extraA, 3, 60)
	f.chapters[extraB] = anchorChapters(extraB, 3, 1)
	mk := func(id int) matching.Candidate {
		return matching.Candidate{Subject: matching.Subject{ID: id, Name: fmt.Sprintf("Re:ゼロから始める異世界生活 4th season %d", id), Media: matching.KindSeries}, NameScore: 0.9}
	}
	capped := []matching.Candidate{anchorCandidate(anchorLossID, 1.0), mk(extraA), mk(extraB)}
	if got := s.seasonSortAnchor(recovery, recoveryChs, g.Query, capped); got != 60 {
		t.Fatalf("anchor=%d want 60 (target+loss+extraA, extraB beyond cap)", got)
	}
	if chaptersCallCount(f, extraB) != 0 {
		t.Fatalf("sibling beyond cap must not be fetched: %v", f.chaptersCalls)
	}
}
