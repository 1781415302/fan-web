package handlers

import (
	"errors"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"fan-web/config"
	"fan-web/database"
	"fan-web/services"
	"fan-web/utils"
)

type SetupHandler struct {
	mu          sync.RWMutex
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

func (h *SetupHandler) IsConfigured() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg.Configured
}

// Status 返回系统是否已完成初始化。
func (h *SetupHandler) Status(c *gin.Context) {
	utils.Success(c, gin.H{"configured": h.IsConfigured()})
}

type setupRequest struct {
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
	VideoRootPath string `json:"video_root_path"`
	Port          int    `json:"port"`
}

// Submit 完成首次初始化：创建管理员、写入配置。
func (h *SetupHandler) Submit(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

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

	// 校验完成后，在提交任何修改前再做一次配置写入可用性预检，
	// 避免在数据库已创建管理员后才暴露配置目录问题。
	if err := h.cfg.PreflightSave(h.configPath); err != nil {
		utils.Error(c, utils.CodeInternal, "保存配置失败: "+err.Error())
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
	createdUserID := user.ID

	// 只修改配置副本。任何后续失败时回滚，不污染 h.cfg。
	nextConfig := *h.cfg
	nextConfig.Admin.Username = request.AdminUsername
	nextConfig.Admin.Password = request.AdminPassword
	nextConfig.Video.RootPath = request.VideoRootPath
	if request.Port != 0 {
		nextConfig.Server.Port = request.Port
	}

	token, expiresAt, err := h.authService.IssueToken(*user)
	if err != nil {
		h.rollbackCreatedUser(c, createdUserID, err, "生成登录凭证失败")
		return
	}

	if err := nextConfig.Save(h.configPath); err != nil {
		rollbackErr := deleteCreatedUser(createdUserID)
		if rollbackErr != nil {
			log.Printf("初始化失败：保存配置出错 %v，且管理员回滚失败（用户 ID %d）: %v", err, createdUserID, rollbackErr)
			utils.Error(c, utils.CodeInternal, "保存配置失败: "+err.Error()+"（管理员回滚失败，请检查数据库）")
			return
		}
		utils.Error(c, utils.CodeInternal, "保存配置失败: "+err.Error())
		return
	}

	// 所有成功后才整体提交到内存状态。
	h.cfg.Admin = nextConfig.Admin
	h.cfg.Video = nextConfig.Video
	h.cfg.Server.Port = nextConfig.Server.Port
	h.cfg.Configured = true

	h.scanner.SetRootPath(request.VideoRootPath)
	if h.library != nil {
		h.library.SetRootPath(request.VideoRootPath)
	}

	utils.Success(c, gin.H{
		"token":      token,
		"expires_at": expiresAt,
		"user":       user,
	})
}

// rollbackCreatedUser 在签发 token 失败时删除本次创建的用户。
func (h *SetupHandler) rollbackCreatedUser(c *gin.Context, userID int64, cause error, message string) {
	rollbackErr := deleteCreatedUser(userID)
	if rollbackErr != nil {
		log.Printf("初始化失败：%v，且管理员回滚失败（用户 ID %d）: %v", cause, userID, rollbackErr)
		utils.Error(c, utils.CodeInternal, message+"（管理员回滚失败，请检查数据库）")
		return
	}
	utils.Error(c, utils.CodeInternal, message)
}

// deleteCreatedUser 仅允许删除本次创建后拿到的精确用户 ID，禁止按用户名删除。
func deleteCreatedUser(userID int64) error {
	if userID <= 0 {
		return nil
	}
	return database.DeleteUser(userID)
}
