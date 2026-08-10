package database

import (
	"database/sql"
	"fmt"
	"strings"
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
	if err := validateMigrationVersions(toApply); err != nil {
		return err
	}
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		return fmt.Errorf("创建迁移元数据表失败: %w", err)
	}

	applied, err := fetchAppliedMigrations(db)
	if err != nil {
		return err
	}
	if err := validateAppliedMigrations(toApply, applied); err != nil {
		return err
	}

	for _, m := range toApply {
		if _, ok := applied[m.version]; ok {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			return fmt.Errorf("应用迁移 v%d(%s) 失败: %w", m.version, m.name, err)
		}
	}
	return nil
}

func fetchAppliedMigrations(db *sql.DB) (map[int]string, error) {
	rows, err := db.Query("SELECT version, name FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("读取已应用迁移失败: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]string)
	for rows.Next() {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return nil, err
		}
		applied[version] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

// validateMigrationVersions 校验迁移版本严格递增且不重复。
func validateMigrationVersions(list []migration) error {
	seen := make(map[int]bool, len(list))
	seenNames := make(map[string]bool, len(list))
	for i, m := range list {
		if m.version <= 0 {
			return fmt.Errorf("迁移版本必须从 1 开始: %d", m.version)
		}
		if strings.TrimSpace(m.name) == "" {
			return fmt.Errorf("迁移 v%d 缺少名称", m.version)
		}
		if seen[m.version] {
			return fmt.Errorf("迁移版本重复: %d", m.version)
		}
		if seenNames[m.name] {
			return fmt.Errorf("迁移名称重复: %s", m.name)
		}
		seen[m.version] = true
		seenNames[m.name] = true
		expectedVersion := i + 1
		if m.version != expectedVersion {
			return fmt.Errorf("迁移版本必须连续: 期望 %d，实际 %d", expectedVersion, m.version)
		}
	}
	return nil
}

// validateAppliedMigrations 阻止旧程序打开由新程序升级过的数据库。
// 数据库记录必须是代码迁移列表从 v1 开始的连续前缀，且名称完全一致。
func validateAppliedMigrations(known []migration, applied map[int]string) error {
	for version, name := range applied {
		if version <= 0 || version > len(known) || known[version-1].version != version {
			return fmt.Errorf("数据库包含当前程序不支持的迁移版本 v%d，拒绝降级启动", version)
		}
		if known[version-1].name != name {
			return fmt.Errorf(
				"数据库迁移 v%d 名称不匹配: 数据库为 %q，程序期望 %q",
				version, name, known[version-1].name,
			)
		}
	}
	for i := 0; i < len(applied); i++ {
		expected := known[i]
		if _, ok := applied[expected.version]; !ok {
			return fmt.Errorf("数据库迁移记录不连续: 缺少 v%d(%s)", expected.version, expected.name)
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
