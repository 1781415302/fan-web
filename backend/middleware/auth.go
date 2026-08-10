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

		// 校验用户仍存在：管理员删除用户后，已签发的 token 立即失效。
		user, err := database.GetUserByID(claims.UserID)
		if err != nil {
			if err == sql.ErrNoRows {
				utils.Error(c, utils.CodeUnauthenticated, "登录状态已失效")
			} else {
				utils.Error(c, utils.CodeInternal, "查询用户失败")
			}
			c.Abort()
			return
		}

		// 用数据库中的最新信息覆盖 claims，避免误用过期角色。
		claims.Username = user.Username
		claims.IsAdmin = user.IsAdmin

		c.Set(claimsContextKey, claims)
		c.Set(userIDContextKey, user.ID)
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

type failureEntry struct {
	attempts []time.Time
	last     time.Time
}

// LoginRateLimiter 基于来源 IP 的失败尝试限流器。
// 只统计失败尝试，成功登录会清空对应来源；带容量上限与过期清理。
type LoginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]failureEntry
	limit    int
	window   time.Duration
	maxKeys  int
	now      func() time.Time
}

// LoginLimiterOption 允许测试注入时钟。
type LoginLimiterOption func(*LoginRateLimiter)

// WithLoginLimiterClock 注入时钟函数。
func WithLoginLimiterClock(clock func() time.Time) LoginLimiterOption {
	return func(l *LoginRateLimiter) {
		l.now = clock
	}
}

// NewLoginRateLimiter 创建限流器。窗口内最多 limit 次失败尝试，来源上限 maxKeys。
func NewLoginRateLimiter(limit int, window time.Duration, opts ...LoginLimiterOption) *LoginRateLimiter {
	l := &LoginRateLimiter{
		attempts: make(map[string]failureEntry),
		limit:    limit,
		window:   window,
		maxKeys:  4096,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// allowLocked 清理当前来源窗口外记录后判断是否允许尝试。
func (l *LoginRateLimiter) allowLocked(source string, now time.Time) bool {
	entry, ok := l.attempts[source]
	if !ok {
		return true
	}
	entry.attempts = freshAttempts(entry.attempts, now.Add(-l.window))
	if len(entry.attempts) == 0 {
		delete(l.attempts, source)
		return true
	}
	entry.last = entry.attempts[len(entry.attempts)-1]
	l.attempts[source] = entry
	return len(entry.attempts) < l.limit
}

// Allow 只检查窗口内失败次数，不记录任何请求。
func (l *LoginRateLimiter) Allow(source string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	return l.allowLocked(source, now)
}

// RecordFailure 仅在认证失败时记录一次失败。
func (l *LoginRateLimiter) RecordFailure(source string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.cleanupExpiredLocked(now)

	entry, exists := l.attempts[source]
	if !exists && len(l.attempts) >= l.maxKeys {
		l.evictOldestLocked()
	}
	entry.attempts = append(entry.attempts, now)
	entry.last = now
	l.attempts[source] = entry
}

// Reset 成功登录后清空对应来源的全部记录。
func (l *LoginRateLimiter) Reset(source string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, source)
}

func (l *LoginRateLimiter) cleanupExpiredLocked(now time.Time) {
	cutoff := now.Add(-l.window)
	for source, entry := range l.attempts {
		entry.attempts = freshAttempts(entry.attempts, cutoff)
		if len(entry.attempts) == 0 {
			delete(l.attempts, source)
			continue
		}
		entry.last = entry.attempts[len(entry.attempts)-1]
		l.attempts[source] = entry
	}
}

func (l *LoginRateLimiter) evictOldestLocked() {
	oldestSource := ""
	var oldestLast time.Time
	for source, entry := range l.attempts {
		if oldestSource == "" || entry.last.Before(oldestLast) ||
			(entry.last.Equal(oldestLast) && source < oldestSource) {
			oldestSource = source
			oldestLast = entry.last
		}
	}
	if oldestSource != "" {
		delete(l.attempts, oldestSource)
	}
}

func freshAttempts(attempts []time.Time, cutoff time.Time) []time.Time {
	fresh := attempts[:0]
	for _, attempt := range attempts {
		if attempt.After(cutoff) {
			fresh = append(fresh, attempt)
		}
	}
	return fresh
}

// Middleware 返回记录失败与检查限流的 Gin 中间件。
// 注意：中间件只负责"检查是否被限流"，失败计数由 AuthHandler 显式调用。
func (l *LoginRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow(ClientIP(c)) {
			utils.Error(c, utils.CodeTooManyRequests, "登录请求过于频繁，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}

// ClientIP 返回经 Gin 可信代理处理的客户端 IP。
func ClientIP(c *gin.Context) string {
	ip := c.ClientIP()
	if ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return "unknown"
}
