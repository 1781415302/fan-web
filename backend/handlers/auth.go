package handlers

import (
	"database/sql"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/services"
	"fan-web/utils"
)

type AuthHandler struct {
	authService *services.AuthService
	rateLimiter *middleware.LoginRateLimiter
}

func NewAuthHandler(authService *services.AuthService, rateLimiter *middleware.LoginRateLimiter) *AuthHandler {
	return &AuthHandler{authService: authService, rateLimiter: rateLimiter}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// dummyPasswordHash 是固定预计算的 bcrypt 哈希（cost=10，与用户密码哈希成本一致）。
// 用户名不存在时也对其执行一次等价的 bcrypt 比较，抹平“用户不存在”与“密码错误”
// 两条路径的响应耗时差异，避免通过时序差异枚举有效用户名。
var dummyPasswordHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

func (h *AuthHandler) Login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.Error(c, utils.CodeInvalidParams, "请求参数错误")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" || request.Password == "" {
		utils.Error(c, utils.CodeInvalidParams, "用户名和密码不能为空")
		return
	}

	// 本次尝试的额度已由限流中间件的 Allow 原子计入，失败时无需再重复计数。
	user, err := database.GetUserByUsername(request.Username)
	if err != nil {
		if err == sql.ErrNoRows {
			// 用户不存在时对固定 dummy 哈希执行一次等价的 bcrypt 比较，
			// 使两条失败路径的耗时一致，避免账号枚举时序侧信道。
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(request.Password))
			utils.Error(c, utils.CodeLoginFailed, "用户名或密码错误")
			return
		}
		h.rateLimiter.Reset(middleware.ClientIP(c))
		utils.Error(c, utils.CodeInternal, "查询用户失败")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(request.Password)) != nil {
		utils.Error(c, utils.CodeLoginFailed, "用户名或密码错误")
		return
	}

	h.rateLimiter.Reset(middleware.ClientIP(c))

	token, expiresAt, err := h.authService.IssueToken(*user)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "生成登录凭证失败")
		return
	}

	utils.Success(c, gin.H{
		"token":      token,
		"expires_at": expiresAt,
		"user":       user,
	})
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}

	user, err := database.GetUserByID(userID)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.Error(c, utils.CodeUnauthenticated, "登录状态已失效")
		} else {
			utils.Error(c, utils.CodeInternal, "查询用户失败")
		}
		return
	}
	utils.Success(c, user)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	utils.Success(c, nil)
}
