package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/models"
	"fan-web/utils"
)

func TestAnimeScanSuccessDeletesUnidentifiedForDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rootPath := t.TempDir()
	relDir := "anime-dir"
	otherDir := "other-dir"
	if err := os.MkdirAll(filepath.Join(rootPath, relDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, relDir, "ep01.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := setupScanHandler(t, rootPath)
	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: relDir, FileName: "ep01.mp4", Reason: "待确认", Candidates: []models.MatchCandidate{}},
		{FilePath: otherDir, FileName: "keep.mkv", Reason: "其它目录", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{Title: "Scan Inbox", FilePath: relDir})
	if err != nil {
		t.Fatal(err)
	}

	code, _ := postAnimeScan(t, handler, anime.ID)
	if code != utils.CodeSuccess {
		t.Fatalf("有剧集 Scan 应成功, got code=%d", code)
	}

	items, total, err := database.ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].FilePath != otherDir {
		t.Fatalf("有剧集 Scan 成功后该 file_path 未识别应消失, got total=%d items=%#v", total, items)
	}
}

func TestAnimeScanZeroEpisodeSuccessKeepsUnidentified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rootPath := t.TempDir()
	relDir := "empty-dir"
	if err := os.MkdirAll(filepath.Join(rootPath, relDir), 0o755); err != nil {
		t.Fatal(err)
	}

	handler := setupScanHandler(t, rootPath)
	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: relDir, FileName: "ghost.mkv", Reason: "待确认", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{Title: "Empty Scan", FilePath: relDir})
	if err != nil {
		t.Fatal(err)
	}

	code, _ := postAnimeScan(t, handler, anime.ID)
	if code != utils.CodeSuccess {
		t.Fatalf("0 集 Success 应返回 0 码, got %d", code)
	}

	_, total, err := database.ListUnidentified(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("0 集 Success 不删未识别, got total=%d", total)
	}
}

func TestAnimeScanDeleteUnidentifiedFailureStillSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rootPath := t.TempDir()
	relDir := "anime-dir"
	if err := os.MkdirAll(filepath.Join(rootPath, relDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, relDir, "ep01.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := setupScanHandler(t, rootPath)
	if err := database.ReplaceUnidentified([]models.UnidentifiedFile{
		{FilePath: relDir, FileName: "ep01.mp4", Reason: "待确认", Candidates: []models.MatchCandidate{}},
	}); err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{Title: "Delete Fail", FilePath: relDir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("DROP TABLE unidentified_files"); err != nil {
		t.Fatal(err)
	}

	code, body := postAnimeScan(t, handler, anime.ID)
	if code != utils.CodeSuccess {
		t.Fatalf("Delete 失败仍应 0 码, got %d body=%s", code, body)
	}
}

func postAnimeScan(t *testing.T, handler *AnimeHandler, animeID int64) (int, string) {
	t.Helper()
	router := gin.New()
	router.POST("/api/animes/:id/scan", handler.Scan)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/animes/"+trimmedID(animeID)+"/scan", nil)
	router.ServeHTTP(recorder, request)
	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return response.Code, recorder.Body.String()
}
