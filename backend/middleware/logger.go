package middleware

import (
	"io"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger 输出不含 query、Authorization、Cookie、请求体或响应体的访问日志。
// 每条日志只含：客户端 IP、方法、URL path、状态码、耗时、内部错误摘要。
func RequestLogger(out io.Writer) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()

		status := c.Writer.Status()
		duration := time.Since(start)
		var errSummary string
		if len(c.Errors) > 0 {
			errSummary = c.Errors.ByType(gin.ErrorTypePrivate).String()
		}

		// 手动格式化，避免 gin 默认日志把 RawQuery 拼进路径。
		logLine := "[HTTP] " + time.Now().Format("2006/01/02 - 15:04:05") +
			" | " + c.ClientIP() +
			" | " + c.Request.Method +
			" | " + path +
			" | " + itoa(status) +
			" | " + duration.String()
		if errSummary != "" {
			logLine += " | err=" + errSummary
		}
		_, _ = io.WriteString(out, logLine+"\n")
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
