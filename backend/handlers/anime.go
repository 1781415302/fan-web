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
	"fan-web/services/matching"
	"fan-web/utils"
)

type AnimeHandler struct {
	bangumi     *services.BangumiService
	scanner     *services.ScannerService
	coverClient *http.Client
	coverSem    chan struct{}
	coverLimit  *coverProxyLimiter
	// confirmLocks 收件箱确认按 bangumi_id 的进程内互斥：int → *sync.Mutex。
	confirmLocks sync.Map
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
		utils.Error(c, utils.CodeInvalidParams, animeDirMismatchMessage)
		return
	}
	if existing != nil {
		utils.Success(c, existing)
		return
	}

	anime, err := h.createAnimeFromBangumi(request.BangumiID, request.FilePath)
	if err != nil {
		utils.Error(c, utils.CodeInternal, createAnimeErrorMessage(err))
		return
	}
	// 仅在并发 TOCTOU 竞态时触发：并发请求于两次查询之间插入了相同 bangumi_id
	// 但不同路径的记录，CreateAnime 的唯一索引回退返回既有行；正常流程不可达。
	if anime.FilePath != request.FilePath {
		utils.Error(c, utils.CodeInvalidParams, animeDirMismatchMessage)
		return
	}
	utils.Success(c, anime)
}

// animeDirMismatchMessage 同一 bangumi_id 已绑定其它目录时的统一文案（Create 与 Confirm 共用）。
const animeDirMismatchMessage = "番剧已存在但目录不同"

var (
	errCreateAnimeSubject = errors.New("获取 Bangumi 数据失败")
	errCreateAnimeInsert  = errors.New("创建番剧失败")
)

// createAnimeFromBangumi 按 Bangumi 条目建档（Create 与 Confirm 共用）。
// 返回值可能是唯一索引回退得到的既有行，调用方不得据此判断是否新建。
func (h *AnimeHandler) createAnimeFromBangumi(bangumiID int, filePath string) (*models.Anime, error) {
	anime, _, err := h.createAnimeFromBangumiReportInserted(bangumiID, filePath)
	return anime, err
}

// createAnimeFromBangumiReportInserted 同 createAnimeFromBangumi，额外返回是否真正插入了新行
// （命中唯一索引回退到既有记录时为 false）。
func (h *AnimeHandler) createAnimeFromBangumiReportInserted(bangumiID int, filePath string) (*models.Anime, bool, error) {
	subject, err := h.bangumi.GetSubject(bangumiID)
	if err != nil {
		log.Printf("[Anime] 获取 Bangumi 条目 %d 失败: %v", bangumiID, err)
		return nil, false, fmt.Errorf("%w: %v", errCreateAnimeSubject, err)
	}
	anime, inserted, err := database.CreateAnimeReportInserted(&models.Anime{
		Title:     subject.Name,
		TitleCn:   subject.NameCn,
		BangumiID: subject.ID,
		Cover:     subject.Cover,
		Summary:   subject.Summary,
		EpCount:   subject.TotalEpisodes,
		FilePath:  filePath,
	})
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", errCreateAnimeInsert, err)
	}
	return anime, inserted, nil
}

func createAnimeErrorMessage(err error) string {
	if errors.Is(err, errCreateAnimeSubject) {
		return errCreateAnimeSubject.Error()
	}
	return errCreateAnimeInsert.Error()
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
	if h.scanner.MatchEngine() == matching.EngineV2 {
		h.scanV2(c, anime)
		return
	}
	h.scanLegacy(c, anime)
}

// scanLegacy 保持原 Scan+SyncEpisodes+整目录清收件箱语义。
func (h *AnimeHandler) scanLegacy(c *gin.Context, anime *models.Anime) {
	episodes, err := h.scanner.Scan(anime.FilePath)
	if err != nil {
		log.Printf("[Anime] 扫描目录 %q 失败: %v", anime.FilePath, err)
		utils.Error(c, utils.CodeInternal, "扫描番剧目录失败")
		return
	}

	existingEpisodes, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "读取已有集数失败")
		return
	}
	if len(existingEpisodes) > 0 && len(episodes) == 0 {
		utils.Error(c, utils.CodeInternal, "未扫描到有效视频，已保留原有剧集")
		return
	}

	if err := database.SyncEpisodes(anime.ID, episodes); err != nil {
		log.Printf("[Anime] 保存番剧 %d 集数失败: %v", anime.ID, err)
		utils.Error(c, utils.CodeInternal, "保存集数失败")
		return
	}
	storedEpisodes, err := database.ListEpisodesByAnimeID(anime.ID)
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

// scanV2：runScanV2（ScanForAnime → ReconcileEpisodes(RemoveMissing=true)）→ 按已保存文件精确清收件箱。
func (h *AnimeHandler) scanV2(c *gin.Context, anime *models.Anime) {
	storedEpisodes, err := h.runScanV2(anime)
	if err != nil {
		var step *scanV2StepError
		if errors.As(err, &step) {
			utils.Error(c, utils.CodeInternal, step.msg)
			return
		}
		utils.Error(c, utils.CodeInternal, scanV2ErrorMessage(err))
		return
	}
	names := storedEpisodeFileNames(storedEpisodes)
	if len(names) > 0 {
		if err := database.DeleteUnidentifiedFiles(anime.FilePath, names); err != nil {
			log.Printf("[Anime] 精确清除目录 %q 未识别失败: %v", anime.FilePath, err)
		}
	}
	utils.Success(c, gin.H{"scanned": len(storedEpisodes), "episodes": storedEpisodes})
}

// scanV2StepError 读库步骤失败（非匹配/对账错误），msg 为 Scan 路径原有的用户文案。
type scanV2StepError struct {
	msg string
	err error
}

func (e *scanV2StepError) Error() string { return e.msg + ": " + e.err.Error() }
func (e *scanV2StepError) Unwrap() error { return e.err }

// runScanV2 单番 v2 扫描核心：先读 existing（原样作为 Expected）→ ScanForAnime(opts) →
// ReconcileEpisodes(RemoveMissing=true) → 返回已保存剧集。不清收件箱，由调用方决定。
func (h *AnimeHandler) runScanV2(anime *models.Anime, opts ...services.ScanOption) ([]models.Episode, error) {
	plan, err := h.prepareScanV2(anime, opts...)
	if err != nil {
		return nil, err
	}
	return h.commitScanV2(plan)
}

// scanV2Plan 单番 v2 扫描的待提交结果：prepareScanV2 只读，commitScanV2 才写库。
type scanV2Plan struct {
	animeID       int64
	expectedAnime models.Anime
	existing      []models.Episode
	inv           services.EpisodeInventory
}

// prepareScanV2 读取快照并生成 Inventory，不写数据库。
func (h *AnimeHandler) prepareScanV2(anime *models.Anime, opts ...services.ScanOption) (scanV2Plan, error) {
	existing, err := database.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		return scanV2Plan{}, &scanV2StepError{msg: "读取已有集数失败", err: err}
	}
	dirPeers, err := database.ListAnimesByFilePath(anime.FilePath)
	if err != nil {
		return scanV2Plan{}, &scanV2StepError{msg: "读取同目录番剧失败", err: err}
	}
	peers := make([]models.Anime, 0, len(dirPeers))
	for _, peer := range dirPeers {
		if peer.ID != anime.ID {
			peers = append(peers, peer)
		}
	}

	expectedAnime := *anime
	inv, err := h.scanner.ScanForAnime(*anime, existing, peers, opts...)
	if err != nil {
		log.Printf("[Anime] v2 扫描目录 %q 失败: %v", anime.FilePath, err)
		return scanV2Plan{}, err
	}
	return scanV2Plan{animeID: anime.ID, expectedAnime: expectedAnime, existing: existing, inv: inv}, nil
}

// commitScanV2 以快照 CAS 在单个事务内提交 Inventory，并返回提交后的剧集。
func (h *AnimeHandler) commitScanV2(plan scanV2Plan) ([]models.Episode, error) {
	if err := database.ReconcileEpisodes(plan.animeID, database.EpisodeReconcileInput{
		ExpectedAnime: plan.expectedAnime,
		Expected:      plan.existing,
		Desired:       plan.inv.Desired,
		PresentFiles:  plan.inv.PresentFiles,
		Renames:       plan.inv.Renames,
		RemoveMissing: true,
	}); err != nil {
		log.Printf("[Anime] v2 保存番剧 %d 集数失败: %v", plan.animeID, err)
		return nil, err
	}

	storedEpisodes, err := database.ListEpisodesByAnimeID(plan.animeID)
	if err != nil {
		return nil, &scanV2StepError{msg: "读取已保存集数失败", err: err}
	}
	return storedEpisodes, nil
}

func storedEpisodeFileNames(episodes []models.Episode) []string {
	names := make([]string, 0, len(episodes))
	for _, ep := range episodes {
		if strings.TrimSpace(ep.FilePath) == "" {
			continue
		}
		names = append(names, ep.FilePath)
	}
	return names
}

// 收件箱确认失败 reason（与前端 ConfirmErrorData.reason 一致）。
const (
	confirmReasonReviewRequired    = "review_required"
	confirmReasonUpstream          = "upstream"
	confirmReasonConflict          = "conflict"
	confirmReasonDirMismatch       = "dir_mismatch"
	confirmReasonNoMatch           = "no_match"
	confirmReasonInvalid           = "invalid"
	confirmReasonUnsupportedEngine = "unsupported_engine"
)

type confirmInboxRequest struct {
	BangumiID     int      `json:"bangumi_id"`
	FilePath      string   `json:"file_path"`
	FileNames     []string `json:"file_names"`
	EpisodeOffset int      `json:"episode_offset"`
}

type confirmErrorData struct {
	Reason    string   `json:"reason"`
	Message   string   `json:"message"`
	FileNames []string `json:"file_names,omitempty"`
}

func confirmFail(c *gin.Context, code int, reason, message string, fileNames []string) {
	utils.ErrorWithData(c, code, message, confirmErrorData{Reason: reason, Message: message, FileNames: fileNames})
}

// normalizeConfirmDir 目录比较前统一分隔符（与 IsFileAssociated 同做法）。
func normalizeConfirmDir(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func validateConfirmFileName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	return !strings.ContainsAny(name, "/\\\x00")
}

func (h *AnimeHandler) confirmLock(bangumiID int) *sync.Mutex {
	v, _ := h.confirmLocks.LoadOrStore(bangumiID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Confirm POST /api/library/inbox/confirm：收件箱点击确认的原子操作。
// 按 bangumi_id 加进程内互斥 → 查/建番剧 → prepareScanV2(WithOnlyFiles, WithEpisodeOffset)（只读）
// → 校验 file_names 全部可归入 → commitScanV2 原子提交 → 精确清收件箱；失败时回滚本请求新建的番剧，created=false 不删任何数据。
func (h *AnimeHandler) Confirm(c *gin.Context) {
	var request confirmInboxRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "请求参数错误", nil)
		return
	}
	if request.BangumiID <= 0 {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "bangumi_id 必须大于 0", nil)
		return
	}
	request.FilePath = strings.TrimSpace(request.FilePath)
	if err := services.ValidateRelativeVideoPath(request.FilePath); err != nil {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, err.Error(), nil)
		return
	}
	if len(request.FileNames) == 0 {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "file_names 不能为空", nil)
		return
	}
	for _, name := range request.FileNames {
		if !validateConfirmFileName(name) {
			confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, fmt.Sprintf("文件名不合法: %q", name), nil)
			return
		}
	}
	if request.EpisodeOffset < 0 {
		confirmFail(c, utils.CodeInvalidParams, confirmReasonInvalid, "episode_offset 不能为负数", nil)
		return
	}
	// 建档之前判断引擎：legacy / 无 MatchingRuntime 不落库。
	if h.scanner.MatchEngine() != matching.EngineV2 {
		confirmFail(c, utils.CodeInternal, confirmReasonUnsupportedEngine, "当前匹配引擎不支持收件箱确认，请使用扫描", nil)
		return
	}

	lock := h.confirmLock(request.BangumiID)
	lock.Lock()
	defer lock.Unlock()

	existing, err := database.GetAnimeByBangumiID(request.BangumiID)
	if err != nil {
		log.Printf("[Anime] 收件箱确认查询番剧 %d 失败: %v", request.BangumiID, err)
		confirmFail(c, utils.CodeInternal, confirmReasonUpstream, "查询已有番剧失败", nil)
		return
	}
	var anime *models.Anime
	created := false
	if existing != nil {
		if normalizeConfirmDir(existing.FilePath) != normalizeConfirmDir(request.FilePath) {
			confirmFail(c, utils.CodeInternal, confirmReasonDirMismatch, animeDirMismatchMessage, nil)
			return
		}
		anime = existing
	} else {
		var inserted bool
		anime, inserted, err = h.createAnimeFromBangumiReportInserted(request.BangumiID, request.FilePath)
		if err != nil {
			confirmFail(c, utils.CodeInternal, confirmReasonUpstream, createAnimeErrorMessage(err), nil)
			return
		}
		// 唯一索引回退返回了其它目录的既有行（并发入库竞态）：不是本请求所建，不得回滚。
		if normalizeConfirmDir(anime.FilePath) != normalizeConfirmDir(request.FilePath) {
			confirmFail(c, utils.CodeInternal, confirmReasonDirMismatch, animeDirMismatchMessage, nil)
			return
		}
		// created 只取决于本次是否真正 INSERT：回退返回的同目录既有行属于其它流程，失败时不得回滚删除。
		created = inserted
	}

	// 先只读生成整批结果并校验 file_names 全部可归入，再原子提交；校验失败时对数据库零写入。
	plan, err := h.prepareScanV2(anime,
		services.WithOnlyFiles(request.FileNames),
		services.WithEpisodeOffset(request.EpisodeOffset),
	)
	if err != nil {
		h.rollbackConfirmedAnime(anime.ID, created, nil)
		confirmFail(c, utils.CodeInternal, confirmScanErrorReason(err), scanV2ErrorMessage(err), nil)
		return
	}

	stored := make(map[string]bool, len(plan.inv.Desired))
	for _, ep := range plan.inv.Desired {
		stored[ep.FilePath] = true
	}
	var missing []string
	seenMissing := map[string]bool{}
	for _, name := range request.FileNames {
		if stored[name] || seenMissing[name] {
			continue
		}
		seenMissing[name] = true
		missing = append(missing, name)
	}
	if len(missing) > 0 {
		h.rollbackConfirmedAnime(anime.ID, created, nil)
		confirmFail(c, utils.CodeInternal, confirmReasonNoMatch,
			"这些文件未能归入该条目："+strings.Join(missing, "、"), missing)
		return
	}

	episodes, err := h.commitScanV2(plan)
	if err != nil {
		h.rollbackConfirmedAnime(anime.ID, created, nil)
		confirmFail(c, utils.CodeInternal, confirmScanErrorReason(err), scanV2ErrorMessage(err), nil)
		return
	}

	if names := storedEpisodeFileNames(episodes); len(names) > 0 {
		if err := database.DeleteUnidentifiedFiles(request.FilePath, names); err != nil {
			log.Printf("[Anime] 精确清除目录 %q 未识别失败: %v", request.FilePath, err)
		}
	}
	utils.Success(c, gin.H{
		"anime":    anime,
		"created":  created,
		"scanned":  len(episodes),
		"episodes": episodes,
	})
}

// confirmScanErrorReason 将 runScanV2 错误映射为 ConfirmErrorData.reason。
func confirmScanErrorReason(err error) string {
	switch {
	case errors.Is(err, services.ErrMatchReviewRequired):
		return confirmReasonReviewRequired
	case errors.Is(err, services.ErrMatchUpstream):
		return confirmReasonUpstream
	case errors.Is(err, database.ErrEpisodeSnapshotChanged):
		return confirmReasonConflict
	default:
		return confirmReasonUpstream
	}
}

// rollbackConfirmedAnime 回滚本请求新建的番剧：created=false 不删任何数据。
// 剧集为空，或剧集全部是本请求 runScanV2 刚写入的（ours，按 ID）时才删除番剧（剧集随外键级联）；
// 出现本请求之外的剧集则保留并记录日志，避免误删其它流程写入的数据。
func (h *AnimeHandler) rollbackConfirmedAnime(animeID int64, created bool, ours []models.Episode) {
	if !created {
		return
	}
	current, err := database.ListEpisodesByAnimeID(animeID)
	if err != nil {
		log.Printf("[Anime] 收件箱确认回滚读取番剧 %d 剧集失败，保留番剧: %v", animeID, err)
		return
	}
	if len(current) > 0 {
		own := make(map[int64]bool, len(ours))
		for _, ep := range ours {
			own[ep.ID] = true
		}
		for _, ep := range current {
			if !own[ep.ID] {
				log.Printf("[Anime] 收件箱确认回滚：番剧 %d 含非本请求剧集，保留", animeID)
				return
			}
		}
	}
	if err := database.DeleteAnime(animeID); err != nil {
		log.Printf("[Anime] 收件箱确认回滚删除番剧 %d 失败: %v", animeID, err)
	}
}

func scanV2ErrorMessage(err error) string {
	switch {
	case errors.Is(err, services.ErrMatchReviewRequired):
		msg := strings.TrimSpace(err.Error())
		if msg == "" || msg == services.ErrMatchReviewRequired.Error() {
			return "匹配需要人工确认，未写入任何变更"
		}
		return msg + "（未写入任何变更）"
	case errors.Is(err, services.ErrMatchUpstream):
		return "匹配上游不可用，未写入任何变更"
	case errors.Is(err, database.ErrEpisodeSnapshotChanged):
		return "番剧或剧集快照已变更，请重试（未写入任何变更）"
	case errors.Is(err, database.ErrEpisodeIdentityConflict):
		return "剧集文件身份冲突，未写入任何变更"
	default:
		return "扫描番剧目录失败"
	}
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
