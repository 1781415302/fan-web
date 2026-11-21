package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"fan-web/database"
	"fan-web/middleware"
	"fan-web/models"
	"fan-web/utils"
)

// apiKeyRandomBytes 明文 Key 随机部分的字节数，hex 编码后为 64 个字符。
const apiKeyRandomBytes = 32

// apiKeyNameMaxRunes 备注名 Trim 后允许的最大字符数（按 rune 计）。
const apiKeyNameMaxRunes = 64

type APIKeyHandler struct{}

func NewAPIKeyHandler() *APIKeyHandler {
	return &APIKeyHandler{}
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
}

func (h *APIKeyHandler) List(c *gin.Context) {
	keys, err := database.ListAPIKeys()
	if err != nil {
		utils.Error(c, utils.CodeInternal, "查询 API Key 列表失败")
		return
	}
	utils.Success(c, keys)
}

func (h *APIKeyHandler) Create(c *gin.Context) {
	var request createAPIKeyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		utils.Error(c, utils.CodeInvalidParams, "请求参数错误")
		return
	}
	name := strings.TrimSpace(request.Name)
	if count := utf8.RuneCountInString(name); count < 1 || count > apiKeyNameMaxRunes {
		utils.Error(c, utils.CodeInvalidParams, "备注名长度需为 1-64 个字符")
		return
	}

	currentUserID, ok := middleware.CurrentUserID(c)
	if !ok {
		utils.Error(c, utils.CodeUnauthenticated, "未登录")
		return
	}

	// 明文仅在本次响应返回一次；库中只存 SHA-256 哈希与掩码前缀。
	buf := make([]byte, apiKeyRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		utils.Error(c, utils.CodeInternal, "生成 API Key 失败")
		return
	}
	raw := middleware.APIKeyTokenPrefix + hex.EncodeToString(buf)
	keyPrefix := raw[:11] + "..." + raw[len(raw)-4:]
	sum := sha256.Sum256([]byte(raw))
	keyHash := hex.EncodeToString(sum[:])

	created, err := database.CreateAPIKey(name, keyHash, keyPrefix, currentUserID)
	if err != nil {
		utils.Error(c, utils.CodeInternal, "创建 API Key 失败")
		return
	}
	utils.Success(c, models.APIKeyCreated{APIKey: *created, Key: raw})
}

func (h *APIKeyHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		utils.Error(c, utils.CodeInvalidParams, "API Key ID 无效")
		return
	}

	if err := database.DeleteAPIKey(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.Error(c, utils.CodeNotFound, "API Key 不存在")
		} else {
			utils.Error(c, utils.CodeInternal, "删除 API Key 失败")
		}
		return
	}
	utils.Success(c, nil)
}
