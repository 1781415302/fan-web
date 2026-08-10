package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/models"
	"fan-web/services"
)

func setupRBAC(t *testing.T) (*httptest.ResponseRecorder, *gin.Engine, *services.AuthService, *models.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "rbac.db")
	if err := database.Init(dbPath); err != nil {
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
	auth := services.NewAuthService("rbac-secret", 24*60*60*1e9)
	rootPath := t.TempDir()
	scanner := services.NewScannerService(rootPath)
	bangumiService := services.NewBangumiService()
	animeHandler := NewAnimeHandler(bangumiService, scanner)
	episodeHandler := NewEpisodeHandler(auth, scanner)
	libraryHandler := NewLibraryHandler(services.NewLibraryService(bangumiService, rootPath))
	bangumiHandler := NewBangumiHandler(bangumiService)

	router := gin.New()
	protected := router.Group("/api")
	protected.Use(middleware.JWTAuth(auth))
	protected.GET("/animes", animeHandler.List)
	protected.GET("/progress/:episode_id", episodeHandler.GetProgress)
	protected.POST("/progress/:episode_id", episodeHandler.ReportProgress)
	manager := protected.Group("")
	manager.Use(middleware.RequireAdmin)
	manager.POST("/animes", animeHandler.Create)
	manager.PUT("/animes/:id", animeHandler.Update)
	manager.DELETE("/animes/:id", animeHandler.Delete)
	manager.POST("/animes/:id/scan", animeHandler.Scan)
	manager.POST("/library/scan", libraryHandler.Scan)
	manager.GET("/bangumi/search", bangumiHandler.Search)
	manager.GET("/bangumi/subject/:id", bangumiHandler.Subject)

	return httptest.NewRecorder(), router, auth, admin
}

func doRBACRequest(t *testing.T, router *gin.Engine, token string, method, path string, body []byte) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(recorder, request)
	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Code
}

func TestOrdinaryUserDeniedFromWriteEndpoints(t *testing.T) {
	_, router, auth, admin := setupRBAC(t)
	ordinary, err := database.CreateUser("ordinary", "password", false)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryToken, _, err := auth.IssueToken(*ordinary)
	if err != nil {
		t.Fatal(err)
	}

	_, beforeCount, err := database.ListAnimes(1, 100, "", ordinary.ID)
	if err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{Title: "RBAC Anime"})
	if err != nil {
		t.Fatal(err)
	}
	animeID := strconv.FormatInt(anime.ID, 10)

	// 五个写接口全部返回 2002，且不新增/修改/删除番剧。
	createBody := []byte(`{"bangumi_id":1,"file_path":"dir"}`)
	if code := doRBACRequest(t, router, ordinaryToken, http.MethodPost, "/api/animes", createBody); code != 2002 {
		t.Fatalf("POST /animes expected 2002, got %d", code)
	}
	updateBody := []byte(`{"title":"Hacked","ep_count":9,"file_path":"dir"}`)
	if code := doRBACRequest(t, router, ordinaryToken, http.MethodPut, "/api/animes/"+animeID, updateBody); code != 2002 {
		t.Fatalf("PUT /animes/:id expected 2002, got %d", code)
	}
	if code := doRBACRequest(t, router, ordinaryToken, http.MethodDelete, "/api/animes/"+animeID, nil); code != 2002 {
		t.Fatalf("DELETE /animes/:id expected 2002, got %d", code)
	}
	if code := doRBACRequest(t, router, ordinaryToken, http.MethodPost, "/api/animes/"+animeID+"/scan", nil); code != 2002 {
		t.Fatalf("POST /animes/:id/scan expected 2002, got %d", code)
	}
	if code := doRBACRequest(t, router, ordinaryToken, http.MethodPost, "/api/library/scan", nil); code != 2002 {
		t.Fatalf("POST /library/scan expected 2002, got %d", code)
	}
	if code := doRBACRequest(t, router, ordinaryToken, http.MethodGet, "/api/bangumi/search?keyword=test", nil); code != 2002 {
		t.Fatalf("GET /bangumi/search expected 2002, got %d", code)
	}
	if code := doRBACRequest(t, router, ordinaryToken, http.MethodGet, "/api/bangumi/subject/1", nil); code != 2002 {
		t.Fatalf("GET /bangumi/subject/:id expected 2002, got %d", code)
	}

	_, afterCount, err := database.ListAnimes(1, 100, "", ordinary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterCount != beforeCount+1 {
		t.Fatalf("rejected requests must not create/modify/delete anime, got %d initially vs %d after", beforeCount, afterCount)
	}
	updated, err := database.GetAnimeByID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "RBAC Anime" || updated.EpCount != 0 {
		t.Fatalf("anime must not be modified by rejected request: %+v", updated)
	}

	// 管理员对应调用能进入（无 Bangumi 数据时返回内部错误，但不会是 2002）。
	adminToken, _, err := auth.IssueToken(*admin)
	if err != nil {
		t.Fatal(err)
	}
	adminChecks := []struct {
		method string
		path   string
		body   []byte
	}{
		{method: http.MethodPost, path: "/api/animes", body: []byte(`{}`)},
		{method: http.MethodPut, path: "/api/animes/0", body: updateBody},
		{method: http.MethodPost, path: "/api/animes/99999/scan"},
		{method: http.MethodPost, path: "/api/library/scan"},
		{method: http.MethodGet, path: "/api/bangumi/search"},
		{method: http.MethodGet, path: "/api/bangumi/subject/0"},
	}
	for _, check := range adminChecks {
		if code := doRBACRequest(t, router, adminToken, check.method, check.path, check.body); code == 2002 {
			t.Fatalf("admin must enter %s %s", check.method, check.path)
		}
	}
	if code := doRBACRequest(t, router, adminToken, http.MethodDelete, "/api/animes/"+animeID, nil); code != 0 {
		t.Fatalf("admin DELETE /animes/:id expected success code 0, got %d", code)
	}
}

func TestOrdinaryUserCanReadAndWriteOwnProgress(t *testing.T) {
	_, router, auth, _ := setupRBAC(t)
	ordinary, err := database.CreateUser("reader", "password", false)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.IssueToken(*ordinary)
	if err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{Title: "Readable Anime"})
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
	if len(episodes) != 1 {
		t.Fatalf("expected one episode, got %d", len(episodes))
	}

	// 读取番剧列表（普通用户可读）。
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/animes", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, request)
	var listResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Code != 0 {
		t.Fatalf("ordinary user should read anime list, got %d", listResp.Code)
	}

	episodeID := strconv.FormatInt(episodes[0].ID, 10)
	progressBody := []byte(`{"position":123,"watched":false}`)
	if code := doRBACRequest(t, router, token, http.MethodPost, "/api/progress/"+episodeID, progressBody); code != 0 {
		t.Fatalf("ordinary user should report own progress, got %d", code)
	}

	progressRecorder := httptest.NewRecorder()
	progressRequest := httptest.NewRequest(http.MethodGet, "/api/progress/"+episodeID, nil)
	progressRequest.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(progressRecorder, progressRequest)
	var progressResp struct {
		Code int `json:"code"`
		Data struct {
			Position int `json:"position"`
		} `json:"data"`
	}
	if err := json.Unmarshal(progressRecorder.Body.Bytes(), &progressResp); err != nil {
		t.Fatal(err)
	}
	if progressResp.Code != 0 || progressResp.Data.Position != 123 {
		t.Fatalf("ordinary user should read own progress, got %s", progressRecorder.Body.String())
	}
}
