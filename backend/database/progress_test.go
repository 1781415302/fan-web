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

func TestWatchedIsIrreversible(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "progress-irreversible-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
		}
	})

	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "irr-user", "x", 0); err != nil {
		t.Fatal(err)
	}
	user, err := GetUserByUsername("irr-user")
	if err != nil {
		t.Fatal(err)
	}
	anime, err := CreateAnime(&models.Anime{Title: "Irreversible Anime", EpCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "e01.mp4"}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	epID := episodes[0].ID

	// 无记录 -> 未看
	p, err := GetProgress(user.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Watched {
		t.Fatalf("expected unwatched for no record, got watched")
	}

	// 未看 -> 观看中
	if err := UpsertProgress(user.ID, epID, 60, false); err != nil {
		t.Fatal(err)
	}
	p, err = GetProgress(user.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Watched || p.Position != 60 {
		t.Fatalf("expected in-progress (pos=60, watched=false), got pos=%d watched=%v", p.Position, p.Watched)
	}

	// 观看中 -> 看完
	if err := UpsertProgress(user.ID, epID, 590, true); err != nil {
		t.Fatal(err)
	}
	p, err = GetProgress(user.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Watched {
		t.Fatalf("expected watched after reporting true, got unwatched")
	}

	// 看完 -> position=0, watched=false 仍为看完
	if err := UpsertProgress(user.ID, epID, 0, false); err != nil {
		t.Fatal(err)
	}
	p, err = GetProgress(user.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Watched {
		t.Fatalf("watched should be irreversible after position=0 watched=false, got unwatched")
	}
	if p.Position != 0 {
		t.Fatalf("position should update to 0, got %d", p.Position)
	}

	// 看完 -> position>0, watched=false 仍为看完
	if err := UpsertProgress(user.ID, epID, 100, false); err != nil {
		t.Fatal(err)
	}
	p, err = GetProgress(user.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Watched {
		t.Fatalf("watched should be irreversible after position=100 watched=false, got unwatched")
	}
	if p.Position != 100 {
		t.Fatalf("position should update to 100, got %d", p.Position)
	}

	// 看完 -> watched=true 仍为看完
	if err := UpsertProgress(user.ID, epID, 200, true); err != nil {
		t.Fatal(err)
	}
	p, err = GetProgress(user.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Watched || p.Position != 200 {
		t.Fatalf("expected watched=true pos=200, got watched=%v pos=%d", p.Watched, p.Position)
	}
}
