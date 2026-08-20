package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/services"
	"fan-web/utils"
)

type LibraryHandler struct {
	library *services.LibraryService
	job     *services.LibraryJob
}

func NewLibraryHandler(library *services.LibraryService) *LibraryHandler {
	return &LibraryHandler{
		library: library,
		job:     services.NewLibraryJob(library),
	}
}

func (h *LibraryHandler) Scan(c *gin.Context) {
	utils.Success(c, h.job.Start())
}

func (h *LibraryHandler) Status(c *gin.Context) {
	utils.Success(c, h.job.Snapshot())
}

func (h *LibraryHandler) Unidentified(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	items, total, err := database.ListUnidentified(page, pageSize)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询未识别文件失败")
		return
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	} else if pageSize > 100 {
		pageSize = 100
	}
	utils.Success(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *LibraryHandler) Dirs(c *gin.Context) {
	items, err := services.NewScannerService(h.library.RootPath()).ListSubDirs()
	if err != nil {
		utils.Error(c, utils.CodeInternal, "读取目录失败")
		return
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			out = append(out, item)
		}
	}
	utils.Success(c, gin.H{"items": out})
}
