package handlers

import (
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/models"
	"fan-web/services"
	"fan-web/utils"
)

type EpisodeHandler struct {
	auth    *services.AuthService
	scanner *services.ScannerService
	sync    *services.BangumiSync
}

func NewEpisodeHandler(auth *services.AuthService, scanner *services.ScannerService) *EpisodeHandler {
	return &EpisodeHandler{auth: auth, scanner: scanner}
}

func (h *EpisodeHandler) SetBangumiSync(sync *services.BangumiSync) {
	h.sync = sync
}

// Stream serves a video file after validating Authorization Bearer or ?media_token=.
func (h *EpisodeHandler) Stream(c *gin.Context) {
	fullPath, ok := h.resolveEpisodePath(c)
	if !ok {
		return
	}

	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	http.ServeFile(c.Writer, c.Request, fullPath)
}

// Download 以附件形式下发整集视频文件，鉴权与路径解析语义与 Stream 逐字一致。
// 浏览器原生下载：先设置下载头再交由 http.ServeFile 处理 Range，不自绘进度、不做 Range 预检。
func (h *EpisodeHandler) Download(c *gin.Context) {
	fullPath, ok := h.resolveEpisodePath(c)
	if !ok {
		return
	}

	// 下载文件名取解析后完整路径的 Base；必须先设头再 ServeFile，否则 Content-Type 会被嗅探覆盖。
	c.Header("Content-Disposition", buildAttachmentDisposition(filepath.Base(fullPath)))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	http.ServeFile(c.Writer, c.Request, fullPath)
}

// buildAttachmentDisposition 构造下载用的 Content-Disposition 头。
// 1) 控制字符(<0x20、0x7F)、双引号、反斜杠一律替换为 _，阻断头注入与引号逃逸。
// 2) 优先用 mime.FormatMediaType：纯 ASCII（含方括号）走 filename=，非 ASCII 走 filename*=utf-8'' 编码。
// 3) 标准库输出若含裸非 ASCII/CR/LF，或输入含非 ASCII 却缺 filename*=，则退回手工 filename+filename* 双写。
func buildAttachmentDisposition(name string) string {
	sanitized := strings.Map(func(r rune) rune {
		// CR、LF 已包含在 <0x20 内，此处与双引号、反斜杠一并替换，防止响应头拆分。
		if r < 0x20 || r == 0x7F || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)

	if v := mime.FormatMediaType("attachment", map[string]string{"filename": sanitized}); v != "" {
		// 校验标准库输出：绝不允许 CR/LF 与裸非 ASCII；输入含非 ASCII 时必须带 filename*=。
		hasNonASCIIInput := false
		for i := 0; i < len(sanitized); i++ {
			if sanitized[i] > 127 {
				hasNonASCIIInput = true
				break
			}
		}
		valid := !strings.ContainsAny(v, "\r\n")
		if valid {
			for i := 0; i < len(v); i++ {
				if v[i] > 127 {
					valid = false
					break
				}
			}
		}
		if valid && hasNonASCIIInput && !strings.Contains(v, "filename*=") {
			valid = false
		}
		if valid {
			return v
		}
	}

	// 兜底手工双写：filename 放纯 ASCII 回退名兼容老客户端，filename* 放 RFC5987 编码供新客户端还原。
	ascii := strings.Map(func(r rune) rune {
		if r > 127 {
			return '_'
		}
		return r
	}, sanitized)
	return `attachment; filename="` + ascii + `"; filename*=utf-8''` + encodeRFC5987Value(sanitized)
}

// encodeRFC5987Value 按 RFC5987 attr-char 逐字节百分号编码（大写十六进制），空格等一律 %XX。
func encodeRFC5987Value(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if '0' <= c && c <= '9' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' ||
			c == '!' || c == '#' || c == '$' || c == '&' || c == '+' || c == '-' ||
			c == '.' || c == '^' || c == '_' || c == '`' || c == '|' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
	}
	return b.String()
}

// Subtitles lists embedded text tracks, or returns one selected track as VTT.
// ArtPlayer loads subtitle files with fetch() and cannot attach the app's
// Authorization interceptor, so ?media_token= is accepted in addition to Bearer.
func (h *EpisodeHandler) Subtitles(c *gin.Context) {
	fullPath, ok := h.resolveEpisodePath(c)
	if !ok {
		return
	}

	trackParam := strings.TrimSpace(c.Query("track"))
	if trackParam == "" {
		tracks, err := services.ReadMatroskaSubtitleTracks(fullPath)
		if err != nil {
			utils.Error(c, utils.CodeInternal, "读取字幕轨道失败")
			return
		}
		utils.Success(c, tracks)
		return
	}

	trackNumber, err := strconv.ParseUint(trackParam, 10, 64)
	if err != nil || trackNumber == 0 {
		utils.Error(c, utils.CodeInvalidParams, "无效的字幕轨道")
		return
	}
	track, vtt, err := services.ReadMatroskaSubtitleVTT(fullPath, trackNumber)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			utils.Error(c, utils.CodeNotFound, "字幕轨道不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "读取字幕失败")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Disposition", `inline; filename="subtitle-`+strconv.FormatUint(track.TrackNumber, 10)+`.vtt"`)
	c.Data(http.StatusOK, "text/vtt; charset=utf-8", vtt)
}

// IssueMediaToken 为当前登录用户签发指定 episode 的短期媒体票据。
func (h *EpisodeHandler) IssueMediaToken(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	episodeID, ok := parsePositiveID(c.Param("id"))
	if !ok {
		utils.Error(c, utils.CodeInvalidParams, "无效的集数 ID")
		return
	}
	if _, err := database.GetEpisodeByID(episodeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "集数不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询集数失败")
		return
	}
	token, expiresAt, err := h.auth.IssueMediaToken(userID, episodeID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "签发媒体票据失败")
		return
	}
	utils.Success(c, gin.H{
		"token":      token,
		"expires_at": expiresAt,
	})
}

func (h *EpisodeHandler) resolveEpisodePath(c *gin.Context) (string, bool) {
	episodeID, ok := parsePositiveID(c.Param("id"))
	if !ok {
		utils.Error(c, utils.CodeInvalidParams, "无效的集数 ID")
		return "", false
	}

	// Authorization Bearer 优先于 ?media_token=；一旦提供某一种凭证，只按该凭证校验，不降级尝试其他凭证。
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	parts := strings.Fields(authorization)
	hasBearer := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer")
	mediaToken := strings.TrimSpace(c.Query("media_token"))

	userID, ok := h.authenticateMedia(c, episodeID, hasBearer, parts, mediaToken)
	if !ok {
		return "", false
	}

	episode, err := database.GetEpisodeByID(episodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "集数不存在")
			return "", false
		}
		utils.Error(c, utils.CodeInternal, "查询集数失败")
		return "", false
	}
	anime, err := database.GetAnimeByID(episode.AnimeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return "", false
		}
		utils.Error(c, utils.CodeInternal, "查询番剧失败")
		return "", false
	}
	fullPath, err := h.scanner.ResolveFilePath(anime.FilePath, episode.FilePath)
	if err != nil {
		utils.Error(c, utils.CodeNotFound, "视频文件不存在或不可访问")
		return "", false
	}
	_ = userID
	return fullPath, true
}

// authenticateMedia 校验资源访问者并按需确认用户仍存在。
func (h *EpisodeHandler) authenticateMedia(
	c *gin.Context,
	episodeID int64,
	hasBearer bool,
	parts []string,
	mediaToken string,
) (int64, bool) {
	switch {
	case hasBearer:
		claims, err := h.auth.ParseToken(parts[1])
		if err != nil {
			utils.Error(c, utils.CodeUnauthenticated, "登录状态已失效")
			return 0, false
		}
		if _, err := database.GetUserByID(claims.UserID); err != nil {
			utils.Error(c, utils.CodeUnauthenticated, "登录状态已失效")
			return 0, false
		}
		return claims.UserID, true

	case mediaToken != "":
		claims, err := h.auth.ParseMediaToken(mediaToken, episodeID)
		if err != nil {
			utils.Error(c, utils.CodeUnauthenticated, "媒体票据无效或已过期")
			return 0, false
		}
		// 媒体票据虽经服务签字校验，但仍需确认用户仍存在：
		// 已删除/注销用户的合法票据在过期前应立刻失效，不能继续拉流。
		if _, err := database.GetUserByID(claims.UserID); err != nil {
			utils.Error(c, utils.CodeUnauthenticated, "登录状态已失效")
			return 0, false
		}
		return claims.UserID, true

	default:
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return 0, false
	}
}

func toProgressResponse(progress models.WatchProgress, includeEpisodeID bool) gin.H {
	data := gin.H{
		"position":   progress.Position,
		"watched":    progress.Watched,
		"updated_at": progress.UpdatedAt,
	}
	if includeEpisodeID {
		data["episode_id"] = progress.EpisodeID
	}
	return data
}

func (h *EpisodeHandler) GetProgress(c *gin.Context) {
	userID, episodeID, ok := h.progressIDs(c, "episode_id")
	if !ok {
		return
	}
	if _, err := database.GetEpisodeByID(episodeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "集数不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询集数失败")
		return
	}
	progress, err := database.GetProgress(userID, episodeID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询播放进度失败")
		return
	}
	utils.Success(c, toProgressResponse(*progress, false))
}

type reportProgressRequest struct {
	Position *int `json:"position"`
	Watched  bool `json:"watched"`
}

func (h *EpisodeHandler) ReportProgress(c *gin.Context) {
	userID, episodeID, ok := h.progressIDs(c, "episode_id")
	if !ok {
		return
	}
	if _, err := database.GetEpisodeByID(episodeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "集数不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询集数失败")
		return
	}
	var request reportProgressRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Position == nil || *request.Position < 0 {
		utils.Error(c, utils.CodeInvalidParams, "position 必须是大于等于 0 的整数")
		return
	}
	if err := database.UpsertProgress(userID, episodeID, *request.Position, request.Watched); err != nil {
		utils.Error(c, utils.CodeInternal, "保存播放进度失败")
		return
	}
	utils.Success(c, nil)
	if request.Watched {
		// 派发前缓存本地指针，避免并发读 h.sync 产生数据竞态；
		// goroutine 内 recover 防止未预期 panic 终止进程。
		if sync := h.sync; sync != nil {
			go func() {
				defer func() { _ = recover() }()
				sync.EnqueueWatched(userID, episodeID)
			}()
		}
	}
}

func (h *EpisodeHandler) AnimeProgress(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	animeID, ok := parsePositiveID(c.Param("anime_id"))
	if !ok {
		utils.Error(c, utils.CodeInvalidParams, "无效的番剧 ID")
		return
	}
	if _, err := database.GetAnimeByID(animeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询番剧失败")
		return
	}
	progressList, err := database.ListProgressByAnime(userID, animeID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询番剧进度失败")
		return
	}
	data := make([]gin.H, 0, len(progressList))
	for _, progress := range progressList {
		data = append(data, toProgressResponse(progress, true))
	}
	utils.Success(c, data)
}

func (h *EpisodeHandler) Continue(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 {
		limit = 20
	} else if limit > 50 {
		limit = 50
	}
	items, err := database.ListContinueWatching(userID, limit)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询继续观看失败")
		return
	}
	utils.Success(c, gin.H{"items": items})
}

func (h *EpisodeHandler) progressIDs(c *gin.Context, parameter string) (int64, int64, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return 0, 0, false
	}
	episodeID, ok := parsePositiveID(c.Param(parameter))
	if !ok {
		utils.Error(c, utils.CodeInvalidParams, "无效的集数 ID")
		return 0, 0, false
	}
	return userID, episodeID, true
}

func parsePositiveID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}
