package services

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestListPublicSubjectEpisodes(t *testing.T) {
	t.Run("pagination merge", func(t *testing.T) {
		var sawOffset200 atomic.Bool
		var sawAuth atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v0/episodes" {
				t.Errorf("unexpected path %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Header.Get("Authorization") != "" {
				sawAuth.Store(true)
				t.Errorf("public request must not set Authorization, got %q", r.Header.Get("Authorization"))
			}
			if r.URL.Query().Get("type") != "0" || r.URL.Query().Get("limit") != "200" {
				t.Errorf("unexpected query %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("subject_id") != "99" {
				t.Errorf("unexpected subject_id %q", r.URL.Query().Get("subject_id"))
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
		got, err := svc.ListPublicSubjectEpisodes(99)
		if err != nil {
			t.Fatal(err)
		}
		if sawAuth.Load() {
			t.Fatal("request set Authorization")
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
	})

	t.Run("404 not found", func(t *testing.T) {
		var sawAuth atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				sawAuth.Store(true)
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		svc := NewBangumiService()
		svc.SetBaseURL(server.URL)
		got, err := svc.ListPublicSubjectEpisodes(404)
		if !errors.Is(err, ErrBangumiNotFound) {
			t.Fatalf("err = %v, want ErrBangumiNotFound", err)
		}
		if got != nil {
			t.Fatalf("got %d episodes on 404, want nil", len(got))
		}
		if sawAuth.Load() {
			t.Fatal("request set Authorization")
		}
	})
}
