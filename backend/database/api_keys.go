package database

import (
	"database/sql"
	"fmt"
	"strings"

	"fan-web/models"
)

const apiKeySelect = `
	SELECT id, name, key_prefix, created_by, last_used_at, created_at
	FROM api_keys
`

// CreateAPIKey 插入一条 API Key 记录并回查新行。keyHash 为明文 Key 的 SHA-256 小写
// hex，明文本身不落库；keyPrefix 为掩码前缀，仅用于列表展示。
// name / keyHash / keyPrefix 不得为空（Trim 后判断），createdBy 必须为正整数；
// 重复 keyHash 由 api_keys.key_hash 的 UNIQUE 约束兜底并原样返回驱动错误。
func CreateAPIKey(name, keyHash, keyPrefix string, createdBy int64) (*models.APIKey, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("CreateAPIKey: name 不得为空")
	}
	if strings.TrimSpace(keyHash) == "" {
		return nil, fmt.Errorf("CreateAPIKey: keyHash 不得为空")
	}
	if strings.TrimSpace(keyPrefix) == "" {
		return nil, fmt.Errorf("CreateAPIKey: keyPrefix 不得为空")
	}
	if createdBy <= 0 {
		return nil, fmt.Errorf("CreateAPIKey: createdBy 必须为正整数")
	}

	result, err := DB.Exec(
		"INSERT INTO api_keys (name, key_hash, key_prefix, created_by) VALUES (?, ?, ?, ?)",
		name, keyHash, keyPrefix, createdBy,
	)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return scanAPIKey(DB.QueryRow(apiKeySelect+" WHERE id = ?", id))
}

// ListAPIKeys 按 id 降序返回全部密钥；无记录时返回非 nil 空切片。
func ListAPIKeys() ([]models.APIKey, error) {
	rows, err := DB.Query(apiKeySelect + " ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]models.APIKey, 0)
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

// GetAPIKeyByHash 按 key_hash 精确匹配；无匹配行时返回 (nil, sql.ErrNoRows)。
func GetAPIKeyByHash(keyHash string) (*models.APIKey, error) {
	key, err := scanAPIKey(DB.QueryRow(apiKeySelect+" WHERE key_hash = ?", keyHash))
	if err != nil {
		return nil, err
	}
	return key, nil
}

// TouchAPIKeyLastUsed 把指定密钥的 last_used_at 更新为当前时间。
// 影响行数为 0（密钥已被吊销等）不视为错误，返回 nil。
func TouchAPIKeyLastUsed(id int64) error {
	_, err := DB.Exec("UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// DeleteAPIKey 删除指定密钥；影响行数为 0 时返回 sql.ErrNoRows。
func DeleteAPIKey(id int64) error {
	result, err := DB.Exec("DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// scanAPIKey 扫描一行 api_keys；last_used_at 可空，NULL 映射为 nil 指针。
func scanAPIKey(row scanner) (*models.APIKey, error) {
	var key models.APIKey
	var lastUsedAt sql.NullTime
	if err := row.Scan(
		&key.ID,
		&key.Name,
		&key.KeyPrefix,
		&key.CreatedBy,
		&lastUsedAt,
		&key.CreatedAt,
	); err != nil {
		return nil, err
	}
	if lastUsedAt.Valid {
		usedAt := lastUsedAt.Time
		key.LastUsedAt = &usedAt
	}
	return &key, nil
}
