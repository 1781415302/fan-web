package services

import (
	"path/filepath"
	"testing"
)

func TestDeriveDirTitle(t *testing.T) {
	tests := []struct {
		name       string
		relDir     string
		wantTitle  string
		wantSeason int
	}{
		{name: "empty", relDir: "", wantTitle: "", wantSeason: 0},
		{name: "title only", relDir: "葬送的芙莉莲", wantTitle: "葬送的芙莉莲"},
		{
			name:       "title then season",
			relDir:     filepath.Join("葬送的芙莉莲", "Season 2"),
			wantTitle:  "葬送的芙莉莲",
			wantSeason: 2,
		},
		{
			name:       "season then title",
			relDir:     filepath.Join("Season 2", "葬送的芙莉莲"),
			wantTitle:  "葬送的芙莉莲",
			wantSeason: 2,
		},
		{
			name:      "title then quality dir",
			relDir:    filepath.Join("葬送的芙莉莲", "BD 1080p"),
			wantTitle: "葬送的芙莉莲",
		},
		{name: "generic downloads", relDir: "downloads"},
		{name: "generic 新番", relDir: "新番"},
		{
			name:      "metadata group then title",
			relDir:    filepath.Join("[VCB-Studio]", "Frieren"),
			wantTitle: "Frieren",
		},
		{
			name:      "year then title",
			relDir:    filepath.Join("2024", "芙莉莲"),
			wantTitle: "芙莉莲",
		},

		{
			name:      "title then remux",
			relDir:    filepath.Join("芙莉莲", "Remux"),
			wantTitle: "芙莉莲",
		},
		{
			name:      "title then 高清",
			relDir:    filepath.Join("芙莉莲", "高清"),
			wantTitle: "芙莉莲",
		},
		{
			name:      "title then SP",
			relDir:    filepath.Join("芙莉莲", "SP"),
			wantTitle: "芙莉莲",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DeriveDirTitle(test.relDir)
			if got.Title != test.wantTitle || got.Season != test.wantSeason {
				t.Fatalf("DeriveDirTitle(%q) = {Title: %q, Season: %d}, want {Title: %q, Season: %d}",
					test.relDir, got.Title, got.Season, test.wantTitle, test.wantSeason)
			}
		})
	}
}
