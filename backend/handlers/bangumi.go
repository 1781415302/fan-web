package handlers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"fan-web/services"
	"fan-web/utils"
)

type BangumiHandler struct {
	service *services.BangumiService
}

func NewBangumiHandler(service *services.BangumiService) *BangumiHandler {
	return &BangumiHandler{service: service}
}

func (h *BangumiHandler) Search(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	if keyword == "" || len([]rune(keyword)) > 100 {
		utils.Error(c, utils.CodeInvalidParams, "搜索关键词不能为空且不能超过 100 个字符")
		return
	}
	results, err := h.service.Search(keyword)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "搜索失败: "+err.Error())
		return
	}
	utils.Success(c, results)
}

func (h *BangumiHandler) Subject(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, utils.CodeInvalidParams, "无效的条目 ID")
		return
	}
	subject, err := h.service.GetSubject(id)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "获取条目失败: "+err.Error())
		return
	}
	utils.Success(c, subject)
}
