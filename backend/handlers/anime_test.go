package handlers

import (
	"encoding/json"
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
