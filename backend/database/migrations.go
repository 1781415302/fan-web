package database

import (
	"database/sql"
	"fmt"
)

// migration 描述一次顺序数据库迁移。
type migration struct {
	version int
	name    string
	fn      func(tx *sql.Tx) error
}

func (m migration) run(tx *sql.Tx) error {
	if m.fn == nil {
		return fmt.Errorf("迁移 v%d(%s) 缺少执行函数", m.version, m.name)
	}
	return m.fn(tx)
}

// schemaMigrationsDDL 维护迁移元数据表的 DDL。迁移执行器会自行创建该表，
// 并用幂等 DDL 兼容"老库首次升级"的场景。
const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

// runMigrations 按版本升序执行未应用的迁移。每个迁移使用独立事务，
// 并在同一事务中写入 schema_migrations 记录，保证原子性。
func runMigrations(db *sql.DB) error {
	return runMigrationsWith(db, migrations)
}

// runMigrationsWith 是核心执行器，允许测试传入自定义迁移列表。
// 它不做公开导出，仅在本包内使用。
func runMigrationsWith(db *sql.DB, toApply []migration) error {
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		return fmt.Errorf("创建迁移元数据表失败: %w", err)
	}

	applied, err := fetchAppliedVersions(db)
	if err != nil {
		return err
	}

	if err := validateMigrationVersions(toApply); err != nil {
		return err
	}

	for _, m := range toApply {
		if applied[m.version] {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			return fmt.Errorf("应用迁移 v%d(%s) 失败: %w", m.version, m.name, err)
		}
	}
	return nil
}

func fetchAppliedVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("读取已应用迁移失败: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

// validateMigrationVersions 校验迁移版本严格递增且不重复。
func validateMigrationVersions(list []migration) error {
	seen := make(map[int]bool, len(list))
	for i, m := range list {
		if seen[m.version] {
			return fmt.Errorf("迁移版本重复: %d", m.version)
		}
		seen[m.version] = true
		if i > 0 && m.version <= list[i-1].version {
			return fmt.Errorf("迁移版本未按升序排列: %d 后出现 %d", list[i-1].version, m.version)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := m.run(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES (?, ?)",
		m.version, m.name,
	); err != nil {
		return err
	}
	return tx.Commit()
}
