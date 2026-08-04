package database

import (
	"path/filepath"
	"testing"

	"fan-web/models"
)

func TestListAnimesReturnsTotal(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "anime-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
		}
	})

	if _, err := CreateAnime(&models.Anime{Title: "Test Anime", EpCount: 1}); err != nil {
		t.Fatal(err)
	}
	animes, total, err := ListAnimes(1, 20, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(animes) != 1 || total != 1 {
		t.Fatalf("expected one anime and total 1, got len=%d total=%d", len(animes), total)
	}
}
