package database

import "database/sql"

// migrateAPIKeys 建立外部工具接入用的 API Key 表。仅保存明文 Key 的
// SHA-256 哈希与掩码前缀，创建者账号删除时级联删除其密钥。
func migrateAPIKeys(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			created_by INTEGER NOT NULL,
			last_used_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_created_by ON api_keys(created_by);`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
