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

func (h *AuthHandler) Login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.rateLimiter.RecordFailure(middleware.ClientIP(c))
		utils.Error(c, utils.CodeInvalidParams, "请求参数错误")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" || request.Password == "" {
		h.rateLimiter.RecordFailure(middleware.ClientIP(c))
		utils.Error(c, utils.CodeInvalidParams, "用户名和密码不能为空")
		return
	}

	user, err := database.GetUserByUsername(request.Username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(request.Password)) != nil {
		h.rateLimiter.RecordFailure(middleware.ClientIP(c))
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
