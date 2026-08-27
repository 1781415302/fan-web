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
	scanner *services.ScannerService
}

func NewLibraryHandler(library *services.LibraryService) *LibraryHandler {
	return &LibraryHandler{
		library: library,
		job:     services.NewLibraryJob(library),
		scanner: services.NewScannerService(library.RootPath()),
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
	// 分页的下限/上限裁剪已统一收敛到 database.ListUnidentified，
	// handler 不再重复裁剪，避免两层策略漂移；此处原样回显请求的分页参数。
	utils.Success(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *LibraryHandler) Dirs(c *gin.Context) {
	// 复用缓存的 ScannerService，避免每次请求新建；同步最新根目录以反映初始化后的变更。
	h.scanner.SetRootPath(h.library.RootPath())
	items, err := h.scanner.ListSubDirs()
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
