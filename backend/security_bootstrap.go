package main

import (
	"fmt"
	"log"
	"os"

	"fan-web/config"
	"fan-web/database"
)

// prepareConfiguredInstance 处理已配置实例的启动迁移：
// JWT 密钥轮换、明文管理员密码移除、兼容旧式"配置含凭据但数据库为空"的部署。
// 数据库无管理员且配置无旧明文密码时返回错误中止启动（绝不降级为未初始化，
// 防止未认证的 /api/setup 被远程抢注管理员），需要人工介入检查数据库。
// 必须在 database.Init 之后、AuthService 创建之前调用。
// 只使用配置副本工作，磁盘保存成功后才更新传入的内存配置。
func prepareConfiguredInstance(configPath string, cfg *config.Config) error {
	return prepareConfiguredInstanceWithDelete(configPath, cfg, database.DeleteUser)
}

func prepareConfiguredInstanceWithDelete(configPath string, cfg *config.Config, deleteUser func(int64) error) error {
	if !cfg.Configured {
		// 未初始化，交给 Web 初始化页面处理。
		return nil
	}

	adminCount, err := database.CountAdmins()
	if err != nil {
		return fmt.Errorf("查询管理员数量失败: %w", err)
	}

	mustRewrite := false
	next := *cfg

	// JWT 密钥轮换：只在非安全密钥时生成新密钥。
	if config.IsInsecureJWTSecret(next.JWT.Secret) {
		generated, genErr := config.GenerateJWTSecret()
		if genErr != nil {
			return genErr
		}
		next.JWT.Secret = generated
		mustRewrite = true
		log.Println("检测到不安全 JWT 密钥，自动生成新密钥；旧会话需要重新登录一次")
	}

	// 旧明文管理员密码：保存时移除。
	hasLegacyPassword := false
	if next.Admin.LegacyPassword != "" {
		hasLegacyPassword = true
		mustRewrite = true
	}

	if adminCount == 0 {
		if next.Admin.Username != "" && hasLegacyPassword {
			// 旧式部署：配置里有用户名+密码，但数据库还没有管理员。
			if err := next.PreflightSave(configPath); err != nil {
				return err
			}
			user, createErr := database.CreateUser(next.Admin.Username, next.Admin.LegacyPassword, true)
			if createErr != nil {
				return fmt.Errorf("按旧配置创建管理员失败: %w", createErr)
			}
			if mustRewrite {
				if err := next.Save(configPath); err != nil {
					if rollbackErr := deleteUser(user.ID); rollbackErr != nil {
						return fmt.Errorf(
							"迁移配置失败: %v；管理员回滚失败（用户 ID %d）: %v，请人工检查数据库",
							err,
							user.ID,
							rollbackErr,
						)
					}
					return fmt.Errorf("迁移配置失败: %w", err)
				}
			}
			log.Printf("按旧配置创建管理员 %q 并完成配置迁移\n", next.Admin.Username)
		} else {
			// 数据库无管理员且无旧明文密码：绝不静默降级为"未初始化"。
			// 一旦降级，未认证的 POST /api/setup 即可被任何能访问端口的人
			// 抢注管理员（典型场景：正常使用中的部署数据库文件丢失后重启）。
			// 直接报错中止启动，要求人工介入检查数据库。
			return fmt.Errorf(
				"配置已标记为已初始化，但数据库中没有管理员（数据库文件 %s）：请人工检查数据库是否丢失或被误删，恢复数据后重启；不要重新执行 /api/setup 初始化，以免管理员账户被抢注",
				cfg.Database.Path,
			)
		}
	} else {
		// 数据库已有管理员：只做密钥轮换和明文密码清理。
		if mustRewrite {
			if err := next.Save(configPath); err != nil {
				return fmt.Errorf("迁移配置失败: %w", err)
			}
		}
	}

	// 无需重写时，收紧紧配置权限到 0600。
	if !mustRewrite {
		if err := tightenConfigPermissions(configPath); err != nil {
			return err
		}
	}

	// 只在磁盘保存成功后提交内存状态。
	next.Admin.LegacyPassword = ""
	*cfg = next
	return nil
}

// tightenConfigPermissions 将现有配置文件权限收紧为 0600。文件不存在时忽略。
func tightenConfigPermissions(configPath string) error {
	info, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("检查配置文件 %s 失败: %w", configPath, err)
	}
	if info.Mode().Perm() != 0o600 {
		if err := os.Chmod(configPath, 0o600); err != nil {
			return fmt.Errorf("收紧配置文件权限 %s 失败: %w", configPath, err)
		}
		log.Printf("已将配置文件权限收紧为 0600: %s", configPath)
	}
	return nil
}
