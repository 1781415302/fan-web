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
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	if err := runMigrations(db); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
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
