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
		defer func() {
			if recover() == nil {
				return
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
	}
}
