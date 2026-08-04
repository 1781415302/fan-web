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
	if err := createTables(db); err != nil {
		return fmt.Errorf("创建表失败: %w", err)
	}

	DB = db
	closeOnError = false
	log.Println("数据库初始化成功")
	return nil
}

func createTables(db *sql.DB) error {
	tables := []string{
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

	for _, table := range tables {
		if _, err := db.Exec(table); err != nil {
			return err
		}
	}
	return nil
}

// InitAdmin 首次启动时创建管理员账号。
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
