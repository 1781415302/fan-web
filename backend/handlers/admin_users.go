package handlers

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/services"
	"fan-web/utils"
)

type AdminUserHandler struct{}

func NewAdminUserHandler() *AdminUserHandler {
	return &AdminUserHandler{}
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AdminUserHandler) List(c *gin.Context) {
	users, err := database.ListUsers()
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询用户列表失败")
		return
	}
	utils.Success(c, users)
}

func (h *AdminUserHandler) Create(c *gin.Context) {
	var request createUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.Error(c, utils.CodeInvalidParams, "请求参数错误")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if err := services.ValidateNewCredentials(request.Username, request.Password); err != nil {
		utils.Error(c, utils.CodeInvalidParams, err.Error())
		return
	}

	user, err := database.CreateUser(request.Username, request.Password, false)
	if err != nil {
		if errors.Is(err, database.ErrUsernameExists) {
			utils.Error(c, utils.CodeUsernameExists, "用户名已存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "创建用户失败")
		return
	}
	utils.Success(c, user)
}

func (h *AdminUserHandler) Delete(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		utils.Error(c, utils.CodeInvalidParams, "用户 ID 无效")
		return
	}

	currentUserID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	if currentUserID == userID {
		utils.Error(c, utils.CodeForbidden, "不能删除当前登录用户")
		return
	}

	target, err := database.GetUserByID(userID)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.Error(c, utils.CodeNotFound, "用户不存在")
		} else {
			utils.Error(c, utils.CodeInternal, "查询用户失败")
		}
		return
	}
	if target.IsAdmin {
		adminCount, err := database.CountAdmins()
		if err != nil {
			utils.Error(c, utils.CodeInternal, "查询管理员数量失败")
			return
		}
		if adminCount <= 1 {
			utils.Error(c, utils.CodeForbidden, "至少需要保留一名管理员")
			return
		}
	}

	if err := database.DeleteUser(userID); err != nil {
		if err == sql.ErrNoRows {
			utils.Error(c, utils.CodeNotFound, "用户不存在")
		} else {
			utils.Error(c, utils.CodeInternal, "删除用户失败")
		}
		return
	}
	utils.Success(c, nil)
}
