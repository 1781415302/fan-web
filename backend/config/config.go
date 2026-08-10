package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
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
	Password string `yaml:"password"`
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
	if c.JWT.Secret == "" {
		c.JWT.Secret = "default-secret"
	}
	if c.JWT.Expire == 0 {
		c.JWT.Expire = 168 * time.Hour
	}
	if c.Admin.Username == "" {
		c.Admin.Username = "admin"
	}
	if c.Admin.Password == "" {
		c.Admin.Password = "admin123"
	}
}
