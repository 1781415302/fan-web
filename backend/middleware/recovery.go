package middleware

import (
	"io"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

// Recovery catches panics without dumping the request line, query, headers,
// body, or panic value. Runtime values may contain credentials, so only the
// safe method/path pair and stack are written to the error log.
func Recovery(out io.Writer) gin.HandlerFunc {
	if out == nil {
		out = io.Discard
	}
	return func(c *gin.Context) {
		completed := false
		defer func() {
			rec := recover()
			if rec == nil {
				if completed {
					return
				}
				// panic(nil)：recover 已吞掉 panic，但 c.Next() 并未正常完成。
				// 此时必须中止请求并返回 500，否则会静默返回空响应（gin 默认 200）。
				rec = "nil panic"
			}

			_, _ = io.WriteString(out,
				"[Recovery] "+time.Now().Format("2006/01/02 - 15:04:05")+
					" | "+c.Request.Method+
					" | "+sanitizeLogField(c.Request.URL.Path)+
					" | panic recovered\n"+
					string(debug.Stack()),
			)
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
		completed = true
	}
}
