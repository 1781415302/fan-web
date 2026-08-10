package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLoggerOmitsQueryAndSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	router := gin.New()
	router.Use(RequestLogger(&buf))
	router.GET("/api/animes", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/animes?token=DO_NOT_LOG_THIS_TOKEN&media_token=also-secret", nil)
	request.Header.Set("Authorization", "Bearer JWT-SECRET-VALUE")
	router.ServeHTTP(recorder, request)

	output := buf.String()
	if !strings.Contains(output, "/api/animes") {
		t.Fatalf("log must contain path, got %q", output)
	}
	for _, forbidden := range []string{"DO_NOT_LOG_THIS_TOKEN", "also-secret", "JWT-SECRET-VALUE", "media_token", "token="} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("log must not contain %q:\n%s", forbidden, output)
		}
	}
}

func TestLimitJSONBodyRejectsOversize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LimitJSONBody(64 << 10))
	router.POST("/api/test", func(c *gin.Context) {
		var payload map[string]string
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 1001, "message": "请求参数错误"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})

	// 超过 64 KiB 的请求体应得到业务参数错误 1001，不 panic、不返回 Gin HTML。
	big := make([]byte, 70<<10)
	for i := range big {
		big[i] = 'a'
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewReader(big))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var response struct {
		Code int `json:"code"`
	}
	if err := decodeJSON(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected JSON error body, got: %s", recorder.Body.String())
	}
	if response.Code != 1001 {
		t.Fatalf("expected code 1001 for oversize body, got %d", response.Code)
	}

	// 服务仍可用：普通小请求正常。
	small := []byte(`{"k":"v"}`)
	smallRecorder := httptest.NewRecorder()
	smallRequest := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewReader(small))
	smallRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(smallRecorder, smallRequest)
	if smallRecorder.Code != http.StatusOK {
		t.Fatalf("server must stay usable, got %d", smallRecorder.Code)
	}
}

func decodeJSON(data []byte, target interface{}) error {
	return json.Unmarshal(data, target)
}
