package database

import (
	"context"
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

// ErrLastAdmin 表示删除该用户后系统将不再有任何管理员。
var ErrLastAdmin = errors.New("cannot delete the last admin")

// DeleteUserWithLastAdminGuard 在单个事务内重新校验"删除后仍至少保留一名管理员"再执行删除，
// 保证"检查-删除"的原子性。CountAdmins + DeleteUser 是两条独立 SQL 语句，MaxOpenConns=1
// 只序列化单条语句：两个管理员并发互相删除时，两个请求都能先读到 count>=2 通过检查，
// 随后各自删除对方，导致管理员被删光。这里把检查与删除放入同一事务（以 serializable
// 隔离级别开启，SQLite 驱动会立即获取写锁），且事务持有唯一的数据库连接直到提交，
// 并发删除请求只能排队等本事务提交后执行，从而只能看到删除后的状态。
func DeleteUserWithLastAdminGuard(id int64) error {
	tx, err := DB.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	var adminCount int
	if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&adminCount); err != nil {
		return err
	}
	if adminCount <= 1 {
		return ErrLastAdmin
	}

	result, err := tx.Exec("DELETE FROM users WHERE id = ?", id)
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
	return tx.Commit()
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
