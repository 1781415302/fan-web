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
	"fan-web/services"
)

func TestStreamRequiresTokenAndSupportsRange(t *testing.T) {
	rootPath := t.TempDir()
	videoPath := filepath.Join(rootPath, "episode.mp4")
	videoData := []byte("0123456789")
	if err := os.WriteFile(videoPath, videoData, 0o644); err != nil {
		t.Fatal(err)
	}

	databasePath := filepath.Join(t.TempDir(), "stream-test.db")
	if err := database.Init(databasePath); err != nil {
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
	anime, err := database.CreateAnime(&models.Anime{Title: "Stream Anime"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "episode.mp4"}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected one episode, got %d", len(episodes))
	}

	auth := services.NewAuthService("stream-test-secret", 24*60*60*1e9)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewEpisodeHandler(auth, services.NewScannerService(rootPath))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/episodes/:id/stream", handler.Stream)

	unauthenticated := httptest.NewRecorder()
	unauthenticatedRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/1/stream", nil)
	router.ServeHTTP(unauthenticated, unauthenticatedRequest)
	if unauthenticated.Code != http.StatusOK {
		t.Fatalf("expected application error with HTTP 200, got %d", unauthenticated.Code)
	}
	var errorResponse struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(unauthenticated.Body.Bytes(), &errorResponse); err != nil {
		t.Fatal(err)
	}
	if errorResponse.Code != 2001 {
		t.Fatalf("expected unauthenticated code 2001, got %d", errorResponse.Code)
	}

	rangeRecorder := httptest.NewRecorder()
	rangeRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/1/stream?token="+token, nil)
	rangeRequest.Header.Set("Range", "bytes=2-5")
	router.ServeHTTP(rangeRecorder, rangeRequest)
	if rangeRecorder.Code != http.StatusPartialContent {
		t.Fatalf("expected HTTP 206 for range request, got %d", rangeRecorder.Code)
	}
	if got := rangeRecorder.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("unexpected Content-Range: %q", got)
	}
	if got := rangeRecorder.Body.String(); got != "2345" {
		t.Fatalf("unexpected range body: %q", got)
	}
}
