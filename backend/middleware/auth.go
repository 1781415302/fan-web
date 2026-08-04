package middleware

import (
	"database/sql"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/services"
	"fan-web/utils"
)

const (
	claimsContextKey = "auth_claims"
	userIDContextKey = "auth_user_id"
)

func JWTAuth(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			utils.Error(c, utils.CodeUnauthenticated, "未登录")
			c.Abort()
			return
		}

		claims, err := authService.ParseToken(parts[1])
		if err != nil {
			utils.Error(c, utils.CodeUnauthenticated, "登录状态已失效")
			c.Abort()
			return
		}

		c.Set(claimsContextKey, claims)
		c.Set(userIDContextKey, claims.UserID)
		c.Next()
	}
}

func RequireAdmin(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		c.Abort()
		return
	}

	user, err := database.GetUserByID(userID)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.Error(c, utils.CodeUnauthenticated, "登录状态已失效")
		} else {
			utils.Error(c, utils.CodeInternal, "查询用户失败")
		}
		c.Abort()
		return
	}
	if !user.IsAdmin {
		utils.Error(c, utils.CodeForbidden, "无权限")
		c.Abort()
		return
	}

	c.Set(userIDContextKey, user.ID)
	c.Next()
}

func CurrentUserID(c *gin.Context) (int64, bool) {
	value, exists := c.Get(userIDContextKey)
	if !exists {
		return 0, false
	}
	id, ok := value.(int64)
	return id, ok
}

func CurrentClaims(c *gin.Context) (*services.Claims, bool) {
	value, exists := c.Get(claimsContextKey)
	if !exists {
		return nil, false
	}
	claims, ok := value.(*services.Claims)
	return claims, ok
}

type LoginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	limit    int
	window   time.Duration
}

func NewLoginRateLimiter(limit int, window time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{
		attempts: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (l *LoginRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := clientIP(c)
		now := time.Now()
		cutoff := now.Add(-l.window)

		l.mu.Lock()
		attempts := l.attempts[key]
		fresh := attempts[:0]
		for _, attempt := range attempts {
			if attempt.After(cutoff) {
				fresh = append(fresh, attempt)
			}
		}
		if len(fresh) >= l.limit {
			l.attempts[key] = fresh
			l.mu.Unlock()
			utils.Error(c, utils.CodeTooManyRequests, "登录请求过于频繁，请稍后再试")
			c.Abort()
			return
		}
		l.attempts[key] = append(fresh, now)
		l.mu.Unlock()

		c.Next()
	}
}

func clientIP(c *gin.Context) string {
	if ip := c.ClientIP(); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return "unknown"
}
