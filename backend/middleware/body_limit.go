package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// LimitJSONBody 限制 POST/PUT/PATCH 的请求体大小，超限返回业务参数错误。
// 使用 http.MaxBytesReader，超限请求体读取会立即获得 MaxBytesError。
func LimitJSONBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		default:
			// GET/HEAD 不处理。
		}
		c.Next()
	}
}
