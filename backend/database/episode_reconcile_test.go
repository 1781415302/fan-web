package database

import (
	"errors"
	"path/filepath"
	"testing"

	"fan-web/models"
)

func newReconcileTestDB(t *testing.T) (userID, animeID int64, anime models.Anime, eps []models.Episode) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "reconcile-test.db")
	if err := Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reconcileCommitHook = nil
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
	if _, err := DB.Exec(`INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)`, "rec-user", "x", 0); err != nil {
		t.Fatal(err)
	}
	user, err := GetUserByUsername("rec-user")
	if err != nil {
		t.Fatal(err)
	}
	created, err := CreateAnime(&models.Anime{
		Title: "Ranma", TitleCn: "乱马", BangumiID: 1001, EpCount: 2, FilePath: "Ranma",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateEpisode(&models.Episode{AnimeID: created.ID, EpNumber: 2, Title: "旧编号2", FilePath: "01.mkv", Duration: 1400}); err != nil {
		t.Fatal(err)
	}
	if err := CreateEpisode(&models.Episode{AnimeID: created.ID, EpNumber: 1, Title: "旧编号1", FilePath: "02.mkv", Duration: 1400}); err != nil {
		t.Fatal(err)
	}
	eps, err = ListEpisodesByAnimeID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	// ListEpisodes 按 ep_number 排序：ep1=02.mkv, ep2=01.mkv
	return user.ID, created.ID, *created, eps
}

func findEpByPath(eps []models.Episode, path string) models.Episode {
	for _, ep := range eps {
		if ep.FilePath == path {
			return ep
		}
	}
	return models.Episode{}
}

func TestReconcileEpisodesCorrectsNumberKeepsProgressOutbox(t *testing.T) {
	userID, animeID, anime, eps := newReconcileTestDB(t)
	ep01 := findEpByPath(eps, "01.mkv") // 旧 ep_number=2
	ep02 := findEpByPath(eps, "02.mkv") // 旧 ep_number=1
	if ep01.ID == 0 || ep02.ID == 0 {
		t.Fatalf("seed paths missing: %#v", eps)
	}
	if err := UpsertProgress(userID, ep01.ID, 73, false); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueBangumiOutbox(userID, ep01.ID); err != nil {
		t.Fatal(err)
	}

	desired := []models.Episode{
		{FilePath: "01.mkv", EpNumber: 1},
		{FilePath: "02.mkv", EpNumber: 2},
	}
	err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      eps,
		Desired:       desired,
		PresentFiles:  []string{"01.mkv", "02.mkv"},
		RemoveMissing: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	after, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	a01 := findEpByPath(after, "01.mkv")
	a02 := findEpByPath(after, "02.mkv")
	if a01.ID != ep01.ID || a02.ID != ep02.ID {
		t.Fatalf("IDs must be preserved: 01 %d→%d, 02 %d→%d", ep01.ID, a01.ID, ep02.ID, a02.ID)
	}
	if a01.EpNumber != 1 || a02.EpNumber != 2 {
		t.Fatalf("numbers not corrected: %#v %#v", a01, a02)
	}
	if a01.Title != "旧编号2" {
		t.Fatalf("same-file match must keep title, got %q", a01.Title)
	}
	progress, err := GetProgress(userID, ep01.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 73 {
		t.Fatalf("progress must stay on ID, got %#v", progress)
	}
	outbox, err := ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox) != 1 || outbox[0].EpisodeID != ep01.ID {
		t.Fatalf("outbox must still reference episode ID, got %#v", outbox)
	}

	// 幂等再扫
	afterSnap := after
	anime2, err := GetAnimeByID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: *anime2,
		Expected:      afterSnap,
		Desired:       desired,
		PresentFiles:  []string{"01.mkv", "02.mkv"},
		RemoveMissing: true,
	}); err != nil {
		t.Fatal(err)
	}
	again, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if findEpByPath(again, "01.mkv").ID != ep01.ID {
		t.Fatal("idempotent rescan changed ID")
	}
}

func TestReconcileEpisodesAnimeAndEpisodeCAS(t *testing.T) {
	_, animeID, anime, eps := newReconcileTestDB(t)
	ep01 := findEpByPath(eps, "01.mkv")

	staleAnime := anime
	staleAnime.BangumiID = 9999
	err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: staleAnime,
		Expected:      eps,
		Desired:       []models.Episode{{FilePath: "01.mkv", EpNumber: 1}, {FilePath: "02.mkv", EpNumber: 2}},
		PresentFiles:  []string{"01.mkv", "02.mkv"},
		RemoveMissing: true,
	})
	if !errors.Is(err, ErrEpisodeSnapshotChanged) {
		t.Fatalf("want ErrEpisodeSnapshotChanged, got %v", err)
	}

	staleEps := append([]models.Episode{}, eps...)
	staleEps[0].FilePath = "changed.mkv"
	err = ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      staleEps,
		Desired:       []models.Episode{{FilePath: "01.mkv", EpNumber: 1}, {FilePath: "02.mkv", EpNumber: 2}},
		PresentFiles:  []string{"01.mkv", "02.mkv"},
		RemoveMissing: true,
	})
	if !errors.Is(err, ErrEpisodeSnapshotChanged) {
		t.Fatalf("want episode CAS ErrEpisodeSnapshotChanged, got %v", err)
	}

	// 零写入：编号未变
	cur, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if findEpByPath(cur, "01.mkv").EpNumber != ep01.EpNumber {
		t.Fatalf("CAS failure must not write, got %#v", cur)
	}
}

func TestReconcileEpisodesPresentFileNotDeleted(t *testing.T) {
	_, animeID, anime, eps := newReconcileTestDB(t)
	err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      eps,
		Desired:       []models.Episode{{FilePath: "01.mkv", EpNumber: 1}}, // 02.mkv 仍在磁盘却未进入 Desired
		PresentFiles:  []string{"01.mkv", "02.mkv"},
		RemoveMissing: true,
	})
	if !errors.Is(err, ErrEpisodeIdentityConflict) {
		t.Fatalf("want IdentityConflict, got %v", err)
	}
	cur, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cur) != 2 {
		t.Fatalf("must not delete present files, got %d", len(cur))
	}
}

func TestReconcileEpisodesRemoveMissingOnlyWhenAbsent(t *testing.T) {
	_, animeID, anime, eps := newReconcileTestDB(t)
	// 02.mkv 已不在 PresentFiles；01 保留并纠正编号
	err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      eps,
		Desired:       []models.Episode{{FilePath: "01.mkv", EpNumber: 1}},
		PresentFiles:  []string{"01.mkv"},
		RemoveMissing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cur, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cur) != 1 || cur[0].FilePath != "01.mkv" || cur[0].EpNumber != 1 {
		t.Fatalf("expected only corrected 01.mkv, got %#v", cur)
	}

	// RemoveMissing=false：缺失旧行保留，且同号新文件冲突
	_, animeID2, anime2, eps2 := newReconcileTestDB(t)
	err = ReconcileEpisodes(animeID2, EpisodeReconcileInput{
		ExpectedAnime: anime2,
		Expected:      eps2,
		Desired: []models.Episode{
			{FilePath: "01.mkv", EpNumber: 2},
			{FilePath: "03.mkv", EpNumber: 1}, // 与缺失的 02.mkv 旧行争用集号 1
		},
		PresentFiles:  []string{"01.mkv", "03.mkv"},
		RemoveMissing: false,
	})
	if !errors.Is(err, ErrEpisodeIdentityConflict) {
		t.Fatalf("want IdentityConflict for missing same-number without rename, got %v", err)
	}
}

func TestReconcileEpisodesSwapEpNumbersAndFaultRollback(t *testing.T) {
	userID, animeID, anime, eps := newReconcileTestDB(t)
	ep01 := findEpByPath(eps, "01.mkv")
	ep02 := findEpByPath(eps, "02.mkv")
	if err := UpsertProgress(userID, ep01.ID, 11, false); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProgress(userID, ep02.ID, 22, true); err != nil {
		t.Fatal(err)
	}

	desired := []models.Episode{
		{FilePath: "01.mkv", EpNumber: 1},
		{FilePath: "02.mkv", EpNumber: 2},
	}
	if err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      eps,
		Desired:       desired,
		PresentFiles:  []string{"01.mkv", "02.mkv"},
		RemoveMissing: true,
	}); err != nil {
		t.Fatal(err)
	}
	after, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	if findEpByPath(after, "01.mkv").ID != ep01.ID || findEpByPath(after, "02.mkv").ID != ep02.ID {
		t.Fatalf("swap must keep IDs: %#v", after)
	}
	p1, _ := GetProgress(userID, ep01.ID)
	p2, _ := GetProgress(userID, ep02.ID)
	if p1.Position != 11 || p2.Position != 22 {
		t.Fatalf("progress moved: p1=%#v p2=%#v", p1, p2)
	}

	// 故障注入：提交前失败，中间负编号不得外露
	anime3, err := GetAnimeByID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	reconcileCommitHook = func() error { return errors.New("injected failure") }
	err = ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: *anime3,
		Expected:      before,
		Desired: []models.Episode{
			{FilePath: "01.mkv", EpNumber: 2},
			{FilePath: "02.mkv", EpNumber: 1},
		},
		PresentFiles:  []string{"01.mkv", "02.mkv"},
		RemoveMissing: true,
	})
	if err == nil || err.Error() != "injected failure" {
		t.Fatalf("want injected failure, got %v", err)
	}
	reconcileCommitHook = nil
	rolled, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range rolled {
		if ep.EpNumber < 0 {
			t.Fatalf("temp negative leaked after rollback: %#v", ep)
		}
		if ep.EpNumber != findEpByPath(before, ep.FilePath).EpNumber {
			t.Fatalf("rollback incomplete: before=%#v after=%#v", before, rolled)
		}
	}
}

func TestReconcileEpisodesRenamePreservesID(t *testing.T) {
	userID, animeID, anime, eps := newReconcileTestDB(t)
	ep01 := findEpByPath(eps, "01.mkv")
	ep02 := findEpByPath(eps, "02.mkv")
	if err := UpsertProgress(userID, ep01.ID, 50, false); err != nil {
		t.Fatal(err)
	}
	if err := EnqueueBangumiOutbox(userID, ep01.ID); err != nil {
		t.Fatal(err)
	}

	// 01.mkv 改名为 01-new.mkv，磁盘上旧名已消失；02 仍在
	err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      eps,
		Desired: []models.Episode{
			{FilePath: "01-new.mkv", EpNumber: 1},
			{FilePath: "02.mkv", EpNumber: 2},
		},
		PresentFiles: []string{"01-new.mkv", "02.mkv"},
		Renames: []EpisodeRename{
			{EpisodeID: ep01.ID, From: "01.mkv", To: "01-new.mkv"},
		},
		RemoveMissing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := ListEpisodesByAnimeID(animeID)
	if err != nil {
		t.Fatal(err)
	}
	renamed := findEpByPath(after, "01-new.mkv")
	if renamed.ID != ep01.ID || renamed.EpNumber != 1 {
		t.Fatalf("rename must keep ID and set number: %#v want id=%d", renamed, ep01.ID)
	}
	if findEpByPath(after, "02.mkv").ID != ep02.ID {
		t.Fatal("untouched episode ID changed")
	}
	progress, err := GetProgress(userID, ep01.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 50 {
		t.Fatalf("progress lost: %#v", progress)
	}
}

func TestReconcileEpisodesSameNumberNewFileWithoutRenameConflicts(t *testing.T) {
	_, animeID, anime, eps := newReconcileTestDB(t)
	// 旧 01.mkv 消失，新 stranger.mkv 想占同一集号且无 Rename
	err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      eps,
		Desired: []models.Episode{
			{FilePath: "stranger.mkv", EpNumber: 2}, // 旧 01.mkv 的集号是 2
			{FilePath: "02.mkv", EpNumber: 1},
		},
		PresentFiles:  []string{"stranger.mkv", "02.mkv"},
		RemoveMissing: true,
	})
	if !errors.Is(err, ErrEpisodeIdentityConflict) {
		t.Fatalf("want IdentityConflict, got %v", err)
	}
}

func TestReconcileEpisodesConcurrentClaimOtherAnime(t *testing.T) {
	_, animeID, anime, eps := newReconcileTestDB(t)

	other, err := CreateAnime(&models.Anime{
		Title: "Peer", BangumiID: 2002, EpCount: 1, FilePath: "Ranma", // 同目录
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateEpisode(&models.Episode{AnimeID: other.ID, EpNumber: 1, FilePath: "03.mkv"}); err != nil {
		t.Fatal(err)
	}

	err = ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      eps,
		Desired: []models.Episode{
			{FilePath: "01.mkv", EpNumber: 1},
			{FilePath: "02.mkv", EpNumber: 2},
			{FilePath: "03.mkv", EpNumber: 3}, // 已被同目录其他 anime 认领
		},
		PresentFiles:  []string{"01.mkv", "02.mkv", "03.mkv"},
		RemoveMissing: true,
	})
	if !errors.Is(err, ErrEpisodeIdentityConflict) {
		t.Fatalf("want IdentityConflict for concurrent claim, got %v", err)
	}
}

func TestReconcileEpisodesRejectsDuplicateDesired(t *testing.T) {
	_, animeID, anime, eps := newReconcileTestDB(t)
	err := ReconcileEpisodes(animeID, EpisodeReconcileInput{
		ExpectedAnime: anime,
		Expected:      eps,
		Desired: []models.Episode{
			{FilePath: "01.mkv", EpNumber: 1},
			{FilePath: "01.mkv", EpNumber: 2},
		},
		PresentFiles:  []string{"01.mkv", "02.mkv"},
		RemoveMissing: true,
	})
	if err == nil {
		t.Fatal("expected duplicate path validation error")
	}
}
