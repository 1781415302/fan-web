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
	if err := DB.QueryRow("SELECT version FROM schema_migrations").Scan(&version); err != nil {
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
	if err := DB.QueryRow("SELECT version FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("期望接管后记录版本 2，got %d", version)
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
