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
	if err := SyncEpisodes(anime.ID, []models.Episode{
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
	if err := SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "e01.mp4"}}); err != nil {
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

	// 看完 -> position=0, watched=false 仍为看完，且不得用 0 覆盖已有正进度
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
	if p.Position != 590 {
		t.Fatalf("position=0 must not overwrite existing >0, got %d", p.Position)
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

func TestUpsertProgressFirstInsertZero(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "progress-insert-zero.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
		}
	})
	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "zero-user", "x", 0); err != nil {
		t.Fatal(err)
	}
	user, err := GetUserByUsername("zero-user")
	if err != nil {
		t.Fatal(err)
	}
	anime, err := CreateAnime(&models.Anime{Title: "Zero Insert", EpCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "e01.mp4"}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(user.ID, episodes[0].ID, 0, false); err != nil {
		t.Fatal(err)
	}
	p, err := GetProgress(user.ID, episodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Position != 0 || p.Watched {
		t.Fatalf("first INSERT of 0 must persist, got %#v", p)
	}
}

func TestPickContinueEpisodeInProgressBeforeUnwatched(t *testing.T) {
	episodes := []models.Episode{
		{ID: 1, EpNumber: 1},
		{ID: 2, EpNumber: 2},
		{ID: 3, EpNumber: 3},
	}
	progress := []models.WatchProgress{
		{EpisodeID: 1, Watched: false, Position: 0},
		{EpisodeID: 2, Watched: false, Position: 50},
	}
	got := PickContinueEpisode(episodes, progress)
	if got == nil || got.ID != 2 {
		t.Fatalf("进行中应优先于未看，got %#v", got)
	}
}

func TestPickContinueEpisodeAllWatchedReturnsNil(t *testing.T) {
	episodes := []models.Episode{{ID: 1, EpNumber: 1}, {ID: 2, EpNumber: 2}}
	progress := []models.WatchProgress{
		{EpisodeID: 1, Watched: true, Position: 100},
		{EpisodeID: 2, Watched: true, Position: 200},
	}
	if got := PickContinueEpisode(episodes, progress); got != nil {
		t.Fatalf("全 watched 应返回 nil，got %#v", got)
	}
}

func TestListContinueWatchingOrdersByRecentActivityAndHonorsLimit(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "continue-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "cw-user", "x", 0); err != nil {
		t.Fatal(err)
	}
	user, err := GetUserByUsername("cw-user")
	if err != nil {
		t.Fatal(err)
	}

	mustAnime := func(title, dir string) (int64, []models.Episode) {
		t.Helper()
		anime, err := CreateAnime(&models.Anime{Title: title, EpCount: 2, FilePath: dir})
		if err != nil {
			t.Fatal(err)
		}
		if err := SyncEpisodes(anime.ID, []models.Episode{
			{EpNumber: 1, FilePath: dir + "/01.mp4"},
			{EpNumber: 2, FilePath: dir + "/02.mp4"},
		}); err != nil {
			t.Fatal(err)
		}
		eps, err := ListEpisodesByAnimeID(anime.ID)
		if err != nil {
			t.Fatal(err)
		}
		return anime.ID, eps
	}
	oldID, oldEps := mustAnime("Old", "old")
	newID, newEps := mustAnime("New", "new")
	doneID, doneEps := mustAnime("Done", "done")
	_, extraEps := mustAnime("Extra", "extra")

	if err := UpsertProgress(user.ID, oldEps[1].ID, 30, false); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(user.ID, newEps[0].ID, 10, false); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(user.ID, doneEps[0].ID, 100, true); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(user.ID, doneEps[1].ID, 100, true); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(user.ID, extraEps[0].ID, 5, false); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`UPDATE watch_progress SET updated_at = ? WHERE episode_id = ?`, "2026-01-01 00:00:00", oldEps[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`UPDATE watch_progress SET updated_at = ? WHERE episode_id = ?`, "2026-03-01 00:00:00", newEps[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`UPDATE watch_progress SET updated_at = ? WHERE episode_id IN (?, ?)`, "2026-02-01 00:00:00", doneEps[0].ID, doneEps[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`UPDATE watch_progress SET updated_at = ? WHERE episode_id = ?`, "2026-04-01 00:00:00", extraEps[0].ID); err != nil {
		t.Fatal(err)
	}

	items, err := ListContinueWatching(user.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("limit=2 应截断为 2，got %d items=%#v", len(items), items)
	}
	if items[0].Anime.ID != extraEps[0].AnimeID || items[1].Anime.ID != newID {
		t.Fatalf("应按最近活动排序且跳过全看完，got anime IDs %d,%d want extra=%d new=%d (old=%d done=%d)",
			items[0].Anime.ID, items[1].Anime.ID, extraEps[0].AnimeID, newID, oldID, doneID)
	}
	if items[1].Episode.ID != newEps[0].ID || items[1].Position != 10 || items[1].Watched {
		t.Fatalf("New 番应续看 ep1 pos=10: %#v", items[1])
	}

	all, err := ListContinueWatching(user.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("全看完应跳过，期望 3 条（extra/new/old），got %d", len(all))
	}
	if all[2].Anime.ID != oldID || all[2].Episode.ID != oldEps[1].ID {
		t.Fatalf("最旧活动应为 Old 的进行中集，got %#v", all[2])
	}
}
