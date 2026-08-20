package handlers

import (
	"database/sql"
	"errors"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/models"
	"fan-web/utils"
)

type rebindAnimeRequest struct {
	BangumiID int `json:"bangumi_id"`
}

func (h *AnimeHandler) Rebind(c *gin.Context) {
	id, ok := animeID(c)
	if !ok {
		return
	}
	var request rebindAnimeRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.BangumiID <= 0 {
		utils.Error(c, utils.CodeInvalidParams, "bangumi_id 必须大于 0")
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

	subject, err := h.bangumi.GetSubject(request.BangumiID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "获取 Bangumi 数据失败: "+err.Error())
		return
	}

	if err := database.UpdateAnimeBangumi(id, &models.Anime{
		Title:     subject.Name,
		TitleCn:   subject.NameCn,
		BangumiID: subject.ID,
		Cover:     subject.Cover,
		Summary:   subject.Summary,
		EpCount:   subject.TotalEpisodes,
	}); err != nil {
		if errors.Is(err, database.ErrBangumiBound) {
			utils.Error(c, utils.CodeInvalidParams, "该 Bangumi 条目已绑定其他番剧")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "番剧不存在")
			return
		}
		utils.Error(c, utils.CodeInternal, "更新番剧失败")
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
