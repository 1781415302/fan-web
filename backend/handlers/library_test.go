package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/models"
	"fan-web/services"
	"fan-web/utils"
)

func TestLibraryDirsMatchesListSubDirs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	for _, name := range []string{"show-a", "show-b"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "orphan.mkv"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}

	want, err := services.NewScannerService(root).ListSubDirs()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewLibraryHandler(services.NewLibraryService(nil, root))
	router := gin.New()
	router.GET("/api/library/dirs", handler.Dirs)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/library/dirs", nil)
	router.ServeHTTP(recorder, request)

	var response struct {
		Code int `json:"code"`
		Data struct {
			Items []string `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || response.Code != utils.CodeSuccess {
		t.Fatalf("GET /library/dirs HTTP %d code %d body=%s", recorder.Code, response.Code, recorder.Body.String())
	}
	if response.Data.Items == nil {
		t.Fatal("items must be a slice, got nil")
	}
	for _, item := range response.Data.Items {
		if item == "" {
			t.Fatal("items must not contain empty string")
		}
	}
	if !reflect.DeepEqual(response.Data.Items, want) {
		t.Fatalf("dirs items = %#v, want ListSubDirs %#v", response.Data.Items, want)
	}
}

func TestLibraryEndpointsRejectNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.Init(filepath.Join(t.TempDir(), "library-rbac.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	ordinary, err := database.CreateUser("viewer", "password", false)
	if err != nil {
		t.Fatal(err)
	}
	auth := services.NewAuthService("library-rbac-secret", time.Hour)
	token, _, err := auth.IssueToken(*ordinary)
	if err != nil {
		t.Fatal(err)
	}

	handler := NewLibraryHandler(services.NewLibraryService(nil, t.TempDir()))
	router := gin.New()
	admin := router.Group("/api")
	admin.Use(middleware.JWTAuth(auth), middleware.RequireAdmin)
	admin.POST("/library/scan", handler.Scan)
	admin.GET("/library/scan", handler.Status)
	admin.GET("/library/unidentified", handler.Unidentified)
	admin.GET("/library/dirs", handler.Dirs)

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/library/scan"},
		{http.MethodGet, "/api/library/scan"},
		{http.MethodGet, "/api/library/unidentified"},
		{http.MethodGet, "/api/library/dirs"},
	}
	for _, check := range paths {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(check.method, check.path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(recorder, request)
		var response struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("%s %s decode %q: %v", check.method, check.path, recorder.Body.String(), err)
		}
		if recorder.Code != http.StatusOK || response.Code != utils.CodeForbidden {
			t.Fatalf("%s %s HTTP %d code %d, want 200/2002", check.method, check.path, recorder.Code, response.Code)
		}
	}
}

func TestLibraryScanStatusIdleThenJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.Init(filepath.Join(t.TempDir(), "library-job-http.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	root := t.TempDir()
	handler := NewLibraryHandler(services.NewLibraryService(nil, root))
	t.Cleanup(func() { waitLibraryJobIdle(t, handler) })

	router := gin.New()
	router.POST("/api/library/scan", handler.Scan)
	router.GET("/api/library/scan", handler.Status)
	router.GET("/api/library/unidentified", handler.Unidentified)

	idle := getScanJob(t, router)
	if idle.State != services.ScanJobIdle {
		t.Fatalf("从未扫过 state = %q, want idle", idle.State)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/library/scan", nil)
	router.ServeHTTP(recorder, request)
	var started struct {
		Code int              `json:"code"`
		Data services.ScanJob `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || started.Code != utils.CodeSuccess {
		t.Fatalf("POST scan HTTP %d code %d body=%s", recorder.Code, started.Code, recorder.Body.String())
	}
	if started.Data.State != services.ScanJobRunning && started.Data.State != services.ScanJobDone {
		t.Fatalf("POST scan state = %q, want running or done", started.Data.State)
	}

	waitLibraryJobIdle(t, handler)
	done := getScanJob(t, router)
	if done.State != services.ScanJobDone || done.Result == nil {
		t.Fatalf("GET scan after job: state=%q result=%v error=%q", done.State, done.Result, done.Error)
	}

	unidentifiedRecorder := httptest.NewRecorder()
	unidentifiedRequest := httptest.NewRequest(http.MethodGet, "/api/library/unidentified?page=1&page_size=50", nil)
	router.ServeHTTP(unidentifiedRecorder, unidentifiedRequest)
	var list struct {
		Code int `json:"code"`
		Data struct {
			Items    []models.UnidentifiedFile `json:"items"`
			Total    int                       `json:"total"`
			Page     int                       `json:"page"`
			PageSize int                       `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(unidentifiedRecorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Code != utils.CodeSuccess || list.Data.Page != 1 || list.Data.PageSize != 50 {
		t.Fatalf("GET unidentified unexpected %#v body=%s", list, unidentifiedRecorder.Body.String())
	}
	if list.Data.Items == nil {
		t.Fatal("items must be a slice, got nil")
	}
}

func getScanJob(t *testing.T, router *gin.Engine) services.ScanJob {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/library/scan", nil)
	router.ServeHTTP(recorder, request)
	var response struct {
		Code int              `json:"code"`
		Data services.ScanJob `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || response.Code != utils.CodeSuccess {
		t.Fatalf("GET scan HTTP %d code %d body=%s", recorder.Code, response.Code, recorder.Body.String())
	}
	return response.Data
}

func waitLibraryJobIdle(t *testing.T, handler *LibraryHandler) {
	t.Helper()
	if handler == nil || handler.job == nil {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if handler.job.Snapshot().State != services.ScanJobRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("library scan job still running")
}
