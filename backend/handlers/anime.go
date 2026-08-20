package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/models"
	"fan-web/services"
	"fan-web/utils"
)

type AnimeHandler struct {
	bangumi     *services.BangumiService
	scanner     *services.ScannerService
	coverClient *http.Client
	coverSem    chan struct{}
	coverLimit  *coverProxyLimiter
}

func NewAnimeHandler(bangumi *services.BangumiService, scanner *services.ScannerService) *AnimeHandler {
	return &AnimeHandler{
		bangumi:     bangumi,
		scanner:     scanner,
		coverClient: newCoverClient(),
		coverSem:    make(chan struct{}, maxCoverConcurrency),
		coverLimit:  &coverProxyLimiter{},
	}
}

// newCoverClient 构造封面代理客户端：每次重定向都重新校验目标主机，最多 3 次。
func newCoverClient() *http.Client {
	client := &http.Client{Timeout: 15 * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxCoverRedirects {
			return fmt.Errorf("封面代理重定向次数超限")
		}
		if !isTrustedCoverURL(req.URL) {
			return fmt.Errorf("封面代理指向非信任主机")
		}
		return nil
	}
	return client
}

// normalizedImageType 解析并规范化允许的 JPEG/PNG/WebP/GIF 类型。
func normalizedImageType(contentType string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", false
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "image/pjpeg" {
		mediaType = "image/jpeg"
	}
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return mediaType, true
	default:
		return "", false
	}
}

func (h *AnimeHandler) List(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	animes, total, err := database.ListAnimes(page, pageSize, keyword, userID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询番剧列表失败")
		return
	}
	utils.Success(c, gin.H{
		"items":     animes,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *AnimeHandler) Get(c *gin.Context) {
	id, ok := animeID(c)
	if !ok {
		return
	}
	anime, err := database.GetAnimeByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询番剧失败")
		return
	}
	utils.Success(c, anime)
}

// Cover proxies trusted Bangumi cover URLs so mobile clients only need to
// reach the configured server, not the external image host directly.
const (
	maxCoverBytes        = 10 << 20 // 10 MiB
	maxCoverRedirects    = 3
	maxCoverConcurrency  = 4
	coverRateLimitWindow = time.Minute
	coverRateLimitMax    = 120
)

// coverProxyLimiter 封面代理的固定窗口速率限制（按处理器实例），
// 防止任意登录用户对第三方 CDN 产生出站流量放大。
type coverProxyLimiter struct {
	mu     sync.Mutex
	window time.Time
	count  int
}

func (l *coverProxyLimiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.window) >= coverRateLimitWindow {
		l.window = now
		l.count = 0
	}
	if l.count >= coverRateLimitMax {
		return false
	}
	l.count++
	return true
}

func (h *AnimeHandler) Cover(c *gin.Context) {
	id, ok := animeID(c)
	if !ok {
		return
	}
	anime, err := database.GetAnimeByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusInternalServerError)
		return
	}
	coverURL, err := url.Parse(strings.TrimSpace(anime.Cover))
	if err != nil || !isTrustedCoverURL(coverURL) {
		c.Status(http.StatusNotFound)
		return
	}

	// 速率限制与并发上限：防止任意登录用户并发请求造成内存与出站流量放大。
	if h.coverLimit != nil && !h.coverLimit.allow(time.Now()) {
		c.Status(http.StatusTooManyRequests)
		return
	}
	if h.coverSem != nil {
		select {
		case h.coverSem <- struct{}{}:
			defer func() { <-h.coverSem }()
		default:
			c.Status(http.StatusTooManyRequests)
			return
		}
	}

	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, coverURL.String(), nil)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	request.Header.Set("User-Agent", "fan-web/1.0 (private anime library)")
	client := h.coverClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		c.Status(http.StatusBadGateway)
		return
	}

	declaredType, ok := normalizedImageType(response.Header.Get("Content-Type"))
	if !ok {
		c.Status(http.StatusUnsupportedMediaType)
		return
	}

	if response.ContentLength > maxCoverBytes {
		c.Status(http.StatusBadGateway)
		return
	}

	// 必须先完整校验再返回，避免已发送 200 后才发现超限或类型伪造。
	limited := io.LimitReader(response.Body, maxCoverBytes+1)
	body, readErr := io.ReadAll(limited)
	if readErr != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	if len(body) > maxCoverBytes {
		c.Status(http.StatusBadGateway)
		return
	}
	detectedType, ok := normalizedImageType(http.DetectContentType(body))
	if !ok || detectedType != declaredType {
		c.Status(http.StatusUnsupportedMediaType)
		return
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, detectedType, body)
}

func isTrustedCoverURL(coverURL *url.URL) bool {
	if coverURL == nil || (coverURL.Scheme != "https" && coverURL.Scheme != "http") {
		return false
	}
	host := strings.ToLower(coverURL.Hostname())
	return host == "lain.bgm.tv" || strings.HasSuffix(host, ".bgm.tv")
}

type createAnimeRequest struct {
	BangumiID int    `json:"bangumi_id"`
	FilePath  string `json:"file_path"`
}

func (h *AnimeHandler) Create(c *gin.Context) {
	var request createAnimeRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.BangumiID <= 0 {
		utils.Error(c, utils.CodeInvalidParams, "bangumi_id 必须大于 0")
		return
	}
	request.FilePath = strings.TrimSpace(request.FilePath)
	if err := services.ValidateRelativeVideoPath(request.FilePath); err != nil {
		utils.Error(c, utils.CodeInvalidParams, err.Error())
		return
	}

	existing, err := database.GetAnimeByBangumiID(request.BangumiID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询已有番剧失败")
		return
	}
	if existing != nil && existing.FilePath != request.FilePath {
		utils.Error(c, utils.CodeInvalidParams, "番剧已存在但目录不同")
		return
	}
	if existing != nil {
		utils.Success(c, existing)
		return
	}

	subject, err := h.bangumi.GetSubject(request.BangumiID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "获取 Bangumi 数据失败: "+err.Error())
		return
	}
	anime, err := database.CreateAnime(&models.Anime{
		Title:     subject.Name,
		TitleCn:   subject.NameCn,
		BangumiID: subject.ID,
		Cover:     subject.Cover,
		Summary:   subject.Summary,
		EpCount:   subject.TotalEpisodes,
		FilePath:  request.FilePath,
	})
	if err != nil {
		utils.Error(c, utils.CodeInternal, "创建番剧失败")
		return
	}
	if anime.FilePath != request.FilePath {
		utils.Error(c, utils.CodeInvalidParams, "番剧已存在但目录不同")
		return
	}
	utils.Success(c, anime)
}

type updateAnimeRequest struct {
	Title    string `json:"title"`
	TitleCn  string `json:"title_cn"`
	Summary  string `json:"summary"`
	EpCount  int    `json:"ep_count"`
	FilePath string `json:"file_path"`
}

func (h *AnimeHandler) Update(c *gin.Context) {
	id, ok := animeID(c)
	if !ok {
		return
	}
	var request updateAnimeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.Error(c, utils.CodeInvalidParams, "请求参数错误")
		return
	}
	request.Title = strings.TrimSpace(request.Title)
	request.TitleCn = strings.TrimSpace(request.TitleCn)
	request.FilePath = strings.TrimSpace(request.FilePath)
	if request.Title == "" || request.EpCount < 0 {
		utils.Error(c, utils.CodeInvalidParams, "标题不能为空且总集数不能为负数")
		return
	}
	if err := services.ValidateRelativeVideoPath(request.FilePath); err != nil {
		utils.Error(c, utils.CodeInvalidParams, err.Error())
		return
	}

	if err := database.UpdateAnime(&models.Anime{
		ID: id, Title: request.Title, TitleCn: request.TitleCn,
		Summary: request.Summary, EpCount: request.EpCount, FilePath: request.FilePath,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "更新番剧失败")
		return
	}
	utils.Success(c, nil)
}

func (h *AnimeHandler) Delete(c *gin.Context) {
	id, ok := animeID(c)
	if !ok {
		return
	}
	if err := database.DeleteAnime(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "删除番剧失败")
		return
	}
	utils.Success(c, nil)
}

func (h *AnimeHandler) Scan(c *gin.Context) {
	id, ok := animeID(c)
	if !ok {
		return
	}
	anime, err := database.GetAnimeByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询番剧失败")
		return
	}
	episodes, err := h.scanner.Scan(anime.FilePath)
	if err != nil {
		utils.Error(c, utils.CodeInternal, err.Error())
		return
	}

	existingEpisodes, err := database.ListEpisodesByAnimeID(id)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "读取已有集数失败")
		return
	}
	if len(existingEpisodes) > 0 && len(episodes) == 0 {
		utils.Error(c, utils.CodeInternal, "未扫描到有效视频，已保留原有剧集")
		return
	}

	if err := database.SyncEpisodes(id, episodes); err != nil {
		utils.Error(c, utils.CodeInternal, "保存集数失败: "+err.Error())
		return
	}
	storedEpisodes, err := database.ListEpisodesByAnimeID(id)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "读取已保存集数失败")
		return
	}
	if len(storedEpisodes) > 0 {
		if err := database.DeleteUnidentifiedByDir(anime.FilePath); err != nil {
			log.Printf("[Anime] 清除目录 %q 的未识别文件失败: %v", anime.FilePath, err)
		}
	}
	utils.Success(c, gin.H{"scanned": len(storedEpisodes), "episodes": storedEpisodes})
}

func (h *AnimeHandler) Episodes(c *gin.Context) {
	id, ok := animeID(c)
	if !ok {
		return
	}
	if _, err := database.GetAnimeByID(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询番剧失败")
		return
	}
	episodes, err := database.ListEpisodesByAnimeID(id)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询集数列表失败")
		return
	}
	utils.Success(c, episodes)
}

func animeID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		utils.Error(c, utils.CodeInvalidParams, "无效的番剧 ID")
		return 0, false
	}
	return id, true
}
