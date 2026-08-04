package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"fan-web/models"

	"golang.org/x/crypto/bcrypt"
)

const userSelect = `
	SELECT id, username, password, is_admin, created_at
	FROM users
`

func GetUserByID(id int64) (*models.User, error) {
	row := DB.QueryRow(userSelect+" WHERE id = ?", id)
	return scanUser(row)
}

func GetUserByUsername(username string) (*models.User, error) {
	row := DB.QueryRow(userSelect+" WHERE username = ?", username)
	return scanUser(row)
}

func ListUsers() ([]models.User, error) {
	rows, err := DB.Query(userSelect + " ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]models.User, 0)
	for rows.Next() {
		user, err := scanUserFromRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func CreateUser(username, password string, isAdmin bool) (*models.User, error) {
	if _, err := GetUserByUsername(username); err == nil {
		return nil, ErrUsernameExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("生成用户密码哈希失败: %w", err)
	}

	adminValue := 0
	if isAdmin {
		adminValue = 1
	}
	result, err := DB.Exec(
		"INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)",
		username, string(hashedPassword), adminValue,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.username") {
			return nil, ErrUsernameExists
		}
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return GetUserByID(id)
}

func DeleteUser(id int64) error {
	result, err := DB.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func CountAdmins() (int, error) {
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (*models.User, error) {
	return scanUserFromRows(row)
}

func scanUserFromRows(row scanner) (*models.User, error) {
	var user models.User
	var isAdmin int
	if err := row.Scan(
		&user.ID,
		&user.Username,
		&user.Password,
		&isAdmin,
		&user.CreatedAt,
	); err != nil {
		return nil, err
	}
	user.IsAdmin = isAdmin != 0
	return &user, nil
}
