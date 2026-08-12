package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func newTestDB(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "migrate-test.db")
	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
}

func TestMigrationsInitializeSchema(t *testing.T) {
	newTestDB(t)

	for _, table := range []string{"users", "animes", "episodes", "watch_progress", "schema_migrations"} {
		var name string
		if err := DB.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name); err != nil {
			t.Fatalf("期望表 %s 存在: %v", table, err)
		}
	}

	var indexCount int
	if err := DB.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_watch_progress_user_episode'",
	).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatalf("期望唯一索引存在，got %d", indexCount)
	}

	var version int
	if err := DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("期望版本 2 已记录，got %d", version)
	}
}

func TestMigrationsAdoptLegacyDatabase(t *testing.T) {
	t.Helper()

	// 手工构建一个"老库"：有业务表和数据，但没有 schema_migrations 表。
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyDDL := []string{
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
	for _, ddl := range legacyDDL {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		"INSERT INTO users (username, password, is_admin) VALUES ('legacy', 'hash', 1)",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO animes (title, ep_count) VALUES ('Legacy Anime', 1)",
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// 用 Init 接管老库。
	DB = nil
	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})

	var userCount int
	if err := DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 1 {
		t.Fatalf("期望老库用户保留，got %d", userCount)
	}

	var version int
	if err := DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("期望接管后记录版本 2，got %d", version)
	}
}

func TestMigrationsMergeDuplicateAnimePreservesData(t *testing.T) {
	// 回归：v2 迁移曾直接 DELETE 重复番剧下的剧集，触发 watch_progress 级联删除，
	// 丢失观看记录。本例构造旧库的重复 bangumi + 冲突集数 + 观看进度，断言迁移后
	// 剧集被 reparent、进度被合并而非丢失。
	dbPath := filepath.Join(t.TempDir(), "dup.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyDDL := []string{
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
	for _, ddl := range legacyDDL {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	mustExec(`INSERT INTO users (username, password, is_admin) VALUES ('u', 'h', 0)`)
	// anime 10（保留）: ep1, ep2；anime 20（重复 bangumi）: ep3 + 与 ep1 冲突的 ep1'。
	mustExec(`INSERT INTO animes (id, title, bangumi_id, ep_count) VALUES (10, 'A', 123, 2)`)
	mustExec(`INSERT INTO animes (id, title, bangumi_id, ep_count) VALUES (20, 'A dup', 123, 2)`)
	mustExec(`INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (1, 10, 1, 'ep1', '/1')`)
	mustExec(`INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (2, 10, 2, 'ep2', '/2')`)
	mustExec(`INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (3, 20, 3, 'ep3', '/3')`)
	mustExec(`INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (4, 20, 1, 'ep1dup', '/4')`)
	// ep3 已看完（bug 报告里会被级联删除的进度）；ep1' 部分观看；ep1 已看完。
	mustExec(`INSERT INTO watch_progress (user_id, episode_id, position, watched) VALUES (1, 3, 600, 1)`)
	mustExec(`INSERT INTO watch_progress (user_id, episode_id, position, watched) VALUES (1, 4, 100, 0)`)
	mustExec(`INSERT INTO watch_progress (user_id, episode_id, position, watched) VALUES (1, 1, 200, 1)`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	DB = nil
	if err := Init(dbPath); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})

	// 仅保留 anime 10。
	var keeperID int64
	if err := DB.QueryRow(`SELECT id FROM animes WHERE bangumi_id = 123`).Scan(&keeperID); err != nil {
		t.Fatal(err)
	}
	if keeperID != 10 {
		t.Fatalf("期望保留 id=10，got %d", keeperID)
	}
	var animeCount int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM animes WHERE bangumi_id = 123`).Scan(&animeCount); err != nil {
		t.Fatal(err)
	}
	if animeCount != 1 {
		t.Fatalf("期望 bangumi_id=123 仅 1 条，got %d", animeCount)
	}

	// 三集都在 anime 10 下：ep_number 1,2,3（ep3 被 reparent 而非删除）。
	rows, err := DB.Query(`SELECT ep_number FROM episodes WHERE anime_id = 10 ORDER BY ep_number`)
	if err != nil {
		t.Fatal(err)
	}
	eps := make([]int, 0)
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		eps = append(eps, n)
	}
	rows.Close()
	if len(eps) != 3 || eps[0] != 1 || eps[1] != 2 || eps[2] != 3 {
		t.Fatalf("期望 ep_number [1 2 3]，got %v", eps)
	}

	// ep3 的观看进度被保留（核心回归点）。
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

	// ep1 冲突合并：watched 保持 1，position 取较大值 200（而非被删）。
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

	// 进度总行数：ep1 + ep3 = 2（ep2 无进度，ep4 已合并进 ep1 而非各自保留/丢失）。
	var progressCount int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM watch_progress`).Scan(&progressCount); err != nil {
		t.Fatal(err)
	}
	if progressCount != 2 {
		t.Fatalf("期望 2 条进度，got %d", progressCount)
	}

	// 唯一索引应已建立。
	for _, idx := range []string{"idx_animes_bangumi_id", "idx_episodes_anime_ep"} {
		var n int
		if err := DB.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx,
		).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("期望索引 %s 存在", idx)
		}
	}
}

func TestMigrationsIdempotentOnReinit(t *testing.T) {
	newTestDB(t)

	dbPath := filepath.Join(t.TempDir(), "reinit.db")
	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	DB.Close()

	if err := Init(dbPath); err != nil {
		t.Fatalf("二次初始化失败: %v", err)
	}
	defer func() {
		_ = DB.Close()
		DB = nil
	}()

	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("期望迁移记录仍为 2 条，got %d", count)
	}
}

func TestMigrationsFailedMigrationRollsBack(t *testing.T) {
	// 使用独立数据库文件，避免污染包级 DB。
	dbPath := filepath.Join(t.TempDir(), "rollback.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	failing := []migration{
		{
			version: 1,
			name:    "will_fail",
			fn: func(tx *sql.Tx) error {
				if _, err := tx.Exec("CREATE TABLE should_not_persist (id INTEGER)"); err != nil {
					return err
				}
				return sql.ErrNoRows
			},
		},
	}
	if err := runMigrationsWith(db, failing); err == nil {
		t.Fatal("期望失败的迁移返回错误")
	}

	var tableCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='should_not_persist'",
	).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 {
		t.Fatal("期望失败迁移的 DDL 已回滚")
	}
}

func TestMigrationsRejectsDuplicateAndOutOfOrder(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "validate.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	duplicate := []migration{
		{version: 1, name: "a", fn: func(tx *sql.Tx) error { return nil }},
		{version: 1, name: "b", fn: func(tx *sql.Tx) error { return nil }},
	}
	if err := runMigrationsWith(db, duplicate); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("期望重复版本报错，got %v", err)
	}

	outOfOrder := []migration{
		{version: 2, name: "a", fn: func(tx *sql.Tx) error { return nil }},
		{version: 1, name: "b", fn: func(tx *sql.Tx) error { return nil }},
	}
	if err := runMigrationsWith(db, outOfOrder); err == nil || !strings.Contains(err.Error(), "连续") {
		t.Fatalf("期望乱序版本报错，got %v", err)
	}
}

func TestMigrationsRejectsDatabaseNewerThanBinary(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "newer.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES (1, 'initial_schema'), (2, 'unique_anime_episode_keys'), (3, 'future_schema')",
	); err != nil {
		t.Fatal(err)
	}

	if err := runMigrationsWith(db, migrations); err == nil || !strings.Contains(err.Error(), "拒绝降级启动") {
		t.Fatalf("期望未知高版本阻止启动，got %v", err)
	}
}

func TestMigrationsRejectsAppliedGap(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gap.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES (1, 'one'), (3, 'three')",
	); err != nil {
		t.Fatal(err)
	}
	known := []migration{
		{version: 1, name: "one", fn: func(tx *sql.Tx) error { return nil }},
		{version: 2, name: "two", fn: func(tx *sql.Tx) error { return nil }},
		{version: 3, name: "three", fn: func(tx *sql.Tx) error { return nil }},
	}

	if err := runMigrationsWith(db, known); err == nil || !strings.Contains(err.Error(), "不连续") {
		t.Fatalf("期望迁移缺口阻止启动，got %v", err)
	}
}

func TestMigrationsRejectsAppliedNameMismatch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "name-mismatch.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES (1, 'unexpected_name')",
	); err != nil {
		t.Fatal(err)
	}

	if err := runMigrationsWith(db, migrations); err == nil || !strings.Contains(err.Error(), "名称不匹配") {
		t.Fatalf("期望迁移名称不匹配阻止启动，got %v", err)
	}
}
