package handlers

import (
	"github.com/gin-gonic/gin"

	"fan-web/services"
	"fan-web/utils"
)

type LibraryHandler struct {
	library *services.LibraryService
}

func NewLibraryHandler(library *services.LibraryService) *LibraryHandler {
	return &LibraryHandler{library: library}
}

func (h *LibraryHandler) Scan(c *gin.Context) {
	result, err := h.library.Scan()
	if err != nil {
		utils.Error(c, utils.CodeInternal, "库扫描失败: "+err.Error())
		return
	}
	utils.Success(c, result)
}
