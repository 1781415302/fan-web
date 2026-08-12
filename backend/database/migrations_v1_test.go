package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// legacyDDL 构造一个只有业务表（无 schema_migrations）的"老库"，与 migrations_test.go
// 中手工老库保持一致，供 v2 迁移测试直接写入初始数据。
var v1LegacyDDL = []string{
	`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password TEXT NOT NULL,
		is_admin INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`,
	`CREATE TABLE animes (
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
	`CREATE TABLE episodes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		anime_id INTEGER NOT NULL,
		ep_number INTEGER NOT NULL,
		title TEXT,
		file_path TEXT NOT NULL,
		duration INTEGER,
		FOREIGN KEY (anime_id) REFERENCES animes(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE watch_progress (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		episode_id INTEGER NOT NULL,
		position INTEGER DEFAULT 0,
		watched INTEGER DEFAULT 0,
		updated_at DATETIME,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY (episode_id) REFERENCES episodes(id) ON DELETE CASCADE
	);`,
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// buildLegacyDB 构造一个只有业务表的老库并执行 setup 写入初始数据，返回数据库路径。
// 调用方负责关闭自身打开的连接。
func buildLegacyDB(t *testing.T, setup func(db *sql.DB)) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range v1LegacyDDL {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	setup(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

// cleanupDB 关闭包级 DB 并置空。Init 无论成败都必须调用，否则 TempDir 清理时
// SQLite 连接仍持有文件句柄，Windows 下会报文件占用（基线 TestMigrationsIdempotentOnReinit
// 就泄漏过句柄，这里不复刻）。
func cleanupDB(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
}

func TestMigrationsRejectDuplicateBangumiDifferentDirs(t *testing.T) {
	// 回归：v2 合并重复 bangumi 番剧时若目录不一致，reparent 会让剧集文件路径失效。
	// 本例 anime 10 在目录 A、anime 20 在目录 B，剧集 02.mkv 属于 20；合并会把该剧集
	// 改挂到 10 下、实际文件却在 B，无法播放。迁移必须拒绝且不产生任何部分状态。
	dbPath := buildLegacyDB(t, func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO animes (id, title, bangumi_id, file_path) VALUES (10, 'A', 123, 'A')`)
		mustExec(t, db, `INSERT INTO animes (id, title, bangumi_id, file_path) VALUES (20, 'B', 123, 'B')`)
		mustExec(t, db, `INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (1, 20, 1, 'ep1', '02.mkv')`)
	})

	DB = nil
	err := Init(dbPath)
	if err == nil {
		t.Fatal("期望跨目录重复番剧拒绝迁移，实际成功")
	}
	cleanupDB(t)

	// 错误信息必须点名 bangumi_id、两个番剧 id 与各自目录。
	msg := err.Error()
	for _, want := range []string{"123", "10", "20", "A", "B"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息应包含 %q，实际: %s", want, msg)
		}
	}
	if !strings.Contains(msg, "目录") {
		t.Fatalf("错误信息应包含 目录 提示，实际: %s", msg)
	}

	// 迁移失败后自行打开库核查现场：schema_migrations 不得记录 v2，数据不得被改动。
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var v2Count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE version = 2",
	).Scan(&v2Count); err != nil {
		t.Fatal(err)
	}
	if v2Count != 0 {
		t.Fatalf("期望 v2 未记录，got %d", v2Count)
	}

	var animeCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM animes WHERE bangumi_id = 123",
	).Scan(&animeCount); err != nil {
		t.Fatal(err)
	}
	if animeCount != 2 {
		t.Fatalf("期望 2 个番剧均保留，got %d", animeCount)
	}

	// 剧集仍属于 anime 20，未被 reparent。
	var epAnimeID int64
	if err := db.QueryRow(
		"SELECT anime_id FROM episodes WHERE file_path = '02.mkv'",
	).Scan(&epAnimeID); err != nil {
		t.Fatal(err)
	}
	if epAnimeID != 20 {
		t.Fatalf("期望剧集仍属于 anime 20（未被 reparent），got %d", epAnimeID)
	}
}

func TestMigrationsRejectDuplicateBangumiNullAndDir(t *testing.T) {
	// NULL/空目录视为独立的取值：anime 10 无目录、anime 20 目录为 A，同样拒绝合并。
	dbPath := buildLegacyDB(t, func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO animes (id, title, bangumi_id, file_path) VALUES (10, 'A', 123, NULL)`)
		mustExec(t, db, `INSERT INTO animes (id, title, bangumi_id, file_path) VALUES (20, 'B', 123, 'A')`)
		mustExec(t, db, `INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (1, 20, 1, 'ep1', '02.mkv')`)
	})

	DB = nil
	err := Init(dbPath)
	if err == nil {
		t.Fatal("期望 NULL 与目录并存的重复番剧拒绝迁移，实际成功")
	}
	cleanupDB(t)

	msg := err.Error()
	if !strings.Contains(msg, "无目录") {
		t.Fatalf("错误信息应包含 无目录，实际: %s", msg)
	}
	if !strings.Contains(msg, "123") || !strings.Contains(msg, "10") || !strings.Contains(msg, "20") {
		t.Fatalf("错误信息应点名 bangumi_id 与番剧 id，实际: %s", msg)
	}
}

func TestMigrationsMergeDuplicateBangumiSameDir(t *testing.T) {
	// 同一 bangumi 下所有番剧目录一致（都为 'A'），reparent 后剧集仍在同一目录，合并安全。
	// 数据布局对齐 TestMigrationsMergeDuplicateAnimePreservesData：anime 10 保留 ep1/ep2，
	// anime 20 带 ep3 与冲突的 ep1'（ep4，file_path '02.mkv'），并带观看进度。
	dbPath := buildLegacyDB(t, func(db *sql.DB) {
		mustExec(t, db, `INSERT INTO users (username, password, is_admin) VALUES ('u', 'h', 0)`)
		mustExec(t, db, `INSERT INTO animes (id, title, bangumi_id, file_path, ep_count) VALUES (10, 'A', 123, 'A', 2)`)
		mustExec(t, db, `INSERT INTO animes (id, title, bangumi_id, file_path, ep_count) VALUES (20, 'A dup', 123, 'A', 2)`)
		mustExec(t, db, `INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (1, 10, 1, 'ep1', '/1')`)
		mustExec(t, db, `INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (2, 10, 2, 'ep2', '/2')`)
		mustExec(t, db, `INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (3, 20, 3, 'ep3', '/3')`)
		mustExec(t, db, `INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (4, 20, 1, 'ep1dup', '02.mkv')`)
		mustExec(t, db, `INSERT INTO watch_progress (user_id, episode_id, position, watched) VALUES (1, 3, 600, 1)`)
		mustExec(t, db, `INSERT INTO watch_progress (user_id, episode_id, position, watched) VALUES (1, 4, 100, 0)`)
		mustExec(t, db, `INSERT INTO watch_progress (user_id, episode_id, position, watched) VALUES (1, 1, 200, 1)`)
	})

	DB = nil
	if err := Init(dbPath); err != nil {
		t.Fatalf("同目录重复番剧应合并成功: %v", err)
	}
	cleanupDB(t)

	// 仅保留 anime 10。
	var keeperID int64
	if err := DB.QueryRow(
		"SELECT id FROM animes WHERE bangumi_id = 123",
	).Scan(&keeperID); err != nil {
		t.Fatal(err)
	}
	if keeperID != 10 {
		t.Fatalf("期望保留 id=10，got %d", keeperID)
	}

	// 核心断言：每个剧集 JOIN 到的番剧目录必须是 'A'，不得出现孤儿路径。
	rows, err := DB.Query(`
		SELECT e.id, e.ep_number, a.file_path
		FROM episodes e JOIN animes a ON e.anime_id = a.id
		ORDER BY e.ep_number
	`)
	if err != nil {
		t.Fatal(err)
	}
	epNumbers := make([]int, 0)
	for rows.Next() {
		var id int64
		var n int
		var dir string
		if err := rows.Scan(&id, &n, &dir); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if dir != "A" {
			rows.Close()
			t.Fatalf("剧集 id=%d 所在番剧目录应为 A，实际 %q（孤儿路径）", id, dir)
		}
		epNumbers = append(epNumbers, n)
	}
	rows.Close()
	// 剧集数据保留：ep_number 1,2,3 都在（ep3 被 reparent、ep1' 并入 ep1）。
	if len(epNumbers) != 3 || epNumbers[0] != 1 || epNumbers[1] != 2 || epNumbers[2] != 3 {
		t.Fatalf("期望 ep_number [1 2 3]，got %v", epNumbers)
	}

	// ep3 的观看进度被保留（reparent 不丢进度）。
	var ep3Watched int
	if err := DB.QueryRow(`
		SELECT wp.watched FROM watch_progress wp
		JOIN episodes e ON wp.episode_id = e.id
		WHERE e.anime_id = 10 AND e.ep_number = 3
	`).Scan(&ep3Watched); err != nil {
		t.Fatal(err)
	}
	if ep3Watched != 1 {
		t.Fatalf("期望 ep3 仍为已看完，got %d", ep3Watched)
	}

	// ep1 冲突合并：watched 保持 1，position 取较大值 200。
	var ep1Pos, ep1Watched int
	if err := DB.QueryRow(`
		SELECT wp.position, wp.watched FROM watch_progress wp
		JOIN episodes e ON wp.episode_id = e.id
		WHERE e.anime_id = 10 AND e.ep_number = 1
	`).Scan(&ep1Pos, &ep1Watched); err != nil {
		t.Fatal(err)
	}
	if ep1Watched != 1 || ep1Pos != 200 {
		t.Fatalf("期望 ep1 合并后 watched=1 position=200，got pos=%d watched=%d", ep1Pos, ep1Watched)
	}
}
