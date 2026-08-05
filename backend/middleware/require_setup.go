package middleware

import (
	"github.com/gin-gonic/gin"

	"fan-web/utils"
)

// RequireSetup 在系统尚未初始化时，仅放行健康检查与初始化相关接口。
func RequireSetup(configured func() bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if configured() {
			c.Next()
			return
		}

		path := c.Request.URL.Path
		if path == "/api/health" || path == "/api/setup/status" || path == "/api/setup" {
			c.Next()
			return
		}

		utils.Error(c, utils.CodeInternal, "系统未初始化，请先完成初始化")
		c.Abort()
	}
}
