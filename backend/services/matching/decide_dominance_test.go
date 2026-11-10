package matching

import (
	"math"
	"testing"
)

// Plan §4.6 dominance：强等价原名（≈1.00）对衍生标题次优（≈0.92）时分差门放宽到 0.05；
// 其余情况维持 scoreGapMin。以下用例走真实 Rank → Decide，不伪造候选分数。

func dominanceFileTitles(texts ...string) []Title {
	out := make([]Title, 0, len(texts))
	for _, t := range texts {
		out = append(out, Title{Text: t, Source: SourceFile})
	}
	return out
}

func TestDecideDominanceThresholdsNotRelaxed(t *testing.T) {
	if scoreGapMin != 0.10 || scoreAcceptMin != 0.92 || nameScoreWeakCap != 0.90 || nameScoreAcceptMin != 0.88 {
		t.Fatalf("既有阈值被改动: gap=%v accept=%v weakCap=%v nameAccept=%v",
			scoreGapMin, scoreAcceptMin, nameScoreWeakCap, nameScoreAcceptMin)
	}
	if scoreGapMinDerived != 0.05 {
		t.Fatalf("scoreGapMinDerived=%v want 0.05", scoreGapMinDerived)
	}
}

func TestDecideDominanceOriginalVsDerivedAccepted(t *testing.T) {
	confirmed := ChapterDecision{State: ChapterConfirmed}
	cases := []struct {
		name     string
		query    string
		original Subject
		derived  Subject
	}{
		{
			name:     "Bleach",
			query:    "BLEACH",
			original: Subject{ID: 69, Name: "BLEACH", NameCn: "死神", Media: KindSeries, EpisodeCount: 366, Year: 2004},
			derived:  Subject{ID: 302189, Name: "BLEACH: Thousand-Year Blood War", Media: KindSeries, EpisodeCount: 13, Year: 2022},
		},
		{
			name:     "CLANNAD",
			query:    "CLANNAD",
			original: Subject{ID: 51, Name: "CLANNAD", NameCn: "CLANNAD", Media: KindSeries, EpisodeCount: 23, Year: 2007},
			derived:  Subject{ID: 876, Name: "CLANNAD ～AFTER STORY～", NameCn: "CLANNAD 后日谈", Media: KindSeries, EpisodeCount: 24, Year: 2008},
		},
		{
			name:     "Berserk",
			query:    "Berserk",
			original: Subject{ID: 1001, Name: "ベルセルク", NameCn: "剑风传奇", Aliases: []string{"Berserk"}, Media: KindSeries, EpisodeCount: 25, Year: 1997},
			derived:  Subject{ID: 1002, Name: "ベルセルク 黄金時代篇 MEMORIAL EDITION", NameCn: "剑风传奇 黄金时代篇 MEMORIAL EDITION", Aliases: []string{"Berserk: The Golden Age Arc"}, Media: KindSeries, EpisodeCount: 13, Year: 2022},
		},
		{
			name:     "Frieren",
			query:    "葬送的芙莉莲",
			original: Subject{ID: 400602, Name: "葬送のフリーレン", NameCn: "葬送的芙莉莲", Media: KindSeries, EpisodeCount: 28, Year: 2023},
			derived:  Subject{ID: 1003, Name: "葬送のフリーレン ～魔法の才能～", NameCn: "葬送的芙莉莲 ～魔法的才能～", Media: KindSeries, EpisodeCount: 12, Year: 2023},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := Query{Titles: dominanceFileTitles(tc.query), HasFileTitle: true, Media: KindSeries, Episodes: []int{5}}
			chapters := map[int]ChapterDecision{tc.original.ID: confirmed, tc.derived.ID: confirmed}
			ranked := Rank(q, []Subject{tc.original, tc.derived}, Catalog{}, chapters)
			if len(ranked) != 2 || ranked[0].Subject.ID != tc.original.ID {
				t.Fatalf("ranked=%+v", ranked)
			}
			best, second := ranked[0], ranked[1]
			if best.NameEvidence.Kind != NameFullStrict || !best.NameEvidence.DirectSupport {
				t.Fatalf("best evidence=%+v", best.NameEvidence)
			}
			if second.NameEvidence.Kind != NameSubtitle {
				t.Fatalf("second evidence=%+v", second.NameEvidence)
			}
			gap := best.Score - second.Score
			// 前提守卫：分差落在 [0.05, 0.10) 区间，旧规则会 Review。
			if math.Abs(best.Score-1.0) > 1e-9 || math.Abs(second.Score-0.92) > 1e-9 || gap >= scoreGapMin {
				t.Fatalf("前提不成立: best=%.3f second=%.3f", best.Score, second.Score)
			}
			if g := gapRequired(best, second); g != scoreGapMinDerived {
				t.Fatalf("gapRequired=%v want %v", g, scoreGapMinDerived)
			}
			d := Decide(q, ranked, true)
			if d.Code != Accepted || d.WinnerID != tc.original.ID {
				t.Fatalf("want Accepted %d, got %+v", tc.original.ID, d)
			}
		})
	}
}

func TestDecideDominanceBothFullStrictTieStillReview(t *testing.T) {
	// 两个都是 FullStrict（例如别名完全相同）平局 → 维持 scoreGapMin，仍 Review。
	q := Query{Titles: dominanceFileTitles("BLEACH"), HasFileTitle: true, Media: KindSeries}
	subs := []Subject{
		{ID: 69, Name: "BLEACH", Media: KindSeries, EpisodeCount: 366, Year: 2004},
		{ID: 302189, Name: "BLEACH 千年血戦篇", Aliases: []string{"BLEACH"}, Media: KindSeries, EpisodeCount: 13, Year: 2022},
	}
	ranked := Rank(q, subs, Catalog{}, nil)
	if len(ranked) != 2 || ranked[0].NameEvidence.Kind != NameFullStrict || ranked[1].NameEvidence.Kind != NameFullStrict {
		t.Fatalf("前提不成立: %+v", ranked)
	}
	if g := gapRequired(ranked[0], ranked[1]); g != scoreGapMin {
		t.Fatalf("gapRequired=%v want %v", g, scoreGapMin)
	}
	if d := Decide(q, ranked, true); d.Code != Review {
		t.Fatalf("FullStrict 平局必须 Review: %+v", d)
	}

	// 构造：强等价对强等价、分差 0.06（介于 0.05 与 0.10）→ 仍 Review。
	best := Candidate{Subject: Subject{ID: 1}, NameScore: 1, Score: 1,
		NameEvidence: NameEvidence{Kind: NameFullStrict, DirectSupport: true}}
	second := Candidate{Subject: Subject{ID: 2}, NameScore: 0.98, Score: 0.94,
		NameEvidence: NameEvidence{Kind: NameFullNormal, DirectSupport: true}}
	if d := Decide(q, []Candidate{best, second}, true); d.Code != Review {
		t.Fatalf("强等价对强等价不得放宽: %+v", d)
	}
}

func TestDecideDominanceReZeroArcsWithoutEpisodeStillReview(t *testing.T) {
	// 现场 A：丧失篇(547888) 与 夺还篇(633836) 名称分都 1.00，无集号证据 → 仍 Review。
	q := Query{Titles: dominanceFileTitles("Re Zero kara Hajimeru Isekai Seikatsu"), HasFileTitle: true, Media: KindSeries}
	subs := []Subject{
		{ID: 547888, Name: "Re:ゼロから始める異世界生活 4th season 喪失編", Aliases: []string{"Re Zero kara Hajimeru Isekai Seikatsu"}, Media: KindSeries, EpisodeCount: 8},
		{ID: 633836, Name: "Re:ゼロから始める異世界生活 4th season 奪還編", Aliases: []string{"Re Zero kara Hajimeru Isekai Seikatsu"}, Media: KindSeries, EpisodeCount: 8},
	}
	ranked := Rank(q, subs, Catalog{}, nil)
	if len(ranked) != 2 || ranked[0].NameScore != 1 || ranked[1].NameScore != 1 {
		t.Fatalf("前提不成立: %+v", ranked)
	}
	d := Decide(q, ranked, true)
	if d.Code != Review {
		t.Fatalf("丧失篇/夺还篇无集号证据必须 Review: %+v", d)
	}
}

func TestGapRequiredConservativeBranches(t *testing.T) {
	strong := Candidate{Subject: Subject{ID: 1}, NameScore: 1, Score: 1,
		NameEvidence: NameEvidence{Kind: NameFullStrict, DirectSupport: true}}
	derived := Candidate{Subject: Subject{ID: 2}, NameScore: 0.9, Score: 0.92,
		NameEvidence: NameEvidence{Kind: NameSubtitle}}

	for _, k := range []NameMatchKind{NameFullStrict, NameFullNormal, NameBaseStrict, NameBaseNormal, NameCatalog} {
		for _, k2 := range []NameMatchKind{NameSubtitle, NamePrefix, NameFuzzy, NameLoose} {
			b, s := strong, derived
			b.NameEvidence.Kind, s.NameEvidence.Kind = k, k2
			if g := gapRequired(b, s); g != scoreGapMinDerived {
				t.Fatalf("%s vs %s: gap=%v want %v", k, k2, g, scoreGapMinDerived)
			}
		}
	}

	cases := []struct {
		name string
		mut  func(b, s *Candidate)
	}{
		{"best 无 DirectSupport", func(b, s *Candidate) { b.NameEvidence.DirectSupport = false }},
		{"best 有 Blockers", func(b, s *Candidate) { b.Blockers = []string{"x"} }},
		{"best 有 Missing", func(b, s *Candidate) { b.Missing = []string{"x"} }},
		{"best 为 subtitle", func(b, s *Candidate) { b.NameEvidence.Kind = NameSubtitle }},
		{"best 为 none", func(b, s *Candidate) { b.NameEvidence.Kind = NameNone }},
		{"best 零值 Kind", func(b, s *Candidate) { b.NameEvidence.Kind = "" }},
		{"second 为 catalog", func(b, s *Candidate) { s.NameEvidence.Kind = NameCatalog }},
		{"second 为 base_normal", func(b, s *Candidate) { s.NameEvidence.Kind = NameBaseNormal }},
		{"second 为 none", func(b, s *Candidate) { s.NameEvidence.Kind = NameNone }},
		{"second 零值 Kind", func(b, s *Candidate) { s.NameEvidence.Kind = "" }},
	}
	for _, tc := range cases {
		b, s := strong, derived
		tc.mut(&b, &s)
		if g := gapRequired(b, s); g != scoreGapMin {
			t.Fatalf("%s: gap=%v want %v", tc.name, g, scoreGapMin)
		}
	}

	// 放宽后分差仍须 ≥ 0.05：1.00 vs 0.96 衍生 → Review。
	q := Query{HasFileTitle: true}
	closeDerived := derived
	closeDerived.Score = 0.96
	if d := Decide(q, []Candidate{strong, closeDerived}, true); d.Code != Review {
		t.Fatalf("分差 0.04 < 0.05 必须 Review: %+v", d)
	}
	// 1.00 vs 0.92 衍生 → Accepted；缺 DirectSupport 时同分差 → Review。
	if d := Decide(q, []Candidate{strong, derived}, true); d.Code != Accepted || d.WinnerID != 1 {
		t.Fatalf("want Accepted: %+v", d)
	}
	noDirect := strong
	noDirect.NameEvidence.DirectSupport = false
	if d := Decide(q, []Candidate{noDirect, derived}, true); d.Code != Review {
		t.Fatalf("无 DirectSupport 不得放宽: %+v", d)
	}
	// 零值 NameEvidence（旧测试/旧调用方式）维持原门。
	legacyBest := Candidate{Subject: Subject{ID: 1}, NameScore: 1, Score: 1}
	legacySecond := Candidate{Subject: Subject{ID: 2}, NameScore: 0.9, Score: 0.92}
	if d := Decide(q, []Candidate{legacyBest, legacySecond}, true); d.Code != Review {
		t.Fatalf("零值证据须维持 scoreGapMin: %+v", d)
	}
}
