package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/models"
	"fan-web/services"
	"fan-web/utils"
)

var apiKeyRawPattern = regexp.MustCompile(`^fw_[0-9a-f]{64}$`)

// setupAPIKeyHandler 按 main.go 的挂载方式构造 /api/admin/api-keys（JWTAuth + RequireAdmin），
// 另挂一条 JWTOrAPIKeyAuth 的只读路由用于验证新建密钥可用、吊销后立即失效。
func setupAPIKeyHandler(t *testing.T) (*gin.Engine, string, *models.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "api-keys.db")
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
	auth := services.NewAuthService("api-keys-secret", 24*60*60*1e9)
	token, _, err := auth.IssueToken(*admin)
	if err != nil {
		t.Fatal(err)
	}

	handler := NewAPIKeyHandler()
	router := gin.New()
	api := router.Group("/api")
	catalogRead := api.Group("")
	catalogRead.Use(middleware.JWTOrAPIKeyAuth(auth))
	catalogRead.GET("/animes", func(c *gin.Context) { utils.Success(c, nil) })
	protected := api.Group("")
	protected.Use(middleware.JWTAuth(auth))
	adminGroup := protected.Group("/admin")
	adminGroup.Use(middleware.RequireAdmin)
	adminGroup.GET("/api-keys", handler.List)
	adminGroup.POST("/api-keys", handler.Create)
	adminGroup.DELETE("/api-keys/:id", handler.Delete)
	return router, token, admin
}

type apiKeyResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func doAPIKeyRequest(t *testing.T, router *gin.Engine, headers map[string]string, method, path string, body []byte) (apiKeyResponse, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	router.ServeHTTP(recorder, request)
	var response apiKeyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return response, recorder.Body.String()
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func mustCreateAPIKeyViaHandler(t *testing.T, router *gin.Engine, token, name string) (models.APIKeyCreated, string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		t.Fatal(err)
	}
	response, raw := doAPIKeyRequest(t, router, bearer(token), http.MethodPost, "/api/admin/api-keys", body)
	if response.Code != utils.CodeSuccess {
		t.Fatalf("create api key failed: %s", raw)
	}
	var created models.APIKeyCreated
	if err := json.Unmarshal(response.Data, &created); err != nil {
		t.Fatal(err)
	}
	return created, raw
}

func TestAPIKeyHandlerCreateReturnsPlaintextOnce(t *testing.T) {
	router, token, admin := setupAPIKeyHandler(t)

	created, createBody := mustCreateAPIKeyViaHandler(t, router, token, "  Hermes 下载机  ")
	if !apiKeyRawPattern.MatchString(created.Key) || len(created.Key) != 67 {
		t.Fatalf("raw key must be fw_ + 64 lowercase hex (67 chars), got %q", created.Key)
	}
	if !strings.Contains(createBody, `"api_key":"`+created.Key+`"`) {
		t.Fatalf("create response must carry api_key, got %s", createBody)
	}
	if created.Name != "Hermes 下载机" {
		t.Fatalf("name should be trimmed, got %q", created.Name)
	}
	wantPrefix := created.Key[:11] + "..." + created.Key[len(created.Key)-4:]
	if created.KeyPrefix != wantPrefix {
		t.Fatalf("key_prefix = %q, want %q", created.KeyPrefix, wantPrefix)
	}
	if created.ID <= 0 || created.CreatedBy != admin.ID || created.LastUsedAt != nil || created.CreatedAt.IsZero() {
		t.Fatalf("unexpected created record: %+v", created.APIKey)
	}

	// 库里存的是 SHA-256 小写 hex，而不是明文。
	sum := sha256.Sum256([]byte(created.Key))
	keyHash := hex.EncodeToString(sum[:])
	rec, err := database.GetAPIKeyByHash(keyHash)
	if err != nil {
		t.Fatalf("hash of raw key should be stored: %v", err)
	}
	if rec.ID != created.ID {
		t.Fatalf("stored id %d != created id %d", rec.ID, created.ID)
	}
	if _, err := database.GetAPIKeyByHash(created.Key); err == nil {
		t.Fatal("plaintext key must not be stored as key_hash")
	}

	// List 只返回元数据，既不含明文也不含哈希。
	listResponse, listBody := doAPIKeyRequest(t, router, bearer(token), http.MethodGet, "/api/admin/api-keys", nil)
	if listResponse.Code != utils.CodeSuccess {
		t.Fatalf("list failed: %s", listBody)
	}
	if strings.Contains(listBody, created.Key) || strings.Contains(listBody, keyHash) || strings.Contains(listBody, "api_key") || strings.Contains(listBody, "key_hash") {
		t.Fatalf("list must not expose plaintext or hash, got %s", listBody)
	}
	var keys []models.APIKey
	if err := json.Unmarshal(listResponse.Data, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != created.ID || keys[0].KeyPrefix != wantPrefix || keys[0].Name != "Hermes 下载机" {
		t.Fatalf("unexpected list: %+v", keys)
	}

	// 新建密钥立即可用于 JWTOrAPIKeyAuth。
	if response, raw := doAPIKeyRequest(t, router, map[string]string{middleware.APIKeyHeader: created.Key}, http.MethodGet, "/api/animes", nil); response.Code != utils.CodeSuccess {
		t.Fatalf("created key should authenticate, got %s", raw)
	}
}

func TestAPIKeyHandlerCreateGeneratesDistinctKeys(t *testing.T) {
	router, token, _ := setupAPIKeyHandler(t)
	first, _ := mustCreateAPIKeyViaHandler(t, router, token, "a")
	second, _ := mustCreateAPIKeyViaHandler(t, router, token, "b")
	if first.Key == second.Key {
		t.Fatal("each created key must be random")
	}

	response, raw := doAPIKeyRequest(t, router, bearer(token), http.MethodGet, "/api/admin/api-keys", nil)
	if response.Code != utils.CodeSuccess {
		t.Fatalf("list failed: %s", raw)
	}
	var keys []models.APIKey
	if err := json.Unmarshal(response.Data, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0].ID != second.ID || keys[1].ID != first.ID {
		t.Fatalf("list should be id DESC, got %+v", keys)
	}
}

func TestAPIKeyHandlerListEmptyIsArray(t *testing.T) {
	router, token, _ := setupAPIKeyHandler(t)
	response, raw := doAPIKeyRequest(t, router, bearer(token), http.MethodGet, "/api/admin/api-keys", nil)
	if response.Code != utils.CodeSuccess || string(response.Data) != "[]" {
		t.Fatalf("empty list should be [], got %s", raw)
	}
}

func TestAPIKeyHandlerCreateInvalidName(t *testing.T) {
	router, token, _ := setupAPIKeyHandler(t)

	maxName := strings.Repeat("密", 64)
	if utf8.RuneCountInString(maxName) != 64 {
		t.Fatal("fixture should be 64 runes")
	}
	cases := map[string][]byte{
		"empty":          []byte(`{"name":""}`),
		"blank":          []byte(`{"name":"   "}`),
		"missing":        []byte(`{}`),
		"too long":       []byte(`{"name":"` + maxName + `x"}`),
		"malformed":      []byte(`{"name":`),
		"wrong type":     []byte(`{"name":123}`),
		"too long ascii": []byte(`{"name":"` + strings.Repeat("a", 65) + `"}`),
	}
	for name, body := range cases {
		response, raw := doAPIKeyRequest(t, router, bearer(token), http.MethodPost, "/api/admin/api-keys", body)
		if response.Code != utils.CodeInvalidParams {
			t.Fatalf("%s: expected 1001, got %s", name, raw)
		}
	}
	keys, err := database.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("invalid requests must not create keys, got %+v", keys)
	}

	// 64 个 rune（多字节）恰好合法。
	created, _ := mustCreateAPIKeyViaHandler(t, router, token, maxName)
	if created.Name != maxName {
		t.Fatalf("64-rune name should be accepted, got %q", created.Name)
	}
}

func TestAPIKeyHandlerDelete(t *testing.T) {
	router, token, _ := setupAPIKeyHandler(t)
	created, _ := mustCreateAPIKeyViaHandler(t, router, token, "hermes")
	path := "/api/admin/api-keys/" + strconv.FormatInt(created.ID, 10)

	response, raw := doAPIKeyRequest(t, router, bearer(token), http.MethodDelete, path, nil)
	if response.Code != utils.CodeSuccess || string(response.Data) != "null" {
		t.Fatalf("delete should succeed with nil data, got %s", raw)
	}
	keys, err := database.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("key should be removed, got %+v", keys)
	}

	// 吊销后立即失效。
	response, raw = doAPIKeyRequest(t, router, map[string]string{middleware.APIKeyHeader: created.Key}, http.MethodGet, "/api/animes", nil)
	if response.Code != utils.CodeUnauthenticated {
		t.Fatalf("revoked key must be rejected with 2001, got %s", raw)
	}

	// 再次删除 → 1002。
	response, raw = doAPIKeyRequest(t, router, bearer(token), http.MethodDelete, path, nil)
	if response.Code != utils.CodeNotFound || response.Message != "API Key 不存在" {
		t.Fatalf("deleting missing key should return 1002 API Key 不存在, got %s", raw)
	}
}

func TestAPIKeyHandlerDeleteInvalidID(t *testing.T) {
	router, token, _ := setupAPIKeyHandler(t)
	for _, id := range []string{"abc", "0", "-1", "1.5"} {
		response, raw := doAPIKeyRequest(t, router, bearer(token), http.MethodDelete, "/api/admin/api-keys/"+id, nil)
		if response.Code != utils.CodeInvalidParams {
			t.Fatalf("id %q: expected 1001, got %s", id, raw)
		}
	}
}

func TestAPIKeyHandlerRejectsAPIKeyAndOrdinaryUser(t *testing.T) {
	router, token, _ := setupAPIKeyHandler(t)
	created, _ := mustCreateAPIKeyViaHandler(t, router, token, "hermes")

	// 持 API Key 访问 /api/admin/api-keys 一律 2001（只挂 JWTAuth）。
	for name, headers := range map[string]map[string]string{
		"x-api-key":  {middleware.APIKeyHeader: created.Key},
		"bearer fw_": bearer(created.Key),
	} {
		response, raw := doAPIKeyRequest(t, router, headers, http.MethodGet, "/api/admin/api-keys", nil)
		if response.Code != utils.CodeUnauthenticated {
			t.Fatalf("%s: API Key must not reach admin api-keys, got %s", name, raw)
		}
		response, raw = doAPIKeyRequest(t, router, headers, http.MethodPost, "/api/admin/api-keys", []byte(`{"name":"escalate"}`))
		if response.Code != utils.CodeUnauthenticated {
			t.Fatalf("%s: API Key must not create keys, got %s", name, raw)
		}
	}

	// 普通用户 JWT → RequireAdmin 2002。
	ordinary, err := database.CreateUser("viewer", "password", false)
	if err != nil {
		t.Fatal(err)
	}
	auth := services.NewAuthService("api-keys-secret", 24*60*60*1e9)
	ordinaryToken, _, err := auth.IssueToken(*ordinary)
	if err != nil {
		t.Fatal(err)
	}
	response, raw := doAPIKeyRequest(t, router, bearer(ordinaryToken), http.MethodGet, "/api/admin/api-keys", nil)
	if response.Code != utils.CodeForbidden {
		t.Fatalf("ordinary user should get 2002, got %s", raw)
	}
}

func TestAPIKeyHandlerCreatorDemotedOrDeletedInvalidatesKey(t *testing.T) {
	router, token, _ := setupAPIKeyHandler(t)
	second, err := database.CreateUser("second-admin", "password", true)
	if err != nil {
		t.Fatal(err)
	}
	auth := services.NewAuthService("api-keys-secret", 24*60*60*1e9)
	secondToken, _, err := auth.IssueToken(*second)
	if err != nil {
		t.Fatal(err)
	}
	demoted, _ := mustCreateAPIKeyViaHandler(t, router, secondToken, "demoted")
	kept, _ := mustCreateAPIKeyViaHandler(t, router, token, "kept")

	if _, err := database.DB.Exec("UPDATE users SET is_admin = 0 WHERE id = ?", second.ID); err != nil {
		t.Fatal(err)
	}
	response, raw := doAPIKeyRequest(t, router, map[string]string{middleware.APIKeyHeader: demoted.Key}, http.MethodGet, "/api/animes", nil)
	if response.Code != utils.CodeUnauthenticated {
		t.Fatalf("demoted creator key must be rejected, got %s", raw)
	}
	if err := database.DeleteUser(second.ID); err != nil {
		t.Fatal(err)
	}
	response, raw = doAPIKeyRequest(t, router, bearer(demoted.Key), http.MethodGet, "/api/animes", nil)
	if response.Code != utils.CodeUnauthenticated {
		t.Fatalf("deleted creator key must be rejected, got %s", raw)
	}
	// 其他管理员的密钥不受影响。
	response, raw = doAPIKeyRequest(t, router, bearer(kept.Key), http.MethodGet, "/api/animes", nil)
	if response.Code != utils.CodeSuccess {
		t.Fatalf("unrelated key should still work, got %s", raw)
	}
}

func TestAPIKeyHandlerDatabaseErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "api-keys-closed.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Close(); err != nil {
		t.Fatal(err)
	}

	handler := NewAPIKeyHandler()
	router := gin.New()
	// 夹具：模拟已通过 JWTAuth 的上下文（键名与 middleware.userIDContextKey 一致），
	// 因为库已关闭，真实 JWTAuth 会先在查用户时失败，到不了 handler。
	router.Use(func(c *gin.Context) {
		c.Set("auth_user_id", int64(1))
		c.Next()
	})
	router.GET("/api/admin/api-keys", handler.List)
	router.POST("/api/admin/api-keys", handler.Create)
	router.DELETE("/api/admin/api-keys/:id", handler.Delete)

	for _, tc := range []struct {
		method, path string
		body         []byte
	}{
		{http.MethodGet, "/api/admin/api-keys", nil},
		{http.MethodPost, "/api/admin/api-keys", []byte(`{"name":"hermes"}`)},
		{http.MethodDelete, "/api/admin/api-keys/1", nil},
	} {
		response, raw := doAPIKeyRequest(t, router, nil, tc.method, tc.path, tc.body)
		if response.Code != utils.CodeInternal {
			t.Fatalf("%s %s: expected 9999 on database error, got %s", tc.method, tc.path, raw)
		}
	}
}

func TestAPIKeyHandlerCreateWithoutUserContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "api-keys-nouser.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	router := gin.New()
	router.POST("/api/admin/api-keys", NewAPIKeyHandler().Create)
	response, raw := doAPIKeyRequest(t, router, nil, http.MethodPost, "/api/admin/api-keys", []byte(`{"name":"hermes"}`))
	if response.Code != utils.CodeUnauthenticated {
		t.Fatalf("missing user context should return 2001, got %s", raw)
	}
}
