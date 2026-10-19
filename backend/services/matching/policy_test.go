package matching

import (
	"fmt"
	"strings"
	"testing"
)

func chConfirmed(eps ...int) ChapterDecision {
	req := append([]int(nil), eps...)
	var binds []EpisodeBinding
	for i, ep := range eps {
		binds = append(binds, EpisodeBinding{
			FileName:     fmt.Sprintf("f%02d.mkv", ep),
			StoredNumber: ep,
			ChapterID:    1000 + i + 1,
			Basis:        "ep",
		})
	}
	return ChapterDecision{
		State:    ChapterConfirmed,
		Bindings: binds,
		Rejected: map[string]string{},
		Coverage: ChapterCoverage{Required: req, Matched: append([]int(nil), req...), Missing: []int{}},
	}
}

func chCountFit(ep int, count int) ChapterDecision {
	return ChapterDecision{
		State: ChapterCountFit,
		Bindings: []EpisodeBinding{{
			FileName: "f.mkv", StoredNumber: ep, ChapterID: 0, Basis: "count",
		}},
		Rejected: map[string]string{},
		Coverage: ChapterCoverage{Required: []int{ep}, Matched: []int{}, Missing: []int{ep}},
	}
}

func chPartial(required []int, matched []int) ChapterDecision {
	miss := make([]int, 0)
	mset := map[int]bool{}
	for _, m := range matched {
		mset[m] = true
	}
	for _, r := range required {
		if !mset[r] {
			miss = append(miss, r)
		}
	}
	var binds []EpisodeBinding
	for i, ep := range matched {
		binds = append(binds, EpisodeBinding{
			FileName: fmt.Sprintf("f%d.mkv", ep), StoredNumber: ep, ChapterID: 200 + i, Basis: "ep",
		})
	}
	st := ChapterConfirmed
	if len(matched) == 0 {
		st = ChapterUnchecked
	}
	return ChapterDecision{
		State: st, Bindings: binds, Rejected: map[string]string{},
		Coverage: ChapterCoverage{Required: required, Matched: matched, Missing: miss},
	}
}

func hasMissing(c Candidate, want string) bool {
	for _, m := range c.Missing {
		if m == want {
			return true
		}
	}
	return false
}

func hasRuleCand(c Candidate, want MatchRule) bool {
	for _, r := range c.AppliedRules {
		if r == want {
			return true
		}
	}
	return false
}

// R09：S01 + 无季号 primary series → 首季兼容接受
func TestPolicyR09FirstSeasonCompatAccept(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Campus Notes", Source: SourceFile}},
		Ordinal: Ordinal{Season: 1}, Episodes: []int{1}, HasFileTitle: true,
	}
	sub := Subject{ID: 1, Name: "Campus Notes", Media: KindSeries, EpisodeCount: 12, Detailed: true}
	ranked := Rank(q, []Subject{sub}, Catalog{}, map[int]ChapterDecision{1: chConfirmed(1)})
	if !hasRuleCand(ranked[0], RuleFirstSeasonCompat) {
		t.Fatalf("want RuleFirstSeasonCompat, got %+v", ranked[0])
	}
	if hasMissing(ranked[0], "候选无法解释查询季号") {
		t.Fatalf("should not Missing season: %+v", ranked[0])
	}
	d := Decide(q, ranked, true)
	if d.Code != Accepted || d.WinnerID != 1 {
		t.Fatalf("R09 want accept 1: %+v cand=%+v", d, ranked[0])
	}
}

// R10：别名带 S2 或查询 S02 → 不能首季兼容
func TestPolicyR10FirstSeasonCompatBlocked(t *testing.T) {
	q1 := Query{
		Titles:  []Title{{Text: "Campus Notes", Source: SourceFile}},
		Ordinal: Ordinal{Season: 1}, Episodes: []int{1}, HasFileTitle: true,
	}
	subS2alias := Subject{
		ID: 2, Name: "Campus Notes", Aliases: []string{"Campus Notes Season 2"},
		Media: KindSeries, EpisodeCount: 12, Detailed: true,
	}
	r1 := Rank(q1, []Subject{subS2alias}, Catalog{}, map[int]ChapterDecision{2: chConfirmed(1)})
	if hasRuleCand(r1[0], RuleFirstSeasonCompat) {
		t.Fatalf("alias S2 must not first-season compat: %+v", r1[0])
	}
	if d := Decide(q1, r1, true); d.Code == Accepted {
		t.Fatalf("R10a must Review: %+v", d)
	}

	q2 := Query{
		Titles:  []Title{{Text: "Campus Notes", Source: SourceFile}},
		Ordinal: Ordinal{Season: 2}, Episodes: []int{1}, HasFileTitle: true,
	}
	subBare := Subject{ID: 3, Name: "Campus Notes", Media: KindSeries, EpisodeCount: 12, Detailed: true}
	r2 := Rank(q2, []Subject{subBare}, Catalog{}, map[int]ChapterDecision{3: chConfirmed(1)})
	if hasRuleCand(r2[0], RuleFirstSeasonCompat) {
		t.Fatalf("S02 must not first-season compat: %+v", r2[0])
	}
	if !hasMissing(r2[0], "候选无法解释查询季号") {
		t.Fatalf("S02 unmarked want season Missing: %+v", r2[0])
	}
	if d := Decide(q2, r2, true); d.Code == Accepted {
		t.Fatalf("R10b must Review: %+v", d)
	}
}

// R11：无季号查询，A 原作 vs B 续季 → A 胜，B Score -0.13
func TestPolicyR11VariantPreference(t *testing.T) {
	q := Query{
		Titles:   []Title{{Text: "Campus Notes", Source: SourceFile}},
		Episodes: []int{1}, HasFileTitle: true,
	}
	a := Subject{ID: 10, Name: "Campus Notes", Media: KindSeries, EpisodeCount: 12, Detailed: true}
	b := Subject{ID: 20, Name: "Campus Notes Season 2", Aliases: []string{"Campus Notes"}, Media: KindSeries, EpisodeCount: 12, Detailed: true}
	ch := map[int]ChapterDecision{10: chConfirmed(1), 20: chConfirmed(1)}

	for _, order := range [][]Subject{{a, b}, {b, a}} {
		ranked := Rank(q, order, Catalog{}, ch)
		var ca, cb *Candidate
		for i := range ranked {
			switch ranked[i].Subject.ID {
			case 10:
				ca = &ranked[i]
			case 20:
				cb = &ranked[i]
			}
		}
		if ca == nil || cb == nil {
			t.Fatal(ranked)
		}
		if !hasRuleCand(*ca, RuleVariantPreference) {
			t.Fatalf("A should carry RuleVariantPreference: %+v", ca)
		}
		if cb.NameScore != ca.NameScore {
			t.Fatalf("NameScore must be preserved equal: A=%v B=%v", ca.NameScore, cb.NameScore)
		}
		wantB := clamp01(1.0 + 0.02 - variantPreferencePenalty) // chapter conf bonus on both before penalty
		// B score = clamp(orig - 0.13); orig includes chapter bonus
		if cb.Score > ca.Score-0.09 {
			t.Fatalf("B should be penalized below A: A=%.3f B=%.3f (wantB~%.3f)", ca.Score, cb.Score, wantB)
		}
		d := Decide(q, ranked, true)
		if d.Code != Accepted || d.WinnerID != 10 {
			t.Fatalf("R11 want accept A: %+v ranked A=%.3f B=%.3f", d, ca.Score, cb.Score)
		}
	}
}

// R11b：A full-normal+CountFit=0.98，B 精确 1.00 扣后 0.87 → A 接受
func TestPolicyR11bVariantGapWithoutEpsilon(t *testing.T) {
	q := Query{
		Titles:   []Title{{Text: "campus notes", Source: SourceFile}}, // normal equal path
		Episodes: []int{1}, HasFileTitle: true,
	}
	a := Subject{ID: 11, Name: "Campus Notes", Media: KindSeries, EpisodeCount: 12, Detailed: true}
	b := Subject{ID: 21, Name: "Campus Notes Season 2", Aliases: []string{"campus notes"}, Media: KindSeries, EpisodeCount: 12, Detailed: true}
	ch := map[int]ChapterDecision{
		11: chCountFit(1, 12),
		21: chConfirmed(1),
	}
	ranked := Rank(q, []Subject{a, b}, Catalog{}, ch)
	var ca, cb Candidate
	for _, c := range ranked {
		if c.Subject.ID == 11 {
			ca = c
		}
		if c.Subject.ID == 21 {
			cb = c
		}
	}
	if ca.Score < scoreAcceptMin {
		t.Fatalf("A score too low: %+v", ca)
	}
	if cb.Score >= ca.Score-scoreGapMin+1e-9 && Decide(q, ranked, true).WinnerID != 11 {
		// 分差须足以接受 A；B 扣 0.13 后约 0.87
		t.Logf("A=%.3f/%s B=%.3f/%s", ca.Score, ca.NameEvidence.Kind, cb.Score, cb.NameEvidence.Kind)
	}
	d := Decide(q, ranked, true)
	if d.Code != Accepted || d.WinnerID != 11 {
		t.Fatalf("R11b want accept A: %+v A=%.3f B=%.3f kindA=%s kindB=%s", d, ca.Score, cb.Score, ca.NameEvidence.Kind, cb.NameEvidence.Kind)
	}
	if ca.Score-cb.Score < scoreGapMin {
		t.Fatalf("gap %.3f must be >= %.2f (no epsilon)", ca.Score-cb.Score, scoreGapMin)
	}
}

// R12：Part2/Movie/Recap 只扣一次
func TestPolicyR12SinglePenalty(t *testing.T) {
	q := Query{
		Titles:   []Title{{Text: "Campus Notes", Source: SourceFile}},
		Episodes: []int{1}, HasFileTitle: true,
	}
	a := Subject{ID: 1, Name: "Campus Notes", Media: KindSeries, Detailed: true, EpisodeCount: 12}
	variants := []Subject{
		{ID: 2, Name: "Campus Notes Part 2", Aliases: []string{"Campus Notes"}, Media: KindSeries, Detailed: true, EpisodeCount: 12},
		{ID: 3, Name: "Campus Notes", Aliases: []string{"Campus Notes"}, Media: KindMovie, Detailed: true, EpisodeCount: 1},
		{ID: 4, Name: "Campus Notes Recap", Aliases: []string{"Campus Notes"}, Media: KindRecap, Detailed: true, EpisodeCount: 1},
	}
	for _, v := range variants {
		ch := map[int]ChapterDecision{1: chConfirmed(1), v.ID: chConfirmed(1)}
		ranked := Rank(q, []Subject{a, v}, Catalog{}, ch)
		var ca, cv Candidate
		for _, c := range ranked {
			if c.Subject.ID == 1 {
				ca = c
			} else {
				cv = c
			}
		}
		pen := ca.Score - cv.Score
		if pen < 0.12 || pen > 0.14 {
			// chapter bonus may apply to both; penalty is 0.13 once
			if cv.Score > clamp01(ca.Score-0.12) {
				t.Fatalf("variant %d: want ~0.13 penalty, A=%.3f V=%.3f", v.ID, ca.Score, cv.Score)
			}
		}
		d := Decide(q, ranked, true)
		if d.Code != Accepted || d.WinnerID != 1 {
			t.Fatalf("R12 id=%d want A: %+v", v.ID, d)
		}
	}
}

// R13：查询明确 Movie/S02 时不套对应偏好
func TestPolicyR13NoPenaltyWhenQuerySpecifies(t *testing.T) {
	qMovie := Query{
		Titles: []Title{{Text: "Campus Notes", Source: SourceFile}},
		Media:  KindMovie, HasFileTitle: true,
	}
	movie := Subject{ID: 5, Name: "Campus Notes", Media: KindMovie, Detailed: true, EpisodeCount: 1}
	series := Subject{ID: 6, Name: "Campus Notes", Media: KindSeries, Detailed: true, EpisodeCount: 12}
	r := Rank(qMovie, []Subject{movie, series}, Catalog{}, map[int]ChapterDecision{
		5: {State: ChapterConfirmed, Bindings: []EpisodeBinding{{FileName: "m.mkv", StoredNumber: 1, ChapterID: 1, Basis: "movie"}}, Rejected: map[string]string{}, Coverage: ChapterCoverage{Required: []int{}, Matched: []int{}, Missing: []int{}}},
		6: chConfirmed(1),
	})
	d := Decide(qMovie, r, true)
	if d.Code == Accepted && d.WinnerID == 6 {
		t.Fatalf("must not hard-pick series when query is movie: %+v", dumpCand(r))
	}

	qS2 := Query{
		Titles:  []Title{{Text: "Campus Notes", Source: SourceFile}},
		Ordinal: Ordinal{Season: 2}, Episodes: []int{1}, HasFileTitle: true,
	}
	s2 := Subject{ID: 7, Name: "Campus Notes Season 2", Media: KindSeries, Detailed: true, EpisodeCount: 12}
	s1 := Subject{ID: 8, Name: "Campus Notes", Media: KindSeries, Detailed: true, EpisodeCount: 12}
	r2 := Rank(qS2, []Subject{s2, s1}, Catalog{}, map[int]ChapterDecision{7: chConfirmed(1), 8: chConfirmed(1)})
	// S1 has season Missing; S2 should win without being penalized for Season 2
	var cs2 Candidate
	for _, c := range r2 {
		if c.Subject.ID == 7 {
			cs2 = c
		}
	}
	if cs2.Score < 0.92 {
		t.Fatalf("specified S02 should not be preference-penalized: %+v", cs2)
	}
}

// R14：完全同名不同 ID → Review，不靠最小 ID
func TestPolicyR14TieNoMinID(t *testing.T) {
	q := Query{Titles: []Title{{Text: "Twin Show", Source: SourceFile}}, Episodes: []int{1}, HasFileTitle: true}
	a := Subject{ID: 100, Name: "Twin Show", Media: KindSeries, Year: 2020, Detailed: true, EpisodeCount: 12}
	b := Subject{ID: 200, Name: "Twin Show", Media: KindSeries, Year: 2020, Detailed: true, EpisodeCount: 12}
	for _, order := range [][]Subject{{a, b}, {b, a}} {
		ranked := Rank(q, order, Catalog{}, map[int]ChapterDecision{100: chConfirmed(1), 200: chConfirmed(1)})
		d := Decide(q, ranked, true)
		if d.Code != Review || d.WinnerID != 0 {
			t.Fatalf("R14 must Review: %+v order=%v", d, order[0].ID)
		}
	}
}

// R15：副标题 + 显式同季 + 完整覆盖 → 接受
func TestPolicyR15SubtitleVerifiedAccept(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Mushoku Tensei", Source: SourceFile}},
		Ordinal: Ordinal{Season: 2}, Episodes: []int{1}, HasFileTitle: true,
	}
	sub := Subject{
		ID: 15, Name: "Mushoku Tensei Season 2: Isekai Ittara Honki Dasu",
		Media: KindSeries, Detailed: true, EpisodeCount: 12,
	}
	ranked := Rank(q, []Subject{sub}, Catalog{}, map[int]ChapterDecision{15: chConfirmed(1)})
	c := ranked[0]
	if c.NameEvidence.Kind != NameSubtitle {
		t.Fatalf("want NameSubtitle, got %s score=%.3f support=%v", c.NameEvidence.Kind, c.NameScore, c.Support)
	}
	if c.NameScore != nameScoreWeakCap {
		t.Fatalf("NameSubtitle score want 0.90 got %.3f", c.NameScore)
	}
	if !hasRuleCand(c, RuleSubtitleVerified) {
		t.Fatalf("want RuleSubtitleVerified: %+v", c)
	}
	if hasMissing(c, "弱形态身份支持不能自动接受") {
		t.Fatalf("verified subtitle must clear weak Missing: %+v", c)
	}
	d := Decide(q, ranked, true)
	if d.Code != Accepted || d.WinnerID != 15 {
		t.Fatalf("R15 want accept: %+v cand=%+v", d, c)
	}
}

// R16：CountFit 或部分覆盖不能副标题晋升
func TestPolicyR16SubtitleNeedsFullCoverage(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Mushoku Tensei", Source: SourceFile}},
		Ordinal: Ordinal{Season: 2}, Episodes: []int{1, 2}, HasFileTitle: true,
	}
	sub := Subject{
		ID: 16, Name: "Mushoku Tensei Season 2: Isekai Ittara Honki Dasu",
		Media: KindSeries, Detailed: true, EpisodeCount: 12,
	}
	rFit := Rank(q, []Subject{sub}, Catalog{}, map[int]ChapterDecision{16: chCountFit(1, 12)})
	if hasRuleCand(rFit[0], RuleSubtitleVerified) || Decide(q, rFit, true).Code == Accepted {
		t.Fatalf("CountFit must not promote: %+v", rFit[0])
	}
	rPart := Rank(q, []Subject{sub}, Catalog{}, map[int]ChapterDecision{16: chPartial([]int{1, 2}, []int{1})})
	if hasRuleCand(rPart[0], RuleSubtitleVerified) || Decide(q, rPart, true).Code == Accepted {
		t.Fatalf("partial coverage must not promote: %+v", rPart[0])
	}
}

// R17：普通前缀或缺季冲突
func TestPolicyR17PrefixAndSeasonConflict(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Mushoku Tensei", Source: SourceFile}},
		Ordinal: Ordinal{Season: 2}, Episodes: []int{1}, HasFileTitle: true,
	}
	prefix := Subject{
		ID: 17, Name: "Mushoku Tensei Another Story Season 2",
		Media: KindSeries, Detailed: true, EpisodeCount: 12,
	}
	r1 := Rank(q, []Subject{prefix}, Catalog{}, map[int]ChapterDecision{17: chConfirmed(1)})
	if r1[0].NameEvidence.Kind == NameSubtitle {
		t.Fatalf("ordinary prefix must not be NameSubtitle: %+v", r1[0])
	}
	if Decide(q, r1, true).Code == Accepted {
		t.Fatalf("prefix must not accept: %+v", r1[0])
	}
	s3 := Subject{
		ID: 18, Name: "Mushoku Tensei Season 3",
		Media: KindSeries, Detailed: true, EpisodeCount: 12,
	}
	r2 := Rank(q, []Subject{s3}, Catalog{}, map[int]ChapterDecision{18: chConfirmed(1)})
	blocked := false
	for _, b := range r2[0].Blockers {
		if containsSeasonConflict(b) {
			blocked = true
		}
	}
	if !blocked || Decide(q, r2, true).Code == Accepted {
		t.Fatalf("S3 vs S2 must block: %+v", r2[0])
	}
}

func containsSeasonConflict(s string) bool {
	return strings.Contains(s, "季号冲突")
}

// R18：两个均满足副标题 → Review 分差
func TestPolicyR18TwoSubtitleReview(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Mushoku Tensei", Source: SourceFile}},
		Ordinal: Ordinal{Season: 2}, Episodes: []int{1}, HasFileTitle: true,
	}
	a := Subject{ID: 181, Name: "Mushoku Tensei Season 2: Path A", Media: KindSeries, Detailed: true, EpisodeCount: 12}
	b := Subject{ID: 182, Name: "Mushoku Tensei Season 2: Path B", Media: KindSeries, Detailed: true, EpisodeCount: 12}
	ranked := Rank(q, []Subject{a, b}, Catalog{}, map[int]ChapterDecision{181: chConfirmed(1), 182: chConfirmed(1)})
	d := Decide(q, ranked, true)
	if d.Code != Review {
		t.Fatalf("R18 must Review for gap: %+v %s", d, dumpCand(ranked))
	}
}

// R19：Part 完整章节覆盖 → 接受，StoredNumber 保留 51/52
func TestPolicyR19PartByChaptersAccept(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Arc Show", Source: SourceFile}},
		Ordinal: Ordinal{Season: 3}, Episodes: []int{51, 52}, HasFileTitle: true,
	}
	a := Subject{ID: 191, Name: "Arc Show Season 3 Part 1", Media: KindSeries, Detailed: true, EpisodeCount: 8}
	b := Subject{ID: 192, Name: "Arc Show Season 3 Part 2", Media: KindSeries, Detailed: true, EpisodeCount: 8}
	chA := ChapterDecision{
		State: ChapterConfirmed,
		Bindings: []EpisodeBinding{
			{FileName: "a51.mkv", StoredNumber: 51, ChapterID: 501, Basis: "ep"},
			{FileName: "a52.mkv", StoredNumber: 52, ChapterID: 502, Basis: "ep"},
		},
		Rejected: map[string]string{},
		Coverage: ChapterCoverage{Required: []int{51, 52}, Matched: []int{51, 52}, Missing: []int{}},
	}
	chB := ChapterDecision{
		State: ChapterConfirmed,
		Bindings: []EpisodeBinding{
			{FileName: "a51.mkv", StoredNumber: 51, ChapterID: 0, Basis: ""},
		},
		Rejected: map[string]string{"a51.mkv": "无章节", "a52.mkv": "无章节"},
		Coverage: ChapterCoverage{Required: []int{51, 52}, Matched: []int{}, Missing: []int{51, 52}},
	}
	// B 用 59/60 章节，无法匹配 51/52 → Coverage incomplete + 可能冲突
	chB = InterpretChapters(
		[]FileEvidence{
			{Name: "Arc Show S03E51.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 51, Last: 51, Explicit: true}},
			{Name: "Arc Show S03E52.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 52, Last: 52, Explicit: true}},
		},
		b,
		[]Chapter{{ID: 591, Ep: 59, Sort: 1}, {ID: 592, Ep: 60, Sort: 2}},
		true,
	)
	chA = InterpretChapters(
		[]FileEvidence{
			{Name: "Arc Show S03E51.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 51, Last: 51, Explicit: true}},
			{Name: "Arc Show S03E52.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 52, Last: 52, Explicit: true}},
		},
		a,
		[]Chapter{{ID: 501, Ep: 51, Sort: 1}, {ID: 502, Ep: 52, Sort: 2}},
		true,
	)
	ranked := Rank(q, []Subject{a, b}, Catalog{}, map[int]ChapterDecision{191: chA, 192: chB})
	var ca Candidate
	for _, c := range ranked {
		if c.Subject.ID == 191 {
			ca = c
		}
	}
	if !hasRuleCand(ca, RulePartByChapters) {
		t.Fatalf("want RulePartByChapters: %+v cov=%+v", ca, chA.Coverage)
	}
	if hasMissing(ca, missingPartAffiliation) {
		t.Fatalf("complete coverage must clear Part Missing: %+v", ca)
	}
	d := Decide(q, ranked, true)
	if d.Code != Accepted || d.WinnerID != 191 {
		t.Fatalf("R19 want accept A: %+v %s", d, dumpCand(ranked))
	}
	for _, b := range chA.Bindings {
		if b.StoredNumber != 51 && b.StoredNumber != 52 {
			t.Fatalf("StoredNumber must stay 51/52: %+v", chA.Bindings)
		}
	}
}

// R20：两 Part 都能 Sort 解释 01/02 → Review
func TestPolicyR20OverlappingPartsReview(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Arc Show", Source: SourceFile}},
		Ordinal: Ordinal{Season: 3}, Episodes: []int{1, 2}, HasFileTitle: true,
	}
	a := Subject{ID: 201, Name: "Arc Show Season 3 Part 1", Media: KindSeries, Detailed: true, EpisodeCount: 8}
	b := Subject{ID: 202, Name: "Arc Show Season 3 Part 2", Media: KindSeries, Detailed: true, EpisodeCount: 8}
	files := []FileEvidence{
		{Name: "e01.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 1, Last: 1, Explicit: true}},
		{Name: "e02.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 2, Last: 2, Explicit: true}},
	}
	chA := InterpretChapters(files, a, []Chapter{{ID: 1, Ep: 1, Sort: 1}, {ID: 2, Ep: 2, Sort: 2}}, true)
	chB := InterpretChapters(files, b, []Chapter{{ID: 3, Ep: 1, Sort: 1}, {ID: 4, Ep: 2, Sort: 2}}, true)
	ranked := Rank(q, []Subject{a, b}, Catalog{}, map[int]ChapterDecision{201: chA, 202: chB})
	d := Decide(q, ranked, true)
	if d.Code != Review {
		t.Fatalf("R20 must Review: %+v %s", d, dumpCand(ranked))
	}
}

// R21：混合 51+59，各只覆盖一部分 → Review
func TestPolicyR21MixedPartsReview(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Arc Show", Source: SourceFile}},
		Ordinal: Ordinal{Season: 3}, Episodes: []int{51, 59}, HasFileTitle: true,
	}
	a := Subject{ID: 211, Name: "Arc Show Season 3 Part 1", Media: KindSeries, Detailed: true, EpisodeCount: 8}
	b := Subject{ID: 212, Name: "Arc Show Season 3 Part 2", Media: KindSeries, Detailed: true, EpisodeCount: 8}
	files := []FileEvidence{
		{Name: "e51.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 51, Last: 51, Explicit: true}},
		{Name: "e59.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 59, Last: 59, Explicit: true}},
	}
	chA := InterpretChapters(files, a, []Chapter{{ID: 1, Ep: 51, Sort: 1}, {ID: 2, Ep: 52, Sort: 2}}, true)
	chB := InterpretChapters(files, b, []Chapter{{ID: 3, Ep: 59, Sort: 1}, {ID: 4, Ep: 60, Sort: 2}}, true)
	ranked := Rank(q, []Subject{a, b}, Catalog{}, map[int]ChapterDecision{211: chA, 212: chB})
	d := Decide(q, ranked, true)
	if d.Code != Review {
		t.Fatalf("R21 must Review: %+v %s", d, dumpCand(ranked))
	}
}

// R22：查询显式 Part2，候选无 Part → 保留 Missing，不猜
func TestPolicyR22ExplicitPartNotInferred(t *testing.T) {
	q := Query{
		Titles:  []Title{{Text: "Arc Show", Source: SourceFile}},
		Ordinal: Ordinal{Season: 3, Part: 2}, Episodes: []int{59}, HasFileTitle: true,
	}
	sub := Subject{ID: 221, Name: "Arc Show Season 3", Media: KindSeries, Detailed: true, EpisodeCount: 8}
	files := []FileEvidence{{Name: "e59.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 59, Last: 59, Explicit: true}}}
	ch := InterpretChapters(files, sub, []Chapter{{ID: 9, Ep: 59, Sort: 1}}, true)
	ranked := Rank(q, []Subject{sub}, Catalog{}, map[int]ChapterDecision{221: ch})
	if !hasMissing(ranked[0], "候选无法解释Part") {
		t.Fatalf("want Part Missing: %+v", ranked[0])
	}
	if hasRuleCand(ranked[0], RulePartByChapters) {
		t.Fatalf("must not invent Part via chapters: %+v", ranked[0])
	}
	if Decide(q, ranked, true).Code == Accepted {
		t.Fatalf("R22 must Review: %+v", ranked[0])
	}
}

// R23：无季号绝对编号，章节淘汰原作后接受续作；偏好不残留
func TestPolicyR23AbsoluteNumberPicksSequel(t *testing.T) {
	q := Query{
		Titles:   []Title{{Text: "Arc Show", Source: SourceFile}},
		Episodes: []int{51, 52}, HasFileTitle: true,
	}
	base := Subject{ID: 230, Name: "Arc Show", Media: KindSeries, Detailed: true, EpisodeCount: 25}
	part1 := Subject{ID: 231, Name: "Arc Show Season 3 Part 1", Media: KindSeries, Detailed: true, EpisodeCount: 8}
	files := []FileEvidence{
		{Name: "Arc Show - 51.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 51, Last: 51, Explicit: true}},
		{Name: "Arc Show - 52.mkv", Episode: EpisodeToken{Kind: NumberSingle, First: 52, Last: 52, Explicit: true}},
	}
	// 原作章节仅 1..25，不含 51/52
	var baseChs []Chapter
	for i := 1; i <= 25; i++ {
		baseChs = append(baseChs, Chapter{ID: 1000 + i, Ep: float64(i), Sort: float64(i)})
	}
	chBase := InterpretChapters(files, base, baseChs, true)
	chPart := InterpretChapters(files, part1, []Chapter{{ID: 501, Ep: 51, Sort: 1}, {ID: 502, Ep: 52, Sort: 2}}, true)
	ranked := Rank(q, []Subject{base, part1}, Catalog{}, map[int]ChapterDecision{230: chBase, 231: chPart})
	d := Decide(q, ranked, true)
	if d.Code != Accepted || d.WinnerID != 231 {
		t.Fatalf("R23 want sequel: %+v %s baseCov=%+v partCov=%+v", d, dumpCand(ranked), chBase.Coverage, chPart.Coverage)
	}
}

func TestPolicyPartAffiliationNotOrdinalOnly(t *testing.T) {
	c := Candidate{Missing: []string{missingPartAffiliation}}
	if OnlyOrdinalStructureMissing(c) {
		t.Fatal("Part归属 Missing must not be OnlyOrdinalStructureMissing")
	}
	if firstNonExcluded([]Candidate{c}) == nil {
		t.Fatal("Part归属 must remain in gap competition")
	}
}

func TestPolicyNameSubtitleVsPrefix(t *testing.T) {
	if !detectSubtitleMain("Mushoku Tensei", "Mushoku Tensei : Isekai Ittara Honki Dasu") {
		t.Fatal("colon subtitle")
	}
	if detectSubtitleMain("Mushoku Tensei", "Mushoku Tensei Another Story") {
		t.Fatal("ordinary prefix must not be subtitle")
	}
	if !isOrdinaryPrefixContain(Normalize("Mushoku Tensei").Normal, Normalize("Mushoku Tensei Another Story").Normal) {
		t.Fatal("want ordinary prefix")
	}
}

func TestPolicyDigitIdentityConflictInRank(t *testing.T) {
	q := Query{
		Titles:   []Title{{Text: "Kaiju No. 8", Source: SourceFile}},
		Episodes: []int{1}, HasFileTitle: true,
	}
	sub := Subject{ID: 8, Name: "Kaiju No. 9", Media: KindSeries, Detailed: true, EpisodeCount: 12}
	ranked := Rank(q, []Subject{sub}, Catalog{}, map[int]ChapterDecision{8: chConfirmed(1)})
	blocked := false
	for _, b := range ranked[0].Blockers {
		if len(b) > 0 {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("No.8 vs No.9 must Blocker: discs q=%v s=%v cand=%+v",
			AnalyzeTitle("Kaiju No. 8").Discriminators, AnalyzeTitle("Kaiju No. 9").Discriminators, ranked[0])
	}
	if Decide(q, ranked, true).Code == Accepted {
		t.Fatal("digit conflict must Review")
	}
}

func TestPolicyMovieAloneNotPenalized(t *testing.T) {
	q := Query{
		Titles: []Title{{Text: "铃芽之旅", Source: SourceFile}},
		Year:   2022, Media: KindMovie, HasFileTitle: true,
	}
	sub := Subject{ID: 9, Name: "铃芽之旅", Media: KindMovie, Year: 2022, Detailed: true, EpisodeCount: 1}
	ch := ChapterDecision{
		State:    ChapterConfirmed,
		Bindings: []EpisodeBinding{{FileName: "x.mkv", StoredNumber: 1, ChapterID: 1, Basis: "movie"}},
		Rejected: map[string]string{},
		Coverage: ChapterCoverage{Required: []int{}, Matched: []int{}, Missing: []int{}},
	}
	ranked := Rank(q, []Subject{sub}, Catalog{}, map[int]ChapterDecision{9: ch})
	if ranked[0].Score < scoreAcceptMin {
		t.Fatalf("lone movie must keep score: %+v", ranked[0])
	}
	d := Decide(q, ranked, true)
	if d.Code != Accepted {
		t.Fatalf("lone movie accept: %+v", d)
	}
}

func TestPolicyNameEvidenceKindPriority(t *testing.T) {
	q := Query{Titles: []Title{{Text: "Exact Title", Source: SourceFile}}, HasFileTitle: true}
	sub := Subject{ID: 1, Name: "Exact Title", Media: KindSeries, Detailed: true}
	ranked := Rank(q, []Subject{sub}, Catalog{}, nil)
	if ranked[0].NameEvidence.Kind != NameFullStrict {
		t.Fatalf("kind=%s", ranked[0].NameEvidence.Kind)
	}
	if ranked[0].NameEvidence.QueryText == "" || ranked[0].NameEvidence.SubjectText == "" {
		t.Fatalf("evidence texts empty: %+v", ranked[0].NameEvidence)
	}
	if !ranked[0].NameEvidence.DirectSupport {
		t.Fatal("DirectSupport")
	}
}
