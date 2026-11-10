package matching

import (
	"reflect"
	"strings"
	"testing"
)

func titleTexts(ev FileEvidence) []string {
	var out []string
	for _, t := range ev.Titles {
		out = append(out, t.Text)
	}
	return out
}

func assertNoTitleContains(t *testing.T, ev FileEvidence, frags ...string) {
	t.Helper()
	for _, tt := range ev.Titles {
		for _, f := range frags {
			if f != "" && strings.Contains(tt.Text, f) {
				t.Fatalf("标题 %q 不应包含 %q；Titles=%q", tt.Text, f, titleTexts(ev))
			}
		}
	}
}

// 4.5 S1 行：季+集组合、括号集号、总集数（字幕组用 ASCII 组名，不依赖 S2）。
func TestExtractEpisodeTokensBracketForms(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		season     int
		episode    int
		abs        int
		titles     []string
		notInTitle []string
	}{
		{"现场A等价：4th - 19 + 总第85", "[Fansub][Re Zero kara Hajimeru Isekai Seikatsu][4th - 19][总第85][WebRip][1080P_AVC_AAC].mp4", 4, 19, 85, []string{"Re Zero kara Hajimeru Isekai Seikatsu"}, []string{"4th", "总第", "85"}},
		{"S04E19 括号", "[Fansub] Title Name [S04E19][1080p].mkv", 4, 19, 0, []string{"Title Name"}, []string{"S04E19"}},
		{"S4 - 19 括号", "[Fansub] Title Name [S4 - 19][1080p].mkv", 4, 19, 0, []string{"Title Name"}, []string{"S4"}},
		{"S2 - 05 括号", "[Fansub][Title Name][S2 - 05][1080p].mkv", 2, 5, 0, []string{"Title Name"}, []string{"S2"}},
		{"第四季 - 19 括号", "[Fansub] Title Name [第四季 - 19][1080p].mkv", 4, 19, 0, []string{"Title Name"}, []string{"第四季"}},
		{"2nd Season - 05 括号", "[Fansub] Title Name [2nd Season - 05][1080p].mkv", 2, 5, 0, []string{"Title Name"}, []string{"2nd", "Season"}},
		{"第05话", "[Fansub][Title Name][第05话][1080p].mkv", 0, 5, 0, []string{"Title Name"}, []string{"第05话"}},
		{"EP05", "[Fansub][Title Name][EP05][1080p].mkv", 0, 5, 0, []string{"Title Name"}, []string{"EP05"}},
		{"Ep.05", "[Fansub][Title Name][Ep.05][1080p].mkv", 0, 5, 0, []string{"Title Name"}, []string{"Ep.05"}},
		{"Ep 300", "[Fansub][Title Name][Ep 300][1080p].mkv", 0, 300, 0, []string{"Title Name"}, []string{"Ep 300", "300"}},
		{"05 END", "[Fansub][Title Name][05 END][1080p].mkv", 0, 5, 0, []string{"Title Name"}, []string{"END"}},
		{"12 完", "[Fansub][Title Name][12 完][1080p].mkv", 0, 12, 0, []string{"Title Name"}, []string{"完"}},
		{"12(END)", "[Fansub][Title Name][12(END)][1080p].mkv", 0, 12, 0, []string{"Title Name"}, []string{"END"}},
		{"05v2（现有规则保持）", "[Fansub][Title Name][05v2][1080p].mkv", 0, 5, 0, []string{"Title Name"}, []string{"05v2"}},
		{"【】括号第05话", "【Fansub】【Title Name】【第05话】.mp4", 0, 5, 0, []string{"Title Name"}, []string{"第05话"}},
		{"全角 ＥＰ０５ 经 foldWidth", "[Fansub][Title Name][ＥＰ０５].mkv", 0, 5, 0, []string{"Title Name"}, []string{"EP"}},
		{"[86][EP05]：86 保留为标题", "[Fansub][86][EP05][1080p].mkv", 0, 5, 0, []string{"86"}, []string{"EP"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := ParseFile(FileInput{Name: tc.file})
			if ev.Ordinal.Season != tc.season {
				t.Fatalf("Season=%d want %d", ev.Ordinal.Season, tc.season)
			}
			if ev.Episode.Kind != NumberSingle || ev.Episode.First != tc.episode || ev.Episode.Last != tc.episode {
				t.Fatalf("Episode=%+v want single %d", ev.Episode, tc.episode)
			}
			if ev.AbsEpisode != tc.abs {
				t.Fatalf("AbsEpisode=%d want %d", ev.AbsEpisode, tc.abs)
			}
			if got := titleTexts(ev); !reflect.DeepEqual(got, tc.titles) {
				t.Fatalf("Titles=%q want %q", got, tc.titles)
			}
			assertNoTitleContains(t, ev, tc.notInTitle...)
			if len(ev.Conflicts) != 0 {
				t.Fatalf("unexpected conflicts %v", ev.Conflicts)
			}
		})
	}
}

// 4.5 S1 行：裸文件名集号。
func TestExtractEpisodeTokensBareForms(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		episode int
		titles  []string
	}{
		{"Title EP05", "Title EP05.mkv", 5, []string{"Title"}},
		{"Title - E05", "Title - E05.mkv", 5, []string{"Title"}},
		{"Episode 01", "Episode 01.mkv", 1, nil},
		{"EP01", "EP01.mkv", 1, nil},
		{"01 - 副标题", "01 - 副标题.mkv", 1, []string{"副标题"}},
		{"Title_Name_05_BD_1080p", "Title_Name_05_BD_1080p.mkv", 5, []string{"Title_Name"}},
		{"Title.Name.05.1080p.BluRay-GRP", "Title.Name.05.1080p.BluRay-GRP.mkv", 5, []string{"Title Name BluRay-GRP"}},
		{"括号外 EP 与括号规格共存", "[Fansub] Title Name EP05 [1080p].mkv", 5, []string{"Title Name"}},
		{"Ep. 7", "Title Name Ep. 7 [1080p].mkv", 7, []string{"Title Name"}},
		{"标题数字 100 不被当集号", "Mob Psycho 100 EP05.mkv", 5, []string{"Mob Psycho 100"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := ParseFile(FileInput{Name: tc.file})
			if ev.Episode.Kind != NumberSingle || ev.Episode.First != tc.episode || ev.Episode.Last != tc.episode {
				t.Fatalf("Episode=%+v want single %d", ev.Episode, tc.episode)
			}
			if got := titleTexts(ev); !reflect.DeepEqual(got, tc.titles) {
				t.Fatalf("Titles=%q want %q", got, tc.titles)
			}
			if len(ev.Conflicts) != 0 {
				t.Fatalf("unexpected conflicts %v", ev.Conflicts)
			}
		})
	}
}

// “明确才收”：[86]/年份/规格/纯数字括号与已能解析的写法，预抽取保持恒等。
func TestExtractEpisodeTokensLeavesAmbiguousUntouched(t *testing.T) {
	stems := []string{
		"[Fansub][86][05][1080p]",
		"[86][05]",
		"[K-ON!][05]",
		"[中文片名][05]",
		"[Fansub][Title Name][2023][1080P]",
		"[Fansub][Title Name][x264][1080P]",
		"[Fansub][Title Name][86][1080P]",
		"[Fansub][Title Name][12]",
		"[Fansub][Title Name][05v2]",
		"[Fansub][Title Name][E05]",
		"[Fansub][Title Name][01-12]",
		"[Fansub] Title Name - 05 [1080p]",
		"Title S01E05",
		"Title.Name.2019.1080p",
		"Title.12.5.1080p",
		"Mob_Psycho_100_05_BD_1080p",
		"2001 - A Space Odyssey",
		"86 - Eighty Six - 05",
		"Steins Gate 0 - 05",
	}
	for _, s := range stems {
		var ex fileExtras
		if got := extractEpisodeTokens(s, &ex); got != s {
			t.Errorf("%q: 预抽取不应改动，得到 %q", s, got)
		}
		if ex.Episode != 0 || ex.Season != 0 || ex.Abs != 0 {
			t.Errorf("%q: 不应写入 extras，得到 %+v", s, ex)
		}
	}
}

func TestExtractEpisodeTokensNotEpisode(t *testing.T) {
	for _, name := range []string{
		"[Fansub][Title Name][2023][1080P].mkv",
		"[Fansub][Title Name][x264][1080P].mkv",
	} {
		ev := ParseFile(FileInput{Name: name})
		if !isUnknownEp(ev.Episode) {
			t.Errorf("%s: 年份/规格不得当集号，得到 %+v", name, ev.Episode)
		}
	}
	ev := ParseFile(FileInput{Name: "[Fansub][86][05][1080p].mkv"})
	if ev.Episode.First != 5 || !reflect.DeepEqual(titleTexts(ev), []string{"86"}) {
		t.Fatalf("[86][05] 应为标题 86 + 第5集，得到 ep=%+v titles=%q", ev.Episode, titleTexts(ev))
	}
}

// 直接验证返回串移除 token 与 extras 写入。
func TestExtractEpisodeTokensRemovesHitTokens(t *testing.T) {
	tests := []struct {
		stem string
		want string
		ex   fileExtras
	}{
		{"[Fansub][Re Zero][4th - 19][总第85][WebRip]", "[Fansub][Re Zero] [WebRip]", fileExtras{Season: 4, Episode: 19, Abs: 85}},
		{"[Fansub] Title [S04E19][1080p]", "[Fansub] Title [1080p]", fileExtras{Season: 4, Episode: 19}},
		{"[Fansub][Title Name][第05话]", "[Fansub][Title Name]", fileExtras{Episode: 5}},
		{"Title - E05", "Title", fileExtras{Episode: 5}},
		{"EP01", "", fileExtras{Episode: 1}},
		{"01 - 副标题", "副标题", fileExtras{Episode: 1}},
		{"Title_Name_05_BD_1080p", "Title_Name", fileExtras{Episode: 5}},
		{"Title.Name.05.1080p.BluRay-GRP", "Title.Name BluRay-GRP", fileExtras{Episode: 5}},
		// 移除后会暴露标题数字时，原位写入规范明确集号块
		{"[Fansub][86][EP05][1080p]", "[Fansub][86] [05] [1080p]", fileExtras{Episode: 5}},
		{"Mob Psycho 100 EP05", "Mob Psycho 100 [05]", fileExtras{Episode: 5}},
	}
	for _, tc := range tests {
		var ex fileExtras
		got := extractEpisodeTokens(tc.stem, &ex)
		if got != tc.want {
			t.Errorf("%q: got %q want %q", tc.stem, got, tc.want)
		}
		if ex.Season != tc.ex.Season || ex.Episode != tc.ex.Episode || ex.Abs != tc.ex.Abs || ex.EpisodeLast != 0 || ex.Part != 0 || ex.Final {
			t.Errorf("%q: extras=%+v want %+v", tc.stem, ex, tc.ex)
		}
		if len(ex.Notes) == 0 {
			t.Errorf("%q: 应记录诊断 Notes", tc.stem)
		}
	}
}

// 多个互斥集号 token：保守不抽取集号；END 不推断 Final（Final 仅表示最终季）。
func TestExtractEpisodeTokensConservative(t *testing.T) {
	var ex fileExtras
	stem := "[Fansub][Title Name][EP05][第06话]"
	if got := extractEpisodeTokens(stem, &ex); got != stem || ex.Episode != 0 {
		t.Fatalf("互斥集号应放弃抽取，got %q ex=%+v", got, ex)
	}
	ev := ParseFile(FileInput{Name: "[Fansub][Title Name][12 完].mkv"})
	if ev.Ordinal.Final {
		t.Fatalf("[12 完] 不得设置 Ordinal.Final")
	}
	var ex2 fileExtras
	if got := extractEpisodeTokens("", &ex2); got != "" || !reflect.DeepEqual(ex2, fileExtras{}) {
		t.Fatalf("空串应恒等")
	}
	if got := extractEpisodeTokens("[Fansub][Title][EP05]", nil); got != "[Fansub][Title][EP05]" {
		t.Fatalf("nil extras 应恒等")
	}
}

// 与既有 quality 夹具 chinese_blocks 写法共存：纯数字集号括号仍由 classifyBlocks 处理。
func TestExtractEpisodeTokensChineseBlocksUnchanged(t *testing.T) {
	name := "[GM-Team][国漫][仙王的日常生活 第4季][Xian Wang De Ri Chang Sheng Huo Ⅳ][2023][12][AVC][GB][1080P]"
	var ex fileExtras
	if got := extractEpisodeTokens(name, &ex); got != name || ex.Episode != 0 {
		t.Fatalf("chinese_blocks 样本不应被预抽取改动，got %q ex=%+v", got, ex)
	}
}
