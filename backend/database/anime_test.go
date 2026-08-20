package database

import (
	"errors"
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

func newBangumiTestDB(t *testing.T) int64 {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "bangumi-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "bg-user", "x", 0); err != nil {
		t.Fatal(err)
	}
	user, err := GetUserByUsername("bg-user")
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func TestUpdateAnimeBangumiUpdatesMetaAndKeepsFilePath(t *testing.T) {
	_ = newBangumiTestDB(t)
	bound, err := CreateAnime(&models.Anime{Title: "One", BangumiID: 100, Cover: "old", FilePath: "dir1"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := CreateAnime(&models.Anime{Title: "Two", FilePath: "dir2", Cover: "keep-me"})
	if err != nil {
		t.Fatal(err)
	}

	err = UpdateAnimeBangumi(target.ID, &models.Anime{
		Title: "TwoNew", TitleCn: "二", BangumiID: 100, Cover: "newcover", Summary: "s", EpCount: 12,
	})
	if !errors.Is(err, ErrBangumiBound) {
		t.Fatalf("撞 UNIQUE 应返回 ErrBangumiBound，got %v", err)
	}
	unchanged, err := GetAnimeByID(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.FilePath != "dir2" || unchanged.BangumiID != 0 || unchanged.Cover != "keep-me" {
		t.Fatalf("撞车后不得改写，got %#v", unchanged)
	}

	if err := UpdateAnimeBangumi(target.ID, &models.Anime{
		Title: "TwoNew", TitleCn: "二", BangumiID: 200, Cover: "newcover", Summary: "s", EpCount: 12,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := GetAnimeByID(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.BangumiID != 200 || updated.Cover != "newcover" || updated.Title != "TwoNew" || updated.TitleCn != "二" || updated.Summary != "s" || updated.EpCount != 12 {
		t.Fatalf("元数据未更新: %#v", updated)
	}
	if updated.FilePath != "dir2" {
		t.Fatalf("file_path 必须不变，got %q", updated.FilePath)
	}
	keeper, err := GetAnimeByID(bound.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keeper.FilePath != "dir1" || keeper.BangumiID != 100 {
		t.Fatalf("已绑定番不应被改动: %#v", keeper)
	}
}

func TestBangumiTokenSaveGetDelete(t *testing.T) {
	userID := newBangumiTestDB(t)
	token, ok, err := GetBangumiToken(userID)
	if err != nil || ok || token != "" {
		t.Fatalf("空令牌期望 ok=false，got token=%q ok=%v err=%v", token, ok, err)
	}
	if err := SaveBangumiToken(userID, "tok-1"); err != nil {
		t.Fatal(err)
	}
	token, ok, err = GetBangumiToken(userID)
	if err != nil || !ok || token != "tok-1" {
		t.Fatalf("期望 tok-1，got token=%q ok=%v err=%v", token, ok, err)
	}
	if err := SaveBangumiToken(userID, "tok-2"); err != nil {
		t.Fatal(err)
	}
	token, ok, err = GetBangumiToken(userID)
	if err != nil || !ok || token != "tok-2" {
		t.Fatalf("覆盖后期望 tok-2，got token=%q ok=%v err=%v", token, ok, err)
	}
	if err := DeleteBangumiToken(userID); err != nil {
		t.Fatal(err)
	}
	token, ok, err = GetBangumiToken(userID)
	if err != nil || ok || token != "" {
		t.Fatalf("删除后期望 ok=false，got token=%q ok=%v err=%v", token, ok, err)
	}
}

func TestEnqueueWatchedForUserDoesNotDuplicate(t *testing.T) {
	userID := newBangumiTestDB(t)
	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "bg-user-2", "x", 0); err != nil {
		t.Fatal(err)
	}
	other, err := GetUserByUsername("bg-user-2")
	if err != nil {
		t.Fatal(err)
	}

	bound, err := CreateAnime(&models.Anime{Title: "Bound", BangumiID: 42, EpCount: 2, FilePath: "bound"})
	if err != nil {
		t.Fatal(err)
	}
	unbound, err := CreateAnime(&models.Anime{Title: "Unbound", EpCount: 1, FilePath: "unbound"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncEpisodes(bound.ID, []models.Episode{
		{EpNumber: 1, FilePath: "b01.mp4"},
		{EpNumber: 2, FilePath: "b02.mp4"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := SyncEpisodes(unbound.ID, []models.Episode{{EpNumber: 1, FilePath: "u01.mp4"}}); err != nil {
		t.Fatal(err)
	}
	boundEps, err := ListEpisodesByAnimeID(bound.ID)
	if err != nil {
		t.Fatal(err)
	}
	unboundEps, err := ListEpisodesByAnimeID(unbound.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(userID, boundEps[0].ID, 100, true); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(userID, boundEps[1].ID, 10, false); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(userID, unboundEps[0].ID, 200, true); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(other.ID, boundEps[0].ID, 50, true); err != nil {
		t.Fatal(err)
	}

	if err := EnqueueWatchedForUser(userID); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueWatchedForUser(userID); err != nil {
		t.Fatal(err)
	}
	items, err := ListBangumiOutbox(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].UserID != userID || items[0].EpisodeID != boundEps[0].ID {
		t.Fatalf("重复入队不得加倍，期望仅 1 条已看且 bangumi_id>0，got %#v", items)
	}

	if err := EnqueueBangumiOutbox(other.ID, boundEps[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBangumiOutboxByUser(userID); err != nil {
		t.Fatal(err)
	}
	items, err = ListBangumiOutbox(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].UserID != other.ID {
		t.Fatalf("DeleteBangumiOutboxByUser 只清该用户，got %#v", items)
	}
}
