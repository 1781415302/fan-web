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
	"strings"
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

func TestContinueWatchingAPI(t *testing.T) {
	rootPath := t.TempDir()
	databasePath := filepath.Join(t.TempDir(), "continue-api.db")
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

	mustAnime := func(title string) (int64, []models.Episode) {
		t.Helper()
		anime, err := database.CreateAnime(&models.Anime{Title: title, EpCount: 2})
		if err != nil {
			t.Fatal(err)
		}
		if err := database.SyncEpisodes(anime.ID, []models.Episode{
			{EpNumber: 1, FilePath: title + "/01.mp4"},
			{EpNumber: 2, FilePath: title + "/02.mp4"},
		}); err != nil {
			t.Fatal(err)
		}
		eps, err := database.ListEpisodesByAnimeID(anime.ID)
		if err != nil {
			t.Fatal(err)
		}
		return anime.ID, eps
	}
	oldID, oldEps := mustAnime("Old")
	newID, newEps := mustAnime("New")
	doneID, doneEps := mustAnime("Done")

	if err := database.UpsertProgress(user.ID, oldEps[1].ID, 30, false); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProgress(user.ID, newEps[0].ID, 10, false); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProgress(user.ID, doneEps[0].ID, 100, true); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProgress(user.ID, doneEps[1].ID, 100, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`UPDATE watch_progress SET updated_at = ? WHERE episode_id = ?`, "2026-01-01 00:00:00", oldEps[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`UPDATE watch_progress SET updated_at = ? WHERE episode_id = ?`, "2026-03-01 00:00:00", newEps[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`UPDATE watch_progress SET updated_at = ? WHERE episode_id IN (?, ?)`, "2026-02-01 00:00:00", doneEps[0].ID, doneEps[1].ID); err != nil {
		t.Fatal(err)
	}

	auth := services.NewAuthService("continue-test-secret", 24*60*60*1e9)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewEpisodeHandler(auth, services.NewScannerService(rootPath))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// continue 必须写在 /progress/:episode_id 之前，否则 "continue" 会被当成 episode_id。
	router.GET("/api/progress/continue", middleware.JWTAuth(auth), handler.Continue)
	router.GET("/api/progress/:episode_id", middleware.JWTAuth(auth), handler.GetProgress)

	getContinue := func(limit string, withAuth bool) (int, struct {
		Code int `json:"code"`
		Data struct {
			Items []models.ContinueItem `json:"items"`
		} `json:"data"`
	}) {
		t.Helper()
		path := "/api/progress/continue"
		if limit != "" {
			path += "?limit=" + limit
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if withAuth {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d body=%s", recorder.Code, recorder.Body.String())
		}
		var resp struct {
			Code int `json:"code"`
			Data struct {
				Items []models.ContinueItem `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return recorder.Code, resp
	}

	_, unauth := getContinue("", false)
	if unauth.Code != 2001 {
		t.Fatalf("expected unauthenticated code 2001, got %d", unauth.Code)
	}

	_, listed := getContinue("", true)
	if listed.Code != 0 {
		t.Fatalf("expected code 0, got %d", listed.Code)
	}
	if len(listed.Data.Items) != 2 {
		t.Fatalf("expected 2 in-progress animes, got %d items=%#v", len(listed.Data.Items), listed.Data.Items)
	}
	if listed.Data.Items[0].Anime.ID != newID || listed.Data.Items[1].Anime.ID != oldID {
		t.Fatalf("should order by recent updated_at: got %d,%d want new=%d old=%d (done=%d)",
			listed.Data.Items[0].Anime.ID, listed.Data.Items[1].Anime.ID, newID, oldID, doneID)
	}
	for _, item := range listed.Data.Items {
		if item.Anime.ID == doneID {
			t.Fatalf("fully watched anime must not appear: %#v", item)
		}
	}
	if listed.Data.Items[0].Episode.ID != newEps[0].ID || listed.Data.Items[0].Position != 10 || listed.Data.Items[0].Watched {
		t.Fatalf("New should continue ep1 pos=10: %#v", listed.Data.Items[0])
	}
	if listed.Data.Items[1].Episode.ID != oldEps[1].ID || listed.Data.Items[1].Position != 30 || listed.Data.Items[1].Watched {
		t.Fatalf("Old should continue ep2 pos=30: %#v", listed.Data.Items[1])
	}

	_, limited := getContinue("1", true)
	if limited.Code != 0 || len(limited.Data.Items) != 1 || limited.Data.Items[0].Anime.ID != newID {
		t.Fatalf("limit=1 should keep the most recent item, got %#v", limited.Data.Items)
	}

	_, zero := getContinue("0", true)
	if zero.Code != 0 || len(zero.Data.Items) != 2 {
		t.Fatalf("limit=0 should default to 20, got %d items", len(zero.Data.Items))
	}

	_, clamped := getContinue("100", true)
	if clamped.Code != 0 || len(clamped.Data.Items) != 2 {
		t.Fatalf("limit=100 should clamp to 50 and return 2 items, got %d", len(clamped.Data.Items))
	}
}

// newDownloadHarness 为下载接口用例搭建最小内联环境：真实临时视频（.mp4 命中 videoExts）+ 独立 sqlite + 登录 Bearer。
func newDownloadHarness(t *testing.T, fileName string, videoData []byte) (*services.AuthService, string, int64, *gin.Engine) {
	t.Helper()
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, fileName), videoData, 0o644); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "download-test.db")
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
	anime, err := database.CreateAnime(&models.Anime{Title: "Download Anime", FilePath: "."})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: fileName}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected one episode, got %d", len(episodes))
	}
	auth := services.NewAuthService("download-test-secret", 24*60*60*1e9)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewEpisodeHandler(auth, services.NewScannerService(rootPath))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/episodes/:id/download", handler.Download)
	return auth, token, episodes[0].ID, router
}

func downloadWithAuth(router *gin.Engine, epID int64, token string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/download", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

// 1) 无凭证 → HTTP200 + code 2001。
func TestDownloadRequiresAuth(t *testing.T) {
	_, _, epID, router := newDownloadHarness(t, "dl01.mp4", []byte("0123456789"))
	recorder := downloadWithAuth(router, epID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected application error with HTTP 200, got %d", recorder.Code)
	}
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 2001 {
		t.Fatalf("expected unauthenticated code 2001, got %d", resp.Code)
	}
}

// 2) 有效 Bearer + 真实临时视频 → Disposition 前缀 attachment; + octet-stream + body 相等。
func TestDownloadWithBearer(t *testing.T) {
	videoData := []byte("0123456789")
	_, token, epID, router := newDownloadHarness(t, "dl02.mp4", videoData)
	recorder := downloadWithAuth(router, epID, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("unexpected Content-Disposition: %q", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("unexpected Content-Type: %q", got)
	}
	if !bytes.Equal(recorder.Body.Bytes(), videoData) {
		t.Fatalf("unexpected body: %q", recorder.Body.String())
	}
}

// 3) Range bytes=2-5 → 206 + Content-Range + 4 字节。
func TestDownloadRange(t *testing.T) {
	_, token, epID, router := newDownloadHarness(t, "dl03.mp4", []byte("0123456789"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/download", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Range", "bytes=2-5")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("expected HTTP 206 for range request, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("unexpected Content-Range: %q", got)
	}
	if got := recorder.Body.String(); got != "2345" {
		t.Fatalf("unexpected range body: %q", got)
	}
}

// 4) 有效 media_token → 同 2（QueryEscape 后传递）。
func TestDownloadWithMediaToken(t *testing.T) {
	videoData := []byte("0123456789")
	auth, _, epID, router := newDownloadHarness(t, "dl04.mp4", videoData)
	admin, err := database.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	mediaToken, _, err := auth.IssueMediaToken(admin.ID, epID)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/download?media_token="+url.QueryEscape(mediaToken), nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 with media token, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("unexpected Content-Disposition: %q", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("unexpected Content-Type: %q", got)
	}
	if !bytes.Equal(recorder.Body.Bytes(), videoData) {
		t.Fatalf("unexpected body: %q", recorder.Body.String())
	}
}

// 5) 错配票据（他集票据访问本集）→ 2001。
func TestDownloadMismatchedMediaToken(t *testing.T) {
	auth, _, epID, router := newDownloadHarness(t, "dl05.mp4", []byte("0123456789"))
	admin, err := database.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := auth.IssueMediaToken(admin.ID, 99999)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/episodes/"+strconv.FormatInt(epID, 10)+"/download?media_token="+url.QueryEscape(otherToken), nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected application error with HTTP 200, got %d", recorder.Code)
	}
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 2001 {
		t.Fatalf("wrong-episode media token must return 2001, got %d", resp.Code)
	}
}

// 6) 集数不存在 → 1002。
func TestDownloadEpisodeNotFound(t *testing.T) {
	_, token, _, router := newDownloadHarness(t, "dl06.mp4", []byte("0123456789"))
	recorder := downloadWithAuth(router, 99999, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected application error with HTTP 200, got %d", recorder.Code)
	}
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 1002 {
		t.Fatalf("expected not-found code 1002, got %d", resp.Code)
	}
}

// 7) 文件名两分支 + 无 CR/LF：ASCII 走 filename=，中文/全角冒号走 filename*=，控制字符被替换。
func TestDownloadAttachmentDisposition(t *testing.T) {
	assertASCII := func(out string) {
		t.Helper()
		if strings.ContainsAny(out, "\r\n") {
			t.Fatalf("disposition must not contain CR/LF: %q", out)
		}
		for i := 0; i < len(out); i++ {
			if out[i] > 127 {
				t.Fatalf("disposition must be pure ASCII: %q", out)
			}
		}
	}
	ascii := buildAttachmentDisposition("episode[01].mp4")
	if !strings.HasPrefix(ascii, "attachment;") || !strings.Contains(ascii, "filename=") {
		t.Fatalf("unexpected ASCII disposition: %q", ascii)
	}
	assertASCII(ascii)

	nonASCII := buildAttachmentDisposition("第01集：中文.mp4")
	if !strings.HasPrefix(nonASCII, "attachment;") || !strings.Contains(nonASCII, "filename*=") || !strings.Contains(nonASCII, "utf-8''") {
		t.Fatalf("non-ASCII disposition must carry filename*=: %q", nonASCII)
	}
	assertASCII(nonASCII)

	sanitized := buildAttachmentDisposition("a\"b\\c\x01d\x7Fe.mp4")
	assertASCII(sanitized)
	for _, bad := range []string{"%22", "%5C", "%0A", "%00"} {
		if strings.Contains(sanitized, bad) {
			t.Fatalf("sanitized disposition must not encode replaced chars, got %q", sanitized)
		}
	}
	if !strings.Contains(sanitized, "a_b_c_d_e.mp4") {
		t.Fatalf("control/quote/backslash must become _: %q", sanitized)
	}

	crlf := buildAttachmentDisposition("evil\r\nname.mp4")
	assertASCII(crlf)
	if !strings.Contains(crlf, "evil__name.mp4") {
		t.Fatalf("CR/LF must become _: %q", crlf)
	}

	if got := encodeRFC5987Value("第"); got != "%E7%AC%AC" {
		t.Fatalf("unexpected RFC5987 encoding: %q", got)
	}
	if got := encodeRFC5987Value("a b~.mp4"); got != "a%20b~.mp4" {
		t.Fatalf("unexpected RFC5987 encoding: %q", got)
	}
}

// 8) 路由与 Stream/Subtitles 同区注册，且不在 JWTAuth 组内。
func TestDownloadRouteRegistration(t *testing.T) {
	content, err := os.ReadFile("../main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(content)
	const downloadRoute = `api.GET("/episodes/:id/download", episodeHandler.Download)`
	if !strings.Contains(src, downloadRoute) {
		t.Fatalf("main.go must register download route %q", downloadRoute)
	}
	streamIdx := strings.Index(src, `/episodes/:id/stream`)
	downloadIdx := strings.Index(src, `/episodes/:id/download`)
	jwtIdx := strings.Index(src, "middleware.JWTAuth")
	if streamIdx < 0 || downloadIdx < 0 || jwtIdx < 0 {
		t.Fatalf("main.go must contain stream/download routes and JWTAuth (got %d/%d/%d)", streamIdx, downloadIdx, jwtIdx)
	}
	if downloadIdx < streamIdx {
		t.Fatalf("download route must sit beside stream/subtitles, before JWTAuth group")
	}
	if downloadIdx > jwtIdx {
		t.Fatalf("download route must be outside the JWTAuth group (before middleware.JWTAuth)")
	}
}
