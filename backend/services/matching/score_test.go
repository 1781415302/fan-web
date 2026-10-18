package matching

import (
	"fmt"
	"strings"
	"testing"
)

func testScoreCatalog(t *testing.T) Catalog {
	t.Helper()
	cat, err := LoadCatalog(BuiltinTitleAliasesJSON, nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return cat
}

func TestRankScoreBoundaries(t *testing.T) {
	cat := testScoreCatalog(t)
	q := Query{
		Titles:       []Title{{Text: "仙王的日常生活", Source: SourceFile}},
		Ordinal:      Ordinal{Season: 4},
		Year:         2023,
		HasFileTitle: true,
	}
	subjects := []Subject{
		{ID: 405214, Name: "仙王的日常生活 第4季", NameCn: "仙王的日常生活 第4季", Year: 2023, Media: KindSeries},
		{ID: 999001, Name: "完全无关的别的动画", NameCn: "完全无关的别的动画", Year: 2020, Media: KindSeries},
	}
	ranked := Rank(q, subjects, cat, nil)
	if len(ranked) != 2 {
		t.Fatalf("len=%d", len(ranked))
	}
	best := ranked[0]
	if best.Subject.ID != 405214 {
		t.Fatalf("winner=%d blockers=%v missing=%v score=%v name=%v",
			best.Subject.ID, best.Blockers, best.Missing, best.Score, best.NameScore)
	}
	if best.NameScore < nameScoreAcceptMin {
		t.Fatalf("NameScore=%v want >= %v support=%v", best.NameScore, nameScoreAcceptMin, best.Support)
	}
	if best.Score < scoreAcceptMin {
		t.Fatalf("Score=%v want >= %v", best.Score, scoreAcceptMin)
	}
	if best.Score > 1 || best.NameScore > 1 || best.Score < 0 || best.NameScore < 0 {
		t.Fatalf("scores out of [0,1]: name=%v score=%v", best.NameScore, best.Score)
	}
	n, s, g := scoreThresholds()
	if n != 0.88 || s != 0.92 || g != 0.10 {
		t.Fatalf("thresholds drifted: %v %v %v", n, s, g)
	}
}

func TestRankUnmarkedNotSeasonOne(t *testing.T) {
	cat := Catalog{}
	q := Query{
		Titles:       []Title{{Text: "某作品", Source: SourceFile}},
		Ordinal:      Ordinal{Season: 2},
		HasFileTitle: true,
	}
	subjects := []Subject{
		{ID: 1, Name: "某作品", NameCn: "某作品", Media: KindSeries},
	}
	ranked := Rank(q, subjects, cat, nil)
	if len(ranked) != 1 {
		t.Fatal(ranked)
	}
	c := ranked[0]
	found := false
	for _, m := range c.Missing {
		if m == "候选无法解释查询季号" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unmarked must Missing season, got blockers=%v missing=%v", c.Blockers, c.Missing)
	}
	d := Decide(q, ranked, true)
	if d.Code == Accepted {
		t.Fatal("unmarked must not auto-accept as season 1")
	}
}

func TestRankWEBNeutralWithTV(t *testing.T) {
	cat := Catalog{}
	q := Query{
		Titles:       []Title{{Text: "示例动画", Source: SourceFile}},
		Media:        KindSeries,
		HasFileTitle: true,
	}
	subjects := []Subject{
		{ID: 10, Name: "示例动画", NameCn: "示例动画", Media: KindSeries, Platform: "Web"},
	}
	ranked := Rank(q, subjects, cat, nil)
	c := ranked[0]
	for _, b := range c.Blockers {
		if strings.Contains(b, "类型冲突") {
			t.Fatalf("TV/WEB must be neutral, blocker=%q", b)
		}
	}
	d := Decide(q, ranked, true)
	if d.Code != Accepted {
		t.Fatalf("WEB series should accept: code=%s reason=%s blockers=%v missing=%v score=%v name=%v",
			d.Code, d.Reason, c.Blockers, c.Missing, c.Score, c.NameScore)
	}
}

func TestRankFragmentConflict(t *testing.T) {
	cat := Catalog{}
	q := Query{
		Titles: []Title{
			{Text: "辉夜大小姐想让我告白", Source: SourceFile},
			{Text: "Steins Gate", Source: SourceFile},
		},
		HasFileTitle: true,
	}
	subjects := []Subject{
		{ID: 100, Name: "辉夜大小姐想让我告白", NameCn: "辉夜大小姐想让我告白", Media: KindSeries},
		{ID: 200, Name: "Steins Gate", NameCn: "命运石之门", Media: KindSeries},
	}
	ranked := Rank(q, subjects, cat, nil)
	var withFrag int
	for _, c := range ranked {
		for _, m := range c.Missing {
			if m == "并列语言片段支持不同作品" {
				withFrag++
			}
		}
	}
	if withFrag < 2 {
		t.Fatalf("expected fragment conflict on both subjects, ranked=%s", dumpCand(ranked))
	}
	d := Decide(q, ranked, true)
	if d.Code == Accepted {
		t.Fatal("fragment conflict must Review, not accept max")
	}
}

func TestRankCatalogEnglishToChinese(t *testing.T) {
	cat := testScoreCatalog(t)
	q := Query{
		Titles:       []Title{{Text: "The Daily Life of the Immortal King", Source: SourceFile}},
		Ordinal:      Ordinal{Season: 5},
		HasFileTitle: true,
	}
	subjects := []Subject{
		{ID: 529875, Name: "仙王的日常生活 第5季", NameCn: "仙王的日常生活 第5季", Year: 2024, Media: KindSeries},
	}
	ranked := Rank(q, subjects, cat, nil)
	c := ranked[0]
	if c.NameScore < nameScoreCatalogEquiv-0.001 {
		t.Fatalf("catalog equiv NameScore=%v support=%v", c.NameScore, c.Support)
	}
	for _, m := range c.Missing {
		if m == "候选无法解释查询季号" {
			t.Fatalf("season should be explained by subject title: %+v", c)
		}
	}
}

func TestRankMovieBlocksSeries(t *testing.T) {
	cat := Catalog{}
	q := Query{
		Titles:       []Title{{Text: "示例动画", Source: SourceFile}},
		Media:        KindMovie,
		HasFileTitle: true,
	}
	subjects := []Subject{
		{ID: 1, Name: "示例动画", NameCn: "示例动画", Media: KindSeries},
	}
	ranked := Rank(q, subjects, cat, nil)
	blocked := false
	for _, b := range ranked[0].Blockers {
		if strings.Contains(b, "类型冲突") {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("movie vs series must block: %+v", ranked[0])
	}
}

func TestRankDirTitleCannotAcceptWhenHasFileTitle(t *testing.T) {
	cat := Catalog{}
	q := Query{
		Titles: []Title{
			{Text: "Unknown Title", Source: SourceFile},
			{Text: "芙莉莲", Source: SourceDirectory},
		},
		HasFileTitle: true,
	}
	subjects := []Subject{
		{ID: 1, Name: "葬送的芙莉莲", NameCn: "葬送的芙莉莲", Media: KindSeries},
	}
	ranked := Rank(q, subjects, cat, nil)
	d := Decide(q, ranked, true)
	if d.Code == Accepted {
		t.Fatalf("directory-only identity must not auto-accept when HasFileTitle: %+v", ranked[0])
	}
}

func dumpCand(cs []Candidate) string {
	s := ""
	for _, c := range cs {
		s += fmt.Sprintf("{id=%d name=%.2f score=%.2f miss=%v block=%v} ",
			c.Subject.ID, c.NameScore, c.Score, c.Missing, c.Blockers)
	}
	return s
}
