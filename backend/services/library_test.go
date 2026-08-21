package services

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"fan-web/database"
	"fan-web/models"
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
	assertUnidentifiedPathAndEmptyCandidates(t, result.Unidentified[0], "")
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

func TestLibraryScanLowConfidenceReportsCandidates(t *testing.T) {
	root := t.TempDir()
	relDir := filepath.Join("shows", "demo")
	if err := os.MkdirAll(filepath.Join(root, relDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeEmptyFile(filepath.Join(root, relDir, "Demo Show - 01.mkv")); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	result, err := NewLibraryService(mockBangumiSearchJSON(`{"list":[{"id":645948,"name":"MAGI Synthavision 1980 Demo Reel","name_cn":""}]}`), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 0 || result.NewEpisodes != 0 || len(result.Unidentified) != 1 {
		t.Fatalf("expected one low-confidence unidentified file, got %#v", result)
	}
	got := result.Unidentified[0]
	if got.FileName != "Demo Show - 01.mkv" || got.Reason != "匹配置信度不足" {
		t.Fatalf("unexpected unidentified file: %#v", got)
	}
	if got.FilePath != relDir {
		t.Fatalf("file_path = %q, want %q", got.FilePath, relDir)
	}
	if len(got.Candidates) == 0 {
		t.Fatalf("expected match candidates, got %#v", got)
	}
	if got.Candidates[0].ID != 645948 {
		t.Fatalf("expected MAGI candidate first, got %#v", got.Candidates)
	}
}

func TestLibraryScanAutoAcceptCreatesAnimeAndEpisode(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, "[ANi] Bocchi the Rock! - 01 [1080p].mkv")); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	result, err := NewLibraryService(mockBangumiService(), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 1 || result.NewEpisodes != 1 || len(result.Unidentified) != 0 {
		t.Fatalf("auto-accept should create anime+episode, got %#v", result)
	}
	anime, err := database.GetAnimeByBangumiID(1001)
	if err != nil || anime == nil {
		t.Fatalf("expected created anime 1001, err=%v anime=%v", err, anime)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 || episodes[0].EpNumber != 1 {
		t.Fatalf("unexpected episodes: %#v", episodes)
	}
}

func TestLibraryScanPrefersExactMatchOverMAGIRank(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, "Demo Show - 01.mkv")); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	result, err := NewLibraryService(mockBangumiMAGIFirstThenDemoShow(), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 1 || result.NewEpisodes != 1 || len(result.Unidentified) != 0 {
		t.Fatalf("exact Demo Show should auto-accept, got %#v", result)
	}
	wrong, err := database.GetAnimeByBangumiID(645948)
	if err != nil {
		t.Fatal(err)
	}
	if wrong != nil {
		t.Fatalf("must not store MAGI-first hit 645948, got %#v", wrong)
	}
	anime, err := database.GetAnimeByBangumiID(2001)
	if err != nil {
		t.Fatal(err)
	}
	if anime == nil || anime.BangumiID != 2001 || anime.Title != "Demo Show" {
		t.Fatalf("stored bangumi must be exact Demo Show 2001, got %#v", anime)
	}
}

func TestLibraryScanShortenSearchStillScoresOriginalTitle(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, "Alpha Beta Gamma - 01.mkv")); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	result, err := NewLibraryService(mockBangumiShortenThenOriginalWins(), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 1 || result.NewEpisodes != 1 || len(result.Unidentified) != 0 {
		t.Fatalf("original-title winner should auto-accept, got %#v", result)
	}
	shortHit, err := database.GetAnimeByBangumiID(111)
	if err != nil {
		t.Fatal(err)
	}
	if shortHit != nil {
		t.Fatalf("shortened-query exact hit 111 must not win, got %#v", shortHit)
	}
	anime, err := database.GetAnimeByBangumiID(222)
	if err != nil {
		t.Fatal(err)
	}
	if anime == nil || anime.Title != "Alpha Beta Gamma" {
		t.Fatalf("scoring key must stay original title, want 222 Alpha Beta Gamma, got %#v", anime)
	}
}

func TestLibraryScanNoSearchResultKeepsEmptyCandidatesJSON(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, "Missing Show - 01.mkv")); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	result, err := NewLibraryService(mockBangumiSearchJSON(`{"list":[]}`), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unidentified) != 1 || result.Unidentified[0].Reason != "无搜索结果" {
		t.Fatalf("expected 无搜索结果, got %#v", result)
	}
	assertUnidentifiedPathAndEmptyCandidates(t, result.Unidentified[0], "")
}

func TestLibraryScanMixedDirUsesEachFileRelDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dirA"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "dirB"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeEmptyFile(filepath.Join(root, "dirA", "Show Name - 01.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := writeEmptyFile(filepath.Join(root, "dirB", "Show Name - 02.mkv")); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	result, err := NewLibraryService(mockBangumiService(), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 0 || len(result.Unidentified) != 2 {
		t.Fatalf("mixed-dir group should be unidentified, got %#v", result)
	}
	byName := map[string]UnidentifiedFile{}
	for _, file := range result.Unidentified {
		if file.Reason != "同一标题的文件位于不同目录" {
			t.Fatalf("unexpected reason %q on %#v", file.Reason, file)
		}
		assertUnidentifiedPathAndEmptyCandidates(t, file, file.FilePath)
		byName[file.FileName] = file
	}
	if byName["Show Name - 01.mkv"].FilePath != "dirA" {
		t.Fatalf("ep01 file_path = %q, want dirA", byName["Show Name - 01.mkv"].FilePath)
	}
	if byName["Show Name - 02.mkv"].FilePath != "dirB" {
		t.Fatalf("ep02 file_path = %q, want dirB", byName["Show Name - 02.mkv"].FilePath)
	}
}

func setupLibraryDB(t *testing.T) {
	t.Helper()
	if err := database.Init(filepath.Join(t.TempDir(), "library-test.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
}

func assertUnidentifiedPathAndEmptyCandidates(t *testing.T, file UnidentifiedFile, wantPath string) {
	t.Helper()
	if file.FilePath != wantPath {
		t.Fatalf("file_path = %q, want %q (file=%#v)", file.FilePath, wantPath, file)
	}
	if file.Candidates == nil {
		t.Fatal("candidates must be empty slice, got nil")
	}
	if len(file.Candidates) != 0 {
		t.Fatalf("candidates must be empty, got %#v", file.Candidates)
	}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded["candidates"]) != "[]" {
		t.Fatalf("candidates JSON = %s, want [] (raw=%s)", decoded["candidates"], raw)
	}
}

func mockBangumiSearchJSON(searchBody string) *BangumiService {
	return mockBangumiResponses(func(request *http.Request) string {
		if strings.HasPrefix(request.URL.Path, "/search/subject/") {
			return searchBody
		}
		return `{}`
	})
}

func mockBangumiMAGIFirstThenDemoShow() *BangumiService {
	return mockBangumiResponses(func(request *http.Request) string {
		switch {
		case strings.HasPrefix(request.URL.Path, "/search/subject/"):
			return `{"list":[{"id":645948,"name":"MAGI Synthavision 1980 Demo Reel","name_cn":""},{"id":2001,"name":"Demo Show","name_cn":""}]}`
		case request.URL.Path == "/v0/subjects/2001":
			return `{"id":2001,"name":"Demo Show","name_cn":"","summary":"","total_episodes":1,"images":{}}`
		case request.URL.Path == "/v0/subjects/645948":
			return `{"id":645948,"name":"MAGI Synthavision 1980 Demo Reel","name_cn":"","summary":"","total_episodes":1,"images":{}}`
		default:
			return `{}`
		}
	})
}

func mockBangumiShortenThenOriginalWins() *BangumiService {
	return mockBangumiResponses(func(request *http.Request) string {
		switch {
		case strings.HasPrefix(request.URL.Path, "/search/subject/"):
			keyword := strings.TrimPrefix(request.URL.Path, "/search/subject/")
			if keyword == "Alpha Beta Gamma" {
				return `{"list":[]}`
			}
			if keyword == "Alpha Beta" {
				return `{"list":[{"id":111,"name":"Alpha Beta","name_cn":""},{"id":222,"name":"Alpha Beta Gamma","name_cn":""}]}`
			}
			return `{"list":[]}`
		case request.URL.Path == "/v0/subjects/222":
			return `{"id":222,"name":"Alpha Beta Gamma","name_cn":"","summary":"","total_episodes":1,"images":{}}`
		case request.URL.Path == "/v0/subjects/111":
			return `{"id":111,"name":"Alpha Beta","name_cn":"","summary":"","total_episodes":1,"images":{}}`
		default:
			return `{}`
		}
	})
}

func mockBangumiResponses(bodyFor func(*http.Request) string) *BangumiService {
	return &BangumiService{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(bodyFor(request))),
			Request:    request,
		}, nil
	})}}
}

const kaguyaTriggerFile = "[TSDM][Cosmic Princess Kaguya][2026][NF_web-DL][HEVC-10bit 1080p AAC][CHS_JP].mp4"

const kaguyaSearchJP = `{"list":[{"id":604826,"name":"超かぐや姫！","name_cn":"超时空辉夜姬！"}]}`

const kaguyaSubjectAlias = `{"id":604826,"name":"超かぐや姫！","name_cn":"超时空辉夜姬！","summary":"s","total_episodes":1,"platform":"WEB","images":{"large":"https://example.com/c.jpg"},"infobox":[{"key":"别名","value":"Cosmic Princess Kaguya!"}]}`

const kaguyaSubjectNoInfobox = `{"id":604826,"name":"超かぐや姫！","name_cn":"超时空辉夜姬！","summary":"s","total_episodes":1,"images":{"large":"https://example.com/c.jpg"}}`

func mockKaguyaAliasBangumi(subjectHits *atomic.Int32, subjectBody string) *BangumiService {
	return mockBangumiResponses(func(request *http.Request) string {
		if strings.HasPrefix(request.URL.Path, "/search/subject/") {
			if strings.Contains(request.URL.Path, "Cosmic Princess Kaguya") {
				return kaguyaSearchJP
			}
			return `{"list":[]}`
		}
		if strings.HasPrefix(request.URL.Path, "/v0/subjects/") {
			if subjectHits != nil {
				subjectHits.Add(1)
			}
			return subjectBody
		}
		return `{}`
	})
}

func listUnidentifiedSnapshot(t *testing.T) []models.UnidentifiedFile {
	t.Helper()
	items, _, err := database.ListUnidentified(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestLibraryScanKaguyaAliasHappyPath(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, kaguyaTriggerFile)); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)
	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FileName: "keep.mkv", Reason: "old", FilePath: "dir", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}
	before := listUnidentifiedSnapshot(t)

	var subjectHits atomic.Int32
	result, err := NewLibraryService(mockKaguyaAliasBangumi(&subjectHits, kaguyaSubjectAlias), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 1 || result.NewEpisodes != 1 || len(result.Unidentified) != 0 {
		t.Fatalf("kaguya alias happy path: %#v", result)
	}
	after := listUnidentifiedSnapshot(t)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("Scan must not write unidentified table: before=%#v after=%#v", before, after)
	}
	if got := subjectHits.Load(); got != 1 {
		t.Fatalf("/v0/subjects/ hits = %d, want 1 (alias-round cache reuse)", got)
	}

	anime, err := database.GetAnimeByBangumiID(604826)
	if err != nil || anime == nil {
		t.Fatalf("expected created anime 604826, err=%v anime=%v", err, anime)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 || episodes[0].EpNumber != 1 {
		t.Fatalf("expected ep_number==1, got %#v", episodes)
	}
}

func TestLibraryScanKaguyaRescanSkipped(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, kaguyaTriggerFile)); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	service := NewLibraryService(mockKaguyaAliasBangumi(nil, kaguyaSubjectAlias), root)
	first, err := service.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if first.NewAnimes != 1 || first.NewEpisodes != 1 || len(first.Unidentified) != 0 {
		t.Fatalf("unexpected first scan result: %#v", first)
	}

	second, err := service.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if second.NewAnimes != 0 || second.NewEpisodes != 0 || second.Skipped < 1 {
		t.Fatalf("rescan should skip, got %#v", second)
	}
}

func TestLibraryScanKaguyaNoInfoboxLowConfidence(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, kaguyaTriggerFile)); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	result, err := NewLibraryService(mockKaguyaAliasBangumi(nil, kaguyaSubjectNoInfobox), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 0 || result.NewEpisodes != 0 || len(result.Unidentified) != 1 {
		t.Fatalf("no-infobox should not create anime, got %#v", result)
	}
	got := result.Unidentified[0]
	if got.FileName != kaguyaTriggerFile || got.Reason != "匹配置信度不足" {
		t.Fatalf("unexpected unidentified file: %#v", got)
	}
	if len(got.Candidates) == 0 {
		t.Fatalf("expected nonempty candidates, got %#v", got)
	}
	anime, err := database.GetAnimeByBangumiID(604826)
	if err != nil {
		t.Fatal(err)
	}
	if anime != nil {
		t.Fatalf("must not store 604826 without alias, got %#v", anime)
	}
}

func TestLibraryScanCannotIdentifyEpisodeNumber(t *testing.T) {
	files := []string{
		"unknown.mkv",
		"v2.mkv",
		"Arrival[2026].mkv",
	}
	for _, name := range files {
		name := name
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := writeEmptyFile(filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
			setupLibraryDB(t)

			result, err := NewLibraryService(mockBangumiService(), root).Scan()
			if err != nil {
				t.Fatal(err)
			}
			if result.NewAnimes != 0 || result.NewEpisodes != 0 || len(result.Unidentified) != 1 {
				t.Fatalf("expected one unidentified, got %#v", result)
			}
			got := result.Unidentified[0]
			if got.FileName != name || got.Reason != "无法识别集数" {
				t.Fatalf("unexpected unidentified file: %#v", got)
			}
		})
	}
}

func TestLibraryScanChineseTitleDoesNotCreateWrongAnime(t *testing.T) {
	root := t.TempDir()
	name := "[Subs][宇宙公主辉夜][2026][1080p].mp4"
	if err := writeEmptyFile(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	bangumi := mockBangumiResponses(func(request *http.Request) string {
		if strings.HasPrefix(request.URL.Path, "/search/subject/") {
			return `{"list":[{"id":999001,"name":"全然違う作品","name_cn":"完全无关番剧"}]}`
		}
		if strings.HasPrefix(request.URL.Path, "/v0/subjects/") {
			return `{"id":999001,"name":"全然違う作品","name_cn":"完全无关番剧","summary":"s","total_episodes":1,"images":{},"infobox":[{"key":"别名","value":"Unrelated Alias"}]}`
		}
		return `{}`
	})
	result, err := NewLibraryService(bangumi, root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 0 || result.NewEpisodes != 0 {
		t.Fatalf("must not create a wrong anime, got %#v", result)
	}
	if len(result.Unidentified) != 1 {
		t.Fatalf("expected one unidentified, got %#v", result)
	}
	reason := result.Unidentified[0].Reason
	if reason != "无搜索结果" && reason != "匹配置信度不足" {
		t.Fatalf("reason = %q, want 无搜索结果 or 匹配置信度不足 (file=%#v)", reason, result.Unidentified[0])
	}
	wrong, err := database.GetAnimeByBangumiID(999001)
	if err != nil {
		t.Fatal(err)
	}
	if wrong != nil {
		t.Fatalf("must not store unrelated 999001, got %#v", wrong)
	}
	kaguya, err := database.GetAnimeByBangumiID(604826)
	if err != nil {
		t.Fatal(err)
	}
	if kaguya != nil {
		t.Fatalf("must not store 604826, got %#v", kaguya)
	}
}

func TestLibraryScanRealEpisodeBeatsSameTitleMovie(t *testing.T) {
	root := t.TempDir()
	movie := kaguyaTriggerFile
	real := "[TSDM][Cosmic Princess Kaguya][01][1080p].mkv"
	movieParsed := ParseFilename(movie)
	realParsed := ParseFilename(real)
	if movieParsed.Title != realParsed.Title || movieParsed.Title == "" {
		t.Fatalf("Titles must match before asserting: movie=%#v real=%#v", movieParsed, realParsed)
	}
	if movieParsed.Kind != "episode" || movieParsed.EpisodeNum != 0 || realParsed.Kind != "episode" || realParsed.EpisodeNum != 1 {
		t.Fatalf("unexpected kinds/ep: no-ep=%#v real=%#v", movieParsed, realParsed)
	}
	for _, name := range []string{movie, real} {
		if err := writeEmptyFile(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	setupLibraryDB(t)

	bangumi := mockBangumiResponses(func(request *http.Request) string {
		if strings.HasPrefix(request.URL.Path, "/search/subject/") {
			return `{"list":[{"id":604826,"name":"Cosmic Princess Kaguya","name_cn":"超时空辉夜姬！"}]}`
		}
		if request.URL.Path == "/v0/subjects/604826" {
			return `{"id":604826,"name":"Cosmic Princess Kaguya","name_cn":"超时空辉夜姬！","summary":"s","total_episodes":1,"images":{"large":"https://example.com/c.jpg"}}`
		}
		return `{}`
	})
	result, err := NewLibraryService(bangumi, root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 1 || result.NewEpisodes != 1 {
		t.Fatalf("expected real ep1 created, got %#v", result)
	}
	if len(result.Unidentified) != 1 || result.Unidentified[0].FileName != movie || result.Unidentified[0].Reason != "同目录已有第 1 集" {
		t.Fatalf("no-ep file should be 同目录已有第 1 集, got %#v", result)
	}

	anime, err := database.GetAnimeByBangumiID(604826)
	if err != nil || anime == nil {
		t.Fatalf("expected created anime, err=%v anime=%v", err, anime)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 || episodes[0].EpNumber != 1 || episodes[0].FilePath != real {
		t.Fatalf("library ep1 must be the real episode file, got %#v", episodes)
	}
}

func TestLibraryJobKaguyaAliasHappyPathClearsUnidentified(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, kaguyaTriggerFile)); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)
	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FileName: kaguyaTriggerFile, Reason: "old", FilePath: "", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}

	job := NewLibraryJob(NewLibraryService(mockKaguyaAliasBangumi(nil, kaguyaSubjectAlias), root))
	if snap := job.Start(); snap.State != ScanJobRunning {
		t.Fatalf("Start state = %q, want running", snap.State)
	}
	done := waitJobLeftRunning(t, job)
	if done.State != ScanJobDone || done.Result == nil {
		t.Fatalf("job state = %q error=%q result=%#v, want done", done.State, done.Error, done.Result)
	}
	if done.Result.NewAnimes != 1 || done.Result.NewEpisodes != 1 {
		t.Fatalf("unexpected job result: %#v", done.Result)
	}

	items, _, err := database.ListUnidentified(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.FileName == kaguyaTriggerFile {
			t.Fatalf("ListUnidentified must not contain Kaguya after job, got %#v", items)
		}
	}
}

func TestLibraryScanBocchiHitsSubjectOnce(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, "[ANi] Bocchi the Rock! - 01 [1080p].mkv")); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	var subjectHits atomic.Int32
	bangumi := mockBangumiResponses(func(request *http.Request) string {
		if strings.HasPrefix(request.URL.Path, "/search/subject/") {
			return `{"list":[{"id":1001,"name":"Bocchi the Rock!","name_cn":"孤独摇滚！","summary":"summary","eps_count":12,"images":{"large":"https://example.com/cover.jpg"}}]}`
		}
		if strings.HasPrefix(request.URL.Path, "/v0/subjects/") {
			subjectHits.Add(1)
			return `{"id":1001,"name":"Bocchi the Rock!","name_cn":"孤独摇滚！","summary":"summary","total_episodes":12,"images":{"large":"https://example.com/cover.jpg"}}`
		}
		return `{}`
	})
	result, err := NewLibraryService(bangumi, root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 1 || result.NewEpisodes != 1 || len(result.Unidentified) != 0 {
		t.Fatalf("bocchi scan: %#v", result)
	}
	if got := subjectHits.Load(); got != 1 {
		t.Fatalf("/v0/subjects/ hits = %d, want 1", got)
	}
}

func TestLibraryScanOmitsKindFromJSON(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, "unknown.mkv")); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	result, err := NewLibraryService(mockBangumiService(), root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unidentified) != 1 {
		t.Fatalf("expected one unidentified, got %#v", result)
	}
	unidentifiedRaw, err := json.Marshal(result.Unidentified[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unidentifiedRaw), "kind") || strings.Contains(string(unidentifiedRaw), "Kind") {
		t.Fatalf("UnidentifiedFile JSON must not contain kind: %s", unidentifiedRaw)
	}

	parsedRaw, err := json.Marshal(ParseFilename(kaguyaTriggerFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(parsedRaw), "kind") || strings.Contains(string(parsedRaw), "Kind") {
		t.Fatalf("ParsedFilename JSON must not contain kind: %s", parsedRaw)
	}
}

func TestLibraryScanNoEpStrongTitleDoesNotCreateTV(t *testing.T) {
	files := []string{
		"[Fansub][Bocchi the Rock!][2022][1080p].mkv",
		"[Fansub][Bocchi the Rock!][1080p].mkv",
	}
	for _, name := range files {
		name := name
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := writeEmptyFile(filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
			setupLibraryDB(t)

			result, err := NewLibraryService(mockBangumiService(), root).Scan()
			if err != nil {
				t.Fatal(err)
			}
			if result.NewAnimes != 0 || result.NewEpisodes != 0 || len(result.Unidentified) != 1 {
				t.Fatalf("expected 0 animes and one unidentified, got %#v", result)
			}
			got := result.Unidentified[0]
			if got.FileName != name || got.Reason != "无法识别集数" {
				t.Fatalf("unexpected unidentified file: %#v", got)
			}
			anime, err := database.GetAnimeByBangumiID(1001)
			if err != nil {
				t.Fatal(err)
			}
			if anime != nil {
				t.Fatalf("must not create Bocchi from no-ep file, got %#v", anime)
			}
		})
	}
}

func TestLibraryScanSearchesYearFirst(t *testing.T) {
	root := t.TempDir()
	if err := writeEmptyFile(filepath.Join(root, kaguyaTriggerFile)); err != nil {
		t.Fatal(err)
	}
	setupLibraryDB(t)

	var keywords []string
	bangumi := mockBangumiResponses(func(request *http.Request) string {
		if strings.HasPrefix(request.URL.Path, "/search/subject/") {
			keyword := strings.TrimPrefix(request.URL.Path, "/search/subject/")
			keywords = append(keywords, keyword)
			if strings.Contains(keyword, "Cosmic Princess Kaguya") {
				return kaguyaSearchJP
			}
			return `{"list":[]}`
		}
		if strings.HasPrefix(request.URL.Path, "/v0/subjects/") {
			return kaguyaSubjectAlias
		}
		return `{}`
	})
	result, err := NewLibraryService(bangumi, root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if result.NewAnimes != 1 || result.NewEpisodes != 1 {
		t.Fatalf("year-first search should still accept Kaguya, got %#v", result)
	}
	if len(keywords) == 0 || keywords[0] != "Cosmic Princess Kaguya 2026" {
		t.Fatalf("first Search keyword = %#v, want Cosmic Princess Kaguya 2026 first", keywords)
	}
}
