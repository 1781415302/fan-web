package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
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
}

func NewEpisodeHandler(auth *services.AuthService, scanner *services.ScannerService) *EpisodeHandler {
	return &EpisodeHandler{auth: auth, scanner: scanner}
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
