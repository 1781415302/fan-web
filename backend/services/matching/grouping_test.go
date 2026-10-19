package matching

import (
	"path/filepath"
	"testing"
)

func TestGroupMixedYearsSeparate(t *testing.T) {
	dir := filepath.FromSlash("Hunter")
	groups := GroupFiles([]FileInput{
		{Name: "Hunter x Hunter (1999) - 01.mkv", Dir: dir},
		{Name: "Hunter x Hunter (2011) - 02.mkv", Dir: dir},
	})
	if len(groups) != 2 {
		t.Fatalf("want 2 groups got %d: %+v", len(groups), summarizeGroups(groups))
	}
	years := map[int]bool{}
	for _, g := range groups {
		years[g.Query.Year] = true
	}
	if !years[1999] || !years[2011] {
		t.Fatalf("years not isolated: %v", years)
	}
}

func TestGroupXianWangSeasonsSeparate(t *testing.T) {
	groups := GroupFiles([]FileInput{
		{Name: "[GM-Team][国漫][仙王的日常生活 第4季][Xian Wang De Ri Chang Sheng Huo Ⅳ][2023][12][AVC][GB][1080P].mp4", Dir: "xw"},
		{Name: "[GM-Team][国漫][仙王的日常生活 第3季][Xian Wang De Ri Chang Sheng Huo Ⅲ][2022][09][AVC][GB][1080P].mp4", Dir: "xw"},
	})
	if len(groups) != 2 {
		t.Fatalf("want 2 season groups got %d: %v", len(groups), summarizeGroups(groups))
	}
}

func TestGroupPermutationInvariant(t *testing.T) {
	dir := filepath.FromSlash("HxH")
	a := []FileInput{
		{Name: "Hunter x Hunter (1999) - 01.mkv", Dir: dir},
		{Name: "Hunter x Hunter (2011) - 02.mkv", Dir: dir},
		{Name: "Hunter x Hunter (1999) - 03.mkv", Dir: dir},
	}
	b := []FileInput{a[1], a[2], a[0]}
	ga := GroupFiles(a)
	gb := GroupFiles(b)
	if len(ga) != len(gb) {
		t.Fatalf("len %d vs %d", len(ga), len(gb))
	}
	keysA := map[string]bool{}
	for _, g := range ga {
		keysA[g.Key] = true
	}
	for _, g := range gb {
		if !keysA[g.Key] {
			t.Fatalf("missing key %q after permute", g.Key)
		}
	}
	// same year groups collect same episode sets
	eps := func(gs []Group, year int) []int {
		for _, g := range gs {
			if g.Query.Year == year {
				return append([]int(nil), g.Query.Episodes...)
			}
		}
		return nil
	}
	if !intSliceEq(eps(ga, 1999), eps(gb, 1999)) {
		t.Fatalf("1999 eps differ: %v vs %v", eps(ga, 1999), eps(gb, 1999))
	}
}

func TestGroupUnknownYearNotWildcard(t *testing.T) {
	dir := "same"
	groups := GroupFiles([]FileInput{
		{Name: "Show (1999) - 01.mkv", Dir: dir},
		{Name: "Show - 02.mkv", Dir: dir},
	})
	if len(groups) < 2 {
		t.Fatalf("unknown year must not merge with 1999; got %d: %v", len(groups), summarizeGroups(groups))
	}
}

func summarizeGroups(gs []Group) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Key[:min(40, len(g.Key))]+"...")
	}
	return out
}

func intSliceEq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestRecallGroupingBilingualStableAndNamePreserved(t *testing.T) {
	dir := filepath.FromSlash("anime/迷宫饭")
	name := "迷宫饭 ／ Dungeon Meshi - 01.mkv"
	groups := GroupFiles([]FileInput{
		{Name: name, Dir: dir},
		{Name: "迷宫饭 ／ Dungeon Meshi - 02.mkv", Dir: dir},
	})
	if len(groups) != 1 {
		t.Fatalf("want 1 group got %d: %v", len(groups), summarizeGroups(groups))
	}
	g := groups[0]
	if len(g.Files) != 2 {
		t.Fatalf("files=%d", len(g.Files))
	}
	if g.Files[0].Name != name {
		t.Fatalf("Name mutated: %q", g.Files[0].Name)
	}
	var cn, en bool
	for _, ti := range g.Query.Titles {
		if ti.Source != SourceFile {
			continue
		}
		if containsFold(ti.Text, "迷宫饭") {
			cn = true
		}
		if containsFold(ti.Text, "Dungeon Meshi") {
			en = true
		}
	}
	if !cn || !en {
		t.Fatalf("query titles missing bilingual fragments: %#v", g.Query.Titles)
	}
	// 调换顺序分组键稳定
	groups2 := GroupFiles([]FileInput{
		{Name: "迷宫饭 ／ Dungeon Meshi - 02.mkv", Dir: dir},
		{Name: name, Dir: dir},
	})
	if groups2[0].Key != g.Key {
		t.Fatalf("group key unstable: %q vs %q", g.Key, groups2[0].Key)
	}
}

func TestRecallGroupingSurfaceCleanedPeriod(t *testing.T) {
	groups := GroupFiles([]FileInput{
		{Name: "Kimi no Na wa. [01].mkv", Dir: "m"},
		{Name: "[Kimi no Na wa.][02].mkv", Dir: "m"},
	})
	if len(groups) != 1 {
		t.Fatalf("surface-cleaned titles should group together, got %d: %v", len(groups), summarizeGroups(groups))
	}
}
