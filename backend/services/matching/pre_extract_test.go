package matching

import (
	"reflect"
	"testing"
)

func TestMergeExtras(t *testing.T) {
	tests := []struct {
		name          string
		ev            FileEvidence
		ex            fileExtras
		wantEpisode   EpisodeToken
		wantSeason    int
		wantAbs       int
		wantPart      int
		wantFinal     bool
		wantConflicts []string
	}{
		{
			name:        "未知集号时写入单集 NumberSingle 且 Explicit",
			ev:          FileEvidence{Episode: EpisodeToken{Kind: NumberUnknown}},
			ex:          fileExtras{Episode: 19},
			wantEpisode: EpisodeToken{Kind: NumberSingle, First: 19, Last: 19, Explicit: true},
		},
		{
			name:        "未知集号且区间末集不同时写入 NumberRange",
			ev:          FileEvidence{},
			ex:          fileExtras{Episode: 12, EpisodeLast: 19},
			wantEpisode: EpisodeToken{Kind: NumberRange, First: 12, Last: 19, Explicit: true},
		},
		{
			name:        "EpisodeLast 等于 Episode 时仍按单集写入",
			ev:          FileEvidence{Episode: EpisodeToken{Kind: NumberUnknown}},
			ex:          fileExtras{Episode: 5, EpisodeLast: 5},
			wantEpisode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5, Explicit: true},
		},
		{
			name:          "二者均已知且不同则追加集号冲突，不覆盖正文",
			ev:            FileEvidence{Episode: EpisodeToken{Kind: NumberSingle, First: 8, Last: 8, Explicit: true}, Conflicts: []string{"已有原因"}},
			ex:            fileExtras{Episode: 19},
			wantEpisode:   EpisodeToken{Kind: NumberSingle, First: 8, Last: 8, Explicit: true},
			wantConflicts: []string{"已有原因", "预抽取集号19与正文集号8冲突"},
		},
		{
			name:        "二者均已知且相同则不冲突、不改写",
			ev:          FileEvidence{Episode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5, Explicit: true, Raw: "05"}},
			ex:          fileExtras{Episode: 5},
			wantEpisode: EpisodeToken{Kind: NumberSingle, First: 5, Last: 5, Explicit: true, Raw: "05"},
		},
		{
			name:          "正文为区间且与预抽取单集不同则冲突",
			ev:            FileEvidence{Episode: EpisodeToken{Kind: NumberRange, First: 1, Last: 12, Explicit: true}},
			ex:            fileExtras{Episode: 1},
			wantEpisode:   EpisodeToken{Kind: NumberRange, First: 1, Last: 12, Explicit: true},
			wantConflicts: []string{"预抽取集号1与正文集号1冲突"},
		},
		{
			name:       "季号空缺时写入",
			ev:         FileEvidence{},
			ex:         fileExtras{Season: 4},
			wantSeason: 4,
		},
		{
			name:          "季号已有且不同则追加冲突，不覆盖正文",
			ev:            FileEvidence{Ordinal: Ordinal{Season: 2}, Conflicts: []string{"旧冲突"}},
			ex:            fileExtras{Season: 4},
			wantSeason:    2,
			wantConflicts: []string{"旧冲突", "季号冲突"},
		},
		{
			name:       "季号已有且相同则不冲突",
			ev:         FileEvidence{Ordinal: Ordinal{Season: 4}},
			ex:         fileExtras{Season: 4},
			wantSeason: 4,
		},
		{
			name:    "Abs 写入 AbsEpisode，不进入集号/季号",
			ev:      FileEvidence{},
			ex:      fileExtras{Abs: 85},
			wantAbs: 85,
		},
		{
			name:      "Part 空缺时写入；Final 置真",
			ev:        FileEvidence{},
			ex:        fileExtras{Part: 2, Final: true},
			wantPart:  2,
			wantFinal: true,
		},
		{
			name:      "Part 已有则不覆盖；ex.Final 为假不清除已有 Final",
			ev:        FileEvidence{Ordinal: Ordinal{Part: 1, Final: true}},
			ex:        fileExtras{Part: 3, Final: false},
			wantPart:  1,
			wantFinal: true,
		},
		{
			name: "零值 extras 不改写证据",
			ev: FileEvidence{
				Ordinal:    Ordinal{Season: 1, Part: 2, Final: true},
				Episode:    EpisodeToken{Kind: NumberSingle, First: 7, Last: 7, Explicit: true},
				AbsEpisode: 70,
				Conflicts:  []string{"保留"},
			},
			ex:            fileExtras{},
			wantEpisode:   EpisodeToken{Kind: NumberSingle, First: 7, Last: 7, Explicit: true},
			wantSeason:    1,
			wantAbs:       70,
			wantPart:      2,
			wantFinal:     true,
			wantConflicts: []string{"保留"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := tc.ev
			mergeExtras(&ev, tc.ex)
			if ev.Episode != tc.wantEpisode {
				t.Fatalf("Episode=%+v want %+v", ev.Episode, tc.wantEpisode)
			}
			if ev.Ordinal.Season != tc.wantSeason {
				t.Fatalf("Season=%d want %d", ev.Ordinal.Season, tc.wantSeason)
			}
			if ev.AbsEpisode != tc.wantAbs {
				t.Fatalf("AbsEpisode=%d want %d", ev.AbsEpisode, tc.wantAbs)
			}
			if ev.Ordinal.Part != tc.wantPart {
				t.Fatalf("Part=%d want %d", ev.Ordinal.Part, tc.wantPart)
			}
			if ev.Ordinal.Final != tc.wantFinal {
				t.Fatalf("Final=%v want %v", ev.Ordinal.Final, tc.wantFinal)
			}
			if tc.wantConflicts == nil {
				tc.wantConflicts = []string(nil)
			}
			if !reflect.DeepEqual(ev.Conflicts, tc.wantConflicts) {
				t.Fatalf("Conflicts=%v want %v", ev.Conflicts, tc.wantConflicts)
			}
		})
	}
}

func TestMergeExtrasNilEvidence(t *testing.T) {
	mergeExtras(nil, fileExtras{Episode: 1, Season: 2, Abs: 3, Part: 4, Final: true})
}
