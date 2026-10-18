package matching

import (
	"path/filepath"
	"testing"
)

func TestParseXianWangS4(t *testing.T) {
	ev := ParseFile(FileInput{
		Name: "[GM-Team][国漫][仙王的日常生活 第4季][Xian Wang De Ri Chang Sheng Huo Ⅳ][2023][12][AVC][GB][1080P].mp4",
	})
	if ev.Ordinal.Season != 4 {
		t.Fatalf("season=%d want 4; conflicts=%v titles=%v", ev.Ordinal.Season, ev.Conflicts, ev.Titles)
	}
	if ev.Year != 2023 {
		t.Fatalf("year=%d want 2023", ev.Year)
	}
	if ev.Episode.Kind != NumberSingle || ev.Episode.First != 12 {
		t.Fatalf("episode=%+v want Single 12", ev.Episode)
	}
	if len(ev.Titles) < 2 {
		t.Fatalf("want CN+pinyin titles, got %#v", ev.Titles)
	}
}

func TestParseXianWangS3(t *testing.T) {
	ev := ParseFile(FileInput{
		Name: "[GM-Team][国漫][仙王的日常生活 第3季][Xian Wang De Ri Chang Sheng Huo Ⅲ][2022][09][AVC][GB][1080P].mp4",
	})
	if ev.Ordinal.Season != 3 || ev.Year != 2022 {
		t.Fatalf("got season=%d year=%d", ev.Ordinal.Season, ev.Year)
	}
	if ev.Episode.First != 9 {
		t.Fatalf("ep=%d want 9", ev.Episode.First)
	}
}

func TestParseImmortalKingEN(t *testing.T) {
	ev := ParseFile(FileInput{
		Name: "[Dynamis One] The Daily Life of the Immortal King 5 - 03 (CR 1920x1080 AVC AAC MKV) [C41F9AE1].mkv",
	})
	if ev.Ordinal.Season != 5 {
		t.Fatalf("season=%d want 5; titles=%v rest conflicts=%v ep=%+v", ev.Ordinal.Season, ev.Titles, ev.Conflicts, ev.Episode)
	}
	if ev.Episode.Kind != NumberSingle || ev.Episode.First != 3 {
		t.Fatalf("episode=%+v want 3", ev.Episode)
	}
	found := false
	for _, ti := range ev.Titles {
		if containsFold(ti.Text, "immortal king") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing english title: %#v", ev.Titles)
	}
}

func TestParseOshiNoKoKeepsTitle(t *testing.T) {
	ev := ParseFile(FileInput{Name: "【推しの子】 第2期 - 01.mkv"})
	if len(ev.Titles) == 0 {
		t.Fatal("title dropped")
	}
	found := false
	for _, ti := range ev.Titles {
		if containsFold(ti.Text, "推し") {
			found = true
		}
	}
	if !found {
		t.Fatalf("title not kept: %#v", ev.Titles)
	}
	if ev.Ordinal.Season != 2 || ev.Episode.First != 1 {
		t.Fatalf("season=%d ep=%d", ev.Ordinal.Season, ev.Episode.First)
	}
}

func TestParseTitleDigitsProtected(t *testing.T) {
	cases := []struct {
		name  string
		title string
		ep    int
		year  int
	}{
		{"[Sub] 86 - 05.mkv", "86", 5, 0},
		{"Ranma1-2 (2024) - 03.mkv", "Ranma1/2", 3, 2024},
		{"U-17 Show - 03.mkv", "U-17", 3, 0},
	}
	for _, tc := range cases {
		ev := ParseFile(FileInput{Name: tc.name})
		if ev.Episode.First != tc.ep {
			t.Fatalf("%s ep=%d want %d (titles=%v)", tc.name, ev.Episode.First, tc.ep, ev.Titles)
		}
		if tc.year > 0 && ev.Year != tc.year {
			t.Fatalf("%s year=%d want %d", tc.name, ev.Year, tc.year)
		}
		ok := false
		for _, ti := range ev.Titles {
			if containsFold(ti.Text, tc.title) {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("%s missing title %q in %#v", tc.name, tc.title, ev.Titles)
		}
	}
}

func TestParseFourDigitEpisode(t *testing.T) {
	ev := ParseFile(FileInput{Name: "One.Piece.S01E1100.1080p.mkv"})
	if ev.Episode.First != 1100 || ev.Ordinal.Season != 1 {
		t.Fatalf("got season=%d ep=%d", ev.Ordinal.Season, ev.Episode.First)
	}
	ev2 := ParseFile(FileInput{Name: "海贼王 第2023话.mkv"})
	if ev2.Episode.First != 2023 {
		t.Fatalf("第2023话 got ep=%d (must not be year)", ev2.Episode.First)
	}
	if ev2.Year == 2023 {
		t.Fatalf("第2023话 must not set Year=2023")
	}
}

func TestParseUnsupportedNumbering(t *testing.T) {
	rangeEv := ParseFile(FileInput{Name: "Show - 01-02.mkv"})
	if rangeEv.Episode.Kind != NumberRange {
		t.Fatalf("range kind=%s", rangeEv.Episode.Kind)
	}
	frac := ParseFile(FileInput{Name: "Show - 12.5.mkv"})
	if frac.Episode.Kind != NumberFraction {
		t.Fatalf("fraction kind=%s", frac.Episode.Kind)
	}
	sp := ParseFile(FileInput{Name: "Show 第0话.mkv"})
	if sp.Episode.Kind != NumberSpecial {
		t.Fatalf("special kind=%s ep=%+v", sp.Episode.Kind, sp.Episode)
	}
}

func TestParseDirTitleFallback(t *testing.T) {
	ev := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("芙莉莲")})
	if len(ev.Titles) == 0 {
		t.Fatal("dir title missing")
	}
	if ev.Titles[0].Source != SourceDirectory {
		t.Fatalf("source=%s", ev.Titles[0].Source)
	}
	ev2 := ParseFile(FileInput{Name: "Unknown Title - 05.mkv", Dir: filepath.FromSlash("芙莉莲")})
	hasFile := false
	for _, ti := range ev2.Titles {
		if ti.Source == SourceFile {
			hasFile = true
		}
	}
	if !hasFile {
		t.Fatalf("file title should win presence: %#v", ev2.Titles)
	}
}

func TestParseDirKeepsDigitTitle86(t *testing.T) {
	ev := ParseFile(FileInput{Name: "05.mkv", Dir: filepath.FromSlash("86")})
	if len(ev.Titles) == 0 || ev.Titles[0].Text != "86" {
		t.Fatalf("digit dir title lost: %#v", ev.Titles)
	}
}

func TestParseAnalyzeTitleProtectsDiscriminators(t *testing.T) {
	tf := AnalyzeTitle("K-ON!!")
	found := false
	for _, d := range tf.Discriminators {
		if d == "!!" || d == "!" {
			found = true
		}
	}
	if !found {
		t.Fatalf("discriminators=%v", tf.Discriminators)
	}
	tf2 := AnalyzeTitle("Steins;Gate 0")
	if tf2.Base == "" {
		t.Fatal("empty base")
	}
}

func containsFold(s, sub string) bool {
	return len(sub) > 0 && (len(s) >= len(sub)) &&
		(indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	return len([]byte(TitleKey(s)))*0 + // keep TitleKey referenced
		indexBytes(TitleKey(s), TitleKey(sub))
}

func indexBytes(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
