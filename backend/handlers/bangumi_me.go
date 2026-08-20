package handlers

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/services"
	"fan-web/utils"
)

type BangumiMeHandler struct {
	bangumi *services.BangumiService
	sync    *services.BangumiSync
}

func NewBangumiMeHandler(bangumi *services.BangumiService, sync *services.BangumiSync) *BangumiMeHandler {
	return &BangumiMeHandler{bangumi: bangumi, sync: sync}
}

type bangumiLinkData struct {
	Linked bool   `json:"linked"`
	Suffix string `json:"suffix,omitempty"`
}

type putBangumiRequest struct {
	AccessToken string `json:"access_token"`
}

func bangumiLinkStatus(token string, linked bool) bangumiLinkData {
	if !linked {
		return bangumiLinkData{Linked: false}
	}
	suffix := token
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	return bangumiLinkData{Linked: true, Suffix: suffix}
}

func (h *BangumiMeHandler) Get(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	token, linked, err := database.GetBangumiToken(userID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询 Bangumi 绑定失败")
		return
	}
	utils.Success(c, bangumiLinkStatus(token, linked))
}

func (h *BangumiMeHandler) Put(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	var request putBangumiRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.Error(c, utils.CodeInvalidParams, "请求参数错误")
		return
	}
	token := strings.TrimSpace(request.AccessToken)
	if token == "" || len(token) > 512 {
		utils.Error(c, utils.CodeInvalidParams, "access_token 不能为空且不能超过 512 个字符")
		return
	}
	if err := h.bangumi.GetMe(token); err != nil {
		if errors.Is(err, services.ErrBangumiUnauthorized) {
			utils.Error(c, utils.CodeInvalidParams, "Bangumi 令牌无效")
			return
		}
		utils.Error(c, utils.CodeInternal, "校验 Bangumi 令牌失败")
		return
	}
	if err := database.SaveBangumiToken(userID, token); err != nil {
		utils.Error(c, utils.CodeInternal, "保存 Bangumi 令牌失败")
		return
	}
	if err := database.EnqueueWatchedForUser(userID); err != nil {
		utils.Error(c, utils.CodeInternal, "入队已看剧集失败")
		return
	}
	utils.Success(c, bangumiLinkStatus(token, true))
}

func (h *BangumiMeHandler) Delete(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	if err := database.DeleteBangumiToken(userID); err != nil {
		utils.Error(c, utils.CodeInternal, "解除 Bangumi 绑定失败")
		return
	}
	if err := database.DeleteBangumiOutboxByUser(userID); err != nil {
		utils.Error(c, utils.CodeInternal, "清除同步队列失败")
		return
	}
	utils.Success(c, bangumiLinkData{Linked: false})
}

func (h *BangumiMeHandler) Sync(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}
	_, linked, err := database.GetBangumiToken(userID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询 Bangumi 绑定失败")
		return
	}
	if !linked {
		utils.Error(c, utils.CodeInvalidParams, "未绑定 Bangumi")
		return
	}
	result, err := h.sync.SyncInbound(userID)
	if err != nil {
		if errors.Is(err, services.ErrBangumiUnauthorized) {
			utils.Error(c, utils.CodeInvalidParams, "Bangumi 令牌无效")
			return
		}
		utils.Error(c, utils.CodeInternal, "同步 Bangumi 进度失败")
		return
	}
	utils.Success(c, result)
}
