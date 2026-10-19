package matching

import (
	"path/filepath"
	"strings"
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

func TestParseSequelDigitKeptWhenBracketEpisodePresent(t *testing.T) {
	cases := []struct {
		name      string
		wantTitle string
		wantEp    int
	}{
		{"[北宇治字幕组] 悠久之翼2 [01].mkv", "悠久之翼2", 1},
		{"爆漫王。3 [01].mkv", "爆漫王。3", 1},
	}
	for _, tc := range cases {
		ev := ParseFile(FileInput{Name: tc.name})
		if ev.Episode.Kind != NumberSingle || ev.Episode.First != tc.wantEp || !ev.Episode.Explicit {
			t.Fatalf("%s episode=%+v want explicit single %d", tc.name, ev.Episode, tc.wantEp)
		}
		if ev.Ordinal.Season != 0 {
			t.Fatalf("%s season=%d want 0 (digit stays in title)", tc.name, ev.Ordinal.Season)
		}
		found := false
		for _, ti := range ev.Titles {
			if ti.Text == tc.wantTitle {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s titles=%#v want %q retained", tc.name, ev.Titles, tc.wantTitle)
		}
	}
}

func TestParseBareEpisodeStillWorksWithoutBracket(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Review Show 05.mkv"})
	if ev.Episode.Kind != NumberSingle || ev.Episode.First != 5 {
		t.Fatalf("bare ep lost: %+v titles=%v", ev.Episode, ev.Titles)
	}
	ok := false
	for _, ti := range ev.Titles {
		if containsFold(ti.Text, "Review Show") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("title lost: %#v", ev.Titles)
	}
}

func TestRecallParseMetadataBlocksNotTitles(t *testing.T) {
	ev := ParseFile(FileInput{Name: "[BD 1080p][简体内嵌] 葬送的芙莉莲 [01].mkv"})
	if ev.Episode.First != 1 || !ev.Episode.Explicit {
		t.Fatalf("ep=%+v", ev.Episode)
	}
	for _, ti := range ev.Titles {
		low := TitleKey(ti.Text)
		if containsFold(ti.Text, "BD") || containsFold(ti.Text, "1080") || containsFold(ti.Text, "简体") || containsFold(ti.Text, "内嵌") {
			t.Fatalf("meta leaked into title: %#v key=%s", ev.Titles, low)
		}
	}
	ok := false
	for _, ti := range ev.Titles {
		if containsFold(ti.Text, "芙莉莲") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("title missing: %#v", ev.Titles)
	}

	ev2 := ParseFile(FileInput{Name: "[简繁日内封字幕][Dungeon Meshi][01].mkv"})
	ok = false
	for _, ti := range ev2.Titles {
		if ti.Text == "Dungeon Meshi" {
			ok = true
		}
		if containsFold(ti.Text, "简繁") || containsFold(ti.Text, "字幕") {
			t.Fatalf("lang meta as title: %#v", ev2.Titles)
		}
	}
	if !ok {
		t.Fatalf("Dungeon Meshi dropped: %#v", ev2.Titles)
	}

	ev3 := ParseFile(FileInput{Name: "[我与字幕组的故事][01].mkv"})
	ok = false
	for _, ti := range ev3.Titles {
		if containsFold(ti.Text, "我与字幕组的故事") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("CJK title with 字幕 must keep: %#v", ev3.Titles)
	}
}

func TestRecallParseLeadingBracketTitles(t *testing.T) {
	cases := []struct {
		name  string
		title string
	}{
		{"[Kaiju No. 8][01].mkv", "Kaiju No. 8"},
		{"[86][01].mkv", "86"},
		{"[K-ON!][01].mkv", "K-ON!"},
		{"[Dungeon Meshi][01].mkv", "Dungeon Meshi"},
	}
	for _, tc := range cases {
		ev := ParseFile(FileInput{Name: tc.name})
		if ev.Episode.First != 1 {
			t.Fatalf("%s ep=%+v", tc.name, ev.Episode)
		}
		if ev.Ordinal.Season != 0 {
			t.Fatalf("%s season=%d", tc.name, ev.Ordinal.Season)
		}
		found := false
		for _, ti := range ev.Titles {
			if ti.Text == tc.title || containsFold(ti.Text, tc.title) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s titles=%#v want %q", tc.name, ev.Titles, tc.title)
		}
	}
	// 既有组名不回归
	ev := ParseFile(FileInput{Name: "[GM-Team][国漫][仙王的日常生活 第4季][12][1080P].mp4"})
	if ev.Episode.First != 12 {
		t.Fatalf("GM-Team ep=%+v", ev.Episode)
	}
	ev2 := ParseFile(FileInput{Name: "[Dynamis One] The Daily Life of the Immortal King 5 - 03.mkv"})
	if ev2.Ordinal.Season != 5 || ev2.Episode.First != 3 {
		t.Fatalf("Dynamis One got season=%d ep=%+v titles=%v", ev2.Ordinal.Season, ev2.Episode, ev2.Titles)
	}
}

func TestRecallParsePublishSuffixAndSceneYear(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Sousou no Frieren - 01-SENPAI.mkv"})
	if ev.Name != "Sousou no Frieren - 01-SENPAI.mkv" {
		t.Fatalf("Name mutated: %q", ev.Name)
	}
	if ev.Episode.First != 1 {
		t.Fatalf("ep=%+v", ev.Episode)
	}
	for _, ti := range ev.Titles {
		if containsFold(ti.Text, "SENPAI") {
			t.Fatalf("publish suffix kept in title: %#v", ev.Titles)
		}
		if !containsFold(ti.Text, "Frieren") {
			t.Fatalf("title lost: %#v", ev.Titles)
		}
	}

	ev2 := ParseFile(FileInput{Name: "铃芽之旅.2022.1080p.mkv"})
	if ev2.Year != 2022 {
		t.Fatalf("year=%d want 2022 titles=%v", ev2.Year, ev2.Titles)
	}
	if isUnknownEp(ev2.Episode) == false {
		t.Fatalf("must not invent episode: %+v", ev2.Episode)
	}
	ok := false
	for _, ti := range ev2.Titles {
		if ti.Text == "铃芽之旅" {
			ok = true
		}
		if containsFold(ti.Text, "2022") || containsFold(ti.Text, "1080") {
			t.Fatalf("year/meta in title: %#v", ev2.Titles)
		}
	}
	if !ok {
		t.Fatalf("title=%#v", ev2.Titles)
	}

	ev3 := ParseFile(FileInput{Name: "2001.A.Space.Odyssey.1080p.mkv"})
	if ev3.Year != 0 {
		t.Fatalf("2001 must stay title not year; year=%d", ev3.Year)
	}
	ok = false
	for _, ti := range ev3.Titles {
		if containsFold(ti.Text, "2001") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("2001 title lost: %#v", ev3.Titles)
	}

	ev4 := ParseFile(FileInput{Name: "1984.1080p.mkv"})
	if ev4.Year != 0 || !isUnknownEp(ev4.Episode) {
		t.Fatalf("1984 year=%d ep=%+v", ev4.Year, ev4.Episode)
	}
	if len(ev4.Titles) == 0 || ev4.Titles[0].Text != "1984" {
		t.Fatalf("1984 title=%#v", ev4.Titles)
	}
}

func TestRecallParseDigitProtectionAndSurface(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Kaiju No. 8 - 01.mkv"})
	if ev.Ordinal.Season != 0 || ev.Episode.First != 1 {
		t.Fatalf("season=%d ep=%+v", ev.Ordinal.Season, ev.Episode)
	}
	found := false
	for _, ti := range ev.Titles {
		if ti.Text == "Kaiju No. 8" {
			found = true
		}
	}
	if !found {
		t.Fatalf("titles=%#v", ev.Titles)
	}

	ev2 := ParseFile(FileInput{Name: "Kimi no Na wa. [01].mkv"})
	ev3 := ParseFile(FileInput{Name: "[Kimi no Na wa.][01].mkv"})
	for _, ev := range []FileEvidence{ev2, ev3} {
		ok := false
		for _, ti := range ev.Titles {
			if ti.Text == "Kimi no Na wa" {
				ok = true
			}
			if stringsHasSuffixDot(ti.Text) {
				t.Fatalf("terminating period kept: %#v", ev.Titles)
			}
		}
		if !ok {
			t.Fatalf("surface clean failed: %#v", ev.Titles)
		}
	}

	ev4 := ParseFile(FileInput{Name: "迷宫饭 ／ Dungeon Meshi - 01.mkv"})
	if ev4.Name != "迷宫饭 ／ Dungeon Meshi - 01.mkv" {
		t.Fatalf("Name must keep fullwidth slash: %q", ev4.Name)
	}
	if len(ev4.Titles) != 2 {
		t.Fatalf("want 2 bilingual titles, got %#v", ev4.Titles)
	}
	if ev4.Titles[0].Source != SourceFile || ev4.Titles[1].Source != SourceFile {
		t.Fatalf("sources=%v", ev4.Titles)
	}
	joined := ev4.Titles[0].Text + " " + ev4.Titles[1].Text
	if containsFold(joined, "迷宫饭") == false || containsFold(joined, "Dungeon Meshi") == false {
		t.Fatalf("bilingual split bad: %#v", ev4.Titles)
	}
	for _, ti := range ev4.Titles {
		if containsFold(ti.Text, "迷宫饭") && containsFold(ti.Text, "Dungeon") {
			t.Fatalf("must not keep concatenated third title: %#v", ev4.Titles)
		}
	}

	ev5 := ParseFile(FileInput{Name: "Fate/Zero - 01.mkv"})
	ok := false
	for _, ti := range ev5.Titles {
		if ti.Text == "Fate/Zero" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("Fate/Zero must not split: %#v", ev5.Titles)
	}
}

func stringsHasSuffixDot(s string) bool {
	return strings.HasSuffix(s, ".") || strings.HasSuffix(s, "。")
}

func TestRecallParseDigitIdentityDiscriminators(t *testing.T) {
	a := AnalyzeTitle("Kaiju No. 8")
	b := AnalyzeTitle("Kaiju No. 9")
	c := AnalyzeTitle("U-17 World Cup")
	d := AnalyzeTitle("U-18 World Cup")
	if conf := ConflictingDigitIdentities(a.Discriminators, b.Discriminators); len(conf) == 0 {
		t.Fatalf("No.8 vs No.9 must conflict; discs %v vs %v", a.Discriminators, b.Discriminators)
	}
	if conf := ConflictingDigitIdentities(c.Discriminators, d.Discriminators); len(conf) == 0 {
		t.Fatalf("U-17 vs U-18 must conflict; discs %v vs %v", c.Discriminators, d.Discriminators)
	}
	same := ConflictingDigitIdentities(a.Discriminators, AnalyzeTitle("Kaiju No.8").Discriminators)
	if len(same) != 0 {
		t.Fatalf("No.8 vs No.8 must not conflict: %v", same)
	}
}

func TestRecallParseSequelAndBareEpPreserved(t *testing.T) {
	ev := ParseFile(FileInput{Name: "[北宇治字幕组] 悠久之翼2 [01].mkv"})
	if ev.Titles[0].Text != "悠久之翼2" || ev.Episode.First != 1 {
		t.Fatalf("%+v %#v", ev.Episode, ev.Titles)
	}
	ev2 := ParseFile(FileInput{Name: "Title 5 - 03.mkv"})
	if ev2.Ordinal.Season != 5 || ev2.Episode.First != 3 {
		t.Fatalf("season=%d ep=%+v", ev2.Ordinal.Season, ev2.Episode)
	}
	ev3 := ParseFile(FileInput{Name: "Review Show 05.mkv"})
	if ev3.Episode.First != 5 || ev3.Episode.Explicit {
		t.Fatalf("bare ep=%+v", ev3.Episode)
	}
	frac := ParseFile(FileInput{Name: "Show - 12.5.mkv"})
	if frac.Episode.Kind != NumberFraction {
		t.Fatalf("fraction=%+v", frac.Episode)
	}
}
