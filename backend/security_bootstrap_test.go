package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fan-web/config"
	"fan-web/database"
)

func bootstrapTestDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "bootstrap.db")
	if err := database.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
	})
	return dbPath
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBootstrapCustomSecretNotRotated(t *testing.T) {
	bootstrapTestDB(t)
	path := writeConfig(t, `
server:
  port: 8080
jwt:
  secret: custom-secret-1234
admin:
  username: admin
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Configured = true
	if _, err := database.CreateUser("admin", "password", true); err != nil {
		t.Fatal(err)
	}

	if err := prepareConfiguredInstance(path, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.Secret != "custom-secret-1234" {
		t.Fatalf("custom secret must not rotate, got %q", cfg.JWT.Secret)
	}
}

func TestBootstrapInsecureSecretRotated(t *testing.T) {
	for _, insecureSecret := range []string{config.DefaultInsecureSecret, config.TemplateInsecureSecret} {
		t.Run(insecureSecret, func(t *testing.T) {
			bootstrapTestDB(t)
			path := writeConfig(t, `
server:
  port: 8080
jwt:
  secret: `+insecureSecret+`
admin:
  username: admin
`)
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Configured = true
			if _, err := database.CreateUser("admin", "password", true); err != nil {
				t.Fatal(err)
			}

			if err := prepareConfiguredInstance(path, cfg); err != nil {
				t.Fatal(err)
			}
			if config.IsInsecureJWTSecret(cfg.JWT.Secret) {
				t.Fatal("expected secret to be rotated away from insecure value")
			}
		})
	}
}

func TestBootstrapNoAdminWithLegacyPasswordCreatesOnce(t *testing.T) {
	bootstrapTestDB(t)
	path := writeConfig(t, `
server:
  port: 8080
jwt:
  secret: custom-secret-1234
admin:
  username: legacy-admin
  password: legacy-password
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Configured = true

	if err := prepareConfiguredInstance(path, cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Configured {
		t.Fatal("expected instance to stay configured after creating admin")
	}
	user, err := database.GetUserByUsername("legacy-admin")
	if err != nil {
		t.Fatalf("expected admin created: %v", err)
	}
	_ = user

	// 重新加载并二次启动：不重复创建。
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.Configured = true
	if err := prepareConfiguredInstance(path, reloaded); err != nil {
		t.Fatal(err)
	}
	count, err := database.CountAdmins()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one admin after second boot, got %d", count)
	}
}

func TestBootstrapNoAdminWithoutPasswordFallsBackToSetup(t *testing.T) {
	bootstrapTestDB(t)
	path := writeConfig(t, `
server:
  port: 8080
jwt:
  secret: custom-secret-1234
admin:
  username: admin
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Configured = true

	if err := prepareConfiguredInstance(path, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Configured {
		t.Fatal("expected Configured to become false when no legacy password exists")
	}
	count, err := database.CountAdmins()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no default admin created, got %d", count)
	}
}

func TestBootstrapSaveFailureRefusesStartup(t *testing.T) {
	bootstrapTestDB(t)
	// 目标路径是现有目录：PreflightSave 可通过，但最终 Rename 必然失败，
	// 从而真实覆盖“管理员已创建后保存失败”的回滚路径。
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Configured = true
	cfg.JWT.Secret = "default-secret"
	cfg.Admin.Username = "admin"
	cfg.Admin.LegacyPassword = "password"

	err := prepareConfiguredInstance(path, cfg)
	if err == nil {
		t.Fatal("expected bootstrap to fail when config cannot be saved")
	}
	// 数据库无管理员时，兼容管理员应被回滚。
	count, err := database.CountAdmins()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected rolled-back admin, got %d admins", count)
	}
}

func TestBootstrapReportsRollbackFailure(t *testing.T) {
	bootstrapTestDB(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Configured = true
	cfg.JWT.Secret = "default-secret"
	cfg.Admin.Username = "admin"
	cfg.Admin.LegacyPassword = "password"

	err := prepareConfiguredInstanceWithDelete(path, cfg, func(userID int64) error {
		if userID <= 0 {
			t.Fatalf("rollback must receive the created user ID, got %d", userID)
		}
		return errors.New("forced rollback failure")
	})
	if err == nil {
		t.Fatal("expected bootstrap to report save and rollback failures")
	}
	message := err.Error()
	for _, expected := range []string{"迁移配置失败", "管理员回滚失败", "用户 ID", "人工检查数据库"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected error to contain %q, got %q", expected, message)
		}
	}
}

func TestBootstrapLegacyPasswordClearedFromMemory(t *testing.T) {
	bootstrapTestDB(t)
	path := writeConfig(t, `
server:
  port: 8080
jwt:
  secret: custom-secret-1234
admin:
  username: admin
  password: legacy
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Configured = true
	if _, err := database.CreateUser("admin", "x", true); err != nil {
		t.Fatal(err)
	}
	if err := prepareConfiguredInstance(path, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Admin.LegacyPassword != "" {
		t.Fatalf("LegacyPassword must be cleared in memory, got %q", cfg.Admin.LegacyPassword)
	}
}
