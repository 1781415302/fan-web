package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Configured bool `yaml:"-"`
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

// Save 将配置写入指定路径。
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
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
