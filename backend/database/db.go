package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var DB *sql.DB

var (
	ErrUserNotFound   = sql.ErrNoRows
	ErrUsernameExists = errors.New("username already exists")
)

// Init 初始化数据库连接并创建表。
func Init(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建数据库目录失败: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = db.Close()
		}
	}()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("设置 PRAGMA 失败: %w", err)
		}
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}
	// 数据库文件含 bcrypt 密码哈希与用户名，权限收紧为 0600，与 config.yaml 的
	// 保护标准一致。主文件此刻已存在；WAL/SHM 伴随文件按主文件权限为模板创建，
	// 已存在的（如 PRAGMA journal_mode=WAL 时创建）在此一并收紧。
	if err := tightenDBFilePermissions(dbPath); err != nil {
		return err
	}
	// 迁移前先判断是否存在待应用迁移：若存在，先对当前数据库做一致性快照备份。
	// 迁移一旦提交，旧版会因 validateAppliedMigrations 拒绝降级启动，仅回滚 .old
	// 可执行文件不足以恢复；该快照与 .old 同属一套回滚资产，备份失败则中止启动，
	// 绝不带着无备份的状态冒险执行迁移。
	pending, err := HasPendingMigrations(db)
	if err != nil {
		return fmt.Errorf("检查待应用迁移失败: %w", err)
	}
	if pending {
		if err := BackupDatabase(db, dbPath+preMigrationBackupSuffix); err != nil {
			return fmt.Errorf("迁移前数据库备份失败: %w", err)
		}
	}
	if err := runMigrations(db); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	// 迁移期间可能新创建 -wal/-shm 伴随文件，迁移结束后再次收紧覆盖全部文件。
	if err := tightenDBFilePermissions(dbPath); err != nil {
		return err
	}

	DB = db
	closeOnError = false
	log.Println("数据库初始化成功")
	return nil
}

func InitAdmin(username, password string) error {
	if DB == nil {
		return fmt.Errorf("数据库尚未初始化")
	}

	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成管理员密码哈希失败: %w", err)
	}

	if _, err := DB.Exec(
		"INSERT INTO users (username, password, is_admin) VALUES (?, ?, 1)",
		username, string(hashedPassword),
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.username") {
			return ErrUsernameExists
		}
		return fmt.Errorf("创建管理员账号失败: %w", err)
	}

	log.Printf("管理员账号创建成功: %s\n", username)
	return nil
}

// tightenDBFilePermissions 将数据库主文件及 WAL/SHM 伴随文件权限收紧为 0600。
// 不存在的文件（如新库尚未创建的伴随文件）跳过，后续 SQLite 创建时
// 以主文件权限为模板，自动继承 0600。
func tightenDBFilePermissions(dbPath string) error {
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("设置数据库文件权限失败 (%s): %w", p, err)
		}
	}
	return nil
}
