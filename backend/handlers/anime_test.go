package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services"
	"fan-web/utils"
)

type scanResponse struct {
	Code int `json:"code"`
	Data struct {
		Scanned  int              `json:"scanned"`
		Episodes []models.Episode `json:"episodes"`
	} `json:"data"`
}

func setupScanHandler(t *testing.T, rootPath string) *AnimeHandler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "scan-test.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	if _, err := database.DB.Exec(
		"INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)", "scan-user", "x", 0,
	); err != nil {
		t.Fatal(err)
	}
	return NewAnimeHandler(services.NewBangumiService(), services.NewScannerService(rootPath))
}

func TestScanEmptyKeepsExistingEpisodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rootPath := t.TempDir()
	relDir := "anime-dir"
	if err := os.MkdirAll(filepath.Join(rootPath, relDir), 0o755); err != nil {
		t.Fatal(err)
	}

	handler := setupScanHandler(t, rootPath)

	anime, err := database.CreateAnime(&models.Anime{Title: "Scan Guardian", FilePath: relDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SyncEpisodes(anime.ID, []models.Episode{
		{EpNumber: 1, FilePath: "ep01.mp4"},
		{EpNumber: 2, FilePath: "ep02.mp4"},
	}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 {
		t.Fatalf("expected two episodes, got %d", len(episodes))
	}
	ep1ID := episodes[0].ID
	user, err := database.GetUserByUsername("scan-user")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProgress(user.ID, ep1ID, 99, false); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.POST("/api/animes/:id/scan", handler.Scan)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/animes/"+trimmedID(anime.ID)+"/scan", nil)
	router.ServeHTTP(recorder, request)

	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code == 0 {
		t.Fatalf("空扫描应返回非零业务码，got code=0 message=%q", response.Message)
	}
	if !strings.Contains(response.Message, "未扫描到有效视频") {
		t.Fatalf("期望空扫描保护提示，got %q", response.Message)
	}

	after, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].ID != ep1ID {
		t.Fatalf("原有剧集必须保留，got %#v", after)
	}
	progress, err := database.GetProgress(user.ID, ep1ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 99 {
		t.Fatalf("进度必须保留，got position=%d", progress.Position)
	}
}

func TestScanResponseStructure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rootPath := t.TempDir()
	// 构造第 1、2 集视频文件（位于 root 下的相对子目录 anime-dir）。
	relDir := "anime-dir"
	if err := os.MkdirAll(filepath.Join(rootPath, relDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ep01.mp4", "ep02.mp4"} {
		if err := os.WriteFile(filepath.Join(rootPath, relDir, name), []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	handler := setupScanHandler(t, rootPath)

	anime, err := database.CreateAnime(&models.Anime{Title: "Scan Normal", FilePath: relDir})
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.POST("/api/animes/:id/scan", handler.Scan)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/animes/"+trimmedID(anime.ID)+"/scan", nil)
	router.ServeHTTP(recorder, request)

	var response scanResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != utils.CodeSuccess {
		t.Fatalf("expected success, got code=%d body=%s", response.Code, recorder.Body.String())
	}
	if response.Data.Scanned != 2 {
		t.Fatalf("expected 2 scanned episodes, got %d", response.Data.Scanned)
	}
	if len(response.Data.Episodes) != 2 {
		t.Fatalf("expected 2 episodes in response, got %d", len(response.Data.Episodes))
	}
}

func trimmedID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func TestCreateAnimePathMismatchReturns1001(t *testing.T) {
	handler := setupCreateHandler(t)
	if _, err := database.CreateAnime(&models.Anime{Title: "Existing", BangumiID: 1001, FilePath: "a"}); err != nil {
		t.Fatal(err)
	}

	httpCode, code, message, _ := postCreateAnime(t, handler, 1001, "b")
	if httpCode != http.StatusOK || code != utils.CodeInvalidParams {
		t.Fatalf("POST {1001,b} against path=a: HTTP %d code %d, want 200/1001", httpCode, code)
	}
	if message != "番剧已存在但目录不同" {
		t.Fatalf("mismatch message = %q", message)
	}
}

func TestCreateAnimeSamePathReturnsExisting(t *testing.T) {
	handler := setupCreateHandler(t)
	existing, err := database.CreateAnime(&models.Anime{Title: "Existing", BangumiID: 1001, FilePath: "a"})
	if err != nil {
		t.Fatal(err)
	}

	httpCode, code, _, data := postCreateAnime(t, handler, 1001, "a")
	if httpCode != http.StatusOK || code != utils.CodeSuccess {
		t.Fatalf("POST {1001,a} against path=a: HTTP %d code %d, want 200/0", httpCode, code)
	}
	if data == nil || data.ID != existing.ID || data.FilePath != "a" {
		t.Fatalf("expected existing row, got %#v", data)
	}
}

func TestCreateAnimeEmptyPathWhenMissing(t *testing.T) {
	handler := setupCreateHandler(t)

	httpCode, code, _, data := postCreateAnime(t, handler, 1001, "")
	if httpCode != http.StatusOK || code != utils.CodeSuccess {
		t.Fatalf("POST {1001,\"\"} when missing: HTTP %d code %d, want 200/0", httpCode, code)
	}
	if data == nil || data.BangumiID != 1001 || data.FilePath != "" {
		t.Fatalf("expected newly created empty-path row, got %#v", data)
	}
}

func TestCreateAnimeEmptyPathWhenExistingEmpty(t *testing.T) {
	handler := setupCreateHandler(t)
	existing, err := database.CreateAnime(&models.Anime{Title: "Existing", BangumiID: 1001, FilePath: ""})
	if err != nil {
		t.Fatal(err)
	}

	httpCode, code, _, data := postCreateAnime(t, handler, 1001, "")
	if httpCode != http.StatusOK || code != utils.CodeSuccess {
		t.Fatalf("POST {1001,\"\"} against empty path: HTTP %d code %d, want 200/0", httpCode, code)
	}
	if data == nil || data.ID != existing.ID {
		t.Fatalf("expected existing empty-path row, got %#v", data)
	}
}

func TestCreateAnimeEmptyPathWhenExistingOther(t *testing.T) {
	handler := setupCreateHandler(t)
	if _, err := database.CreateAnime(&models.Anime{Title: "Existing", BangumiID: 1001, FilePath: "other"}); err != nil {
		t.Fatal(err)
	}

	httpCode, code, message, _ := postCreateAnime(t, handler, 1001, "")
	if httpCode != http.StatusOK || code != utils.CodeInvalidParams {
		t.Fatalf("POST {1001,\"\"} against path=other: HTTP %d code %d, want 200/1001", httpCode, code)
	}
	if message != "番剧已存在但目录不同" {
		t.Fatalf("empty-vs-other message = %q", message)
	}
}

func setupCreateHandler(t *testing.T) *AnimeHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.Init(filepath.Join(t.TempDir(), "create-test.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	return NewAnimeHandler(mockBangumiForCreate(t), services.NewScannerService(t.TempDir()))
}

func mockBangumiForCreate(t *testing.T) *services.BangumiService {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = createBangumiRoundTrip{}
	t.Cleanup(func() { http.DefaultTransport = original })
	return services.NewBangumiService()
}

type createBangumiRoundTrip struct{}

func (createBangumiRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	body := `{}`
	if strings.HasPrefix(request.URL.Path, "/v0/subjects/") {
		id, _ := strconv.Atoi(strings.TrimPrefix(request.URL.Path, "/v0/subjects/"))
		body = fmt.Sprintf(`{"id":%d,"name":"Demo Show","name_cn":"演示","summary":"summary","total_episodes":12,"images":{"large":"https://lain.bgm.tv/cover.jpg"}}`, id)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func postCreateAnime(t *testing.T, handler *AnimeHandler, bangumiID int, filePath string) (httpCode, code int, message string, anime *models.Anime) {
	t.Helper()
	router := gin.New()
	router.POST("/api/animes", handler.Create)
	payload, err := json.Marshal(map[string]interface{}{"bangumi_id": bangumiID, "file_path": filePath})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/animes", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Code    int           `json:"code"`
		Message string        `json:"message"`
		Data    *models.Anime `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return recorder.Code, response.Code, response.Message, response.Data
}

func setupRebindHandler(t *testing.T) *AnimeHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.Init(filepath.Join(t.TempDir(), "rebind-test.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	return NewAnimeHandler(mockBangumiForCreate(t), services.NewScannerService(t.TempDir()))
}

func setupRebindHandlerFailingBangumi(t *testing.T) *AnimeHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.Init(filepath.Join(t.TempDir(), "rebind-fail-test.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	original := http.DefaultTransport
	http.DefaultTransport = failBangumiRoundTrip{}
	t.Cleanup(func() { http.DefaultTransport = original })
	return NewAnimeHandler(services.NewBangumiService(), services.NewScannerService(t.TempDir()))
}

type failBangumiRoundTrip struct{}

func (failBangumiRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"title":"error"}`)),
		Request:    request,
	}, nil
}

func postRebind(t *testing.T, handler *AnimeHandler, animeID int64, bangumiID int) (httpCode, code int, message string, anime *models.Anime) {
	t.Helper()
	router := gin.New()
	router.POST("/api/animes/:id/rebind", handler.Rebind)
	payload, err := json.Marshal(map[string]interface{}{"bangumi_id": bangumiID})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/animes/"+trimmedID(animeID)+"/rebind", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Code    int           `json:"code"`
		Message string        `json:"message"`
		Data    *models.Anime `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return recorder.Code, response.Code, response.Message, response.Data
}

func TestRebindUpdatesMetadataKeepsFilePathAndEpisodes(t *testing.T) {
	handler := setupRebindHandler(t)
	anime, err := database.CreateAnime(&models.Anime{
		Title: "Old", TitleCn: "旧", BangumiID: 100, Cover: "old.jpg",
		Summary: "old-sum", EpCount: 3, FilePath: "keep-dir",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SyncEpisodes(anime.ID, []models.Episode{
		{EpNumber: 1, Title: "E1", FilePath: "ep01.mp4"},
		{EpNumber: 2, Title: "E2", FilePath: "ep02.mp4"},
	}); err != nil {
		t.Fatal(err)
	}
	beforeEps, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeEps) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(beforeEps))
	}

	httpCode, code, _, data := postRebind(t, handler, anime.ID, 200)
	if httpCode != http.StatusOK || code != utils.CodeSuccess {
		t.Fatalf("rebind success: HTTP %d code %d, want 200/0", httpCode, code)
	}
	if data == nil || data.BangumiID != 200 || data.Title != "Demo Show" || data.TitleCn != "演示" {
		t.Fatalf("expected rebound metadata, got %#v", data)
	}
	if data.Cover != "https://lain.bgm.tv/cover.jpg" || data.Summary != "summary" || data.EpCount != 12 {
		t.Fatalf("expected GetSubject fields, got %#v", data)
	}
	if data.FilePath != "keep-dir" {
		t.Fatalf("file_path 必须不变，got %q", data.FilePath)
	}

	afterEps, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterEps) != 2 || afterEps[0].ID != beforeEps[0].ID || afterEps[1].ID != beforeEps[1].ID {
		t.Fatalf("剧集必须不变，got %#v want %#v", afterEps, beforeEps)
	}
	if afterEps[0].FilePath != "ep01.mp4" || afterEps[1].FilePath != "ep02.mp4" {
		t.Fatalf("剧集路径必须不变，got %#v", afterEps)
	}
}

func TestRebindBoundBangumiReturns1001(t *testing.T) {
	handler := setupRebindHandler(t)
	if _, err := database.CreateAnime(&models.Anime{Title: "Bound", BangumiID: 100, FilePath: "a"}); err != nil {
		t.Fatal(err)
	}
	target, err := database.CreateAnime(&models.Anime{Title: "Target", BangumiID: 200, FilePath: "b"})
	if err != nil {
		t.Fatal(err)
	}

	httpCode, code, message, _ := postRebind(t, handler, target.ID, 100)
	if httpCode != http.StatusOK || code != utils.CodeInvalidParams {
		t.Fatalf("bound rebind: HTTP %d code %d, want 200/1001", httpCode, code)
	}
	if message != "该 Bangumi 条目已绑定其他番剧" {
		t.Fatalf("bound message = %q", message)
	}
	unchanged, err := database.GetAnimeByID(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.BangumiID != 200 || unchanged.FilePath != "b" || unchanged.Title != "Target" {
		t.Fatalf("撞车后不得改写，got %#v", unchanged)
	}
}

func TestRebindZeroBangumiIDReturns1001(t *testing.T) {
	handler := setupRebindHandler(t)
	anime, err := database.CreateAnime(&models.Anime{Title: "X", BangumiID: 100, FilePath: "d"})
	if err != nil {
		t.Fatal(err)
	}

	httpCode, code, _, _ := postRebind(t, handler, anime.ID, 0)
	if httpCode != http.StatusOK || code != utils.CodeInvalidParams {
		t.Fatalf("bangumi_id=0: HTTP %d code %d, want 200/1001", httpCode, code)
	}
	unchanged, err := database.GetAnimeByID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.BangumiID != 100 {
		t.Fatalf("bangumi_id=0 不得改写，got %#v", unchanged)
	}
}

func TestRebindMissingAnimeReturns1002(t *testing.T) {
	handler := setupRebindHandler(t)
	httpCode, code, message, _ := postRebind(t, handler, 99999, 200)
	if httpCode != http.StatusOK || code != utils.CodeNotFound {
		t.Fatalf("missing anime: HTTP %d code %d, want 200/1002", httpCode, code)
	}
	if message != "番剧不存在" {
		t.Fatalf("missing message = %q", message)
	}
}

func TestRebindGetSubjectFailureReturns9999(t *testing.T) {
	handler := setupRebindHandlerFailingBangumi(t)
	anime, err := database.CreateAnime(&models.Anime{Title: "X", BangumiID: 100, FilePath: "d"})
	if err != nil {
		t.Fatal(err)
	}

	httpCode, code, _, _ := postRebind(t, handler, anime.ID, 200)
	if httpCode != http.StatusOK || code != utils.CodeInternal {
		t.Fatalf("GetSubject fail: HTTP %d code %d, want 200/9999", httpCode, code)
	}
}

func TestUpdateAnimeDoesNotChangeBangumiID(t *testing.T) {
	handler := setupRebindHandler(t)
	anime, err := database.CreateAnime(&models.Anime{
		Title: "Keep", BangumiID: 100, FilePath: "dir", EpCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.PUT("/api/animes/:id", handler.Update)
	payload, err := json.Marshal(map[string]interface{}{
		"title": "Keep", "title_cn": "中", "summary": "s",
		"ep_count": 5, "file_path": "dir", "bangumi_id": 999,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/animes/"+trimmedID(anime.ID), bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	if recorder.Code != http.StatusOK || response.Code != utils.CodeSuccess {
		t.Fatalf("PUT: HTTP %d code %d, want 200/0 body=%s", recorder.Code, response.Code, recorder.Body.String())
	}

	after, err := database.GetAnimeByID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.BangumiID != 100 {
		t.Fatalf("PUT 不得改 bangumi_id，got %d", after.BangumiID)
	}
	if after.EpCount != 5 || after.TitleCn != "中" || after.Summary != "s" {
		t.Fatalf("PUT 应更新其它字段，got %#v", after)
	}
}
