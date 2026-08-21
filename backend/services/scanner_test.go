package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerRecognizesSupportedEpisodeNames(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"[Fansub] Title [67].mkv",
		"Title - 02.mp4",
		"Title EP03.avi",
		"Title 第4集.webm",
		"Title S01E05.mov",
		"06.m4v",
		"Title-08v2.mkv",
		"ignored.txt",
		"Title [67].mkv:Zone.Identifier",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 7 {
		t.Fatalf("expected 7 episodes, got %d: %#v", len(episodes), episodes)
	}
	want := []int{2, 3, 4, 5, 6, 8, 67}
	for i, episode := range episodes {
		if episode.EpNumber != want[i] {
			t.Fatalf("episode %d: expected number %d, got %d", i, want[i], episode.EpNumber)
		}
	}
}

func TestScannerRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := NewScannerService(root).Scan("../outside")
	if !errors.Is(err, ErrInvalidVideoPath) {
		t.Fatalf("expected ErrInvalidVideoPath, got %v", err)
	}
}

func TestScannerMovieBecomesEpisodeOne(t *testing.T) {
	root := t.TempDir()
	name := "[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4"
	if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 {
		t.Fatalf("expected EpNumber 1, got %d", episodes[0].EpNumber)
	}
	if episodes[0].FilePath != name {
		t.Fatalf("expected FilePath %q, got %q", name, episodes[0].FilePath)
	}
}

func TestScannerSkipsVersionOnlyFilename(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "v2.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 0 {
		t.Fatalf("expected 0 episodes for v2.mkv, got %d: %#v", len(episodes), episodes)
	}
}

func TestScannerRealEpisodeOneBeatsMovie(t *testing.T) {
	root := t.TempDir()
	real := "[TSDM][Cosmic Princess Kaguya][01][1080p].mkv"
	movie := "[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4"
	for _, name := range []string{movie, real} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected only real ep1, got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 {
		t.Fatalf("expected EpNumber 1, got %d", episodes[0].EpNumber)
	}
	if episodes[0].FilePath != real {
		t.Fatalf("expected real ep file %q, got %q", real, episodes[0].FilePath)
	}
}

func TestScannerTwoMoviesKeepsFirstByFilename(t *testing.T) {
	root := t.TempDir()
	first := "[Fansub][Alpha Title][2026][1080p].mkv"
	later := "[Fansub][Zeta Title][2026][1080p].mkv"
	for _, name := range []string{later, first} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode (second movie silent drop), got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 || episodes[0].FilePath != first {
		t.Fatalf("expected first-by-filename movie as ep1, got %#v", episodes[0])
	}
}
