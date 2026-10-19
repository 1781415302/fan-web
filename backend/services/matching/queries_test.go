package matching

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func testXianWangCatalog(t *testing.T) Catalog {
	t.Helper()
	cat, err := LoadCatalog(BuiltinTitleAliasesJSON, nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return cat
}

func TestQueriesXianWangEnglishRecallsCanonical(t *testing.T) {
	cat := testXianWangCatalog(t)
	q := Query{
		Titles: []Title{
			{Text: "The Daily Life of the Immortal King", Source: SourceFile},
		},
		Ordinal:      Ordinal{Season: 5},
		HasFileTitle: true,
	}
	terms := BuildQueries(q, cat)
	if len(terms) == 0 {
		t.Fatal("no query terms")
	}
	var hasCN bool
	for _, term := range terms {
		if strings.Contains(term.Text, "仙王") {
			hasCN = true
			if term.Source != SourceCatalog {
				t.Fatalf("canonical source=%s want catalog: %+v", term.Source, term)
			}
		}
	}
	if !hasCN {
		t.Fatalf("english title should recall CN canonical via catalog: %#v", terms)
	}
	// 季号保留：应出现带季限定的词，且完整 Query.Ordinal 不被 BuildQueries 清掉
	if q.Ordinal.Season != 5 {
		t.Fatal("BuildQueries must not mutate Query.Ordinal")
	}
	var hasSeason bool
	for _, term := range terms {
		if strings.Contains(term.Text, "第5季") || strings.HasSuffix(strings.TrimSpace(term.Text), " 5") {
			hasSeason = true
		}
	}
	if !hasSeason {
		t.Fatalf("season qualifier missing: %#v", terms)
	}
}

func TestQueriesBudgetMaxSix(t *testing.T) {
	cat := testXianWangCatalog(t)
	q := Query{
		Titles: []Title{
			{Text: "The Daily Life of the Immortal King", Source: SourceFile},
			{Text: "Xian Wang De Ri Chang Sheng Huo", Source: SourceFile},
			{Text: "仙王的日常生活", Source: SourceFile},
			{Text: "Another Completely Different Latin Title Words Here Extra", Source: SourceFile},
		},
		Ordinal:      Ordinal{Season: 4},
		HasFileTitle: true,
	}
	terms := BuildQueries(q, cat)
	if len(terms) > queryTermBudget {
		t.Fatalf("len=%d > %d: %#v", len(terms), queryTermBudget, terms)
	}
	if len(terms) == 0 {
		t.Fatal("empty terms")
	}
	// 去重：同 TitleKey 不应重复
	seen := map[string]struct{}{}
	for _, term := range terms {
		k := TitleKey(term.Text)
		if _, ok := seen[k]; ok {
			t.Fatalf("duplicate term key %q in %#v", k, terms)
		}
		seen[k] = struct{}{}
	}
}

func TestQueriesShortenKeepsAtLeastTwoWords(t *testing.T) {
	cat, err := LoadCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	q := Query{
		Titles: []Title{
			{Text: "The Daily Life of the Immortal King", Source: SourceFile},
		},
		HasFileTitle: true,
	}
	terms := BuildQueries(q, cat)
	var shortened []QueryTerm
	for _, term := range terms {
		if term.Shortened {
			shortened = append(shortened, term)
			words := strings.Fields(term.Text)
			if len(words) < minShortenWords {
				t.Fatalf("shortened below min words: %q", term.Text)
			}
		}
	}
	if len(shortened) == 0 {
		t.Fatalf("expected shortened latin forms, got %#v", terms)
	}
	if len(shortened) > maxShortenSteps {
		t.Fatalf("too many shorten steps: %#v", shortened)
	}
	// 完整标题仍在，且 Shortened=false
	var full bool
	for _, term := range terms {
		if !term.Shortened && strings.Contains(strings.ToLower(term.Text), "immortal king") {
			full = true
		}
	}
	if !full {
		t.Fatalf("full title missing: %#v", terms)
	}
}

func TestQueriesDirectoryOnlyWhenNoFileTitle(t *testing.T) {
	cat, err := LoadCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	withFile := BuildQueries(Query{
		Titles: []Title{
			{Text: "File Title Show", Source: SourceFile},
			{Text: "Directory Hint", Source: SourceDirectory},
		},
		HasFileTitle: true,
	}, cat)
	for _, term := range withFile {
		if term.Source == SourceDirectory {
			t.Fatalf("directory queried while file title present: %#v", withFile)
		}
	}

	dirOnly := BuildQueries(Query{
		Titles: []Title{
			{Text: "Directory Only Show", Source: SourceDirectory},
		},
		HasFileTitle: false,
	}, cat)
	if len(dirOnly) == 0 {
		t.Fatal("directory title should query when no file title")
	}
	found := false
	for _, term := range dirOnly {
		if term.Source == SourceDirectory {
			found = true
		}
	}
	if !found {
		t.Fatalf("want directory source: %#v", dirOnly)
	}
}

func TestQueriesSkipCategoryOnly(t *testing.T) {
	cat, err := LoadCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	terms := BuildQueries(Query{
		Titles:       []Title{{Text: "国漫", Source: SourceFile}},
		HasFileTitle: true,
	}, cat)
	for _, term := range terms {
		if TitleKey(term.Text) == "国漫" {
			t.Fatalf("category-only query emitted: %#v", terms)
		}
	}
}

func TestQueriesConstantsCentralized(t *testing.T) {
	if queryTermBudget != 6 {
		t.Fatalf("budget=%d want 6", queryTermBudget)
	}
	if maxShortenSteps != 2 || minShortenWords != 2 {
		t.Fatalf("shorten caps changed: steps=%d minWords=%d", maxShortenSteps, minShortenWords)
	}
	_ = utf8.RuneCountInString
}

func TestRecallQueriesBilingualWithinBudget(t *testing.T) {
	cat, err := LoadCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ev := ParseFile(FileInput{Name: "迷宫饭 ／ Dungeon Meshi - 01.mkv", Dir: "anime"})
	if len(ev.Titles) < 2 {
		t.Fatalf("parse titles=%#v", ev.Titles)
	}
	q := Query{Titles: ev.Titles, HasFileTitle: true}
	terms := BuildQueries(q, cat)
	if len(terms) == 0 {
		t.Fatal("empty terms")
	}
	if len(terms) > queryTermBudget {
		t.Fatalf("len=%d > %d: %#v", len(terms), queryTermBudget, terms)
	}
	var cn, en bool
	for _, term := range terms {
		if strings.Contains(term.Text, "迷宫饭") {
			cn = true
		}
		if strings.Contains(strings.ToLower(term.Text), "dungeon") {
			en = true
		}
	}
	if !cn || !en {
		t.Fatalf("both languages must be queried within budget: %#v", terms)
	}
}

func TestRecallQueriesKaijuNo8(t *testing.T) {
	cat, err := LoadCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ev := ParseFile(FileInput{Name: "Kaiju No. 8 - 01.mkv"})
	terms := BuildQueries(Query{Titles: ev.Titles, HasFileTitle: true}, cat)
	if len(terms) == 0 || len(terms) > queryTermBudget {
		t.Fatalf("terms=%#v", terms)
	}
	ok := false
	for _, term := range terms {
		if strings.Contains(strings.ToLower(term.Text), "kaiju") {
			ok = true
		}
		if strings.Contains(term.Text, "No.9") || strings.Contains(term.Text, "No. 9") {
			t.Fatalf("must not query No.9: %#v", terms)
		}
	}
	if !ok {
		t.Fatalf("kaiju missing: %#v", terms)
	}
}
