package handlers

import (
	"net/http"

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
			},
		})
		return
	}
	utils.Success(c, result)
}

func (h *UpdateHandler) Perform(c *gin.Context) {
	cv := h.currentVersion()
	if err := services.PerformUpdate(cv); err != nil {
		msg := err.Error()
		if msg == "已是最新版本" {
			utils.Error(c, utils.CodeInvalidParams, msg)
			return
		}
		utils.Error(c, utils.CodeInternal, msg)
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
