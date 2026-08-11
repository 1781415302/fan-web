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
//
// 去重绝不直接丢弃重复行下的剧集与观看进度（本迁移运行时 PRAGMA foreign_keys=ON，
// episodes 删除会级联删除 watch_progress）：
//  1. 同一 bangumi_id(>0) 的重复番剧：把被废弃番剧的剧集 reparent 到保留番剧（仅改
//     anime_id，不触发级联删除），再删除已空掉的重复番剧。
//  2. 同一 (anime_id, ep_number) 的重复剧集（含 reparent 引入的以及迁移前已存在的）：
//     保留最小 id，把其余剧集的观看进度合并（取更大 position、watched 取或、updated_at
//     取更近）到保留剧集后再删除。
// 合并完成后才创建唯一索引。
func migrateUniqueAnimeEpisodeKeys(tx *sql.Tx) error {
	if err := collapseDuplicateBangumiAnimes(tx); err != nil {
		return err
	}
	if err := deduplicateEpisodes(tx); err != nil {
		return err
	}
	statements := []string{
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

// collapseDuplicateBangumiAnimes 把 bangumi_id>0 的重复番剧合并为 MIN(id)：
// 把被废弃番剧下的全部剧集 reparent 到保留番剧（UPDATE 仅改 anime_id，不触发级联删除，
// watch_progress 仍绑定原 episode id 不丢失），之后再删除已无剧集的重复番剧。
// reparent 可能引入的 (anime_id, ep_number) 重复交由 deduplicateEpisodes 统一合并。
func collapseDuplicateBangumiAnimes(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT bangumi_id, MIN(id) FROM animes
		WHERE bangumi_id > 0
		GROUP BY bangumi_id
		HAVING COUNT(*) > 1
	`)
	if err != nil {
		return err
	}
	type bangumiGroup struct {
		bangumiID int
		keeperID  int64
	}
	groups := make([]bangumiGroup, 0)
	for rows.Next() {
		var g bangumiGroup
		if err := rows.Scan(&g.bangumiID, &g.keeperID); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, g := range groups {
		// 把该 bangumi 下所有非保留番剧的剧集改挂到保留番剧。FK 检查通过（保留番剧存在），
		// UPDATE 不触发级联删除，观看进度仍随原 episode id 保留。
		if _, err := tx.Exec(`
			UPDATE episodes
			SET anime_id = ?
			WHERE anime_id IN (
				SELECT id FROM animes WHERE bangumi_id = ? AND id != ?
			)
		`, g.keeperID, g.bangumiID, g.keeperID); err != nil {
			return err
		}
	}
	// 重复番剧的剧集已全部 reparent，此时删除空壳重复番剧不会级联删除任何剧集/进度。
	if _, err := tx.Exec(`
		DELETE FROM animes
		WHERE bangumi_id > 0 AND id NOT IN (
			SELECT MIN(id) FROM animes WHERE bangumi_id > 0 GROUP BY bangumi_id
		)
	`); err != nil {
		return err
	}
	return nil
}

// deduplicateEpisodes 合并同一 (anime_id, ep_number) 下的重复剧集：保留最小 id 作为
// 合并目标，先把其余剧集的观看进度合并到保留剧集（同用户 position 取较大、watched 取或、
// updated_at 取更近），再删除多余剧集。先合并后删除，避免 ON DELETE CASCADE 误删进度。
// 依赖 v1 已建立的 idx_watch_progress_user_episode 唯一索引做 ON CONFLICT 合并。
func deduplicateEpisodes(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT anime_id, ep_number FROM episodes
		GROUP BY anime_id, ep_number
		HAVING COUNT(*) > 1
	`)
	if err != nil {
		return err
	}
	type dupKey struct {
		animeID  int64
		epNumber int
	}
	keys := make([]dupKey, 0)
	for rows.Next() {
		var k dupKey
		if err := rows.Scan(&k.animeID, &k.epNumber); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, k := range keys {
		// 保留最小 id 的剧集作为合并目标。
		var survivorID int64
		if err := tx.QueryRow(`
			SELECT id FROM episodes
			WHERE anime_id = ? AND ep_number = ?
			ORDER BY id ASC LIMIT 1
		`, k.animeID, k.epNumber).Scan(&survivorID); err != nil {
			return err
		}

		loserRows, err := tx.Query(`
			SELECT id FROM episodes
			WHERE anime_id = ? AND ep_number = ? AND id != ?
			ORDER BY id ASC
		`, k.animeID, k.epNumber, survivorID)
		if err != nil {
			return err
		}
		loserIDs := make([]int64, 0)
		for loserRows.Next() {
			var id int64
			if err := loserRows.Scan(&id); err != nil {
				loserRows.Close()
				return err
			}
			loserIDs = append(loserIDs, id)
		}
		if err := loserRows.Err(); err != nil {
			loserRows.Close()
			return err
		}
		loserRows.Close()

		for _, loserID := range loserIDs {
			// 把被删剧集的观看进度合并到保留剧集：同用户取更大 position、watched 取或、
			// updated_at 取更近。ON CONFLICT 依赖 v1 的 idx_watch_progress_user_episode。
			if _, err := tx.Exec(`
				INSERT INTO watch_progress (user_id, episode_id, position, watched, updated_at)
				SELECT user_id, ?, position, watched, updated_at
				FROM watch_progress
				WHERE episode_id = ?
				ON CONFLICT(user_id, episode_id) DO UPDATE SET
					position = MAX(watch_progress.position, excluded.position),
					watched = MAX(watch_progress.watched, excluded.watched),
					updated_at = MAX(watch_progress.updated_at, excluded.updated_at)
			`, survivorID, loserID); err != nil {
				return err
			}
			// 进度已合并到保留剧集，删除重复剧集；级联删除仅清理原属被删剧集的进度行，不丢数据。
			if _, err := tx.Exec(`DELETE FROM episodes WHERE id = ?`, loserID); err != nil {
				return err
			}
		}
	}
	return nil
}
