package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"fan-web/services"
	"fan-web/utils"
)

type UpdateHandler struct {
	version func() string
}

func NewUpdateHandler(version string) *UpdateHandler {
	v := version
	if v == "" {
		v = "dev"
	}
	return &UpdateHandler{version: func() string { return v }}
}

func NewUpdateHandlerWithFunc(fn func() string) *UpdateHandler {
	if fn == nil {
		fn = func() string { return "dev" }
	}
	return &UpdateHandler{version: fn}
}

func (h *UpdateHandler) currentVersion() string {
	if h.version == nil {
		return "dev"
	}
	return h.version()
}

func (h *UpdateHandler) Check(c *gin.Context) {
	cv := h.currentVersion()
	result, err := services.CheckUpdate(cv)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "ok",
			"data": gin.H{
				"has_update":      false,
				"current_version": cv,
				"latest_version":  "",
				"release_notes":   "",
				"error":           "无法连接更新服务器",
				"stale_old":       services.HasStaleUpdateBackup(),
			},
		})
		return
	}
	utils.Success(c, result)
}

// alreadyLatestMsg 与 services.PerformUpdate 返回的“已是最新版本”措辞保持一致。
// 若将来 services 改为返回哨兵错误（如 services.ErrAlreadyLatest），
// 此处应改用 errors.Is(err, services.ErrAlreadyLatest) 以彻底解耦，避免措辞漂移。
const alreadyLatestMsg = "已是最新版本"

func (h *UpdateHandler) Perform(c *gin.Context) {
	cv := h.currentVersion()
	if err := services.PerformUpdate(cv); err != nil {
		msg := strings.TrimSpace(err.Error())
		if msg == alreadyLatestMsg {
			utils.Error(c, utils.CodeInvalidParams, err.Error())
			return
		}
		utils.Error(c, utils.CodeInternal, err.Error())
		return
	}
	utils.Success(c, gin.H{
		"message": "更新完成，正在重启...",
		"hint":    "如果未使用进程管理器（systemd/nohup），请手动重启服务",
	})
}

func (h *UpdateHandler) Version(c *gin.Context) {
	utils.Success(c, gin.H{"version": h.currentVersion()})
}
