package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"fan-web/database"
	"fan-web/models"
)

func TestBangumiEpisodeNumberMapping(t *testing.T) {
	if got := bangumiEpisodeNumber(BangumiEpisode{Ep: 3, Sort: 99}); got != 3 {
		t.Fatalf("ep=3 mapped to %d, want 3", got)
	}
	if got := bangumiEpisodeNumber(BangumiEpisode{Ep: 0, Sort: 3}); got != 3 {
		t.Fatalf("ep=0,sort=3 mapped to %d, want 3", got)
	}
	local := models.Episode{EpNumber: 3}
	matched, ok := matchBangumiEpisode(local, []BangumiEpisode{
		{ID: 11, Ep: 1, Sort: 1},
		{ID: 33, Ep: 0, Sort: 3},
	})
	if !ok || matched.ID != 33 {
		t.Fatalf("expected sort fallback match id=33, got ok=%v %#v", ok, matched)
	}
}

func TestListSubjectEpisodesMergesSecondPage(t *testing.T) {
	var sawOffset200 atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/episodes" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer")
		}
		if r.URL.Query().Get("type") != "0" || r.URL.Query().Get("limit") != "200" {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		offset := r.URL.Query().Get("offset")
		var items []BangumiEpisode
		switch offset {
		case "0", "":
			for i := 1; i <= 200; i++ {
				items = append(items, BangumiEpisode{ID: i, Ep: float64(i), Sort: float64(i)})
			}
		case "200":
			sawOffset200.Store(true)
			items = []BangumiEpisode{{ID: 201, Ep: 201, Sort: 201}, {ID: 202, Ep: 202, Sort: 202}}
		default:
			t.Errorf("unexpected offset %q", offset)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
	}))
	t.Cleanup(server.Close)

	svc := NewBangumiService()
	svc.SetBaseURL(server.URL)
	got, err := svc.ListSubjectEpisodes("tok", 99)
	if err != nil {
		t.Fatal(err)
	}
	if !sawOffset200.Load() {
		t.Fatal("second page offset=200 was not requested")
	}
	if len(got) != 202 {
		t.Fatalf("merged %d episodes, want 202", len(got))
	}
	if got[0].ID != 1 || got[199].ID != 200 || got[200].ID != 201 || got[201].ID != 202 {
		t.Fatalf("unexpected merge order: first=%d mid=%d page2=%d last=%d", got[0].ID, got[199].ID, got[200].ID, got[201].ID)
	}
}

func TestEnsureCollectionGet200DoesNotPost(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/users/-/collections/7" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"subject_id":7}`))
	}))
	t.Cleanup(server.Close)
	svc := NewBangumiService()
	svc.SetBaseURL(server.URL)
	if err := svc.EnsureCollection("tok", 7); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 0 {
		t.Fatalf("GET 200 should not POST, got %d posts", posts.Load())
	}
}

func TestEnsureCollectionGet404PostsType3(t *testing.T) {
	var posts atomic.Int32
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost {
			posts.Add(1)
			raw, _ := io.ReadAll(r.Body)
			body = string(raw)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	t.Cleanup(server.Close)
	svc := NewBangumiService()
	svc.SetBaseURL(server.URL)
	if err := svc.EnsureCollection("tok", 8); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 {
		t.Fatalf("GET 404 should POST once, got %d", posts.Load())
	}
	if !strings.Contains(body, `"type":3`) {
		t.Fatalf("POST body = %q, want type=3", body)
	}
}

func TestEnsureCollectionPost400IsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"title":"already collected"}`))
	}))
	t.Cleanup(server.Close)
	svc := NewBangumiService()
	svc.SetBaseURL(server.URL)
	if err := svc.EnsureCollection("tok", 9); err != nil {
		t.Fatalf("POST 400 should be treated as already collected, got %v", err)
	}
}

func setupSyncDB(t *testing.T) *models.User {
	t.Helper()
	if err := database.Init(filepath.Join(t.TempDir(), "bangumi-sync.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	if err := database.InitAdmin("admin", "password"); err != nil {
		t.Fatal(err)
	}
	user, err := database.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func mustBoundAnime(t *testing.T, bangumiID, epNumber int) (models.Anime, models.Episode) {
	t.Helper()
	anime, err := database.CreateAnime(&models.Anime{
		Title:     "Bound",
		BangumiID: bangumiID,
		EpCount:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SyncEpisodes(anime.ID, []models.Episode{{
		EpNumber: epNumber,
		FilePath: "e.mp4",
	}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil || len(episodes) != 1 {
		t.Fatalf("episodes: %v %#v", err, episodes)
	}
	return *anime, episodes[0]
}

func newTestSync(t *testing.T, handler http.HandlerFunc) *BangumiSync {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	svc := NewBangumiService()
	svc.SetBaseURL(server.URL)
	sync := NewBangumiSync(svc)
	sync.interval = 0
	return sync
}

func TestDrainUnauthorizedClearsTokenAndOutbox(t *testing.T) {
	user := setupSyncDB(t)
	_, episode := mustBoundAnime(t, 101, 1)
	if err := database.SaveBangumiToken(user.ID, "dead-token"); err != nil {
		t.Fatal(err)
	}
	if err := database.EnqueueBangumiOutbox(user.ID, episode.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProgress(user.ID, episode.ID, 12, true); err != nil {
		t.Fatal(err)
	}

	var patched atomic.Int32
	sync := newTestSync(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched.Add(1)
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	sync.Drain()

	if patched.Load() != 0 {
		t.Fatalf("401 must not PATCH, got %d", patched.Load())
	}
	if _, ok, err := database.GetBangumiToken(user.ID); err != nil || ok {
		t.Fatalf("token should be cleared, ok=%v err=%v", ok, err)
	}
	rows, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("outbox should be empty after 401, got %#v", rows)
	}
}

func TestDrainUnmappedDeletesRow(t *testing.T) {
	user := setupSyncDB(t)
	_, episode := mustBoundAnime(t, 102, 9)
	if err := database.SaveBangumiToken(user.ID, "tok"); err != nil {
		t.Fatal(err)
	}
	if err := database.EnqueueBangumiOutbox(user.ID, episode.ID); err != nil {
		t.Fatal(err)
	}

	sync := newTestSync(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v0/me":
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/v0/episodes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []BangumiEpisode{{ID: 1, Ep: 1, Sort: 1}},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	sync.Drain()

	rows, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("unmapped row should be deleted, got %#v", rows)
	}
	if _, ok, err := database.GetBangumiToken(user.ID); err != nil || !ok {
		t.Fatalf("unmapped should not clear token, ok=%v err=%v", ok, err)
	}
}

func TestEnqueueDrainEmptiesOutbox(t *testing.T) {
	user := setupSyncDB(t)
	_, episode := mustBoundAnime(t, 103, 3)
	if err := database.SaveBangumiToken(user.ID, "tok"); err != nil {
		t.Fatal(err)
	}

	var patched atomic.Int32
	sync := newTestSync(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v0/me":
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/v0/episodes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []BangumiEpisode{{ID: 333, Ep: 3, Sort: 3}},
			})
		case r.URL.Path == "/v0/users/-/collections/103" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"subject_id":103}`))
		case r.URL.Path == "/v0/users/-/collections/103/episodes" && r.Method == http.MethodPatch:
			patched.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	sync.EnqueueWatched(user.ID, episode.ID)

	if patched.Load() != 1 {
		t.Fatalf("expected one PATCH, got %d", patched.Load())
	}
	rows, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("Enqueue+Drain should empty outbox, got %#v", rows)
	}
}

func TestSyncInboundType2KeepsPosition(t *testing.T) {
	user := setupSyncDB(t)
	_, episode := mustBoundAnime(t, 104, 3)
	if err := database.UpsertProgress(user.ID, episode.ID, 12, false); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveBangumiToken(user.ID, "tok"); err != nil {
		t.Fatal(err)
	}

	sync := newTestSync(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v0/me":
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/v0/users/-/collections/104/episodes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []BangumiEpisodeCollection{
					{Episode: BangumiEpisode{ID: 1, Ep: 3, Sort: 3}, Type: 2},
					{Episode: BangumiEpisode{ID: 2, Ep: 4, Sort: 4}, Type: 0},
				},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	result, err := sync.SyncInbound(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Animes != 1 || result.EpisodesMarked != 1 {
		t.Fatalf("sync result = %#v", result)
	}
	progress, err := database.GetProgress(user.ID, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !progress.Watched {
		t.Fatal("type=2 should set watched true")
	}
	if progress.Position != 12 {
		t.Fatalf("position = %d, want 12", progress.Position)
	}
}

func TestEnqueueWatchedSkipsWithoutToken(t *testing.T) {
	user := setupSyncDB(t)
	_, episode := mustBoundAnime(t, 201, 1)
	sync := newTestSync(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no Bangumi HTTP expected, got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	sync.EnqueueWatched(user.ID, episode.ID)
	rows, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("unbound user must not enqueue, got %#v", rows)
	}
}

func TestDrainDeletesRowsWithoutToken(t *testing.T) {
	bound := setupSyncDB(t)
	user, err := database.CreateUser("viewer", "password12", false)
	if err != nil {
		t.Fatal(err)
	}
	_, episode := mustBoundAnime(t, 202, 1)
	if err := database.EnqueueBangumiOutbox(user.ID, episode.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveBangumiToken(bound.ID, "tok"); err != nil {
		t.Fatal(err)
	}
	if err := database.EnqueueBangumiOutbox(bound.ID, episode.ID); err != nil {
		t.Fatal(err)
	}

	var patched atomic.Int32
	sync := newTestSync(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v0/me":
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/v0/episodes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []BangumiEpisode{{ID: 1, Ep: 1, Sort: 1}},
			})
		case strings.HasPrefix(r.URL.Path, "/v0/users/-/collections/") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"subject_id":202}`))
		case strings.HasSuffix(r.URL.Path, "/episodes") && r.Method == http.MethodPatch:
			patched.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	sync.Drain()

	rows, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("no-token rows should be deleted and bound row drained, got %#v", rows)
	}
	if patched.Load() != 1 {
		t.Fatalf("bound user should still PATCH, got %d", patched.Load())
	}
}
