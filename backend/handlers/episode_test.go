package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	if err := database.SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "episode.mp4"}}); err != nil {
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
	rangeRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/1/stream", nil)
	rangeRequest.Header.Set("Authorization", "Bearer "+token)
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

	subtitleURL := "/api/episodes/" + strconv.FormatInt(episodes[0].ID, 10) + "/subtitles"
	subtitleRecorder := httptest.NewRecorder()
	subtitleRequest := httptest.NewRequest(http.MethodGet, subtitleURL, nil)
	subtitleRequest.Header.Set("Authorization", "Bearer "+token)
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
	missingTrackRequest := httptest.NewRequest(http.MethodGet, subtitleURL+"?track=1", nil)
	missingTrackRequest.Header.Set("Authorization", "Bearer "+token)
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
	if err := database.SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "e01.mp4"}}); err != nil {
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

func TestIssueMediaTokenAndStreamWithMediaToken(t *testing.T) {
	rootPath := t.TempDir()
	videoData := []byte("0123456789")
	if err := os.WriteFile(filepath.Join(rootPath, "ep01.mp4"), videoData, 0o644); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "media-token.db")
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
	admin, err := database.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{Title: "Media Anime", FilePath: "."})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "ep01.mp4"}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	epID := episodes[0].ID

	auth := services.NewAuthService("media-token-test-secret", 24*60*60*1e9)
	handler := NewEpisodeHandler(auth, services.NewScannerService(rootPath))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("/api")
	protected.Use(middleware.JWTAuth(auth))
	protected.POST("/episodes/:id/media-token", handler.IssueMediaToken)
	router.GET("/api/episodes/:id/stream", handler.Stream)
	router.GET("/api/episodes/:id/subtitles", handler.Subtitles)

	loginToken, _, err := auth.IssueToken(*admin)
	if err != nil {
		t.Fatal(err)
	}

	// 用登录 JWT 请求媒体票据。
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/media-token", nil)
	request.Header.Set("Authorization", "Bearer "+loginToken)
	router.ServeHTTP(recorder, request)
	var tokenResp struct {
		Code int `json:"code"`
		Data struct {
			Token     string `json:"token"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &tokenResp); err != nil {
		t.Fatal(err)
	}
	if tokenResp.Code != 0 || tokenResp.Data.Token == "" || tokenResp.Data.ExpiresAt == "" {
		t.Fatalf("unexpected media-token response: %s", recorder.Body.String())
	}

	// 用媒体票据请求视频流（Range 206）。
	streamRecorder := httptest.NewRecorder()
	streamRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/stream?media_token="+url.QueryEscape(tokenResp.Data.Token), nil)
	streamRequest.Header.Set("Range", "bytes=2-5")
	router.ServeHTTP(streamRecorder, streamRequest)
	if streamRecorder.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 with media token, got %d body=%s", streamRecorder.Code, streamRecorder.Body.String())
	}
	if got := streamRecorder.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("unexpected Content-Range %q", got)
	}

	// 同一媒体票据也能访问当前 episode 的字幕列表。
	subtitleRecorder := httptest.NewRecorder()
	subtitleRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/subtitles?media_token="+url.QueryEscape(tokenResp.Data.Token), nil)
	router.ServeHTTP(subtitleRecorder, subtitleRequest)
	var subtitleResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(subtitleRecorder.Body.Bytes(), &subtitleResp); err != nil {
		t.Fatal(err)
	}
	if subtitleResp.Code != 0 {
		t.Fatalf("media token must access subtitle list, got %s", subtitleRecorder.Body.String())
	}

	// Bearer 优先：无效 Bearer 不能降级尝试同时提供的有效媒体票据。
	invalidBearerRecorder := httptest.NewRecorder()
	invalidBearerRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/stream?media_token="+url.QueryEscape(tokenResp.Data.Token), nil)
	invalidBearerRequest.Header.Set("Authorization", "Bearer invalid-login-token")
	router.ServeHTTP(invalidBearerRecorder, invalidBearerRequest)
	var invalidBearerResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(invalidBearerRecorder.Body.Bytes(), &invalidBearerResp); err != nil {
		t.Fatal(err)
	}
	if invalidBearerResp.Code != 2001 {
		t.Fatalf("invalid Bearer must not fall back to media token, got %d", invalidBearerResp.Code)
	}

	// 有效 Bearer 同样优先于无效 query 凭证。
	validBearerRecorder := httptest.NewRecorder()
	validBearerRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/stream?media_token=invalid&token=invalid", nil)
	validBearerRequest.Header.Set("Authorization", "Bearer "+loginToken)
	router.ServeHTTP(validBearerRecorder, validBearerRequest)
	if validBearerRecorder.Code != http.StatusOK {
		t.Fatalf("valid Bearer must take priority, got %d body=%s", validBearerRecorder.Code, validBearerRecorder.Body.String())
	}

	// A 集票据不能访问 B 集（构造不同 episode 的票据）。
	otherToken, _, err := auth.IssueMediaToken(admin.ID, 99999)
	if err != nil {
		t.Fatal(err)
	}
	wrongRecorder := httptest.NewRecorder()
	wrongRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/stream?media_token="+url.QueryEscape(otherToken), nil)
	router.ServeHTTP(wrongRecorder, wrongRequest)
	var wrongResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(wrongRecorder.Body.Bytes(), &wrongResp); err != nil {
		t.Fatal(err)
	}
	if wrongResp.Code != 2001 {
		t.Fatalf("wrong-episode media token must return 2001, got %d", wrongResp.Code)
	}

	// 删除用户后媒体票据失效。
	user2, _ := database.CreateUser("media-user", "password", false)
	userToken, _, err := auth.IssueMediaToken(user2.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteUser(user2.ID); err != nil {
		t.Fatal(err)
	}
	deletedRecorder := httptest.NewRecorder()
	deletedRequest := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/stream?media_token="+url.QueryEscape(userToken), nil)
	router.ServeHTTP(deletedRecorder, deletedRequest)
	var deletedResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(deletedRecorder.Body.Bytes(), &deletedResp); err != nil {
		t.Fatal(err)
	}
	if deletedResp.Code != 2001 {
		t.Fatalf("deleted-user media token must return 2001, got %d", deletedResp.Code)
	}
}

func TestLegacyTokenQueryRejected(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "ep01.mp4"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "legacy-token.db")
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
	admin, err := database.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{Title: "Legacy Anime", FilePath: "."})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "ep01.mp4"}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	epID := episodes[0].ID

	auth := services.NewAuthService("legacy-token-secret", 24*60*60*1e9)
	handler := NewEpisodeHandler(auth, services.NewScannerService(rootPath))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/episodes/:id/stream", handler.Stream)

	loginToken, _, err := auth.IssueToken(*admin)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/stream?token="+url.QueryEscape(loginToken), nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for legacy token query, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 2001 {
		t.Fatalf("expected code 2001 for ?token= only, got %d body=%s", resp.Code, recorder.Body.String())
	}
	if resp.Message != "未登录" {
		t.Fatalf("expected message 未登录 for ?token= only, got %q", resp.Message)
	}
}
