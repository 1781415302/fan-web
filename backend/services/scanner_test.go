package services

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestScannerRecognizesSupportedEpisodeNames(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"[Fansub] Title [67].mkv",
		"Title - 02.mp4",
		"Title EP03.avi",
		"Title 第4集.webm",
		"Title S01E05.mov",
		"06.m4v",
		"Title-08v2.mkv",
		"ignored.txt",
		"Title [67].mkv:Zone.Identifier",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 7 {
		t.Fatalf("expected 7 episodes, got %d: %#v", len(episodes), episodes)
	}
	want := []int{2, 3, 4, 5, 6, 8, 67}
	for i, episode := range episodes {
		if episode.EpNumber != want[i] {
			t.Fatalf("episode %d: expected number %d, got %d", i, want[i], episode.EpNumber)
		}
	}
}

func TestScannerRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := NewScannerService(root).Scan("../outside")
	if !errors.Is(err, ErrInvalidVideoPath) {
		t.Fatalf("expected ErrInvalidVideoPath, got %v", err)
	}
}

func TestScannerMovieBecomesEpisodeOne(t *testing.T) {
	root := t.TempDir()
	name := "[Subs]某作品 剧场版 [1080p].mkv"
	if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 {
		t.Fatalf("expected EpNumber 1, got %d", episodes[0].EpNumber)
	}
	if episodes[0].FilePath != name {
		t.Fatalf("expected FilePath %q, got %q", name, episodes[0].FilePath)
	}
}

func TestScannerKaguyaYearOnlyReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	name := "[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4"
	if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 0 {
		t.Fatalf("expected 0 episodes for Kaguya-only folder, got %d: %#v", len(episodes), episodes)
	}
}

func TestScannerSkipsVersionOnlyFilename(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "v2.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 0 {
		t.Fatalf("expected 0 episodes for v2.mkv, got %d: %#v", len(episodes), episodes)
	}
}

func TestScannerRealEpisodeOneBeatsMovie(t *testing.T) {
	root := t.TempDir()
	real := "[TSDM][Cosmic Princess Kaguya][01][1080p].mkv"
	movie := "[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4"
	for _, name := range []string{movie, real} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected only real ep1, got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 {
		t.Fatalf("expected EpNumber 1, got %d", episodes[0].EpNumber)
	}
	if episodes[0].FilePath != real {
		t.Fatalf("expected real ep file %q, got %q", real, episodes[0].FilePath)
	}
}

func TestScannerTwoMoviesKeepsFirstByFilename(t *testing.T) {
	root := t.TempDir()
	first := "[Fansub][Alpha Title] 剧场版 [1080p].mkv"
	later := "[Fansub][Zeta Title] 剧场版 [1080p].mkv"
	for _, name := range []string{later, first} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	episodes, err := NewScannerService(root).Scan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode (second movie silent drop), got %d: %#v", len(episodes), episodes)
	}
	if episodes[0].EpNumber != 1 || episodes[0].FilePath != first {
		t.Fatalf("expected first-by-filename movie as ep1, got %#v", episodes[0])
	}
}

// TestScannerConcurrentSetRootPathAndReads 验证 rootPath 的无锁读写竞态修复：
// 并发 SetRootPath 与 RootPath/ListSubDirs（Dirs 请求路径）交错时不允许
// data race 或读到撕裂的中间值（go test -race 下验证）。
func TestScannerConcurrentSetRootPathAndReads(t *testing.T) {
	base := t.TempDir()
	roots := []string{base, filepath.Join(base, "a"), filepath.Join(base, "b")}
	for _, root := range roots {
		if err := os.MkdirAll(filepath.Join(root, "show"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scanner := NewScannerService(roots[0])

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			scanner.SetRootPath(roots[i%len(roots)])
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			// 读路径可能被 SetRootPath 交错；必须总能看到某个完整 root（不允许
			// 无同步读写 string 引发的撕裂/竞态，ListSubDirs 内部做 Abs+EvalSymlinks）。
			_ = scanner.RootPath()
			dirs, err := scanner.ListSubDirs()
			if err != nil {
				// 根目录被 SetRootPath 切走时目录可能不存在，允许 ErrInvalid/不存在。
				if _, statErr := os.Stat(scanner.RootPath()); statErr != nil {
					continue
				}
				t.Errorf("ListSubDirs unexpected error: %v", err)
				return
			}
			_ = dirs
		}
	}()
	// 跑一小段时间后停止。
	time.Sleep(100 * time.Millisecond)
	close(done)
	wg.Wait()
}
