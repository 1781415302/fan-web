package database

import (
	"database/sql"
)

// migrations 是全部的版本化迁移定义，按 version 升序排列。
// 新增表结构变化时，在末尾追加新版本，不要修改已有版本。
var migrations = []migration{
	{
		version: 1,
		name:    "initial_schema",
		fn:      migrateInitialSchema,
	},
}

// migrateInitialSchema 建立初始业务表与索引。
// DDL 全部使用 IF NOT EXISTS，既初始化新库，也可幂等接管老库。
func migrateInitialSchema(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password TEXT NOT NULL,
			is_admin INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS animes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			title_cn TEXT,
			bangumi_id INTEGER,
			cover TEXT,
			summary TEXT,
			ep_count INTEGER DEFAULT 0,
			file_path TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS episodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			anime_id INTEGER NOT NULL,
			ep_number INTEGER NOT NULL,
			title TEXT,
			file_path TEXT NOT NULL,
			duration INTEGER,
			FOREIGN KEY (anime_id) REFERENCES animes(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS watch_progress (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			episode_id INTEGER NOT NULL,
			position INTEGER DEFAULT 0,
			watched INTEGER DEFAULT 0,
			updated_at DATETIME,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (episode_id) REFERENCES episodes(id) ON DELETE CASCADE
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_watch_progress_user_episode
			ON watch_progress(user_id, episode_id);`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
