package database

import "database/sql"

// migrateLibraryInboxAndBangumiSync 建立未识别文件收件箱、用户 Bangumi 令牌
// 与观看进度同步发件箱。不改动既有四张主表字段。
func migrateLibraryInboxAndBangumiSync(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS unidentified_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL,
			file_name TEXT NOT NULL,
			reason TEXT NOT NULL,
			candidates TEXT NOT NULL DEFAULT '[]',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(file_path, file_name)
		);`,
		`CREATE TABLE IF NOT EXISTS user_bangumi_tokens (
			user_id INTEGER PRIMARY KEY,
			access_token TEXT NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS bangumi_sync_outbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			episode_id INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, episode_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (episode_id) REFERENCES episodes(id) ON DELETE CASCADE
		);`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
