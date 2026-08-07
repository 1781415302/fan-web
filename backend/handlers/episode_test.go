package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
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
	router.GET("/api/episodes/:id/subtitles", handler.Subtitles)

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

	subtitleUnauthenticated := httptest.NewRecorder()
	subtitleUnauthenticatedRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(episodes[0].ID, 10)+"/subtitles", nil)
	router.ServeHTTP(subtitleUnauthenticated, subtitleUnauthenticatedRequest)
	if subtitleUnauthenticated.Code != http.StatusOK {
		t.Fatalf("expected subtitle application error with HTTP 200, got %d", subtitleUnauthenticated.Code)
	}
	var subtitleErrorResponse struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(subtitleUnauthenticated.Body.Bytes(), &subtitleErrorResponse); err != nil {
		t.Fatal(err)
	}
	if subtitleErrorResponse.Code != 2001 {
		t.Fatalf("expected subtitle unauthenticated code 2001, got %d", subtitleErrorResponse.Code)
	}

	subtitleURL := "/api/episodes/" + strconv.FormatInt(episodes[0].ID, 10) + "/subtitles?token=" + token
	subtitleRecorder := httptest.NewRecorder()
	subtitleRequest := httptest.NewRequest(http.MethodGet, subtitleURL, nil)
	router.ServeHTTP(subtitleRecorder, subtitleRequest)
	if subtitleRecorder.Code != http.StatusOK {
		t.Fatalf("expected empty subtitle list with HTTP 200, got %d", subtitleRecorder.Code)
	}
	var subtitleResponse struct {
		Code int                      `json:"code"`
		Data []services.SubtitleTrack `json:"data"`
	}
	if err := json.Unmarshal(subtitleRecorder.Body.Bytes(), &subtitleResponse); err != nil {
		t.Fatal(err)
	}
	if subtitleResponse.Code != 0 || len(subtitleResponse.Data) != 0 {
		t.Fatalf("expected no subtitles for MP4, got code=%d data=%#v", subtitleResponse.Code, subtitleResponse.Data)
	}

	missingTrackRecorder := httptest.NewRecorder()
	missingTrackRequest := httptest.NewRequest(http.MethodGet, subtitleURL+"&track=1", nil)
	router.ServeHTTP(missingTrackRecorder, missingTrackRequest)
	if missingTrackRecorder.Code != http.StatusOK {
		t.Fatalf("expected missing subtitle track application error with HTTP 200, got %d", missingTrackRecorder.Code)
	}
	var missingTrackResponse struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(missingTrackRecorder.Body.Bytes(), &missingTrackResponse); err != nil {
		t.Fatal(err)
	}
	if missingTrackResponse.Code != 1002 {
		t.Fatalf("expected missing subtitle track code 1002, got %d", missingTrackResponse.Code)
	}
}

func TestReportProgressWatchedIrreversible(t *testing.T) {
	rootPath := t.TempDir()
	databasePath := filepath.Join(t.TempDir(), "progress-irr-test.db")
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
	anime, err := database.CreateAnime(&models.Anime{Title: "Irr Anime"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "e01.mp4"}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	epID := episodes[0].ID

	auth := services.NewAuthService("irr-test-secret", 24*60*60*1e9)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewEpisodeHandler(auth, services.NewScannerService(rootPath))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/progress/:episode_id", middleware.JWTAuth(auth), handler.ReportProgress)
	router.GET("/api/progress/:episode_id", middleware.JWTAuth(auth), handler.GetProgress)

	// 先上报已看
	reportURL := "/api/progress/" + strconv.FormatInt(epID, 10)
	watchedBody, _ := json.Marshal(map[string]any{"position": 590, "watched": true})
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodPost, reportURL, bytes.NewReader(watchedBody))
	r1.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("report watched failed: %d %s", w1.Code, w1.Body.String())
	}

	// 再上报未看
	unwatchedBody, _ := json.Marshal(map[string]any{"position": 10, "watched": false})
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, reportURL, bytes.NewReader(unwatchedBody))
	r2.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("report unwatched failed: %d %s", w2.Code, w2.Body.String())
	}

	// 查询接口应返回 watched=true
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest(http.MethodGet, reportURL, nil)
	r3.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w3, r3)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Watched  bool `json:"watched"`
			Position int  `json:"position"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w3.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
	if !resp.Data.Watched {
		t.Fatalf("watched should be irreversible: expected true after reporting false, got false")
	}
	if resp.Data.Position != 10 {
		t.Fatalf("position should update to 10, got %d", resp.Data.Position)
	}
}
