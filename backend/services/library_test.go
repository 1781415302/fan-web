package services

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fan-web/database"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestLibraryScanCreatesSkipsAndAppendsEpisodes(t *testing.T) {
	root := t.TempDir()
	firstFile := "[ANi] Bocchi the Rock! - 01 [1080p].mkv"
	secondFile := "[ANi] Bocchi the Rock! - 02 [1080p].mkv"
	if err := writeEmptyFile(filepath.Join(root, firstFile)); err != nil {
		t.Fatal(err)
	}

	databasePath := filepath.Join(t.TempDir(), "library-test.db")
	if err := database.Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	service := NewLibraryService(mockBangumiService(), root)
	first, err := service.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalFiles != 1 || first.Skipped != 0 || first.NewAnimes != 1 || first.NewEpisodes != 1 || len(first.Unidentified) != 0 {
		t.Fatalf("unexpected first scan result: %#v", first)
	}

	second, err := service.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if second.TotalFiles != 1 || second.Skipped != 1 || second.NewAnimes != 0 || second.NewEpisodes != 0 || len(second.Unidentified) != 0 {
		t.Fatalf("unexpected repeated scan result: %#v", second)
	}

	if err := writeEmptyFile(filepath.Join(root, secondFile)); err != nil {
		t.Fatal(err)
	}
	third, err := service.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if third.TotalFiles != 2 || third.Skipped != 1 || third.NewAnimes != 0 || third.NewEpisodes != 1 || len(third.Unidentified) != 0 {
		t.Fatalf("unexpected append scan result: %#v", third)
	}

	anime, err := database.GetAnimeByBangumiID(1001)
	if err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 || episodes[0].EpNumber != 1 || episodes[1].EpNumber != 2 {
		t.Fatalf("unexpected episodes after append: %#v", episodes)
	}
}

func TestLibraryScanReportsUnidentifiedFiles(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, "unknown.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := writeEmptyFile(filepath.Join(root, "notes.txt")); err != nil {
		t.Fatal(err)
	}

	databasePath := filepath.Join(t.TempDir(), "library-unidentified-test.db")
	if err := database.Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	result, err := NewLibraryService(mockBangumiService(), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalFiles != 1 || len(result.Unidentified) != 1 || result.Unidentified[0].FileName != "unknown.mkv" || result.Unidentified[0].Reason != "无法识别集数" {
		t.Fatalf("unexpected unidentified result: %#v", result)
	}
}

func mockBangumiService() *BangumiService {
	return &BangumiService{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body string
		switch {
		case strings.HasPrefix(request.URL.Path, "/search/subject/"):
			body = `{"list":[{"id":1001,"name":"Bocchi the Rock!","name_cn":"孤独摇滚！","summary":"summary","eps_count":12,"images":{"large":"https://example.com/cover.jpg"}}]}`
		case request.URL.Path == "/v0/subjects/1001":
			body = `{"id":1001,"name":"Bocchi the Rock!","name_cn":"孤独摇滚！","summary":"summary","total_episodes":12,"images":{"large":"https://example.com/cover.jpg"}}`
		default:
			body = `{}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}}
}

func writeEmptyFile(path string) error {
	return os.WriteFile(path, nil, 0o644)
}
