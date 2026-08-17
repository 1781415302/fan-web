package services

import (
	"math"
	"testing"
)

func TestItemMatchScoreSeasonGate(t *testing.T) {
	unnamed := BangumiSearchItem{Name: "Sousou no Frieren", NameCn: "葬送的芙莉莲"}
	normS1 := normalizeTitle("葬送的芙莉莲 第1季")
	normS2 := normalizeTitle("葬送的芙莉莲 第2季")
	uncapped := itemMatchScore(normS1, 0, unnamed)
	s1 := itemMatchScore(normS1, 1, unnamed)
	s2 := itemMatchScore(normS2, 2, unnamed)
	if s1 != uncapped {
		t.Fatalf("querySeason==1 + unnamed candidate must not cap, got %v want %v", s1, uncapped)
	}
	if s2 > 0.85 {
		t.Fatalf("querySeason==2 vs unnamed S1 must cap at 0.85, got %v", s2)
	}
}

func TestExtractSeason(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{in: "Title S3", want: 3},
		{in: "Title 4th season", want: 4},
		{in: "Title Season 2", want: 2},
		{in: "葬送的芙莉莲 第2季", want: 2},
		{in: "Title S01E05", want: 1},
		{in: "Bocchi the Rock!", want: 0},
		{in: "番剧 第十一季", want: 0},
	}
	for _, test := range tests {
		if got := extractSeason(test.in); got != test.want {
			t.Fatalf("extractSeason(%q) = %d, want %d", test.in, got, test.want)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "Bocchi the Rock!", want: "bocchi the rock"},
		{in: "TITLE", want: "title"},
		{in: "[ANi] Bocchi the Rock! (2022)", want: "bocchi the rock"},
		{in: "葬送的芙莉莲 第2季", want: "葬送的芙莉莲 第2季"},
		{in: "Title S01", want: "title s01"},
		{in: "Title Season 1", want: "title season 1"},
		{in: "Title 2nd Season", want: "title 2nd season"},
		{in: "SPY×FAMILY", want: "spy family"},
		{in: "  Foo   Bar  ", want: "foo bar"},
	}
	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			got := normalizeTitle(test.in)
			if got != test.want {
				t.Fatalf("normalizeTitle(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestDiceBigram(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want float64
	}{
		{name: "identical", a: "demo show", b: "demo show", want: 1},
		{name: "disjoint", a: "abc", b: "xyz", want: 0},
		{name: "empty", a: "", b: "abc", want: 0},
		{name: "single rune", a: "a", b: "a", want: 0},
		{name: "night nacht", a: "night", b: "nacht", want: 0.25},
		{name: "symmetric", a: "nacht", b: "night", want: 0.25},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := diceBigram(test.a, test.b)
			if math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("diceBigram(%q, %q) = %v, want %v", test.a, test.b, got, test.want)
			}
		})
	}
}

func TestDecideBangumiMatch(t *testing.T) {
	magi := BangumiSearchItem{ID: 645948, Name: "MAGI Synthavision 1980 Demo Reel", NameCn: ""}
	demoShow := BangumiSearchItem{ID: 2001, Name: "Demo Show", NameCn: ""}
	exact := BangumiSearchItem{ID: 1001, Name: "Bocchi the Rock!", NameCn: "孤独摇滚"}
	sameA := BangumiSearchItem{ID: 11, Name: "Same Title", NameCn: ""}
	sameB := BangumiSearchItem{ID: 12, Name: "Same Title", NameCn: ""}
	sameC := BangumiSearchItem{ID: 13, Name: "Same Title Extra", NameCn: ""}
	low := BangumiSearchItem{ID: 7, Name: "Foo", NameCn: ""}

	tests := []struct {
		name         string
		title        string
		items        []BangumiSearchItem
		wantAccept   bool
		wantWinnerID int
		wantCandIDs  []int
		checkPtrIdx  int
	}{
		{
			name:        "sole MAGI vs Demo Show rejected",
			title:       "Demo Show",
			items:       []BangumiSearchItem{magi},
			wantAccept:  false,
			wantCandIDs: []int{645948},
		},
		{
			name:         "exact name match accepted",
			title:        "Bocchi the Rock!",
			items:        []BangumiSearchItem{exact, magi},
			wantAccept:   true,
			wantWinnerID: 1001,
			checkPtrIdx:  0,
		},
		{
			name:        "two close high scores rejected",
			title:       "Same Title",
			items:       []BangumiSearchItem{sameA, sameB, sameC},
			wantAccept:  false,
			wantCandIDs: []int{11, 12, 13},
		},
		{
			name:        "normalized rune length under 2 rejected",
			title:       "A",
			items:       []BangumiSearchItem{exact},
			wantAccept:  false,
			wantCandIDs: []int{1001},
		},
		{
			name:        "single candidate low score rejected",
			title:       "Completely Different Show Name",
			items:       []BangumiSearchItem{low},
			wantAccept:  false,
			wantCandIDs: []int{7},
		},
		{
			name:         "ranking prefers exact Demo Show over MAGI",
			title:        "Demo Show",
			items:        []BangumiSearchItem{magi, demoShow},
			wantAccept:   true,
			wantWinnerID: 2001,
			checkPtrIdx:  1,
		},
		{
			name:       "empty items rejected",
			title:      "Demo Show",
			items:      nil,
			wantAccept: false,
		},
		{
			name:       "empty title rejected",
			title:      "   ",
			items:      []BangumiSearchItem{exact},
			wantAccept: false,
		},
		{
			name:        "Re Zero S3 vs S1 Name rejected",
			title:       "Re Zero kara Hajimeru Isekai Seikatsu S3",
			items:       []BangumiSearchItem{{ID: 3519, Name: "Re:Zero kara Hajimeru Isekai Seikatsu", NameCn: "Re：从零开始的异世界生活"}},
			wantAccept:  false,
			wantCandIDs: []int{3519},
		},
		{
			name:        "芙莉莲 第2季 vs S1 NameCn rejected",
			title:       "葬送的芙莉莲 第2季",
			items:       []BangumiSearchItem{{ID: 400602, Name: "Sousou no Frieren", NameCn: "葬送的芙莉莲"}},
			wantAccept:  false,
			wantCandIDs: []int{400602},
		},
		{
			name:         "芙莉莲 第1季 vs same NameCn may Accept",
			title:        "葬送的芙莉莲 第1季",
			items:        []BangumiSearchItem{{ID: 400602, Name: "Sousou no Frieren", NameCn: "葬送的芙莉莲 第1季"}},
			wantAccept:   true,
			wantWinnerID: 400602,
			checkPtrIdx:  0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DecideBangumiMatch(test.title, test.items)
			if got.Accept != test.wantAccept {
				t.Fatalf("Accept = %v, want %v (winner=%v cands=%v)", got.Accept, test.wantAccept, got.Winner, got.Candidates)
			}
			if test.wantAccept {
				if got.Winner == nil {
					t.Fatal("expected Winner, got nil")
				}
				if got.Winner.ID != test.wantWinnerID {
					t.Fatalf("Winner.ID = %d, want %d", got.Winner.ID, test.wantWinnerID)
				}
				if test.checkPtrIdx >= 0 && test.checkPtrIdx < len(test.items) {
					if got.Winner != &test.items[test.checkPtrIdx] {
						t.Fatalf("Winner must point to items[%d] (id %d), not a copy of another element", test.checkPtrIdx, test.items[test.checkPtrIdx].ID)
					}
				}
				return
			}
			if got.Winner != nil {
				t.Fatalf("expected nil Winner, got id %d", got.Winner.ID)
			}
			if test.wantCandIDs == nil {
				return
			}
			if len(got.Candidates) > 3 {
				t.Fatalf("Candidates len = %d, want <= 3", len(got.Candidates))
			}
			if len(got.Candidates) != len(test.wantCandIDs) {
				t.Fatalf("Candidates = %#v, want ids %v", got.Candidates, test.wantCandIDs)
			}
			for i, id := range test.wantCandIDs {
				if got.Candidates[i].ID != id {
					t.Fatalf("Candidates[%d].ID = %d, want %d (all=%#v)", i, got.Candidates[i].ID, id, got.Candidates)
				}
			}
		})
	}
}
