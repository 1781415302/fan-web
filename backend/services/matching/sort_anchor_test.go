package matching

import "testing"

// 真实 Bangumi 数据复刻（Re:Zero）：
// 袭击篇 425998 ep1–8/sort51–58；反击篇 510728 ep1–8/sort59–66；
// 丧失篇 547888 ep1–11/sort67–77；夺还篇 633836 ep1–8/sort78–85。
const (
	reZeroAttackID   = 425998
	reZeroCounterID  = 510728
	reZeroLossID     = 547888
	reZeroRecoveryID = 633836
)

func reZeroChapters(subjectID, eps, firstSort int) []Chapter {
	out := make([]Chapter, 0, eps)
	for i := 1; i <= eps; i++ {
		out = append(out, Chapter{ID: subjectID*100 + i, Ep: float64(i), Sort: float64(firstSort + i - 1)})
	}
	return out
}

func reZeroLossChapters() []Chapter     { return reZeroChapters(reZeroLossID, 11, 67) }
func reZeroRecoveryChapters() []Chapter { return reZeroChapters(reZeroRecoveryID, 8, 78) }
func reZeroAttackChapters() []Chapter   { return reZeroChapters(reZeroAttackID, 8, 51) }
func reZeroCounterChapters() []Chapter  { return reZeroChapters(reZeroCounterID, 8, 59) }

func reZeroLossSubject() Subject {
	return Subject{ID: reZeroLossID, Name: "Re:ゼロから始める異世界生活 4th season 喪失編", NameCn: "Re：从零开始的异世界生活 第四季 丧失篇", Media: KindSeries, EpisodeCount: 11, Detailed: true}
}

func reZeroRecoverySubject() Subject {
	return Subject{ID: reZeroRecoveryID, Name: "Re:ゼロから始める異世界生活 4th season 奪還編", NameCn: "Re：从零开始的异世界生活 第四季 夺还篇", Media: KindSeries, EpisodeCount: 8, Detailed: true}
}

func reZeroAttackSubject() Subject {
	return Subject{ID: reZeroAttackID, Name: "Re:ゼロから始める異世界生活 3rd season 襲撃編", NameCn: "Re：从零开始的异世界生活 第三季 袭击篇", Media: KindSeries, EpisodeCount: 8, Detailed: true}
}

func reZeroCounterSubject() Subject {
	return Subject{ID: reZeroCounterID, Name: "Re:ゼロから始める異世界生活 3rd season 反撃編", NameCn: "Re：从零开始的异世界生活 第三季 反击篇", Media: KindSeries, EpisodeCount: 8, Detailed: true}
}

func TestComputeSeasonAnchorReZeroSeason4(t *testing.T) {
	got := ComputeSeasonAnchor(map[int][]Chapter{
		reZeroLossID:     reZeroLossChapters(),
		reZeroRecoveryID: reZeroRecoveryChapters(),
	})
	if got != 67 {
		t.Fatalf("anchor=%d want 67", got)
	}
	got = ComputeSeasonAnchor(map[int][]Chapter{
		reZeroAttackID:  reZeroAttackChapters(),
		reZeroCounterID: reZeroCounterChapters(),
	})
	if got != 51 {
		t.Fatalf("season3 anchor=%d want 51", got)
	}
}

func TestComputeSeasonAnchorEmptyOrNonPositive(t *testing.T) {
	if got := ComputeSeasonAnchor(nil); got != 0 {
		t.Fatalf("nil → %d want 0", got)
	}
	if got := ComputeSeasonAnchor(map[int][]Chapter{}); got != 0 {
		t.Fatalf("empty → %d want 0", got)
	}
	if got := ComputeSeasonAnchor(map[int][]Chapter{1: nil, 2: {}}); got != 0 {
		t.Fatalf("no chapters → %d want 0", got)
	}
	if got := ComputeSeasonAnchor(map[int][]Chapter{1: {{ID: 1, Ep: 0, Sort: 0}, {ID: 2, Ep: 1, Sort: -3}}}); got != 0 {
		t.Fatalf("non-positive sort → %d want 0", got)
	}
	// 正值与非正值混合：忽略非正值
	if got := ComputeSeasonAnchor(map[int][]Chapter{1: {{ID: 1, Sort: 0}, {ID: 2, Ep: 1, Sort: 13}}, 2: {{ID: 3, Ep: 1, Sort: 25}}}); got != 13 {
		t.Fatalf("mixed → %d want 13", got)
	}
}

func TestResolveAbsOrAnchorOrder(t *testing.T) {
	chs := reZeroRecoveryChapters()
	// abs 优先于锚点
	ch, basis, ok := ResolveAbsOrAnchor(12, 79, chs, ChapterOptions{SeasonSortAnchor: 67})
	if !ok || basis != "abs_sort" || ch.Sort != 79 || ch.Ep != 2 {
		t.Fatalf("abs first: ok=%v basis=%s ch=%+v", ok, basis, ch)
	}
	// abs 无命中 → 锚点
	ch, basis, ok = ResolveAbsOrAnchor(12, 999, chs, ChapterOptions{SeasonSortAnchor: 67})
	if !ok || basis != "season_sort" || ch.Sort != 78 || ch.Ep != 1 {
		t.Fatalf("anchor: ok=%v basis=%s ch=%+v", ok, basis, ch)
	}
	// 均无 → 未命中
	if _, _, ok = ResolveAbsOrAnchor(12, 0, chs, ChapterOptions{}); ok {
		t.Fatal("zero options must not hit")
	}
	// 锚点仅由目标自身得出（78+12-1=89）→ 无命中，安全失败
	if _, _, ok = ResolveAbsOrAnchor(12, 0, chs, ChapterOptions{SeasonSortAnchor: 78}); ok {
		t.Fatal("target-only anchor must not hit")
	}
}

func TestResolveAbsOrAnchorRequiresUniqueSortAndPositiveIntegerEp(t *testing.T) {
	dup := []Chapter{{ID: 1, Ep: 1, Sort: 78}, {ID: 2, Ep: 2, Sort: 78}}
	if _, _, ok := ResolveAbsOrAnchor(12, 78, dup, ChapterOptions{SeasonSortAnchor: 67}); ok {
		t.Fatal("non-unique sort must not hit")
	}
	badEp := []Chapter{{ID: 1, Ep: 0, Sort: 78}, {ID: 2, Ep: 1.5, Sort: 79}}
	if _, _, ok := ResolveAbsOrAnchor(12, 78, badEp, ChapterOptions{}); ok {
		t.Fatal("Ep=0 must not yield StoredNumber")
	}
	if _, _, ok := ResolveAbsOrAnchor(13, 0, badEp, ChapterOptions{SeasonSortAnchor: 67}); ok {
		t.Fatal("fractional Ep must not yield StoredNumber")
	}
}

func TestSubjectSeasonReZero(t *testing.T) {
	cases := []struct {
		sub  Subject
		want int
	}{
		{reZeroLossSubject(), 4},
		{reZeroRecoverySubject(), 4},
		{reZeroAttackSubject(), 3},
		{reZeroCounterSubject(), 3},
		{Subject{ID: 1, Name: "Re:ゼロから始める異世界生活", Media: KindSeries}, 0},
	}
	for _, tc := range cases {
		if got := SubjectSeason(tc.sub); got != tc.want {
			t.Fatalf("SubjectSeason(%s)=%d want %d", tc.sub.Name, got, tc.want)
		}
		if got := subjectOrdinal(tc.sub).Season; got != SubjectSeason(tc.sub) {
			t.Fatalf("SubjectSeason must wrap subjectOrdinal: %d vs %d", SubjectSeason(tc.sub), got)
		}
	}
}
