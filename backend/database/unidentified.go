package database

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"fan-web/models"
)

// ReplaceUnidentified 在单个事务内清空未识别表并写入 rows。
// 传入空切片则只清空，不插入。
func ReplaceUnidentified(rows []models.UnidentifiedFile) error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM unidentified_files"); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
		INSERT INTO unidentified_files (file_path, file_name, reason, candidates)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, row := range rows {
		candidates, err := marshalCandidates(row.Candidates)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(row.FilePath, row.FileName, row.Reason, candidates); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpsertUnidentifiedFiles 在单个事务内增量写入未识别文件：同 (file_path, file_name)
// 已存在时只更新 reason / candidates / updated_at，不影响表内其他条目（区别于
// ReplaceUnidentified 的整表覆盖）。row.FilePath 先把 \ 统一替换为 / 再落库。
// 传入空切片为零操作；任一 row.FileName Trim 后为空时返回参数错误且不写入任何行。
func UpsertUnidentifiedFiles(rows []models.UnidentifiedFile) error {
	if len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		if strings.TrimSpace(row.FileName) == "" {
			return fmt.Errorf("UpsertUnidentifiedFiles: 文件名不得为空")
		}
	}

	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		"INSERT INTO unidentified_files (file_path, file_name, reason, candidates) VALUES (?, ?, ?, ?) " +
			"ON CONFLICT(file_path, file_name) DO UPDATE SET reason = excluded.reason, candidates = excluded.candidates, updated_at = CURRENT_TIMESTAMP",
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, row := range rows {
		candidates, err := marshalCandidates(row.Candidates)
		if err != nil {
			return err
		}
		filePath := strings.ReplaceAll(row.FilePath, "\\", "/")
		if _, err := stmt.Exec(filePath, row.FileName, row.Reason, candidates); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteUnidentifiedByDir 删除指定目录下的全部未识别条目。两端路径统一把反斜杠替换为
// 斜杠后再比较（与 IsFileAssociated 一致），兼容 Windows（\）与类 Unix（/）分隔符混存。
func DeleteUnidentifiedByDir(filePath string) error {
	_, err := DB.Exec(
		"DELETE FROM unidentified_files WHERE REPLACE(file_path, '\\', '/') = REPLACE(?, '\\', '/')",
		filePath,
	)
	return err
}

// DeleteUnidentifiedFiles 只删除指定目录下显式列出的文件名，不清空整个目录。
// names 为空时为零操作。含空文件名时返回错误。目录比较两端统一把反斜杠替换为斜杠
// （与 IsFileAssociated 一致）。
func DeleteUnidentifiedFiles(dir string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("DeleteUnidentifiedFiles: 文件名不得为空")
		}
	}

	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		"DELETE FROM unidentified_files " +
			"WHERE REPLACE(file_path, '\\', '/') = REPLACE(?, '\\', '/') AND file_name = ?",
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, name := range names {
		if _, err := stmt.Exec(dir, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// NormalizePaging 夹紧分页参数：page<1 按 1；pageSize<1 按 50，>100 按 100。
// 这是未识别列表分页策略的唯一真相源：查询与响应回显都用同一套夹紧后的值，
// 避免 handler 原样回显未裁剪的 query（如 page=0、page_size=999）。
func NormalizePaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	} else if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func ListUnidentified(page, pageSize int) ([]models.UnidentifiedFile, int, error) {
	page, pageSize = NormalizePaging(page, pageSize)

	var total int
	if err := DB.QueryRow("SELECT COUNT(*) FROM unidentified_files").Scan(&total); err != nil {
		return nil, 0, err
	}

	if page-1 > math.MaxInt/pageSize {
		return []models.UnidentifiedFile{}, total, nil
	}
	offset := (page - 1) * pageSize
	rows, err := DB.Query(`
		SELECT id, file_path, file_name, reason, candidates, updated_at
		FROM unidentified_files
		ORDER BY updated_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]models.UnidentifiedFile, 0)
	for rows.Next() {
		item, err := scanUnidentified(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func marshalCandidates(candidates []models.MatchCandidate) (string, error) {
	if candidates == nil {
		return "[]", nil
	}
	raw, err := json.Marshal(candidates)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func scanUnidentified(row scanner) (*models.UnidentifiedFile, error) {
	var item models.UnidentifiedFile
	var candidatesJSON string
	if err := row.Scan(
		&item.ID, &item.FilePath, &item.FileName, &item.Reason, &candidatesJSON, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if candidatesJSON == "" {
		item.Candidates = []models.MatchCandidate{}
	} else if err := json.Unmarshal([]byte(candidatesJSON), &item.Candidates); err != nil {
		return nil, err
	}
	if item.Candidates == nil {
		item.Candidates = []models.MatchCandidate{}
	}
	return &item, nil
}
