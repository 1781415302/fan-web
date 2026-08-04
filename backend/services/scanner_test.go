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
	if len(episodes) != 6 {
		t.Fatalf("expected 6 episodes, got %d: %#v", len(episodes), episodes)
	}
	want := []int{2, 3, 4, 5, 6, 67}
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
