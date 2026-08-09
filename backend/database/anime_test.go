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

func newSyncTestDB(t *testing.T) (int64, int64, []models.Episode) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "sync-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
		}
	})
	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "sync-user", "x", 0); err != nil {
		t.Fatal(err)
	}
	user, err := GetUserByUsername("sync-user")
	if err != nil {
		t.Fatal(err)
	}
	anime, err := CreateAnime(&models.Anime{Title: "Sync Anime", EpCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncEpisodes(anime.ID, []models.Episode{
		{EpNumber: 1, FilePath: "ep01.mp4"},
		{EpNumber: 2, FilePath: "ep02.mp4"},
	}); err != nil {
		t.Fatal(err)
	}
	initial, err := ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	return user.ID, anime.ID, initial
}

func TestSyncEpisodesPreservesIDsAndProgress(t *testing.T) {
	userID, animeID, initial := newSyncTestDB(t)
	if len(initial) != 2 {
		t.Fatalf("expected two episodes, got %d", len(initial))
	}
	ep1ID, ep2ID := initial[0].ID, initial[1].ID

	if err := UpsertProgress(userID, ep1ID, 123, false); err != nil {
		t.Fatal(err)
	}

	// 同步相同集数，但修改第一集文件名和标题。
	updated := []models.Episode{
		{EpNumber: 1, Title: "第一话改名", FilePath: "renamed-ep01.mp4", Duration: 987},
		{EpNumber: 2, FilePath: "ep02.mp4"},
	}
	if err := SyncEpisodes(animeID, updated); err != nil {
		t.Fatal(err)
	}

	after, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if after[0].ID != ep1ID || after[1].ID != ep2ID {
		t.Fatalf("episode IDs must be preserved, got %d/%d want %d/%d", after[0].ID, after[1].ID, ep1ID, ep2ID)
	}
	if after[0].FilePath != "renamed-ep01.mp4" || after[0].Title != "第一话改名" || after[0].Duration != 987 {
		t.Fatalf("episode fields not updated: %#v", after[0])
	}

	progress, err := GetProgress(userID, ep1ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 123 || progress.Watched {
		t.Fatalf("progress must be preserved, got %#v", progress)
	}
}

func TestSyncEpisodesAddsAndRemoves(t *testing.T) {
	userID, animeID, initial := newSyncTestDB(t)
	if len(initial) != 2 {
		t.Fatalf("expected two episodes, got %d", len(initial))
	}
	ep1ID, ep2ID := initial[0].ID, initial[1].ID

	if err := UpsertProgress(userID, ep1ID, 50, false); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(userID, ep2ID, 60, false); err != nil {
		t.Fatal(err)
	}

	// 第 1 集移除，新增第 3 集。
	if err := SyncEpisodes(animeID, []models.Episode{
		{EpNumber: 2, FilePath: "ep02.mp4"},
		{EpNumber: 3, FilePath: "ep03.mp4"},
	}); err != nil {
		t.Fatal(err)
	}

	after, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].EpNumber != 2 || after[1].EpNumber != 3 {
		t.Fatalf("unexpected episodes after sync: %#v", after)
	}
	if after[0].ID != ep2ID {
		t.Fatalf("kept episode ID must be stable, got %d want %d", after[0].ID, ep2ID)
	}

	// 第 2 集进度保留。
	kept, err := GetProgress(userID, ep2ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Position != 60 {
		t.Fatalf("kept episode progress lost: %#v", kept)
	}
	// 第 1 集已删除，进度级联消失（GET 返回空默认记录）。
	deleted, err := GetProgress(userID, ep1ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Position != 0 || deleted.Watched {
		t.Fatalf("deleted episode progress should be gone, got %#v", deleted)
	}
}

func TestSyncEpisodesIsIdempotent(t *testing.T) {
	_, animeID, initial := newSyncTestDB(t)

	input := []models.Episode{
		{EpNumber: 1, FilePath: "ep01.mp4", Duration: 300},
		{EpNumber: 2, FilePath: "ep02.mp4", Duration: 320},
	}
	for pass := 0; pass < 2; pass++ {
		if err := SyncEpisodes(animeID, input); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		after, err := ListEpisodesByAnimeID(animeID)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != 2 {
			t.Fatalf("pass %d: expected 2 episodes, got %d", pass, len(after))
		}
		if after[0].ID != initial[0].ID || after[1].ID != initial[1].ID {
			t.Fatalf("pass %d: IDs must be stable across runs, got %d/%d", pass, after[0].ID, after[1].ID)
		}
	}
}

func TestSyncEpisodesRejectsDuplicateInput(t *testing.T) {
	_, animeID, initial := newSyncTestDB(t)

	err := SyncEpisodes(animeID, []models.Episode{
		{EpNumber: 1, FilePath: "a.mp4"},
		{EpNumber: 1, FilePath: "b.mp4"},
	})
	if err == nil {
		t.Fatal("expected duplicate input to fail")
	}

	after, listErr := ListEpisodesByAnimeID(animeID)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(after) != len(initial) {
		t.Fatalf("database must be unchanged after failed sync, got %d episodes", len(after))
	}
	noProgress, err := GetProgress(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = noProgress
}

func TestSyncEpisodesRollsBackOnFailure(t *testing.T) {
	_, animeID, initial := newSyncTestDB(t)
	ep1ID := initial[0].ID

	// 创建触发器：仅当插入 ep_number=3 时失败，使同步在完成前序 UPDATE 后、插入新集时抛错。
	if _, err := DB.Exec(`
		CREATE TRIGGER fail_ep3 BEFORE INSERT ON episodes
		WHEN NEW.ep_number = 3
		BEGIN
			SELECT RAISE(ABORT, 'ep3 insert forbidden');
		END;`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_, _ = DB.Exec("DROP TRIGGER IF EXISTS fail_ep3")
		}
	})

	input := []models.Episode{
		{EpNumber: 1, FilePath: "changed-ep01.mp4"},
		{EpNumber: 3, FilePath: "ep03.mp4"},
	}
	if err := SyncEpisodes(animeID, input); err == nil {
		t.Fatal("expected sync to fail when inserting ep3")
	}

	after, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(initial) {
		t.Fatalf("rollback must keep original rows, got %d", len(after))
	}
	if after[0].ID != ep1ID || after[0].FilePath != "ep01.mp4" {
		t.Fatalf("pre-failure UPDATE must be rolled back, got %#v", after[0])
	}
}
