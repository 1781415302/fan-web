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

func TestRecallParseFateZeroKeepsHyphenTitle(t *testing.T) {
	cases := []string{
		"Fate-Zero - 01.mkv",
		"Fate-Zero S01E01.mkv",
		"Fate-Zero 第01话.mkv",
		"Fate-Zero [01].mkv",
	}
	for _, name := range cases {
		ev := ParseFile(FileInput{Name: name})
		if ev.Name != name {
			t.Fatalf("%s: Name mutated to %q", name, ev.Name)
		}
		if ev.Episode.First != 1 {
			t.Fatalf("%s: ep=%+v", name, ev.Episode)
		}
		found := false
		for _, ti := range ev.Titles {
			if ti.Text == "Fate-Zero" {
				found = true
			}
			if ti.Text == "Fate" {
				t.Fatalf("%s: title stripped to Fate: %#v", name, ev.Titles)
			}
		}
		if !found {
			t.Fatalf("%s: Fate-Zero title lost: %#v", name, ev.Titles)
		}
	}

	// 正例：集号之后的发布组仍剥离
	pub := ParseFile(FileInput{Name: "Fate-Zero - 01-SENPAI.mkv"})
	if pub.Name != "Fate-Zero - 01-SENPAI.mkv" {
		t.Fatalf("Name mutated: %q", pub.Name)
	}
	if pub.Episode.First != 1 {
		t.Fatalf("ep=%+v", pub.Episode)
	}
	okTitle, keptPub := false, false
	for _, ti := range pub.Titles {
		if ti.Text == "Fate-Zero" {
			okTitle = true
		}
		if containsFold(ti.Text, "SENPAI") {
			keptPub = true
		}
	}
	if !okTitle || keptPub {
		t.Fatalf("publish suffix: titles=%#v", pub.Titles)
	}
}

func TestRecallParseSxxEyyRevisionSuffix(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Campus Notes S01E05v2.mkv"})
	if ev.Name != "Campus Notes S01E05v2.mkv" {
		t.Fatalf("Name mutated: %q", ev.Name)
	}
	if ev.Ordinal.Season != 1 || ev.Episode.First != 5 || ev.Episode.Last != 5 {
		t.Fatalf("want S1E5, got season=%d ep=%+v", ev.Ordinal.Season, ev.Episode)
	}
	if ev.Episode.First == 2 {
		t.Fatal("must not take revision digit as episode")
	}
	found := false
	for _, ti := range ev.Titles {
		if containsFold(ti.Text, "Campus Notes") {
			found = true
		}
		if ti.Text == "Campus Notes S01E05v2" || strings.Contains(strings.ToLower(ti.Text), "v2") {
			t.Fatalf("revision leaked into title: %#v", ev.Titles)
		}
	}
	if !found {
		t.Fatalf("title lost: %#v", ev.Titles)
	}

	// 既有 dash / bracket 修订号不受影响
	dash := ParseFile(FileInput{Name: "Campus Notes - 05v2.mkv"})
	if dash.Episode.First != 5 {
		t.Fatalf("dash 05v2: %+v", dash.Episode)
	}
	br := ParseFile(FileInput{Name: "Campus Notes [05v2].mkv"})
	if br.Episode.First != 5 {
		t.Fatalf("bracket 05v2: %+v", br.Episode)
	}
}

func TestSceneSxxEyyTruncatesEpisodeTitleAndSpecs(t *testing.T) {
	name := "The.Daily.Life.of.the.Immortal.King.S05E12.Siblings.Alliance.1080p.CR.WEB-DL.DUAL.AAC2.0.H.264-VARYG.mkv"
	ev := ParseFile(FileInput{Name: name})
	if ev.Name != name {
		t.Fatalf("Name mutated: %q", ev.Name)
	}
	if ev.Ordinal.Season != 5 || ev.Episode.Kind != NumberSingle || ev.Episode.First != 12 || ev.Episode.Last != 12 {
		t.Fatalf("want S5E12, got season=%d ep=%+v", ev.Ordinal.Season, ev.Episode)
	}
	if len(ev.Titles) != 1 || ev.Titles[0].Text != "The Daily Life of the Immortal King" || ev.Titles[0].Source != SourceFile {
		t.Fatalf("titles=%#v", ev.Titles)
	}
	for _, leak := range []string{"Siblings", "Alliance", "DUAL", "AAC", "264", "VARYG"} {
		if titlesContainSub(ev, leak) {
			t.Fatalf("%q leaked into titles: %#v", leak, ev.Titles)
		}
	}
	if len(ev.Conflicts) != 0 {
		t.Fatalf("conflicts=%v", ev.Conflicts)
	}
}

func TestSceneSxxEyyPrefixFormKeepsTitleAfter(t *testing.T) {
	// hasStrongSceneTitle(before) 为假：前缀式保持 removeEpisodeAt 原逻辑
	ev := ParseFile(FileInput{Name: "S05E12.Title.1080p.mkv"})
	if ev.Ordinal.Season != 5 || ev.Episode.First != 12 {
		t.Fatalf("season=%d ep=%+v", ev.Ordinal.Season, ev.Episode)
	}
	if len(ev.Titles) != 1 || ev.Titles[0].Text != "Title" {
		t.Fatalf("titles=%#v", ev.Titles)
	}
}

func TestSceneSxxEyyMediaFromTruncatedTail(t *testing.T) {
	// 截断前先从 after 探测 media，且后续 stripMedia 不得把它覆盖回 KindUnknown
	ev := ParseFile(FileInput{Name: "Title.Name.S01E03.OVA.1080p.WEB-DL.mkv"})
	if ev.Media != KindOVA {
		t.Fatalf("media=%v want ova", ev.Media)
	}
	if ev.Ordinal.Season != 1 || ev.Episode.First != 3 {
		t.Fatalf("season=%d ep=%+v", ev.Ordinal.Season, ev.Episode)
	}
	if len(ev.Titles) != 1 || ev.Titles[0].Text != "Title Name" {
		t.Fatalf("titles=%#v", ev.Titles)
	}
	// parseRest 直接调用：无任何媒体标记时返回 KindUnknown 而非零值 ""
	_, _, _, _, media := parseRest("Plain Title - 05", false)
	if media != KindUnknown {
		t.Fatalf("parseRest media=%q want KindUnknown", media)
	}
	// 集号之前的媒体标记仍由原 stripMedia 识别
	_, _, _, _, media = parseRest("Title Movie S01E01", false)
	if media != KindMovie {
		t.Fatalf("parseRest media=%q want movie", media)
	}
}

func TestSceneSxxEyyProtectsNoAndDecimal(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Kaiju.No.8.S01E05.1080p.WEB-DL.AAC2.0.H.264-GRP.mkv"})
	if ev.Ordinal.Season != 1 || ev.Episode.First != 5 {
		t.Fatalf("season=%d ep=%+v", ev.Ordinal.Season, ev.Episode)
	}
	if len(ev.Titles) != 1 || ev.Titles[0].Text != "Kaiju No. 8" {
		t.Fatalf("titles=%#v", ev.Titles)
	}
	frac := ParseFile(FileInput{Name: "Show - 12.5.mkv"})
	if frac.Episode.Kind != NumberFraction || frac.Episode.Raw != "12.5" {
		t.Fatalf("fraction=%+v", frac.Episode)
	}
}

func TestStripSceneDotSpecs(t *testing.T) {
	cases := []struct{ in, want string }{
		// 仅本体变空格，左分隔与右侧紧贴的 - 保留，需循环两轮
		{".AAC2.0.H.264-VARYG", ". . -VARYG"},
		{"Title.1080p.AAC2.0.H.264-VARYG", "Title.1080p. . -VARYG"},
		{"Title.DDP5.1.x.265", "Title. . "},
		{"Title EAC3 5.1 h.264", "Title EAC3 5.1  "},
		{"aac2.0", " "},
		// 年份与发布组 token 本身不被吞掉
		{"Show.2022.AAC2.0.H.264-GRP", "Show.2022. . -GRP"},
		// 无前缀的 2.0 / 5.1 / 1.0 不动
		{"Evangelion 2.0", "Evangelion 2.0"},
		{"Evangelion.2.0.You.Can.Not.Advance.2009.1080p.BluRay", "Evangelion.2.0.You.Can.Not.Advance.2009.1080p.BluRay"},
		{"Movie 5.1 Title 1.0", "Movie 5.1 Title 1.0"},
		// No.8 / 12.5 占位保护
		{"Kaiju.No.8.H.264", "Kaiju.No.8. "},
		{"Show - 12.5 AAC2.0", "Show - 12.5  "},
		// 单词内部不命中
		{"Title.FLAC2.0x264", "Title.FLAC2.0x264"},
		{"Bach.264", "Bach.264"},
	}
	for _, tc := range cases {
		if got := stripSceneDotSpecs(tc.in); got != tc.want {
			t.Fatalf("stripSceneDotSpecs(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestSceneDotSpecsKeepSceneYearWithResolution(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Show.2022.1080p.AAC2.0.H.264-GRP.mkv"})
	if ev.Year != 2022 {
		t.Fatalf("year=%d want 2022 titles=%#v", ev.Year, ev.Titles)
	}
	for _, leak := range []string{"2022", "AAC", "264", "1080"} {
		if titlesContainSub(ev, leak) {
			t.Fatalf("%q leaked into titles: %#v", leak, ev.Titles)
		}
	}
}

func TestSceneDotSpecsEvangelionRegression(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Evangelion.2.0.You.Can.Not.Advance.2009.1080p.BluRay.mkv"})
	if ev.Year != 2009 {
		t.Fatalf("year=%d", ev.Year)
	}
	if len(ev.Titles) != 1 || !strings.HasPrefix(ev.Titles[0].Text, "Evangelion 2 0") {
		t.Fatalf("2.0 must stay in title: %#v", ev.Titles)
	}
	ev2 := ParseFile(FileInput{Name: "Evangelion 2.0 You Can (Not) Advance.mkv"})
	if ev2.Episode.Raw != "2.0" {
		t.Fatalf("2.0 must not be stripped before numbering: ep=%+v titles=%#v", ev2.Episode, ev2.Titles)
	}
}

func TestStripSceneNoiseTokensLatinMeta(t *testing.T) {
	got := stripSceneNoiseTokens("Title DUAL MULTI Sub Dub DDP EAC3 TrueHD Atmos 1080p WEB-DL")
	if got != "Title" {
		t.Fatalf("got %q", got)
	}
}

func TestParseDirectoryBothSeparators(t *testing.T) {
	for _, dir := range []string{"Anime/Title", `Anime\Title`, `Anime/Title\`, `\Anime//Title`} {
		titles, _, _, _ := parseDirectory(dir)
		if len(titles) != 1 || titles[0] != "Title" {
			t.Fatalf("%q: titles=%v", dir, titles)
		}
	}
	titles, season, _, _ := parseDirectory(`Title\第2季`)
	if season != 2 || len(titles) != 1 || titles[0] != "Title" {
		t.Fatalf("titles=%v season=%d", titles, season)
	}
	if titles, _, _, _ := parseDirectory(filepath.FromSlash("Anime/Title")); len(titles) != 1 || titles[0] != "Title" {
		t.Fatalf("OS separator: %v", titles)
	}
}

func TestSceneShow2022DotSpecsYearAndGroup(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Show.2022.AAC2.0.H.264-GRP.mkv"})
	if ev.Year != 2022 {
		t.Fatalf("year=%d want 2022 titles=%#v", ev.Year, ev.Titles)
	}
	if len(ev.Titles) != 1 || ev.Titles[0].Text != "Show" || ev.Titles[0].Source != SourceFile {
		t.Fatalf("titles=%#v want only \"Show\"", ev.Titles)
	}
	if !isUnknownEp(ev.Episode) || len(ev.Conflicts) != 0 {
		t.Fatalf("ep=%+v conflicts=%v", ev.Episode, ev.Conflicts)
	}
	ev2 := ParseFile(FileInput{Name: "Show.2022.1080p.WEB-DL.DDP5.1.x265-VARYG.mkv"})
	if ev2.Year != 2022 || len(ev2.Titles) != 1 || ev2.Titles[0].Text != "Show" {
		t.Fatalf("year=%d titles=%#v", ev2.Year, ev2.Titles)
	}
}

func TestSceneShow2022DottedXCodecYear(t *testing.T) {
	for _, name := range []string{
		"Show.2022.x.265-GRP.mkv",
		"Show.2022.x.264-GRP.mkv",
		"Show.2022.X.265-GRP.mkv",
		"Show.2022.H.264-GRP.mkv",
	} {
		ev := ParseFile(FileInput{Name: name})
		if ev.Year != 2022 {
			t.Fatalf("%s: year=%d want 2022 titles=%#v", name, ev.Year, ev.Titles)
		}
		if len(ev.Titles) != 1 || ev.Titles[0].Text != "Show" {
			t.Fatalf("%s: titles=%#v want only \"Show\"", name, ev.Titles)
		}
		if !isUnknownEp(ev.Episode) || len(ev.Conflicts) != 0 {
			t.Fatalf("%s: ep=%+v conflicts=%v", name, ev.Episode, ev.Conflicts)
		}
	}
	for _, low := range []string{"x.265", "x.264", "h.264", "h.265"} {
		if !isSceneMetaAfterToken(low) {
			t.Fatalf("isSceneMetaAfterToken(%q)=false", low)
		}
	}
}

func TestSplitSceneDotPartsProtectsDotSpecs(t *testing.T) {
	got := splitSceneDotParts("Kaiju.No.8.2022.AAC2.0.H.264-GRP.12.5")
	want := []string{"Kaiju", "No.8", "2022", "AAC2.0", "H.264-GRP", "12.5"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("parts=%q want %q", got, want)
	}
	if joinSceneDotParts(got) != "Kaiju.No.8.2022.AAC2.0.H.264-GRP.12.5" {
		t.Fatalf("join not identity: %q", joinSceneDotParts(got))
	}
}

func TestHasSceneMetaAfterDotSpecsAndGroup(t *testing.T) {
	for _, parts := range [][]string{
		{"AAC2.0"}, {"DDP5.1"}, {"H.264-GRP"}, {"x265-VARYG"}, {"1080p-GRP"}, {"WEB-DL"}, {"10-bit"},
	} {
		if !hasSceneMetaAfter(parts) {
			t.Fatalf("%q must count as scene meta", parts)
		}
	}
	for _, parts := range [][]string{
		{"Fate-Zero"}, {"K-ON"}, {"Title"}, {"2.0"}, {"H.264-SP"}, {"x265-ab"}, {" ", " -GRP"},
	} {
		if hasSceneMetaAfter(parts) {
			t.Fatalf("%q must not count as scene meta", parts)
		}
	}
}

func TestStripTrailingSceneGroupGuards(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Show.AAC2.0.H.264-GRP", "Show.AAC2.0.H.264"},
		{"Show.1080p.WEB-DL.x265-VARYG", "Show.1080p.WEB-DL.x265"},
		{"Show 1080p-GRP", "Show 1080p"},
		{"Show.DDP5.1-GRP", "Show.DDP5.1"},
		{"Show.WEB-DL-GRP", "Show.WEB-DL"},
		// 标题连字符不动
		{"Fate-Zero", "Fate-Zero"},
		{"K-ON", "K-ON"},
		{"Title.K-ON", "Title.K-ON"},
		// 整体属于规格词表
		{"Show.1080p.WEB-DL", "Show.1080p.WEB-DL"},
		{"Show.Blu-Ray", "Show.Blu-Ray"},
		{"Show.10-bit", "Show.10-bit"},
		// 发布组护栏：过短、结构词
		{"Show.x264-AB", "Show.x264-AB"},
		{"Show.x264-Part", "Show.x264-Part"},
		{"Show.x264-OVA", "Show.x264-OVA"},
		// 须有 <分隔符> 前缀
		{"x264-GRP", "x264-GRP"},
		// 左侧不是元数据词
		{"Show.Title-GRP", "Show.Title-GRP"},
	}
	for _, tc := range cases {
		if got := stripTrailingSceneGroup(tc.in); got != tc.want {
			t.Fatalf("stripTrailingSceneGroup(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestSceneTrailingGroupNoMisfireParseFile(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"Fate-Zero.2011.1080p.BluRay.x264-GRP.mkv", "Fate-Zero"},
		{"K-ON.2009.1080p.WEB-DL.mkv", "K-ON"},
		{"Fate-Zero - 01.mkv", "Fate-Zero"},
	} {
		ev := ParseFile(FileInput{Name: tc.name})
		if len(ev.Titles) != 1 || ev.Titles[0].Text != tc.want {
			t.Fatalf("%s: titles=%#v want %q", tc.name, ev.Titles, tc.want)
		}
	}
}
