package services

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fan-web/database"
	"fan-web/models"
)

func waitJobLeftRunning(t *testing.T, job *LibraryJob) ScanJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snap := job.Snapshot()
		if snap.State != ScanJobRunning {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job still running")
	return ScanJob{}
}

func TestLibraryJobStartTwiceRunsScanOnce(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, "[ANi] Bocchi the Rock! - 01 [1080p].mkv")); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var searches atomic.Int32
	var closeOnce sync.Once
	bangumi := mockBangumiResponses(func(request *http.Request) string {
		if strings.HasPrefix(request.URL.Path, "/search/subject/") {
			searches.Add(1)
			closeOnce.Do(func() { close(started) })
			<-release
			return `{"list":[{"id":1001,"name":"Bocchi the Rock!","name_cn":"孤独摇滚！","summary":"summary","eps_count":12,"images":{"large":"https://example.com/cover.jpg"}}]}`
		}
		if request.URL.Path == "/v0/subjects/1001" {
			return `{"id":1001,"name":"Bocchi the Rock!","name_cn":"孤独摇滚！","summary":"summary","total_episodes":12,"images":{"large":"https://example.com/cover.jpg"}}`
		}
		return `{}`
	})

	job := NewLibraryJob(NewLibraryService(bangumi, root))
	first := job.Start()
	if first.State != ScanJobRunning {
		t.Fatalf("first Start state = %q, want running", first.State)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Scan did not enter Search")
	}
	second := job.Start()
	if second.State != ScanJobRunning {
		t.Fatalf("second Start state = %q, want running", second.State)
	}
	close(release)

	done := waitJobLeftRunning(t, job)
	if done.State != ScanJobDone {
		t.Fatalf("job state = %q error=%q, want done", done.State, done.Error)
	}
	if searches.Load() != 1 {
		t.Fatalf("Scan ran %d times, want 1", searches.Load())
	}
}

func TestLibraryJobScanErrorDoesNotChangeTable(t *testing.T) {
	setupLibraryDB(t)
	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FileName: "keep.mkv", Reason: "old", FilePath: "dir", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}

	job := NewLibraryJob(NewLibraryService(mockBangumiService(), filepath.Join(t.TempDir(), "missing-root")))
	if snap := job.Start(); snap.State != ScanJobRunning {
		t.Fatalf("Start state = %q, want running", snap.State)
	}
	done := waitJobLeftRunning(t, job)
	if done.State != ScanJobError {
		t.Fatalf("job state = %q error=%q, want error", done.State, done.Error)
	}

	items, total, err := database.ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].FileName != "keep.mkv" || items[0].Reason != "old" {
		t.Fatalf("Scan 失败不得改表, got total=%d items=%#v", total, items)
	}
}

func TestLibraryJobSuccessWritesFilteredUnidentified(t *testing.T) {
	setupLibraryDB(t)
	root := t.TempDir()
	relDir := "Show"
	if err := os.MkdirAll(filepath.Join(root, relDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeEmptyFile(filepath.Join(root, relDir, "unknown.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := writeEmptyFile(filepath.Join(root, relDir, "Alpha - 01.mkv")); err != nil {
		t.Fatal(err)
	}

	var associateOnce sync.Once
	bangumi := mockBangumiResponses(func(request *http.Request) string {
		if strings.HasPrefix(request.URL.Path, "/search/subject/") {
			associateOnce.Do(func() {
				anime, err := database.CreateAnime(&models.Anime{Title: "Manual", FilePath: relDir})
				if err != nil {
					t.Errorf("CreateAnime in Search: %v", err)
					return
				}
				if err := database.CreateEpisode(&models.Episode{
					AnimeID:  anime.ID,
					EpNumber: 1,
					FilePath: "unknown.mkv",
				}); err != nil {
					t.Errorf("CreateEpisode in Search: %v", err)
				}
			})
			return `{"list":[]}`
		}
		return `{}`
	})

	job := NewLibraryJob(NewLibraryService(bangumi, root))
	if snap := job.Start(); snap.State != ScanJobRunning {
		t.Fatalf("Start state = %q, want running", snap.State)
	}
	done := waitJobLeftRunning(t, job)
	if done.State != ScanJobDone || done.Result == nil {
		t.Fatalf("job state = %q error=%q result=%#v, want done with result", done.State, done.Error, done.Result)
	}

	want := make(map[string]UnidentifiedFile)
	for _, file := range done.Result.Unidentified {
		associated, err := database.IsFileAssociated(file.FileName, file.FilePath)
		if err != nil {
			t.Fatal(err)
		}
		if associated {
			continue
		}
		want[file.FilePath+"/"+file.FileName] = file
	}

	items, total, err := database.ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != len(want) || len(items) != len(want) {
		t.Fatalf("ListUnidentified total=%d len=%d, want %d (result=%#v items=%#v)", total, len(items), len(want), done.Result.Unidentified, items)
	}
	for _, item := range items {
		key := item.FilePath + "/" + item.FileName
		_, ok := want[key]
		if !ok {
			t.Fatalf("unexpected stored unidentified %#v, want keys %v", item, keysOf(want))
		}
	}
}

func keysOf(m map[string]UnidentifiedFile) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
