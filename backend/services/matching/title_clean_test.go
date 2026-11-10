package matching

import (
	"reflect"
	"strings"
	"testing"
)

func cleanTitleTexts(ev FileEvidence) []string {
	out := make([]string, 0, len(ev.Titles))
	for _, t := range ev.Titles {
		out = append(out, t.Text)
	}
	return out
}

func titlesContainSub(ev FileEvidence, sub string) bool {
	for _, t := range ev.Titles {
		if strings.Contains(t.Text, sub) {
			return true
		}
	}
	return false
}

// 占位块文本须令 isClearGroupName / looksLikeFansub 恒为真，且在任意位置被现有分类归为字幕组/噪声。
func TestTitleCleanPlaceholderChoice(t *testing.T) {
	if !isClearGroupName(fansubPlaceholder) {
		t.Fatalf("isClearGroupName(%q) must be true", fansubPlaceholder)
	}
	if !looksLikeFansub(fansubPlaceholder) {
		t.Fatalf("looksLikeFansub(%q) must be true", fansubPlaceholder)
	}
	if !looksLikeLeadingFansub(fansubPlaceholder) {
		t.Fatalf("looksLikeLeadingFansub(%q) must be true", fansubPlaceholder)
	}
	if hasTitleDigitIdentity(fansubPlaceholder) || looksLikeKnownTitleForm(fansubPlaceholder) {
		t.Fatalf("placeholder must not look like a title form")
	}
	cb := classifyBlocks([]string{fansubPlaceholder, "标题", "05"}, "")
	if len(cb) != 3 || cb[0].kind != blockFansub || cb[1].kind != blockTitle || cb[2].kind != blockEpisode {
		t.Fatalf("placeholder classification unexpected: %+v", cb)
	}
	if !isCompositeMetaBlock(metaPlaceholder) {
		t.Fatalf("isCompositeMetaBlock(%q) must be true", metaPlaceholder)
	}
	cb = classifyBlocks([]string{metaPlaceholder, "标题", "05"}, "")
	if len(cb) != 3 || cb[0].kind != blockLang || cb[1].kind != blockTitle {
		t.Fatalf("meta placeholder classification unexpected: %+v", cb)
	}
}

func TestTitleCleanExtractFansubMetaReplacesNotDeletes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"[喵萌奶茶屋][标题][05]", "[Fansub][标题][05]"},
		{"【爱恋字幕社】[标题][05]", "[Fansub][标题][05]"},
		{"[喵萌奶茶屋&LoliHouse] 标题 - 05 [1080p]", "[Fansub] 标题 - 05 [1080p]"},
		{"[喵萌奶茶屋×LoliHouse][标题] - 05", "[Fansub][标题] - 05"},
		{"[Group] Title [简日双语内嵌] - 05", "[Group] Title [字幕] - 05"},
	}
	for _, tc := range cases {
		var ex fileExtras
		got := extractFansubMeta(tc.in, &ex)
		if got != tc.want {
			t.Fatalf("extractFansubMeta(%q)=%q want %q", tc.in, got, tc.want)
		}
		if len(ex.Notes) == 0 {
			t.Fatalf("extractFansubMeta(%q) should record a note", tc.in)
		}
		if ex.Season != 0 || ex.Episode != 0 || ex.Abs != 0 || ex.Part != 0 || ex.Final {
			t.Fatalf("extractFansubMeta must not write numbering extras: %+v", ex)
		}
	}
}

func TestTitleCleanLeadingCJKFansubParseFile(t *testing.T) {
	cases := []struct {
		name, fansub string
		want         []string
	}{
		{"[喵萌奶茶屋][标题][05].mp4", "喵萌奶茶屋", []string{"标题"}},
		{"[爱恋字幕社][标题][05].mp4", "爱恋字幕社", []string{"标题"}},
		{"[晚街与灯][Re Zero kara Hajimeru Isekai Seikatsu] - 05.mp4", "晚街与灯", []string{"Re Zero kara Hajimeru Isekai Seikatsu"}},
		{"[喵萌奶茶屋&LoliHouse] 标题 - 05 [1080p].mkv", "喵萌奶茶屋", []string{"标题"}},
		{"[喵萌奶茶屋×LoliHouse][标题] - 05.mkv", "LoliHouse", []string{"标题"}},
	}
	for _, tc := range cases {
		ev := ParseFile(FileInput{Name: tc.name})
		if titlesContainSub(ev, tc.fansub) {
			t.Fatalf("%s: fansub %q leaked into titles %v", tc.name, tc.fansub, cleanTitleTexts(ev))
		}
		if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: titles=%v want %v", tc.name, got, tc.want)
		}
		if ev.Episode.First != 5 {
			t.Fatalf("%s: episode=%+v want 5", tc.name, ev.Episode)
		}
		if titlesContainSub(ev, fansubPlaceholder) {
			t.Fatalf("%s: placeholder leaked into titles %v", tc.name, cleanTitleTexts(ev))
		}
	}
}

// 首块位置语义：字幕组块被替换为占位块而非删除，紧随的块不会变成"首块"。
func TestTitleCleanFirstBlockPositionSemantics(t *testing.T) {
	// 若直接删除首块，[Bocchi the Rock] 成为首块并按 looksLikeLeadingFansub 被当成字幕组。
	deleted := ParseFile(FileInput{Name: "[Bocchi the Rock][孤独摇滚][05].mkv"})
	if titlesContainSub(deleted, "Bocchi") {
		t.Fatalf("precondition: deletion counterfactual should lose the leading latin title, got %v", cleanTitleTexts(deleted))
	}
	ev := ParseFile(FileInput{Name: "[喵萌奶茶屋][Bocchi the Rock][孤独摇滚][05].mkv"})
	if got, want := cleanTitleTexts(ev), []string{"Bocchi the Rock", "孤独摇滚"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("placeholder must keep following block position: titles=%v want %v", got, want)
	}

	// [喵萌奶茶屋][标题][05]：中文短标题保留。
	ev = ParseFile(FileInput{Name: "[喵萌奶茶屋][标题][05].mp4"})
	if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, []string{"标题"}) || ev.Episode.First != 5 {
		t.Fatalf("[喵萌奶茶屋][标题][05]: titles=%v ep=%+v", got, ev.Episode)
	}

	// [喵萌奶茶屋][ABC][05]（短英文标题）：字幕组不进标题；结果与 ASCII 组名形态 [Fansub][ABC][05]
	// 完全一致（ABC 处于非首块位置，沿用现有非首块分类），且目录兜底可用。
	ev = ParseFile(FileInput{Name: "[喵萌奶茶屋][ABC][05].mp4"})
	ascii := ParseFile(FileInput{Name: "[Fansub][ABC][05].mp4"})
	if titlesContainSub(ev, "喵萌奶茶屋") {
		t.Fatalf("[喵萌奶茶屋][ABC][05]: fansub leaked into titles %v", cleanTitleTexts(ev))
	}
	if !reflect.DeepEqual(cleanTitleTexts(ev), cleanTitleTexts(ascii)) || ev.Episode != ascii.Episode {
		t.Fatalf("[喵萌奶茶屋][ABC][05] must parse like [Fansub][ABC][05]: %v/%+v vs %v/%+v",
			cleanTitleTexts(ev), ev.Episode, cleanTitleTexts(ascii), ascii.Episode)
	}
	if ev.Episode.First != 5 {
		t.Fatalf("[喵萌奶茶屋][ABC][05]: episode=%+v", ev.Episode)
	}
	withDir := ParseFile(FileInput{Name: "[喵萌奶茶屋][ABC][05].mp4", Dir: "ABC"})
	if len(withDir.Titles) != 1 || withDir.Titles[0].Text != "ABC" || withDir.Titles[0].Source != SourceDirectory {
		t.Fatalf("[喵萌奶茶屋][ABC][05] + dir ABC: titles=%+v", withDir.Titles)
	}
}

// 防误伤：单块/首块即唯一标题、非 CJK 首块、季号/类别首块均不当字幕组。
func TestTitleCleanFansubNoMisfire(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{"[K-ON!][05].mp4", []string{"K-ON!"}},
		{"[86][05].mp4", []string{"86"}},
		{"[中文片名][05].mp4", []string{"中文片名"}},
		{"[中文片名][1080p][05].mp4", []string{"中文片名"}},
		{"[中文片名] - 05.mkv", []string{"中文片名"}},
		{"[喵萌奶茶屋][05].mp4", []string{"喵萌奶茶屋"}},
	}
	for _, tc := range cases {
		stem := strings.TrimSuffix(tc.name, ".mp4")
		stem = strings.TrimSuffix(stem, ".mkv")
		var ex fileExtras
		if got := extractFansubMeta(foldWidthAndT2S(stem), &ex); got != foldWidthAndT2S(stem) {
			t.Fatalf("%s: extractFansubMeta must be identity, got %q", tc.name, got)
		}
		ev := ParseFile(FileInput{Name: tc.name})
		if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: titles=%v want %v", tc.name, got, tc.want)
		}
		if ev.Episode.First != 5 {
			t.Fatalf("%s: episode=%+v want 5", tc.name, ev.Episode)
		}
	}
	for _, stem := range []string{"[第二季][标题][05]", "[剧场版][标题]", "[国漫][标题][05]", "[Title][喵萌奶茶屋][05]", "标题 [喵萌奶茶屋] - 05", "[SPY×FAMILY][间谍过家家][05]"} {
		if got := extractFansubMeta(stem, &fileExtras{}); got != stem {
			t.Fatalf("extractFansubMeta(%q) must be identity, got %q", stem, got)
		}
	}
}

func TestTitleCleanMetaBlocks(t *testing.T) {
	ev := ParseFile(FileInput{Name: "[Fansub] Title [简日 简中 繁日双语 粤日双语 ENG SUB Dual Audio MultiSub Web 1080p CRC32 ABCD1234] - 05.mkv"})
	if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, []string{"Title"}) {
		t.Fatalf("meta block leaked: titles=%v", got)
	}
	if ev.Episode.First != 5 {
		t.Fatalf("episode=%+v", ev.Episode)
	}
	ev = ParseFile(FileInput{Name: "[Group][标题][简日双语内嵌][05].mp4"})
	if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, []string{"标题"}) {
		t.Fatalf("[简日双语内嵌] leaked: titles=%v", got)
	}
	// 元数据块位于首块时以占位块保持位置
	ev = ParseFile(FileInput{Name: "[简日双语][标题][05].mp4"})
	if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, []string{"标题"}) {
		t.Fatalf("leading meta block: titles=%v", got)
	}

	positives := []string{"简日双语内嵌", "简日 简中 繁日双语 粤日双语", "ENG SUB", "Dual Audio", "CRC32 ABCD1234", "简繁日双语", "MultiSub Web 1080p"}
	for _, b := range positives {
		if !isFansubMetaBlock(b) {
			t.Fatalf("isFansubMetaBlock(%q) should be true", b)
		}
	}
	negatives := []string{"Dual", "Web", "Title Name", "简日 标题", "双语少女", "Dual Audio Girl", "ABCD1234 Title"}
	for _, b := range negatives {
		if isFansubMetaBlock(b) {
			t.Fatalf("isFansubMetaBlock(%q) should be false", b)
		}
	}
}

// 词表按长度降序贪心。
func TestTitleCleanMetaTokensLongestFirst(t *testing.T) {
	for i := 1; i < len(fansubMetaTokens); i++ {
		if len(fansubMetaTokens[i]) > len(fansubMetaTokens[i-1]) {
			t.Fatalf("fansubMetaTokens not sorted by length desc at %d: %q > %q", i, fansubMetaTokens[i], fansubMetaTokens[i-1])
		}
	}
	for _, s := range []string{"简繁日双语", "简日双语内嵌", "简繁日内封字幕", "繁日双语"} {
		if !greedyMatchTokens(s, fansubMetaTokens) {
			t.Fatalf("greedyMatchTokens(%q) should be true", s)
		}
	}
	if greedyMatchTokens("简日双语少女", fansubMetaTokens) {
		t.Fatalf("partial match must fail")
	}
}

func TestTitleCleanOrdinalWordsParseFile(t *testing.T) {
	ev := ParseFile(FileInput{Name: "Title Name Part 2 - 05.mkv"})
	if ev.Ordinal.Season != 0 || ev.Ordinal.Part != 2 || ev.Episode.First != 5 {
		t.Fatalf("Part 2: ordinal=%+v ep=%+v", ev.Ordinal, ev.Episode)
	}
	if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, []string{"Title Name"}) || titlesContainSub(ev, "Part") {
		t.Fatalf("Part 2: titles=%v", got)
	}

	ev = ParseFile(FileInput{Name: "Title Part II - 05.mkv"})
	if ev.Ordinal.Season != 0 || ev.Ordinal.Part != 2 || titlesContainSub(ev, "Part") {
		t.Fatalf("Part II: ordinal=%+v titles=%v", ev.Ordinal, cleanTitleTexts(ev))
	}

	for _, tc := range []struct {
		name   string
		season int
	}{
		{"Title Second Season - 05.mkv", 2},
		{"Title Third Season - 05.mkv", 3},
		{"Title_Fourth_Season - 05.mkv", 4},
	} {
		ev = ParseFile(FileInput{Name: tc.name})
		if ev.Ordinal.Season != tc.season || ev.Ordinal.Final || ev.Episode.First != 5 {
			t.Fatalf("%s: ordinal=%+v ep=%+v", tc.name, ev.Ordinal, ev.Episode)
		}
		if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, []string{"Title"}) {
			t.Fatalf("%s: titles=%v", tc.name, got)
		}
	}

	ev = ParseFile(FileInput{Name: "Title Final Season - 05.mkv"})
	if !ev.Ordinal.Final || ev.Ordinal.Season != 0 || ev.Episode.First != 5 {
		t.Fatalf("Final Season: ordinal=%+v ep=%+v", ev.Ordinal, ev.Episode)
	}

	// 已有 2nd Season 写法不受影响
	ev = ParseFile(FileInput{Name: "Title 2nd Season - 05.mkv"})
	if ev.Ordinal.Season != 2 {
		t.Fatalf("2nd Season regression: ordinal=%+v", ev.Ordinal)
	}
}

func TestTitleCleanExtractOrdinalWordsUnit(t *testing.T) {
	var ex fileExtras
	if got := extractOrdinalWords("Title Name Part 2 - 05", &ex); strings.Contains(got, "Part") || ex.Part != 2 || ex.Season != 0 {
		t.Fatalf("Part 2: got %q ex=%+v", got, ex)
	}
	ex = fileExtras{}
	if got := extractOrdinalWords("Title Second Season - 05", &ex); strings.Contains(got, "Season") || ex.Season != 2 {
		t.Fatalf("Second Season: got %q ex=%+v", got, ex)
	}
	ex = fileExtras{}
	if got := extractOrdinalWords("Title Final Season - 05", &ex); got != "Title Final Season - 05" || !ex.Final || ex.Season != 0 {
		t.Fatalf("Final Season: got %q ex=%+v", got, ex)
	}
	// 前序 pass 已有不同季号：不覆盖、不移除
	ex = fileExtras{Season: 4}
	if got := extractOrdinalWords("Title Second Season - 05", &ex); got != "Title Second Season - 05" || ex.Season != 4 {
		t.Fatalf("season conflict: got %q ex=%+v", got, ex)
	}
	// 单词内部的 part / season 不命中
	for _, s := range []string{"Counterpart 2 - 05", "Departure - 05", "Partner 3 - 05", "Seasons Second - 05"} {
		ex = fileExtras{}
		if got := extractOrdinalWords(s, &ex); got != s || ex.Part != 0 || ex.Season != 0 || ex.Final {
			t.Fatalf("extractOrdinalWords(%q) must be identity, got %q ex=%+v", s, got, ex)
		}
	}
}

func TestTitleCleanParallelTitlesParseFile(t *testing.T) {
	cases := []struct {
		name   string
		want   []string
		season int
	}{
		{"孤独摇滚！ Bocchi the Rock! - 05.mkv", []string{"孤独摇滚!", "Bocchi the Rock!"}, 0},
		{"碧蓝之海_Grand Blue Season 2 - 05.mkv", []string{"碧蓝之海", "Grand Blue"}, 2},
		{"A ｜ B - 05.mkv", []string{"A", "B"}, 0},
	}
	for _, tc := range cases {
		ev := ParseFile(FileInput{Name: tc.name})
		if got := cleanTitleTexts(ev); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: titles=%v want %v", tc.name, got, tc.want)
		}
		if ev.Ordinal.Season != tc.season || ev.Episode.First != 5 {
			t.Fatalf("%s: ordinal=%+v ep=%+v", tc.name, ev.Ordinal, ev.Episode)
		}
	}
}
