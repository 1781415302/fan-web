package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/models"
	"fan-web/services"
)

type bangumiMeAPI struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setupBangumiMe(t *testing.T, meStatus int) (*gin.Engine, *services.AuthService, *models.User, *services.BangumiService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.Init(filepath.Join(t.TempDir(), "bangumi-me.db")); err != nil {
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

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/me" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		w.WriteHeader(meStatus)
		if meStatus == http.StatusOK {
			_, _ = w.Write([]byte(`{"id":1}`))
		}
	}))
	t.Cleanup(upstream.Close)

	bangumi := services.NewBangumiService()
	bangumi.SetBaseURL(upstream.URL)
	sync := services.NewBangumiSync(bangumi)
	handler := NewBangumiMeHandler(bangumi, sync)
	auth := services.NewAuthService("bangumi-me-secret", time.Hour)
	router := gin.New()
	protected := router.Group("/api")
	protected.Use(middleware.JWTAuth(auth))
	protected.GET("/me/bangumi", handler.Get)
	protected.PUT("/me/bangumi", handler.Put)
	protected.DELETE("/me/bangumi", handler.Delete)
	protected.POST("/me/bangumi/sync", handler.Sync)
	return router, auth, user, bangumi
}

func doBangumiMe(t *testing.T, router *gin.Engine, token, method, path string, body []byte) bangumiMeAPI {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s HTTP %d body=%s", method, path, recorder.Code, recorder.Body.String())
	}
	var resp bangumiMeAPI
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return resp
}

func TestBangumiMeUnboundOmitsSuffix(t *testing.T) {
	router, auth, user, _ := setupBangumiMe(t, http.StatusOK)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	resp := doBangumiMe(t, router, token, http.MethodGet, "/api/me/bangumi", nil)
	if resp.Code != 0 {
		t.Fatalf("code=%d message=%s", resp.Code, resp.Message)
	}
	var data map[string]any
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["linked"] != false {
		t.Fatalf("unbound linked=%v", data["linked"])
	}
	if _, ok := data["suffix"]; ok {
		t.Fatalf("unbound must omit suffix, data=%s", resp.Data)
	}
}

func TestBangumiMePut401DoesNotSave(t *testing.T) {
	router, auth, user, _ := setupBangumiMe(t, http.StatusUnauthorized)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"access_token": "bad-token-xxxx"})
	resp := doBangumiMe(t, router, token, http.MethodPut, "/api/me/bangumi", body)
	if resp.Code != 1001 || resp.Message != "Bangumi 令牌无效" {
		t.Fatalf("PUT 401 got code=%d message=%q", resp.Code, resp.Message)
	}
	if _, ok, err := database.GetBangumiToken(user.ID); err != nil || ok {
		t.Fatalf("401 must not save token, ok=%v err=%v", ok, err)
	}
}

func TestBangumiMePut200SavesAndEnqueuesWatched(t *testing.T) {
	router, auth, user, _ := setupBangumiMe(t, http.StatusOK)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	anime, err := database.CreateAnime(&models.Anime{Title: "Show", BangumiID: 55, EpCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "01.mp4"}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertProgress(user.ID, episodes[0].ID, 8, true); err != nil {
		t.Fatal(err)
	}

	pat := "personal-access-wxyz"
	body, _ := json.Marshal(map[string]string{"access_token": pat})
	resp := doBangumiMe(t, router, token, http.MethodPut, "/api/me/bangumi", body)
	if resp.Code != 0 {
		t.Fatalf("PUT 200 got code=%d message=%s", resp.Code, resp.Message)
	}
	saved, ok, err := database.GetBangumiToken(user.ID)
	if err != nil || !ok || saved != pat {
		t.Fatalf("token not saved, ok=%v saved=%q err=%v", ok, saved, err)
	}
	rows, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].EpisodeID != episodes[0].ID {
		t.Fatalf("EnqueueWatchedForUser missing, rows=%#v", rows)
	}

	get := doBangumiMe(t, router, token, http.MethodGet, "/api/me/bangumi", nil)
	if get.Code != 0 {
		t.Fatalf("GET bound code=%d", get.Code)
	}
	var data map[string]any
	if err := json.Unmarshal(get.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["linked"] != true || data["suffix"] != "wxyz" {
		t.Fatalf("bound GET data=%s", get.Data)
	}
	if strings.Contains(string(get.Data), pat) {
		t.Fatalf("GET must not contain full token: %s", get.Data)
	}
	if len(data) != 2 {
		t.Fatalf("bound GET should only have linked+suffix, data=%s", get.Data)
	}

	del := doBangumiMe(t, router, token, http.MethodDelete, "/api/me/bangumi", nil)
	if del.Code != 0 {
		t.Fatalf("DELETE code=%d", del.Code)
	}
	var deleted map[string]any
	if err := json.Unmarshal(del.Data, &deleted); err != nil {
		t.Fatal(err)
	}
	if deleted["linked"] != false {
		t.Fatalf("DELETE data=%s", del.Data)
	}
	if _, ok := deleted["suffix"]; ok {
		t.Fatalf("DELETE must omit suffix, data=%s", del.Data)
	}
	if _, ok, err := database.GetBangumiToken(user.ID); err != nil || ok {
		t.Fatalf("DELETE should clear token, ok=%v err=%v", ok, err)
	}
	left, err := database.ListBangumiOutbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("DELETE should clear outbox, got %#v", left)
	}
}

func TestBangumiMePutRejectsEmptyAndTooLong(t *testing.T) {
	router, auth, user, _ := setupBangumiMe(t, http.StatusOK)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := json.Marshal(map[string]string{"access_token": "  "})
	if resp := doBangumiMe(t, router, token, http.MethodPut, "/api/me/bangumi", empty); resp.Code != 1001 {
		t.Fatalf("empty token code=%d", resp.Code)
	}
	long, _ := json.Marshal(map[string]string{"access_token": strings.Repeat("a", 513)})
	if resp := doBangumiMe(t, router, token, http.MethodPut, "/api/me/bangumi", long); resp.Code != 1001 {
		t.Fatalf("long token code=%d", resp.Code)
	}
	if _, ok, err := database.GetBangumiToken(user.ID); err != nil || ok {
		t.Fatalf("invalid PUT must not save, ok=%v err=%v", ok, err)
	}
}

func TestBangumiMeSyncUnbound(t *testing.T) {
	router, auth, user, _ := setupBangumiMe(t, http.StatusOK)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	resp := doBangumiMe(t, router, token, http.MethodPost, "/api/me/bangumi/sync", nil)
	if resp.Code != 1001 || resp.Message != "未绑定 Bangumi" {
		t.Fatalf("unbound sync code=%d message=%q", resp.Code, resp.Message)
	}
}

func TestReportProgressDoesNotRequireBangumiHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.Init(filepath.Join(t.TempDir(), "progress-bangumi.db")); err != nil {
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
	anime, err := database.CreateAnime(&models.Anime{Title: "P", BangumiID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SyncEpisodes(anime.ID, []models.Episode{{EpNumber: 1, FilePath: "01.mp4"}}); err != nil {
		t.Fatal(err)
	}
	episodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	bangumi := services.NewBangumiService()
	bangumi.SetBaseURL(upstream.URL)
	sync := services.NewBangumiSync(bangumi)

	auth := services.NewAuthService("progress-bangumi-secret", time.Hour)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewEpisodeHandler(auth, services.NewScannerService(t.TempDir()))
	handler.SetBangumiSync(sync)
	router := gin.New()
	router.POST("/api/progress/:episode_id", middleware.JWTAuth(auth), handler.ReportProgress)

	body, _ := json.Marshal(map[string]any{"position": 3, "watched": true})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/progress/"+strconv.FormatInt(episodes[0].ID, 10), bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP %d body=%s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 {
		t.Fatalf("ReportProgress must succeed without Bangumi HTTP, code=%d body=%s", resp.Code, recorder.Body.String())
	}
	time.Sleep(150 * time.Millisecond)
}
