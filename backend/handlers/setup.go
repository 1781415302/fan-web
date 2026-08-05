package handlers

import (
	"errors"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"fan-web/config"
	"fan-web/database"
	"fan-web/services"
	"fan-web/utils"
)

type SetupHandler struct {
	configPath  string
	cfg         *config.Config
	authService *services.AuthService
	scanner     *services.ScannerService
	library     *services.LibraryService
}

func NewSetupHandler(configPath string, cfg *config.Config, authService *services.AuthService, scanner *services.ScannerService, library *services.LibraryService) *SetupHandler {
	return &SetupHandler{
		configPath:  configPath,
		cfg:         cfg,
		authService: authService,
		scanner:     scanner,
		library:     library,
	}
}

// Status 返回系统是否已完成初始化。
func (h *SetupHandler) Status(c *gin.Context) {
	utils.Success(c, gin.H{"configured": h.cfg.Configured})
}

type setupRequest struct {
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
	VideoRootPath string `json:"video_root_path"`
	Port          int    `json:"port"`
}

// Submit 完成首次初始化：创建管理员、写入配置。
func (h *SetupHandler) Submit(c *gin.Context) {
	if h.cfg.Configured {
		utils.Error(c, utils.CodeForbidden, "系统已初始化")
		return
	}

	var request setupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.Error(c, utils.CodeInvalidParams, "请求参数错误")
		return
	}
	request.AdminUsername = strings.TrimSpace(request.AdminUsername)
	request.VideoRootPath = strings.TrimSpace(request.VideoRootPath)

	if request.AdminUsername == "" || request.AdminPassword == "" {
		utils.Error(c, utils.CodeInvalidParams, "用户名和密码不能为空")
		return
	}
	if request.VideoRootPath == "" {
		utils.Error(c, utils.CodeInvalidParams, "视频根目录不能为空")
		return
	}
	if request.Port != 0 && (request.Port < 1 || request.Port > 65535) {
		utils.Error(c, utils.CodeInvalidParams, "端口号无效")
		return
	}

	info, err := os.Stat(request.VideoRootPath)
	if err != nil || !info.IsDir() {
		utils.Error(c, utils.CodeInvalidParams, "视频根目录不存在或不是目录")
		return
	}

	user, err := database.CreateUser(request.AdminUsername, request.AdminPassword, true)
	if err != nil {
		if errors.Is(err, database.ErrUsernameExists) {
			utils.Error(c, utils.CodeUsernameExists, "用户名已存在")
		} else {
			utils.Error(c, utils.CodeInternal, "创建管理员失败")
		}
		return
	}

	h.cfg.Admin.Username = request.AdminUsername
	h.cfg.Admin.Password = request.AdminPassword
	h.cfg.Video.RootPath = request.VideoRootPath
	if request.Port != 0 {
		h.cfg.Server.Port = request.Port
	}
	if err := h.cfg.Save(h.configPath); err != nil {
		utils.Error(c, utils.CodeInternal, "保存配置失败: "+err.Error())
		return
	}
	h.cfg.Configured = true

	h.scanner.SetRootPath(request.VideoRootPath)
	if h.library != nil {
		h.library.SetRootPath(request.VideoRootPath)
	}

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
