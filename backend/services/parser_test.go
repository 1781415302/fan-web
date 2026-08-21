package services

import (
	"encoding/json"
	"testing"
)

func TestParseFilename(t *testing.T) {
	tests := []struct {
		name       string
		wantTitle  string
		wantNumber int
		wantSeason int
		wantKind   string
	}{
		{
			name:       "[CheeseAni] Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen [67][CR-WebRip HEVC AAC SRT][简繁内封].mkv",
			wantTitle:  "Re：Zero kara Hajimeru Isekai Seikatsu Soshitsu-Hen 第4季",
			wantNumber: 67,
			wantSeason: 4,
			wantKind:   "episode",
		},
		{
			name:       "[YunFog][Re Zero kara Hajimeru Isekai Seikatsu S3][01][HEVC][x265 10bit][1080p][JPSC].mp4",
			wantTitle:  "Re Zero kara Hajimeru Isekai Seikatsu 第3季",
			wantNumber: 1,
			wantSeason: 3,
			wantKind:   "episode",
		},
		{name: "[ANi] Bocchi the Rock! - 01 [1080p].mkv", wantTitle: "Bocchi the Rock!", wantNumber: 1, wantKind: "episode"},
		{name: "Frieren - S01E05 [1080p].mkv", wantTitle: "Frieren 第1季", wantNumber: 5, wantSeason: 1, wantKind: "episode"},
		{name: "[SubGroup] 葬送的芙莉莲 第3集 [1080p].mkv", wantTitle: "葬送的芙莉莲", wantNumber: 3, wantKind: "episode"},
		{name: "[Sub] Title [01v2][1080p].mkv", wantTitle: "Title", wantNumber: 1, wantKind: "episode"},
		{name: "Title-01v2.mkv", wantTitle: "Title", wantNumber: 1, wantKind: "episode"},
		{name: "[Fansub][v2].mkv", wantTitle: "v2", wantNumber: 0, wantKind: "episode"},
		{name: "v2.mkv", wantTitle: "v2", wantNumber: 0, wantKind: "episode"},
		{name: "[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4", wantTitle: "Cosmic Princess Kaguya", wantNumber: 0, wantKind: "episode"},
		{name: "[Subs][宇宙公主辉夜][2026][1080p].mp4", wantTitle: "宇宙公主辉夜", wantNumber: 0, wantKind: "episode"},
		{name: "[Subs]某作品 剧场版 [1080p].mkv", wantTitle: "某作品 剧场版", wantNumber: 0, wantKind: "movie"},
		{name: "[Subs]某作品 劇場版 [1080p].mkv", wantTitle: "某作品 劇場版", wantNumber: 0, wantKind: "movie"},
		{name: "Some Title the Movie [1080p].mkv", wantTitle: "Some Title the Movie", wantNumber: 0, wantKind: "movie"},
		{name: "[Subs]某作品 剧场版 [12][1080p].mkv", wantTitle: "某作品 剧场版", wantNumber: 12, wantKind: "episode"},
		{name: "[Fansub][Bocchi the Rock!][1080p].mkv", wantTitle: "Bocchi the Rock!", wantNumber: 0, wantKind: "episode"},
		{name: "[Fansub][Bocchi the Rock!][2022][1080p].mkv", wantTitle: "Bocchi the Rock!", wantNumber: 0, wantKind: "episode"},
		{name: "Arrival[2026].mkv", wantTitle: "Arrival", wantNumber: 0, wantKind: "episode"},
		{name: "unknown.mkv", wantTitle: "unknown", wantNumber: 0, wantKind: "episode"},
		{name: "[Fansub][Show Name][NCOP][1080p].mkv", wantTitle: "Show Name NCOP", wantNumber: 0, wantKind: "episode"},
		{name: "[Subs][Show Name][1080][CHS].mkv", wantTitle: "Show Name", wantNumber: 0, wantKind: "episode"},
		{name: "[TSDM][Show Name][01][2026][NF_web-DL][1080p].mkv", wantTitle: "Show Name", wantNumber: 1, wantKind: "episode"},
		{name: "[TSDM][Show Name][S01][2026][NF_web-DL].mkv", wantTitle: "Show Name 第1季", wantNumber: 0, wantSeason: 1, wantKind: "episode"},
		{name: "[Fansub][Show Name][NCOP][2026][1080p].mkv", wantTitle: "Show Name NCOP", wantNumber: 0, wantKind: "episode"},
		{name: "[Fansub][Show Name][SPECIAL][2026].mkv", wantTitle: "Show Name SPECIAL", wantNumber: 0, wantKind: "episode"},
		{name: "[Fansub][Show Name][Trailer][2026].mkv", wantTitle: "Show Name Trailer", wantNumber: 0, wantKind: "episode"},
		{name: "Filmography of Someone [1080p].mkv", wantTitle: "Filmography of Someone", wantNumber: 0, wantKind: "episode"},
		{name: "Movie.mkv", wantTitle: "Movie", wantNumber: 0, wantKind: "episode"},
		{name: "Film.mkv", wantTitle: "Film", wantNumber: 0, wantKind: "episode"},
		{name: "[TSDM][Show Name][E01][2026][NF_web-DL].mkv", wantTitle: "Show Name", wantNumber: 1, wantKind: "episode"},
		{name: "[TSDM][Show Name][00][2026][1080p].mkv", wantTitle: "Show Name", wantNumber: 0, wantKind: "episode"},
		{name: "[TSDM][Show Name][000][2026][1080p].mkv", wantTitle: "Show Name", wantNumber: 0, wantKind: "episode"},
		{name: "[Fansub] Show Name_01_[1080p].mkv", wantTitle: "Show Name", wantNumber: 1, wantKind: "episode"},
		{name: "[Subs]某作品 第０１集 [1080p].mkv", wantTitle: "某作品", wantNumber: 1, wantKind: "episode"},
		{name: "[Subs][某作品][０１][1080p].mkv", wantTitle: "某作品", wantNumber: 1, wantKind: "episode"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := ParseFilename(test.name)
			if parsed.Title != test.wantTitle || parsed.EpisodeNum != test.wantNumber || parsed.Season != test.wantSeason || parsed.FileName != test.name || parsed.Kind != test.wantKind {
				t.Fatalf("ParseFilename(%q) = {Title: %q, EpisodeNum: %d, Season: %d, FileName: %q, Kind: %q}, want {Title: %q, EpisodeNum: %d, Season: %d, FileName: %q, Kind: %q}",
					test.name, parsed.Title, parsed.EpisodeNum, parsed.Season, parsed.FileName, parsed.Kind, test.wantTitle, test.wantNumber, test.wantSeason, test.name, test.wantKind)
			}
		})
	}

	t.Run("json omits kind", func(t *testing.T) {
		parsed := ParseFilename("[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4")
		if parsed.Kind != "episode" || parsed.EpisodeNum != 0 {
			t.Fatalf("Kaguya Kind=%q EpisodeNum=%d, want episode/0", parsed.Kind, parsed.EpisodeNum)
		}
		raw, err := json.Marshal(parsed)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if _, ok := obj["kind"]; ok {
			t.Fatalf("json has kind: %s", raw)
		}
		if _, ok := obj["Kind"]; ok {
			t.Fatalf("json has Kind: %s", raw)
		}
	})
}
