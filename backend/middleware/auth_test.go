package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

func TestLoginRateLimiterUsesRollingWindow(t *testing.T) {
	current := time.Unix(7000, 0)
	limiter := NewLoginRateLimiter(5, time.Minute, WithLoginLimiterClock(func() time.Time { return current }))

	for i := 0; i < 4; i++ {
		limiter.RecordFailure("4.4.4.4")
		current = current.Add(20 * time.Second)
	}
	// t=80s: the first two failures are outside the preceding minute, so the
	// source must still be allowed instead of being blocked by a lifetime count.
	if !limiter.Allow("4.4.4.4") {
		t.Fatal("failures outside the rolling window must not count")
	}
	// Allow 会在放行的同时原子计入本次尝试：窗口内 t=40、t=60 两次旧失败，
	// 加上本次占用的额度，共 3 次。
	limiter.mu.Lock()
	remaining := len(limiter.attempts["4.4.4.4"].attempts)
	limiter.mu.Unlock()
	if remaining != 3 {
		t.Fatalf("expected three fresh attempts (two failures plus the consumed one), got %d", remaining)
	}
}

func TestLoginRateLimiterAllowDeletesExpiredSource(t *testing.T) {
	current := time.Unix(8000, 0)
	limiter := NewLoginRateLimiter(5, time.Minute, WithLoginLimiterClock(func() time.Time { return current }))
	limiter.RecordFailure("5.5.5.5")
	current = current.Add(time.Minute)
	if !limiter.Allow("5.5.5.5") {
		t.Fatal("expired source must be allowed")
	}
	// Allow 会清掉窗口外记录并原子计入本次尝试，过期记录不应残留。
	limiter.mu.Lock()
	entry, exists := limiter.attempts["5.5.5.5"]
	limiter.mu.Unlock()
	if !exists || len(entry.attempts) != 1 {
		t.Fatalf("Allow must drop the expired attempt and record only the new one, got %v", entry.attempts)
	}
}

func TestLoginRateLimiterCapacityBound(t *testing.T) {
	now := time.Unix(9000, 0)
	limiter := NewLoginRateLimiter(5, time.Minute, WithLoginLimiterClock(func() time.Time { return now }))
	// 灌入远超上限的来源。
	for i := 0; i < 5000; i++ {
		limiter.RecordFailure(fmt.Sprintf("source-%04d", i))
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

// newAPIKeyAuthTestEnv 构造挂 JWTOrAPIKeyAuth 的只读组、JWTOrAPIKeyAuth + RequireAdmin 的管理组，
// 以及只挂 JWTAuth 的个人/管理员组，模拟 main.go 的三路路由隔离。
func newAPIKeyAuthTestEnv(t *testing.T) (*services.AuthService, *gin.Engine) {
	t.Helper()
	auth, _ := newAuthTestEnv(t)

	whoami := func(c *gin.Context) {
		userID, ok := CurrentUserID(c)
		claims, claimsOK := CurrentClaims(c)
		if !ok || !claimsOK {
			c.JSON(http.StatusOK, gin.H{"code": 9999, "message": "no auth context"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code":     0,
			"user_id":  userID,
			"username": claims.Username,
			"is_admin": claims.IsAdmin,
		})
	}

	router := gin.New()
	api := router.Group("/api")
	catalogRead := api.Group("")
	catalogRead.Use(JWTOrAPIKeyAuth(auth))
	catalogRead.GET("/animes", whoami)
	manager := api.Group("")
	manager.Use(JWTOrAPIKeyAuth(auth), RequireAdmin)
	manager.POST("/library/assist-match", whoami)
	protected := api.Group("")
	protected.Use(JWTAuth(auth))
	protected.GET("/me/continue", whoami)
	admin := protected.Group("/admin")
	admin.Use(RequireAdmin)
	admin.GET("/api-keys", whoami)
	return auth, router
}

// mustCreateRawAPIKey 直接经 database 层落一条密钥并返回明文，哈希算法与 handler 一致（SHA-256 小写 hex）。
func mustCreateRawAPIKey(t *testing.T, createdBy int64, raw string) *models.APIKey {
	t.Helper()
	sum := sha256.Sum256([]byte(raw))
	rec, err := database.CreateAPIKey("hermes", hex.EncodeToString(sum[:]), raw[:11]+"..."+raw[len(raw)-4:], createdBy)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

type authResult struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

func doAuthRequest(t *testing.T, router *gin.Engine, method, path string, headers map[string]string) authResult {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	router.ServeHTTP(recorder, request)
	var result authResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return result
}

const testRawAPIKey = "fw_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestAPIKeyXHeaderValidGetsThrough(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	admin := mustCreateUser(t, "admin", true)
	rec := mustCreateRawAPIKey(t, admin.ID, testRawAPIKey)
	if rec.LastUsedAt != nil {
		t.Fatalf("new key should have nil last_used_at, got %v", rec.LastUsedAt)
	}

	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/api/animes"},
		{http.MethodPost, "/api/library/assist-match"},
	} {
		result := doAuthRequest(t, router, target.method, target.path, map[string]string{APIKeyHeader: "  " + testRawAPIKey + "  "})
		if result.Code != 0 {
			t.Fatalf("%s %s: expected X-API-Key to pass, got %+v", target.method, target.path, result)
		}
		if result.UserID != admin.ID || result.Username != "admin" || !result.IsAdmin {
			t.Fatalf("%s %s: claims should come from creator, got %+v", target.method, target.path, result)
		}
	}

	// 校验成功后应调用 TouchAPIKeyLastUsed。
	keys, err := database.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].LastUsedAt == nil {
		t.Fatalf("expected last_used_at to be touched, got %+v", keys)
	}
}

func TestAPIKeyBearerPrefixValidGetsThrough(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	admin := mustCreateUser(t, "admin", true)
	mustCreateRawAPIKey(t, admin.ID, testRawAPIKey)

	result := doAuthRequest(t, router, http.MethodPost, "/api/library/assist-match", map[string]string{"Authorization": "Bearer " + testRawAPIKey})
	if result.Code != 0 || result.UserID != admin.ID || !result.IsAdmin {
		t.Fatalf("expected Bearer fw_ key to pass manager group, got %+v", result)
	}
	keys, err := database.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].LastUsedAt == nil {
		t.Fatalf("expected last_used_at to be touched via Bearer, got %+v", keys)
	}
}

func TestAPIKeyInvalidRejected(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	admin := mustCreateUser(t, "admin", true)
	mustCreateRawAPIKey(t, admin.ID, testRawAPIKey)
	unknown := "fw_ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	for name, headers := range map[string]map[string]string{
		"x-api-key unknown":     {APIKeyHeader: unknown},
		"x-api-key no prefix":   {APIKeyHeader: "not-a-key"},
		"bearer fw_ unknown":    {"Authorization": "Bearer " + unknown},
		"bearer fw_ truncated":  {"Authorization": "Bearer " + testRawAPIKey[:20]},
		"x-api-key over bearer": {APIKeyHeader: unknown, "Authorization": "Bearer " + testRawAPIKey},
	} {
		result := doAuthRequest(t, router, http.MethodGet, "/api/animes", headers)
		if result.Code != utils.CodeUnauthenticated || result.Message != "API Key 无效或已失效" {
			t.Fatalf("%s: expected 2001 API Key 无效或已失效, got %+v", name, result)
		}
	}
}

func TestAPIKeyXHeaderTakesPriorityOverBearer(t *testing.T) {
	auth, router := newAPIKeyAuthTestEnv(t)
	admin := mustCreateUser(t, "admin", true)
	mustCreateRawAPIKey(t, admin.ID, testRawAPIKey)
	ordinary := mustCreateUser(t, "viewer", false)
	token, _, err := auth.IssueToken(*ordinary)
	if err != nil {
		t.Fatal(err)
	}

	// X-API-Key 有效时忽略 Authorization（即使是非法格式）。
	result := doAuthRequest(t, router, http.MethodGet, "/api/animes", map[string]string{
		APIKeyHeader:    testRawAPIKey,
		"Authorization": "Basic garbage",
	})
	if result.Code != 0 || result.UserID != admin.ID {
		t.Fatalf("X-API-Key should take priority, got %+v", result)
	}
	// 空白 X-API-Key 视为未提供，回落到 Bearer JWT。
	result = doAuthRequest(t, router, http.MethodGet, "/api/animes", map[string]string{
		APIKeyHeader:    "   ",
		"Authorization": "Bearer " + token,
	})
	if result.Code != 0 || result.UserID != ordinary.ID || result.IsAdmin {
		t.Fatalf("blank X-API-Key should fall back to JWT, got %+v", result)
	}
}

func TestAPIKeyRevokedRejected(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	admin := mustCreateUser(t, "admin", true)
	rec := mustCreateRawAPIKey(t, admin.ID, testRawAPIKey)

	if result := doAuthRequest(t, router, http.MethodGet, "/api/animes", map[string]string{APIKeyHeader: testRawAPIKey}); result.Code != 0 {
		t.Fatalf("key should work before revoke, got %+v", result)
	}
	if err := database.DeleteAPIKey(rec.ID); err != nil {
		t.Fatal(err)
	}
	for name, headers := range map[string]map[string]string{
		"x-api-key":  {APIKeyHeader: testRawAPIKey},
		"bearer fw_": {"Authorization": "Bearer " + testRawAPIKey},
	} {
		result := doAuthRequest(t, router, http.MethodGet, "/api/animes", headers)
		if result.Code != utils.CodeUnauthenticated || result.Message != "API Key 无效或已失效" {
			t.Fatalf("%s: revoked key must be rejected with 2001, got %+v", name, result)
		}
	}
}

func TestAPIKeyCreatorDeletedRejected(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	creator := mustCreateUser(t, "creator", true)
	mustCreateUser(t, "other-admin", true)
	mustCreateRawAPIKey(t, creator.ID, testRawAPIKey)

	if err := database.DeleteUser(creator.ID); err != nil {
		t.Fatal(err)
	}
	result := doAuthRequest(t, router, http.MethodPost, "/api/library/assist-match", map[string]string{APIKeyHeader: testRawAPIKey})
	if result.Code != utils.CodeUnauthenticated || result.Message != "API Key 无效或已失效" {
		t.Fatalf("key of deleted creator must be rejected with 2001, got %+v", result)
	}
}

func TestAPIKeyCreatorDemotedRejected(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	creator := mustCreateUser(t, "creator", true)
	mustCreateRawAPIKey(t, creator.ID, testRawAPIKey)

	if _, err := database.DB.Exec("UPDATE users SET is_admin = 0 WHERE id = ?", creator.ID); err != nil {
		t.Fatal(err)
	}
	// 降权后连只读组也必须拒绝（2001，而不是 RequireAdmin 的 2002）。
	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/api/animes"},
		{http.MethodPost, "/api/library/assist-match"},
	} {
		result := doAuthRequest(t, router, target.method, target.path, map[string]string{"Authorization": "Bearer " + testRawAPIKey})
		if result.Code != utils.CodeUnauthenticated || result.Message != "API Key 无效或已失效" {
			t.Fatalf("%s %s: demoted creator key must be rejected with 2001, got %+v", target.method, target.path, result)
		}
	}
	keys, err := database.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].LastUsedAt != nil {
		t.Fatalf("rejected key must not be touched, got %+v", keys)
	}
}

func TestAPIKeyRejectedByJWTAuthGroups(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	admin := mustCreateUser(t, "admin", true)
	mustCreateRawAPIKey(t, admin.ID, testRawAPIKey)

	for _, path := range []string{"/api/me/continue", "/api/admin/api-keys"} {
		result := doAuthRequest(t, router, http.MethodGet, path, map[string]string{APIKeyHeader: testRawAPIKey})
		if result.Code != utils.CodeUnauthenticated {
			t.Fatalf("%s: X-API-Key must be rejected by JWTAuth with 2001, got %+v", path, result)
		}
		result = doAuthRequest(t, router, http.MethodGet, path, map[string]string{"Authorization": "Bearer " + testRawAPIKey})
		if result.Code != utils.CodeUnauthenticated {
			t.Fatalf("%s: Bearer fw_ must be rejected by JWTAuth with 2001, got %+v", path, result)
		}
	}
}

func TestAPIKeyDatabaseErrorReturnsInternal(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	admin := mustCreateUser(t, "admin", true)
	mustCreateRawAPIKey(t, admin.ID, testRawAPIKey)
	if err := database.DB.Close(); err != nil {
		t.Fatal(err)
	}

	result := doAuthRequest(t, router, http.MethodGet, "/api/animes", map[string]string{APIKeyHeader: testRawAPIKey})
	if result.Code != utils.CodeInternal {
		t.Fatalf("expected 9999 on database error, got %+v", result)
	}
}

func TestJWTOrAPIKeyAuthBearerJWTStillWorks(t *testing.T) {
	auth, router := newAPIKeyAuthTestEnv(t)
	ordinary := mustCreateUser(t, "viewer", false)
	token, _, err := auth.IssueToken(*ordinary)
	if err != nil {
		t.Fatal(err)
	}

	// 普通用户 JWT 仍可访问只读组，但管理组被 RequireAdmin 拒绝（2002）。
	result := doAuthRequest(t, router, http.MethodGet, "/api/animes", map[string]string{"Authorization": "Bearer " + token})
	if result.Code != 0 || result.UserID != ordinary.ID || result.Username != "viewer" || result.IsAdmin {
		t.Fatalf("ordinary JWT should pass catalogRead, got %+v", result)
	}
	result = doAuthRequest(t, router, http.MethodPost, "/api/library/assist-match", map[string]string{"Authorization": "Bearer " + token})
	if result.Code != utils.CodeForbidden {
		t.Fatalf("ordinary JWT should be forbidden on manager group, got %+v", result)
	}

	// 角色以数据库为准：提升为管理员后同一 token 可进入管理组。
	if err := promoteToAdmin(ordinary.ID); err != nil {
		t.Fatal(err)
	}
	result = doAuthRequest(t, router, http.MethodPost, "/api/library/assist-match", map[string]string{"Authorization": "bearer " + token})
	if result.Code != 0 || !result.IsAdmin {
		t.Fatalf("promoted JWT should pass manager group, got %+v", result)
	}

	// 非法 JWT 与被删除用户的 JWT 与 JWTAuth 返回一致。
	result = doAuthRequest(t, router, http.MethodGet, "/api/animes", map[string]string{"Authorization": "Bearer not-a-valid-token"})
	if result.Code != utils.CodeUnauthenticated || result.Message != "登录状态已失效" {
		t.Fatalf("invalid JWT should return 2001 登录状态已失效, got %+v", result)
	}
	if err := database.DeleteUser(ordinary.ID); err != nil {
		t.Fatal(err)
	}
	result = doAuthRequest(t, router, http.MethodGet, "/api/animes", map[string]string{"Authorization": "Bearer " + token})
	if result.Code != utils.CodeUnauthenticated || result.Message != "登录状态已失效" {
		t.Fatalf("deleted user JWT should return 2001 登录状态已失效, got %+v", result)
	}
}

func TestJWTOrAPIKeyAuthMalformedBearerRejected(t *testing.T) {
	_, router := newAPIKeyAuthTestEnv(t)
	for name, headers := range map[string]map[string]string{
		"missing":     {},
		"basic":       {"Authorization": "Basic abc"},
		"bearer only": {"Authorization": "Bearer"},
		"extra parts": {"Authorization": "Bearer a b"},
	} {
		result := doAuthRequest(t, router, http.MethodGet, "/api/animes", headers)
		if result.Code != utils.CodeUnauthenticated || result.Message != "未登录" {
			t.Fatalf("%s: expected 2001 未登录, got %+v", name, result)
		}
	}
}

func TestJWTAuthErrorMessagesUnchanged(t *testing.T) {
	auth, router := newAuthTestEnv(t)
	user := mustCreateUser(t, "dave", false)
	token, _, err := auth.IssueToken(*user)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		headers map[string]string
		message string
	}{
		{"missing header", map[string]string{}, "未登录"},
		{"wrong scheme", map[string]string{"Authorization": "Token " + token}, "未登录"},
		{"invalid token", map[string]string{"Authorization": "Bearer nope"}, "登录状态已失效"},
	}
	for _, tc := range cases {
		result := doAuthRequest(t, router, http.MethodGet, "/api/animes", tc.headers)
		if result.Code != utils.CodeUnauthenticated || result.Message != tc.message {
			t.Fatalf("%s: expected 2001 %s, got %+v", tc.name, tc.message, result)
		}
	}

	if err := database.DB.Close(); err != nil {
		t.Fatal(err)
	}
	result := doAuthRequest(t, router, http.MethodGet, "/api/animes", map[string]string{"Authorization": "Bearer " + token})
	if result.Code != utils.CodeInternal || result.Message != "查询用户失败" {
		t.Fatalf("expected 9999 查询用户失败 on database error, got %+v", result)
	}
}
