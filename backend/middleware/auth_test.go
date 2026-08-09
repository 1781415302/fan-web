package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
