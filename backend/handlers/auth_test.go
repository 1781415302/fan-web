package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/services"
)

func TestLoginRateLimiterIntegrationCountsFailuresAndResetsOnSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.Init(filepath.Join(t.TempDir(), "login-rate.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	if _, err := database.CreateUser("alice", "correct-password", false); err != nil {
		t.Fatal(err)
	}

	limiter := middleware.NewLoginRateLimiter(5, time.Minute)
	handler := NewAuthHandler(services.NewAuthService("login-test-secret", time.Hour), limiter)
	router := gin.New()
	router.POST("/api/auth/login", limiter.Middleware(), handler.Login)

	for i := 0; i < 4; i++ {
		if code := loginResponseCode(t, router, "alice", "wrong-password"); code != 2003 {
			t.Fatalf("failure %d should reach authentication, got %d", i+1, code)
		}
	}
	if code := loginResponseCode(t, router, "alice", "correct-password"); code != 0 {
		t.Fatalf("successful login must be allowed and reset failures, got %d", code)
	}
	for i := 0; i < 5; i++ {
		if code := loginResponseCode(t, router, "alice", "wrong-password"); code != 2003 {
			t.Fatalf("post-reset failure %d should reach authentication, got %d", i+1, code)
		}
	}
	if code := loginResponseCode(t, router, "alice", "wrong-password"); code != 1003 {
		t.Fatalf("request after five failures must be rate limited, got %d", code)
	}
}

func loginResponseCode(t *testing.T, router http.Handler, username, password string) int {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	var response struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return response.Code
}
