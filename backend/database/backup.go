package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
)

// preMigrationBackupSuffix 是迁移前数据库一致性快照的文件后缀。
// 该备份与自更新替换出的 <可执行文件>.old 同属一套回滚资产：
// 迁移一旦提交，旧版会因 validateAppliedMigrations 拒绝降级启动，
// 仅回滚可执行文件不足以恢复，必须连同数据库快照一起回滚。
const preMigrationBackupSuffix = ".pre-migration.bak"

// BackupDatabase 对数据库做一致性快照备份到 backupPath。
// 使用 SQLite 的 "VACUUM INTO"：在 WAL 模式下也能捕获已提交内容，
// 直接复制主数据库文件会漏掉 -wal 中尚未 checkpoint 的提交，不足以回滚。
// 先 VACUUM INTO 临时文件（backupPath+".tmp"），chmod 0600 后再替换目标。
// 已有 dest 先改名为 sidecar，tmp 就位后再删 sidecar；任一步失败都把 dest 还原。
// 注意：VACUUM INTO 的目标不支持绑定参数，只能以转义后的字符串字面量拼入 SQL，
// 路径中的单引号按 SQLite 字面量规则翻倍转义。
func BackupDatabase(db *sql.DB, backupPath string) error {
	tmpPath := backupPath + ".tmp"
	sidecarPath := backupPath + ".prevsnap"
	_ = os.Remove(tmpPath)
	_ = os.Remove(sidecarPath)

	escaped := strings.ReplaceAll(tmpPath, "'", "''")
	if _, err := db.Exec(fmt.Sprintf("VACUUM INTO '%s'", escaped)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("VACUUM INTO 创建迁移前备份失败: %w", err)
	}
	// 备份含 bcrypt 密码哈希与用户名，权限收紧为 0600，与主库一致。
	if err := os.Chmod(tmpPath, 0o600); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("设置迁移前备份权限失败 (%s): %w", tmpPath, err)
	}

	movedDest := false
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Rename(backupPath, sidecarPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("挪开旧的迁移前备份 %s 失败: %w", backupPath, err)
		}
		movedDest = true
	} else if err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("检查迁移前备份 %s 失败: %w", backupPath, err)
	}

	if err := os.Rename(tmpPath, backupPath); err != nil {
		if movedDest {
			_ = os.Rename(sidecarPath, backupPath)
		}
		_ = os.Remove(tmpPath)
		return fmt.Errorf("提交迁移前备份 %s 失败: %w", backupPath, err)
	}
	_ = os.Remove(sidecarPath)
	log.Printf("已创建迁移前数据库备份 %s", backupPath)
	return nil
}

// HasPendingMigrations 判断当前数据库是否存在尚未应用的版本化迁移。
// 只读检查（必要时创建 schema_migrations 元数据表以复用现有查询），绝不应用迁移。
// 返回 true 表示有版本缺失、后续需要执行迁移，调用方应在此之前做一致性备份。
func HasPendingMigrations(db *sql.DB) (bool, error) {
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		return false, fmt.Errorf("创建迁移元数据表失败: %w", err)
	}
	applied, err := fetchAppliedMigrations(db)
	if err != nil {
		return false, err
	}
	for _, m := range migrations {
		if _, ok := applied[m.version]; !ok {
			return true, nil
		}
	}
	return false, nil
}

// CleanupPreMigrationBackup 删除 dbPath 对应的迁移前备份（<dbPath>.pre-migration.bak）。
// 由新版本在绑定到配置端口后调用，与 CleanupUpdateBackup 一并清理整套回滚资产；
// 端口回退时不得调用，以便保留回滚现场。
// 文件不存在视为正常（无待清理），真实失败记录日志并返回错误。
func CleanupPreMigrationBackup(dbPath string) error {
	backupPath := dbPath + preMigrationBackupSuffix
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("检查迁移前备份 %s 失败: %w", backupPath, err)
	}
	if err := os.Remove(backupPath); err != nil {
		log.Printf("清理迁移前备份 %s 失败: %v（可手动删除）", backupPath, err)
		return fmt.Errorf("清理迁移前备份 %s 失败: %w", backupPath, err)
	}
	log.Printf("已清理迁移前数据库备份 %s", backupPath)
	return nil
}
