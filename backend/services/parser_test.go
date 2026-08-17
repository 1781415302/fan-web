package services

import "testing"

func TestParseFilename(t *testing.T) {
	tests := []struct {
		name       string
		wantTitle  string
		wantNumber int
		wantSeason int
	}{
		{
			name:       "[CheeseAni] Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen [67][CR-WebRip HEVC AAC SRT][简繁内封].mkv",
			wantTitle:  "Re：Zero kara Hajimeru Isekai Seikatsu Soshitsu-Hen 第4季",
			wantNumber: 67,
			wantSeason: 4,
		},
		{
			name:       "[YunFog][Re Zero kara Hajimeru Isekai Seikatsu S3][01][HEVC][x265 10bit][1080p][JPSC].mp4",
			wantTitle:  "Re Zero kara Hajimeru Isekai Seikatsu 第3季",
			wantNumber: 1,
			wantSeason: 3,
		},
		{name: "[ANi] Bocchi the Rock! - 01 [1080p].mkv", wantTitle: "Bocchi the Rock!", wantNumber: 1},
		{name: "Frieren - S01E05 [1080p].mkv", wantTitle: "Frieren", wantNumber: 5},
		{name: "[SubGroup] 葬送的芙莉莲 第3集 [1080p].mkv", wantTitle: "葬送的芙莉莲", wantNumber: 3},
		{name: "[Sub] Title [01v2][1080p].mkv", wantTitle: "Title", wantNumber: 1},
		{name: "Title-01v2.mkv", wantTitle: "Title", wantNumber: 1},
		{name: "[Fansub][v2].mkv", wantTitle: "v2", wantNumber: 0},
		{name: "v2.mkv", wantTitle: "v2", wantNumber: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := ParseFilename(test.name)
			if parsed.Title != test.wantTitle || parsed.EpisodeNum != test.wantNumber || parsed.Season != test.wantSeason || parsed.FileName != test.name {
				t.Fatalf("ParseFilename(%q) = {Title: %q, EpisodeNum: %d, Season: %d}, want {Title: %q, EpisodeNum: %d, Season: %d}",
					test.name, parsed.Title, parsed.EpisodeNum, parsed.Season, test.wantTitle, test.wantNumber, test.wantSeason)
			}
		})
	}
}
