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
	{
		version: 2,
		name:    "unique_anime_episode_keys",
		fn:      migrateUniqueAnimeEpisodeKeys,
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

// migrateUniqueAnimeEpisodeKeys 为 animes(bangumi_id) 与 episodes(anime_id, ep_number)
// 建立唯一索引，作为库扫描并发写入的数据库层兜底，防止产生重复番剧/剧集。
// 老库中已经存在的重复行必须先按"保留最小 id"策略去重，否则唯一索引无法创建。
// 注意：仅对 bangumi_id > 0 建部分索引，多个未关联 Bangumi 的番剧（NULL/0）可并存。
func migrateUniqueAnimeEpisodeKeys(tx *sql.Tx) error {
	statements := []string{
		// 先删掉重复番剧（bangumi_id > 0）名下的剧集，再删重复番剧本身。
		`DELETE FROM episodes WHERE anime_id IN (
			SELECT id FROM animes
			WHERE bangumi_id > 0 AND id NOT IN (
				SELECT MIN(id) FROM animes WHERE bangumi_id > 0 GROUP BY bangumi_id
			)
		);`,
		`DELETE FROM animes
		 WHERE bangumi_id > 0 AND id NOT IN (
			 SELECT MIN(id) FROM animes WHERE bangumi_id > 0 GROUP BY bangumi_id
		 );`,
		// 再删同一番剧内重复的剧集行，每个 (anime_id, ep_number) 保留最小 id。
		`DELETE FROM episodes WHERE id NOT IN (
			SELECT MIN(id) FROM episodes GROUP BY anime_id, ep_number
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_animes_bangumi_id
			ON animes(bangumi_id) WHERE bangumi_id > 0;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_episodes_anime_ep
			ON episodes(anime_id, ep_number);`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
