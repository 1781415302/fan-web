package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// 仓库公开默认密钥：含有这些值即视为不安全，v1.2.4 会生成随机密钥。
const (
	DefaultInsecureSecret  = "default-secret"
	TemplateInsecureSecret = "fan-web-secret-key-change-in-production"
	MediaAudience          = "fan-web-media"
)

type Config struct {
	Configured bool           `yaml:"-"`
	Server     ServerConfig   `yaml:"server"`
	Database   DatabaseConfig `yaml:"database"`
	JWT        JWTConfig      `yaml:"jwt"`
	Admin      AdminConfig    `yaml:"admin"`
	Video      VideoConfig    `yaml:"video"`
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type JWTConfig struct {
	Secret string        `yaml:"secret"`
	Expire time.Duration `yaml:"expire"`
}

// UnmarshalYAML accepts human-readable duration strings such as "168h".
func (c *JWTConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Secret string `yaml:"secret"`
		Expire string `yaml:"expire"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}

	c.Secret = raw.Secret
	if raw.Expire == "" {
		return nil
	}

	expire, err := time.ParseDuration(raw.Expire)
	if err != nil {
		return fmt.Errorf("解析 jwt.expire 失败: %w", err)
	}
	c.Expire = expire
	return nil
}

type AdminConfig struct {
	Username string `yaml:"username"`
	// LegacyPassword 仅用于兼容读取旧 config.yaml 的 admin.password 键，
	// 标记为不序列化；保存后的新格式只输出 admin.username。
	LegacyPassword string `yaml:"-"`
}

// UnmarshalYAML 兼容读取旧 admin.password 键，新格式没有 password 时保持为空。
func (c *AdminConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.Username = raw.Username
	c.LegacyPassword = raw.Password
	return nil
}

type VideoConfig struct {
	RootPath string `yaml:"root_path"`
}

// Default 返回未初始化时的默认配置。
func Default() *Config {
	cfg := &Config{}
	cfg.applyDefaults()
	return cfg
}

// Load 从指定路径加载配置文件。
// 配置文件不存在时返回带默认值的配置，Configured 为 false，调用方可进入初始化流程。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			cfg.Configured = false
			return cfg, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.Configured = true
	cfg.applyDefaults()

	// server.mode 直接进入 gin.SetMode，非法取值会导致 gin panic 崩溃；
	// 与 jwt.expire 等配置项一致，在加载时返回明确错误。
	switch cfg.Server.Mode {
	case "debug", "release", "test":
	default:
		return nil, fmt.Errorf("无效的 server.mode: %q，仅支持 debug/release/test", cfg.Server.Mode)
	}

	return &cfg, nil
}

// PreflightSave 在提交任何持久化修改前，验证目标配置路径的父目录可写且可创建临时文件。
// 用于初始化流程提前暴露配置目录问题，避免先写入数据库后再因配置失败无法回滚。
func (c *Config) PreflightSave(path string) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".preflight-*")
	if err != nil {
		return fmt.Errorf("配置目录不可写: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)
	if err := f.Close(); err != nil {
		return fmt.Errorf("关闭配置预检文件失败: %w", err)
	}
	if err := os.Remove(tmpPath); err != nil {
		return fmt.Errorf("清理配置预检文件失败: %w", err)
	}
	return nil
}

// Save 将配置原子写入指定路径：
// 在同一目录创建隐藏临时文件、完整写入并 Sync、关闭校验，然后 Rename 覆盖目标。
// 任一环节失败都会清理临时文件，目标文件保持不变。
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时配置文件失败: %w", err)
	}
	tmpPath := f.Name()
	cleanup := true
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("设置临时配置文件权限失败: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("同步临时配置文件失败: %w", err)
	}
	closeErr := f.Close()
	closed = true
	if closeErr != nil {
		return fmt.Errorf("关闭临时配置文件失败: %w", closeErr)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	cleanup = false
	return nil
}

func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Mode == "" {
		c.Server.Mode = "debug"
	}
	if c.Database.Path == "" {
		c.Database.Path = "./data/fan-web.db"
	}
	if c.JWT.Expire == 0 {
		c.JWT.Expire = 168 * time.Hour
	}
	if c.Admin.Username == "" {
		c.Admin.Username = "admin"
	}
	// 不填充默认 JWT 密钥，也不填充管理员默认密码：
	// 密钥由首次初始化自动生成，密码只以数据库哈希形式存在。
}

// GenerateJWTSecret 生成 32 字节随机密钥，无填充 Base64 URL 编码。
func GenerateJWTSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机密钥失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// IsInsecureJWTSecret 判断密钥是否为空或等于两个仓库公开默认值。
func IsInsecureJWTSecret(secret string) bool {
	return secret == "" ||
		secret == DefaultInsecureSecret ||
		secret == TemplateInsecureSecret
}
