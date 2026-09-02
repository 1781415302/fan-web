package middleware

import (
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS 允许的来源默认指向开发环境前端（localhost:5173 / 127.0.0.1:5173）。
// 生产环境前端由本程序同源托管、通常无需 CORS；若管理员需从其他主机/端口访问，
// 可设置环境变量 FAN_WEB_CORS_ORIGINS（逗号分隔）覆盖允许的源，避免硬编码。
func CORS() gin.HandlerFunc {
	allowOrigins := []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	if env := os.Getenv("FAN_WEB_CORS_ORIGINS"); env != "" {
		origins := strings.Split(env, ",")
		trimmed := make([]string, 0, len(origins))
		for _, raw := range origins {
			if o := strings.TrimSpace(raw); o != "" {
				trimmed = append(trimmed, o)
			}
		}
		if len(trimmed) > 0 {
			allowOrigins = trimmed
		}
	}
	return cors.New(cors.Config{
		AllowOrigins:     allowOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	})
}
