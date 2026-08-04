package handlers

import (
	"database/sql"
	"errors"
	"net/http"
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

// Stream serves a video file after validating the JWT from the query string.
func (h *EpisodeHandler) Stream(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		parts := strings.Fields(strings.TrimSpace(c.GetHeader("Authorization")))
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token = parts[1]
		}
	}
	claims, err := h.auth.ParseToken(token)
	if err != nil {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	if _, err := database.GetUserByID(claims.UserID); err != nil {
		utils.Error(c, utils.CodeUnauthenticated, "登录状态已失效")
		return
	}

	episodeID, ok := parsePositiveID(c.Param("id"))
	if !ok {
		utils.Error(c, utils.CodeInvalidParams, "无效的集数 ID")
		return
	}
	episode, err := database.GetEpisodeByID(episodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "集数不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询集数失败")
		return
	}
	anime, err := database.GetAnimeByID(episode.AnimeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "查询番剧失败")
		return
	}
	fullPath, err := h.scanner.ResolveFilePath(anime.FilePath, episode.FilePath)
	if err != nil {
		utils.Error(c, utils.CodeNotFound, "视频文件不存在或不可访问")
		return
	}

	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	http.ServeFile(c.Writer, c.Request, fullPath)
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
