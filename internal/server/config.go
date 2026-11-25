package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DefaultListen 是管理台默认监听地址：只绑定回环地址，避免误暴露。
	DefaultListen = "127.0.0.1:8080"

	defaultMaxRuns       = 500
	defaultMaxAgeDays    = 30
	defaultMaxLogBytes   = 10 * 1024 * 1024
	defaultMaxConcurrent = 2
)

// DataXConfig 描述 datax 执行器的定位方式。
type DataXConfig struct {
	Bin string `json:"bin"`
}

// RetentionConfig 描述运行记录与日志的保留上限。
type RetentionConfig struct {
	MaxRuns     int   `json:"maxRuns"`
	MaxAgeDays  int   `json:"maxAgeDays"`
	MaxLogBytes int64 `json:"maxLogBytes"`
}

// RuntimeConfig 描述执行器的运行参数。
type RuntimeConfig struct {
	MaxConcurrent int `json:"maxConcurrent"`
}

// AuthConfig 描述管理台的唯一账号与令牌设置。
type AuthConfig struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	TokenTTL  string `json:"tokenTTL"`
	JWTSecret string `json:"jwtSecret"`
}

// Config 是 etl-server 的完整配置。
type Config struct {
	Listen    string          `json:"listen"`
	DataDir   string          `json:"dataDir"`
	Auth      AuthConfig      `json:"auth"`
	DataX     DataXConfig     `json:"datax"`
	Retention RetentionConfig `json:"retention"`
	Runtime   RuntimeConfig   `json:"runtime"`

	// path 是配置文件的加载路径，不参与 JSON 序列化。
	path string
}

// DefaultConfig 返回给定数据目录下的默认配置。
func DefaultConfig(dataDir string) Config {
	return Config{
		Listen:  DefaultListen,
		DataDir: dataDir,
		Auth: AuthConfig{
			TokenTTL: "12h",
		},
		Retention: RetentionConfig{
			MaxRuns:     defaultMaxRuns,
			MaxAgeDays:  defaultMaxAgeDays,
			MaxLogBytes: defaultMaxLogBytes,
		},
		Runtime: RuntimeConfig{
			MaxConcurrent: defaultMaxConcurrent,
		},
	}
}

// WithOverrides 返回应用了非空命令行覆盖值的新配置。
func (c Config) WithOverrides(listen, dataDir string) Config {
	if listen != "" {
		c.Listen = listen
	}
	if dataDir != "" {
		c.DataDir = dataDir
	}
	return c
}

// LogsDir 是运行日志目录。
func (c Config) LogsDir() string { return filepath.Join(c.DataDir, "logs") }

// SnapshotsDir 是运行快照目录。
func (c Config) SnapshotsDir() string { return filepath.Join(c.DataDir, "snapshots") }

// DatabasePath 是管理库文件路径。
func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, "admin.db") }

// LoadOrCreate 读取配置文件；文件不存在时写入默认配置。
// 它只负责配置本身，数据目录由调用方通过 EnsureDataDirs 准备。
func LoadOrCreate(path, dataDir string) (Config, error) {
	cfg := DefaultConfig(dataDir)

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
		applyDefaults(&cfg, dataDir)
		cfg.path = path
		return cfg, nil

	case os.IsNotExist(err):
		cfg.path = path
		if err := Save(path, cfg); err != nil {
			return cfg, err
		}
		return cfg, nil

	default:
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
}

// Save 写入配置文件，并尽力收紧为仅当前用户可读写。
func Save(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	// 已存在的文件不会被 WriteFile 收紧权限；Windows 上这是尽力而为。
	_ = os.Chmod(path, 0o600)
	return nil
}

// EnsureDataDirs 创建数据目录、日志目录与快照目录，并收紧目录权限。
func EnsureDataDirs(cfg Config) error {
	for _, dir := range []string{cfg.DataDir, cfg.LogsDir(), cfg.SnapshotsDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create data directory %s: %w", dir, err)
		}
		_ = os.Chmod(dir, 0o700)
	}
	return nil
}

func applyDefaults(cfg *Config, dataDir string) {
	if cfg.Listen == "" {
		cfg.Listen = DefaultListen
	}
	if cfg.DataDir == "" {
		cfg.DataDir = dataDir
	}
	if cfg.Retention.MaxRuns <= 0 {
		cfg.Retention.MaxRuns = defaultMaxRuns
	}
	if cfg.Retention.MaxAgeDays <= 0 {
		cfg.Retention.MaxAgeDays = defaultMaxAgeDays
	}
	if cfg.Retention.MaxLogBytes <= 0 {
		cfg.Retention.MaxLogBytes = defaultMaxLogBytes
	}
	if cfg.Runtime.MaxConcurrent <= 0 {
		cfg.Runtime.MaxConcurrent = defaultMaxConcurrent
	}
}
