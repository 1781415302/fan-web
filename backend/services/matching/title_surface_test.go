package matching

import (
	"testing"
)

func TestRecallSurfaceCleanTitlePeriodAndIdentity(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Kimi no Na wa.", "Kimi no Na wa"},
		{"Kimi no Na wa。", "Kimi no Na wa"},
		{"  Kimi no Na wa.  ", "Kimi no Na wa"},
		{"STEINS;GATE", "STEINS;GATE"},
		{"K-ON!", "K-ON!"},
		{"K-ON!!", "K-ON!!"},
		{"Title...", "Title..."},
		{"Title…", "Title…"},
		{"A:B", "A:B"},
		{"Re-Zero", "Re-Zero"},
		{"No. 8", "No. 8"},
		{"No.8", "No. 8"},
		{"no.8", "No. 8"},
		{"Kaiju No.8", "Kaiju No. 8"},
		{"Kaiju No. 8", "Kaiju No. 8"},
		{"", ""},
	}
	for _, tc := range cases {
		got := CleanTitleSurface(tc.in)
		if got != tc.want {
			t.Fatalf("CleanTitleSurface(%q)=%q want %q", tc.in, got, tc.want)
		}
		// 幂等
		if again := CleanTitleSurface(got); again != got {
			t.Fatalf("CleanTitleSurface not idempotent: %q → %q → %q", tc.in, got, again)
		}
	}
}

func TestRecallSurfaceSplitParallelTitles(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"迷宫饭 / Dungeon Meshi", []string{"迷宫饭", "Dungeon Meshi"}},
		{"迷宫饭 ／ Dungeon Meshi", []string{"迷宫饭", "Dungeon Meshi"}},
		{"Dungeon Meshi / 迷宫饭", []string{"Dungeon Meshi", "迷宫饭"}},
		{"Fate/Zero", []string{"Fate/Zero"}},
		{"Fate / Zero", []string{"Fate / Zero"}},
		{"らんま1/2", []string{"らんま1/2"}},
		{"Ranma 1 / 2", []string{"Ranma 1 / 2"}},
		{"单纯标题", []string{"单纯标题"}},
		{"English Only Title", []string{"English Only Title"}},
	}
	for _, tc := range cases {
		got := SplitParallelTitles(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("SplitParallelTitles(%q) len=%d %v want %v", tc.in, len(got), got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("SplitParallelTitles(%q)[%d]=%q want %q (full %v)", tc.in, i, got[i], tc.want[i], got)
			}
		}
	}
	if SplitParallelTitles("") != nil && len(SplitParallelTitles("")) != 0 {
		t.Fatalf("empty input should yield nil/empty, got %#v", SplitParallelTitles(""))
	}
	if SplitParallelTitles("   ") != nil && len(SplitParallelTitles("   ")) != 0 {
		t.Fatalf("blank input should yield nil/empty")
	}
}

func TestRecallSurfaceSplitDoesNotTreatPathAsLanguage(t *testing.T) {
	// 目录路径不能被当成并列标题；本函数只处理标题字符串，且无两侧空白的 / 不拆。
	path := "anime/迷宫饭/Season 1"
	got := SplitParallelTitles(path)
	if len(got) != 1 || got[0] != path {
		t.Fatalf("path-like %q unexpectedly split: %v", path, got)
	}
}
