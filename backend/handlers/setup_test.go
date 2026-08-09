package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"fan-web/config"
	"fan-web/database"
	"fan-web/services"
)

func TestSetupStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "status.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	cfg := config.Default()
	cfg.Configured = false
	handler := NewSetupHandler(filepath.Join(t.TempDir(), "config.yaml"), cfg, services.NewAuthService("secret", 0), services.NewScannerService(""), nil)

	router := gin.New()
	router.GET("/api/setup/status", handler.Status)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	var response struct {
		Code int `json:"code"`
		Data struct {
			Configured bool `json:"configured"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 0 {
		t.Fatalf("expected success code 0, got %d", response.Code)
	}
	if response.Data.Configured {
		t.Fatal("expected configured=false for default config")
	}
}

func TestSetupSubmit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "setup.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	videoRoot := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Default()
	cfg.Configured = false
	scanner := services.NewScannerService("")
	handler := NewSetupHandler(configPath, cfg, services.NewAuthService("setup-secret", 24*60*60*1e9), scanner, nil)

	router := gin.New()
	router.POST("/api/setup", handler.Submit)

	payload := map[string]interface{}{
		"admin_username":  "admin",
		"admin_password":  "s3cret-password",
		"video_root_path": videoRoot,
		"port":            9090,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	var response struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
			User  struct {
				Username string `json:"username"`
				IsAdmin  bool   `json:"is_admin"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 0 {
		t.Fatalf("expected success code 0, got %d (message: %v)", response.Code, recorder.Body.String())
	}
	if response.Data.Token == "" {
		t.Fatal("expected token to be returned")
	}
	if response.Data.User.Username != "admin" || !response.Data.User.IsAdmin {
		t.Fatalf("unexpected admin user: %+v", response.Data.User)
	}

	if !cfg.Configured {
		t.Fatal("expected cfg.Configured to become true after setup")
	}
	if cfg.Video.RootPath != videoRoot {
		t.Fatalf("expected video root %q, got %q", videoRoot, cfg.Video.RootPath)
	}
	if cfg.Server.Port != 9090 {
		t.Fatalf("expected port 9090, got %d", cfg.Server.Port)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if scanner.RootPath() != videoRoot {
		t.Fatalf("expected scanner root path to be updated, got %q", scanner.RootPath())
	}
}

func TestSetupSubmitInvalidVideoRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "setup-invalid.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	cfg := config.Default()
	cfg.Configured = false
	handler := NewSetupHandler(filepath.Join(t.TempDir(), "config.yaml"), cfg, services.NewAuthService("secret", 0), services.NewScannerService(""), nil)

	router := gin.New()
	router.POST("/api/setup", handler.Submit)

	payload := map[string]interface{}{
		"admin_username":  "admin",
		"admin_password":  "password",
		"video_root_path": "/path/that/does/not/exist",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code == 0 {
		t.Fatal("expected setup to fail for non-existent video root")
	}
}

func TestSetupSubmitAlreadyConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "setup-already.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	cfg := config.Default()
	cfg.Configured = true
	handler := NewSetupHandler(filepath.Join(t.TempDir(), "config.yaml"), cfg, services.NewAuthService("secret", 0), services.NewScannerService(""), nil)

	router := gin.New()
	router.POST("/api/setup", handler.Submit)

	payload := map[string]interface{}{
		"admin_username":  "admin",
		"admin_password":  "password",
		"video_root_path": t.TempDir(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 2002 {
		t.Fatalf("expected forbidden code 2002, got %d", response.Code)
	}
}

func TestSetupSubmitSaveFailureRollsBackAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "setup-savefail.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	// 配置保存失败：父目录不存在。
	configPath := filepath.Join(t.TempDir(), "missing", "config.yaml")
	cfg := config.Default()
	cfg.Configured = false
	cfg.Server.Port = 7777
	cfg.Video.RootPath = "/original"
	cfg.Admin.Username = "keep-admin"
	handler := NewSetupHandler(configPath, cfg, services.NewAuthService("secret", 0), services.NewScannerService(""), nil)

	router := gin.New()
	router.POST("/api/setup", handler.Submit)

	payload := map[string]interface{}{
		"admin_username":  "new-admin",
		"admin_password":  "password",
		"video_root_path": t.TempDir(),
		"port":            9090,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code == 0 {
		t.Fatalf("expected setup to fail when config save fails, got success")
	}

	// 1. 本次管理员必须回滚。
	if _, err := database.GetUserByUsername("new-admin"); err == nil {
		t.Fatal("expected rolled-back admin user to not exist")
	}

	// 2. 内存配置不得被污染。
	if cfg.Admin.Username != "keep-admin" || cfg.Video.RootPath != "/original" || cfg.Server.Port != 7777 {
		t.Fatalf("expected in-memory config unchanged, got %+v", cfg)
	}
	if cfg.Configured {
		t.Fatal("expected Configured to stay false after failure")
	}
}

func TestSetupSubmitRetryAfterFixingConfigPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "setup-retry.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	badConfigPath := filepath.Join(t.TempDir(), "missing", "config.yaml")
	goodConfigPath := filepath.Join(t.TempDir(), "config.yaml")
	videoRoot := t.TempDir()
	cfg := config.Default()
	cfg.Configured = false
	handler := NewSetupHandler(badConfigPath, cfg, services.NewAuthService("secret", 0), services.NewScannerService(""), nil)

	router := gin.New()
	router.POST("/api/setup", handler.Submit)

	payload := map[string]interface{}{
		"admin_username":  "samedev",
		"admin_password":  "password",
		"video_root_path": videoRoot,
	}
	body, _ := json.Marshal(payload)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	var failResp struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &failResp)
	if failResp.Code == 0 {
		t.Fatal("first attempt should fail (bad config path)")
	}

	// 修正 config path 后，同一用户名应能重试成功。
	handler.configPath = goodConfigPath
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var okResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &okResp); err != nil {
		t.Fatal(err)
	}
	if okResp.Code != 0 {
		t.Fatalf("expected retry to succeed after fixing config path, got %d body=%s", okResp.Code, recorder.Body.String())
	}
	if _, err := database.GetUserByUsername("samedev"); err != nil {
		t.Fatalf("admin user should exist after successful retry: %v", err)
	}
}
