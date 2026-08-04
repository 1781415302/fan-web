package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
	Admin    AdminConfig    `yaml:"admin"`
	Video    VideoConfig    `yaml:"video"`
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

// Load 从指定路径加载配置文件。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Mode == "" {
		cfg.Server.Mode = "debug"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/fan-web.db"
	}
	if cfg.JWT.Secret == "" {
		cfg.JWT.Secret = "default-secret"
	}
	if cfg.JWT.Expire == 0 {
		cfg.JWT.Expire = 168 * time.Hour
	}
	if cfg.Admin.Username == "" {
		cfg.Admin.Username = "admin"
	}
	if cfg.Admin.Password == "" {
		cfg.Admin.Password = "admin123"
	}

	return &cfg, nil
}
