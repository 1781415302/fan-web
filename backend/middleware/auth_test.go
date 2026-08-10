package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/models"
	"fan-web/services"
	"fan-web/utils"
)

func newAuthTestEnv(t *testing.T) (*services.AuthService, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "auth-test.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})

	auth := services.NewAuthService("auth-test-secret", 24*60*60*1e9)

	router := gin.New()
	protected := router.Group("/api")
	protected.Use(JWTAuth(auth))
	protected.GET("/animes", func(c *gin.Context) {
		userID, ok := CurrentUserID(c)
		if !ok {
			c.JSON(http.StatusOK, gin.H{"code": 9999, "message": "no user id"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "user_id": userID})
	})
	return auth, router
}

func authRequest(t *testing.T, router *gin.Engine, token string) map[string]interface{} {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/animes", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(recorder, request)

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func mustCreateUser(t *testing.T, username string, isAdmin bool) *models.User {
	t.Helper()
	user, err := database.CreateUser(username, "password", isAdmin)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func TestJWTAuthValidUserGetsThrough(t *testing.T) {
	auth, router := newAuthTestEnv(t)
	user := mustCreateUser(t, "alice", false)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}

	result := authRequest(t, router, token)
	if result["code"].(float64) != 0 {
		t.Fatalf("expected downstream handler to run with code 0, got %v", result)
	}
	if result["user_id"].(float64) != float64(user.ID) {
		t.Fatalf("unexpected user id: %v", result["user_id"])
	}
}

func TestJWTAuthDeletedUserRejected(t *testing.T) {
	auth, router := newAuthTestEnv(t)
	user := mustCreateUser(t, "bob", false)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteUser(user.ID); err != nil {
		t.Fatal(err)
	}

	result := authRequest(t, router, token)
	if result["code"].(float64) != utils.CodeUnauthenticated {
		t.Fatalf("expected code 2001 for deleted user, got %v", result)
	}
}

func TestJWTAuthInvalidTokenRejected(t *testing.T) {
	_, router := newAuthTestEnv(t)
	result := authRequest(t, router, "not-a-valid-token")
	if result["code"].(float64) != utils.CodeUnauthenticated {
		t.Fatalf("expected code 2001 for invalid token, got %v", result)
	}
}

func TestJWTAuthAdminRoleFromDatabase(t *testing.T) {
	auth, _ := newAuthTestEnv(t)
	user := mustCreateUser(t, "carol", false)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}

	// 把人抬成管理员，token 仍应通过且 RequireAdmin 以数据库状态为准。
	if err := promoteToAdmin(user.ID); err != nil {
		t.Fatal(err)
	}

	adminRouter := gin.New()
	adminGroup := adminRouter.Group("/api")
	adminGroup.Use(JWTAuth(auth), RequireAdmin)
	adminGroup.GET("/admin/users", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "admin": true})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	adminRouter.ServeHTTP(recorder, request)

	var result struct {
		Code  int  `json:"code"`
		Admin bool `json:"admin"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != 0 || !result.Admin {
		t.Fatalf("promoted user should pass RequireAdmin, got %+v", result)
	}
}

func promoteToAdmin(userID int64) error {
	_, err := database.DB.Exec("UPDATE users SET is_admin = 1 WHERE id = ?", userID)
	return err
}

func TestLoginRateLimiterOnlyCountsFailures(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := NewLoginRateLimiter(5, time.Minute, WithLoginLimiterClock(func() time.Time { return now }))

	for i := 0; i < 5; i++ {
		limiter.RecordFailure("1.1.1.1")
	}
	if limiter.Allow("1.1.1.1") {
		t.Fatal("5 failures should block within window")
	}
	// 成功登录清空。
	limiter.Reset("1.1.1.1")
	if !limiter.Allow("1.1.1.1") {
		t.Fatal("reset should allow again")
	}
	// 未失败的来源始终允许。
	if !limiter.Allow("2.2.2.2") {
		t.Fatal("no-failure source should always be allowed")
	}
}

func TestLoginRateLimiterWindowExpires(t *testing.T) {
	base := time.Unix(5000, 0)
	current := base
	limiter := NewLoginRateLimiter(5, time.Minute, WithLoginLimiterClock(func() time.Time { return current }))
	for i := 0; i < 5; i++ {
		limiter.RecordFailure("3.3.3.3")
	}
	if limiter.Allow("3.3.3.3") {
		t.Fatal("should be blocked")
	}
	// 越过窗口。
	current = base.Add(61 * time.Second)
	if !limiter.Allow("3.3.3.3") {
		t.Fatal("window expiry should restore access")
	}
}

func TestLoginRateLimiterCapacityBound(t *testing.T) {
	now := time.Unix(9000, 0)
	limiter := NewLoginRateLimiter(5, time.Minute, WithLoginLimiterClock(func() time.Time { return now }))
	// 灌入远超上限的来源。
	for i := 0; i < 5000; i++ {
		limiter.RecordFailure(time.Now().Format("20060102150405.000000000"))
		now = now.Add(time.Millisecond)
	}
	// 容量之后不应超过 maxKeys（4096），且不 panic。
	total := 0
	limiter.mu.Lock()
	total = len(limiter.attempts)
	limiter.mu.Unlock()
	if total > 4096 {
		t.Fatalf("attempts map exceeded capacity: %d", total)
	}
}
