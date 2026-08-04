package database

import (
	"path/filepath"
	"testing"

	"fan-web/models"
)

func TestProgressDefaultUpsertAndUserIsolation(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "progress-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
		}
	})

	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "user-one", "x", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "user-two", "x", 0); err != nil {
		t.Fatal(err)
	}
	userOne, err := GetUserByUsername("user-one")
	if err != nil {
		t.Fatal(err)
	}
	userTwo, err := GetUserByUsername("user-two")
	if err != nil {
		t.Fatal(err)
	}

	anime, err := CreateAnime(&models.Anime{Title: "Progress Anime", EpCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceEpisodes(anime.ID, []models.Episode{
		{EpNumber: 1, FilePath: "episode-01.mp4"},
		{EpNumber: 2, FilePath: "episode-02.mp4"},
	}); err != nil {
		t.Fatal(err)
	}
	episodes, err := ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 {
		t.Fatalf("expected two episodes, got %d", len(episodes))
	}

	initial, err := GetProgress(userOne.ID, episodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Position != 0 || initial.Watched || initial.UserID != userOne.ID || initial.EpisodeID != episodes[0].ID {
		t.Fatalf("unexpected initial progress: %#v", initial)
	}

	if err := UpsertProgress(userOne.ID, episodes[0].ID, 120, false); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(userOne.ID, episodes[0].ID, 300, true); err != nil {
		t.Fatal(err)
	}
	updated, err := GetProgress(userOne.ID, episodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Position != 300 || !updated.Watched {
		t.Fatalf("expected upserted progress, got %#v", updated)
	}

	otherUserProgress, err := GetProgress(userTwo.ID, episodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if otherUserProgress.Position != 0 || otherUserProgress.Watched {
		t.Fatalf("progress leaked between users: %#v", otherUserProgress)
	}

	progressList, err := ListProgressByAnime(userOne.ID, anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(progressList) != 1 || progressList[0].EpisodeID != episodes[0].ID {
		t.Fatalf("unexpected anime progress list: %#v", progressList)
	}
	count, err := CountWatchedByAnime(userOne.ID, anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one watched episode, got %d", count)
	}

	items, total, err := ListAnimes(1, 20, "", userOne.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].WatchedCount != 1 {
		t.Fatalf("unexpected list progress for first user: total=%d items=%#v", total, items)
	}
	otherItems, _, err := ListAnimes(1, 20, "", userTwo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherItems) != 1 || otherItems[0].WatchedCount != 0 {
		t.Fatalf("unexpected list progress for second user: %#v", otherItems)
	}
}
