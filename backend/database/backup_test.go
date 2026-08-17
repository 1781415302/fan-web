package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// v1TableDDL 是版本 1 初始化迁移建立的业务表 DDL（不含 schema_migrations），
// 与 migrations_v1.go 的 migrateInitialSchema 保持一致，用于手工构造"仅 v1"老库。
var v1TableDDL = []string{
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
	`CREATE UNIQUE INDEX idx_watch_progress_user_episode
		ON watch_progress(user_id, episode_id);`,
}

// seedV1Database 手工构造一个"仅 v1 迁移已应用"的老库：含 v1 业务表、数据与
// schema_migrations(v1) 记录。构造完毕后关闭连接，交给后续 Init / 回滚流程打开。
func seedV1Database(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, ddl := range v1TableDDL {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("建 v1 表失败: %v", err)
		}
	}
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES (1, 'initial_schema')",
	); err != nil {
		t.Fatal(err)
	}
	data := []string{
		`INSERT INTO users (username, password, is_admin) VALUES ('alice', 'hash', 1)`,
		`INSERT INTO animes (id, title, bangumi_id, ep_count) VALUES (1, 'A', 123, 1)`,
		`INSERT INTO episodes (id, anime_id, ep_number, title, file_path) VALUES (1, 1, 1, 'ep1', '/1')`,
		`INSERT INTO watch_progress (user_id, episode_id, position, watched) VALUES (1, 1, 300, 1)`,
	}
	for _, stmt := range data {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("写入 v1 数据失败: %v", err)
		}
	}
}

// closePackageDB 关闭包级 DB 并置空，避免 Windows 上文件句柄未释放导致后续
// 删除/覆盖数据库文件时被锁。
func closePackageDB() {
	if DB != nil {
		_ = DB.Close()
		DB = nil
	}
}

// assertIndexCount 断言 sqlite_master 中指定名称的索引数量。
func assertIndexCount(t *testing.T, db *sql.DB, name string, want int) {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", name,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("期望索引 %s 数量 %d，got %d", name, want, n)
	}
}

func TestBackupCreatedBeforeMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "upgrade.db")
	backupPath := dbPath + preMigrationBackupSuffix
	seedV1Database(t, dbPath)

	// 新版启动：v1 库有待应用迁移，应先产生迁移前一致快照，再应用 v2。
	DB = nil
	if err := Init(dbPath); err != nil {
		t.Fatalf("升级初始化失败: %v", err)
	}
	defer closePackageDB()

	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("期望迁移前备份存在: %v", err)
	}

	// 独立打开备份文件，验证其为迁移前一致快照。
	backup, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()

	versions := make([]int, 0)
	rows, err := backup.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	rows.Close()
	if len(versions) != 1 || versions[0] != 1 {
		t.Fatalf("期望备份仅含 v1 迁移记录，got %v", versions)
	}

	// 数据完整。
	var userCount int
	if err := backup.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 1 {
		t.Fatalf("期望备份中用户完整，got %d", userCount)
	}
	var title string
	if err := backup.QueryRow("SELECT title FROM animes WHERE id = 1").Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "A" {
		t.Fatalf("期望备份中番剧数据完整，got %q", title)
	}
	var watched int
	if err := backup.QueryRow("SELECT watched FROM watch_progress WHERE episode_id = 1").Scan(&watched); err != nil {
		t.Fatal(err)
	}
	if watched != 1 {
		t.Fatalf("期望备份中观看进度完整，got %d", watched)
	}

	// 备份不含 v2 才建立的唯一索引。
	assertIndexCount(t, backup, "idx_animes_bangumi_id", 0)
	assertIndexCount(t, backup, "idx_episodes_anime_ep", 0)

	// 主库（迁移后）应含 v2 索引，证明备份确实早于迁移。
	assertIndexCount(t, DB, "idx_animes_bangumi_id", 1)
	assertIndexCount(t, DB, "idx_episodes_anime_ep", 1)
}

func TestBackupRestoreAllowsOldBinaryStartup(t *testing.T) {
	// 核心回滚回归：迁移已提交、但新版启动失败（如端口绑定失败）后，
	// 用迁移前快照恢复数据库，旧版（仅认识 v1 迁移）必须能正常打开——这正是
	// 本备份存在的意义，否则旧版会因 validateAppliedMigrations 拒绝降级启动。
	dbPath := filepath.Join(t.TempDir(), "rollback-upgrade.db")
	backupPath := dbPath + preMigrationBackupSuffix
	seedV1Database(t, dbPath)

	// 新版启动并完成 v2 迁移。
	DB = nil
	if err := Init(dbPath); err != nil {
		t.Fatalf("升级初始化失败: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("期望迁移前备份存在: %v", err)
	}
	// 关闭主库连接，模拟进程退出（启动失败）后的回滚现场，释放 Windows 文件锁。
	closePackageDB()

	// 用快照覆盖主库文件，并删除 WAL/SHM 伴随文件，模拟旧版回滚流程。
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			t.Fatalf("删除伴随文件 %s 失败: %v", p, err)
		}
	}

	// 旧版（仅认识 v1 迁移列表）打开恢复后的库必须成功，不得报"拒绝降级启动"。
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := runMigrationsWith(db, migrations[:1]); err != nil {
		t.Fatalf("旧版打开恢复后的数据库失败: %v", err)
	}

	// 数据完整。
	var userCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 1 {
		t.Fatalf("期望恢复后用户完整，got %d", userCount)
	}
	var watched int
	if err := db.QueryRow("SELECT watched FROM watch_progress WHERE episode_id = 1").Scan(&watched); err != nil {
		t.Fatal(err)
	}
	if watched != 1 {
		t.Fatalf("期望恢复后观看进度完整，got %d", watched)
	}

	// 已回滚到 v1：不再含 v2 迁移记录与唯一索引。
	var maxVersion int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&maxVersion); err != nil {
		t.Fatal(err)
	}
	if maxVersion != 1 {
		t.Fatalf("期望恢复后仅 v1 迁移记录，got %d", maxVersion)
	}
	assertIndexCount(t, db, "idx_animes_bangumi_id", 0)
	assertIndexCount(t, db, "idx_episodes_anime_ep", 0)
}

func TestNoBackupWhenNoPendingMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stable.db")
	backupPath := dbPath + preMigrationBackupSuffix

	// 首次初始化应用迁移，会产生备份；随后删除，验证二次初始化（已 v2）不再产生。
	DB = nil
	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("期望首次初始化产生备份: %v", err)
	}
	closePackageDB()
	if err := os.Remove(backupPath); err != nil {
		t.Fatal(err)
	}

	DB = nil
	if err := Init(dbPath); err != nil {
		t.Fatalf("二次初始化失败: %v", err)
	}
	defer closePackageDB()

	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("期望无待迁移时不再产生备份，got %v", err)
	}
}

func TestCleanupPreMigrationBackup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cleanup.db")
	backupPath := dbPath + preMigrationBackupSuffix

	// 备份不存在时视为正常，返回 nil。
	if err := CleanupPreMigrationBackup(dbPath); err != nil {
		t.Fatalf("期望缺失备份清理返回 nil，got %v", err)
	}

	if err := os.WriteFile(backupPath, []byte("snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupPreMigrationBackup(dbPath); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("期望备份已删除，got %v", err)
	}
	// 清理后再清一次同样返回 nil。
	if err := CleanupPreMigrationBackup(dbPath); err != nil {
		t.Fatalf("期望再次清理返回 nil，got %v", err)
	}
}

func TestHasPendingMigrations(t *testing.T) {
	// 全新空库：schema_migrations 为空，v1/v2 均未应用，视为有待迁移。
	dbPath := filepath.Join(t.TempDir(), "pending.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pending, err := HasPendingMigrations(db)
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("期望空库有待应用迁移")
	}

	// 手工应用 v1 后仍有 v2 待应用。
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES (1, 'initial_schema')",
	); err != nil {
		t.Fatal(err)
	}
	pending, err = HasPendingMigrations(db)
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("期望 v1 库仍有 v2 待应用")
	}

	// 补齐 v1+v2 后不再有待应用迁移。
	if _, err := db.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES (2, 'unique_anime_episode_keys')",
	); err != nil {
		t.Fatal(err)
	}
	pending, err = HasPendingMigrations(db)
	if err != nil {
		t.Fatal(err)
	}
	if pending {
		t.Fatal("期望 v2 库无待应用迁移")
	}
}

func TestBackupDatabaseClosedDBLeavesDestUnchanged(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "src.db")
	dest := filepath.Join(dir, "dest.bak")
	marker := []byte("original-dest-bytes-must-survive")
	if err := os.WriteFile(dest, marker, 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := BackupDatabase(db, dest); err == nil {
		t.Fatal("expected BackupDatabase on closed DB to fail")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(marker) {
		t.Fatalf("dest bytes changed on VACUUM failure: %q", got)
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp should be cleaned up after failure, got %v", err)
	}
}

func TestBackupDatabaseReplacesDestAndRemovesSidecar(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "src.db")
	dest := filepath.Join(dir, "dest.bak")
	if err := os.WriteFile(dest, []byte("stale-dest"), 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := BackupDatabase(db, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected dest after success: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "stale-dest" {
		t.Fatal("dest should be the new snapshot, not the old marker")
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp should be gone, got %v", err)
	}
	if _, err := os.Stat(dest + ".prevsnap"); !os.IsNotExist(err) {
		t.Fatalf("sidecar should be gone, got %v", err)
	}
}

func TestBackupDatabaseRestoresSidecarWhenDestMissing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "src.db")
	dest := filepath.Join(dir, "dest.bak")
	sidecar := dest + ".prevsnap"
	marker := []byte("only-copy-in-sidecar")
	if err := os.WriteFile(sidecar, marker, 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := BackupDatabase(db, dest); err == nil {
		t.Fatal("expected BackupDatabase on closed DB to fail")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(marker) {
		t.Fatalf("sidecar should be restored to dest on crash-window entry, got %q", got)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("sidecar should have been moved back to dest, got %v", err)
	}
}
