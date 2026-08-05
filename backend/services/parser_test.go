package services

import "testing"

func TestParseFilename(t *testing.T) {
	tests := []struct {
		name       string
		wantTitle  string
		wantNumber int
	}{
		{
			name:       "[CheeseAni] Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen [67][CR-WebRip HEVC AAC SRT][简繁内封].mkv",
			wantTitle:  "Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen",
			wantNumber: 67,
		},
		{
			name:       "[YunFog][Re Zero kara Hajimeru Isekai Seikatsu S3][01][HEVC][x265 10bit][1080p][JPSC].mp4",
			wantTitle:  "Re Zero kara Hajimeru Isekai Seikatsu S3",
			wantNumber: 1,
		},
		{name: "[ANi] Bocchi the Rock! - 01 [1080p].mkv", wantTitle: "Bocchi the Rock!", wantNumber: 1},
		{name: "Frieren - S01E05 [1080p].mkv", wantTitle: "Frieren", wantNumber: 5},
		{name: "[SubGroup] 葬送的芙莉莲 第3集 [1080p].mkv", wantTitle: "葬送的芙莉莲", wantNumber: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := ParseFilename(test.name)
			if parsed.Title != test.wantTitle || parsed.EpisodeNum != test.wantNumber || parsed.FileName != test.name {
				t.Fatalf("ParseFilename(%q) = {Title: %q, EpisodeNum: %d}, want {Title: %q, EpisodeNum: %d}",
					test.name, parsed.Title, parsed.EpisodeNum, test.wantTitle, test.wantNumber)
			}
		})
	}
}
