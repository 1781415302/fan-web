package matching

import (
	"reflect"
	"strings"
	"testing"
)

func shiftTestGroup(t *testing.T, names ...string) Group {
	t.Helper()
	inputs := make([]FileInput, 0, len(names))
	for _, n := range names {
		inputs = append(inputs, FileInput{Name: n, Dir: "Show"})
	}
	groups := GroupFiles(inputs)
	if len(groups) != 1 {
		t.Fatalf("期望 1 组，得到 %d", len(groups))
	}
	return groups[0]
}

func TestShiftGroupEpisodesRefreshesQueryEpisodes(t *testing.T) {
	g := shiftTestGroup(t,
		"[Group] Shift Show - 13 [1080p].mkv",
		"[Group] Shift Show - 14 [1080p].mkv",
	)
	if !reflect.DeepEqual(g.Query.Episodes, []int{13, 14}) {
		t.Fatalf("前置：Query.Episodes=%v", g.Query.Episodes)
	}
	g.Files[0].AbsEpisode = 85
	shifted := ShiftGroupEpisodes(g, 12)
	if !reflect.DeepEqual(shifted.Query.Episodes, []int{1, 2}) {
		t.Fatalf("Query.Episodes 未刷新：%v", shifted.Query.Episodes)
	}
	for i, f := range shifted.Files {
		if f.Episode.Kind != NumberSingle || f.Episode.First != i+1 || f.Episode.Last != i+1 {
			t.Fatalf("Files[%d].Episode=%+v", i, f.Episode)
		}
		if f.AbsEpisode != 0 {
			t.Fatalf("Files[%d].AbsEpisode=%d，期望偏移后置 0", i, f.AbsEpisode)
		}
	}
	if len(shifted.Query.Conflicts) != 0 {
		t.Fatalf("不应有冲突：%v", shifted.Query.Conflicts)
	}
	// 纯函数：入参不被改写
	if g.Files[0].Episode.First != 13 || g.Files[0].AbsEpisode != 85 || !reflect.DeepEqual(g.Query.Episodes, []int{13, 14}) {
		t.Fatalf("入参被修改：%+v / %v", g.Files[0], g.Query.Episodes)
	}
	if shifted.Key != g.Key || shifted.Dir != g.Dir {
		t.Fatalf("Key/Dir 不应变化")
	}
}

func TestShiftGroupEpisodesNonPositiveBecomesUnknownConflict(t *testing.T) {
	g := shiftTestGroup(t,
		"[Group] Shift Show - 05 [1080p].mkv",
		"[Group] Shift Show - 13 [1080p].mkv",
	)
	shifted := ShiftGroupEpisodes(g, 5)
	var low FileEvidence
	for _, f := range shifted.Files {
		if strings.Contains(f.Name, "- 05") {
			low = f
		}
	}
	if low.Episode.Kind != NumberUnknown || low.Episode.First != 0 {
		t.Fatalf("偏移后<=0 应置 Unknown：%+v", low.Episode)
	}
	found := false
	for _, c := range low.Conflicts {
		if c == "集号偏移后<=0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("文件冲突缺失：%v", low.Conflicts)
	}
	qFound := false
	for _, c := range shifted.Query.Conflicts {
		if c == "集号偏移后<=0" {
			qFound = true
		}
	}
	if !qFound {
		t.Fatalf("重建后 Query.Conflicts 应含偏移冲突：%v", shifted.Query.Conflicts)
	}
	if !reflect.DeepEqual(shifted.Query.Episodes, []int{8}) {
		t.Fatalf("Query.Episodes=%v", shifted.Query.Episodes)
	}
	if len(g.Query.Conflicts) != 0 {
		t.Fatalf("入参 Query 被修改：%v", g.Query.Conflicts)
	}
}

func TestShiftGroupEpisodesSkipsNonSingle(t *testing.T) {
	g := Group{Key: "k", Dir: "Show", Files: []FileEvidence{
		{Name: "a", Episode: EpisodeToken{Kind: NumberRange, First: 13, Last: 24}, AbsEpisode: 40},
		{Name: "b", Episode: EpisodeToken{Kind: NumberSpecial, Raw: "SP"}},
		{Name: "c", Episode: EpisodeToken{Kind: NumberSingle, First: 20, Last: 20, Explicit: true}},
	}}
	g.Query = buildGroupQuery(g.Files)
	shifted := ShiftGroupEpisodes(g, 12)
	if shifted.Files[0].Episode.First != 13 || shifted.Files[0].Episode.Last != 24 || shifted.Files[0].AbsEpisode != 40 {
		t.Fatalf("Range 不应平移：%+v", shifted.Files[0])
	}
	if shifted.Files[1].Episode.Kind != NumberSpecial {
		t.Fatalf("Special 不应改动：%+v", shifted.Files[1])
	}
	if shifted.Files[2].Episode.First != 8 || !shifted.Files[2].Episode.Explicit {
		t.Fatalf("Single 平移错误：%+v", shifted.Files[2])
	}
	if !reflect.DeepEqual(shifted.Query.Episodes, []int{8}) {
		t.Fatalf("Query.Episodes=%v", shifted.Query.Episodes)
	}
}

func TestShiftGroupEpisodesZeroOffsetIdentity(t *testing.T) {
	g := shiftTestGroup(t, "[Group] Shift Show - 13 [1080p].mkv")
	for _, off := range []int{0, -3} {
		got := ShiftGroupEpisodes(g, off)
		if !reflect.DeepEqual(got, g) {
			t.Fatalf("offset=%d 应原样返回", off)
		}
	}
}
