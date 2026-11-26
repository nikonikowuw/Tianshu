package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	defaultDevJWTSecret = "dev-only-change-me-at-least-32-bytes-secret"
	minJWTSecretLength  = 32
)

// DefaultPaths lists candidate TOML config files loaded in ascending precedence when no explicit path is provided.
var DefaultPaths = []string{
	"configs/config.toml",
	"config.toml",
	"configs/config.local.toml",
	"config.local.toml",
}

// Config holds all runtime configuration loaded from TOML and TIANSHU_* environment variables.
type Config struct {
	App       AppConfig       `koanf:"app"`
	Server    ServerConfig    `koanf:"server"`
	CORS      CORSConfig      `koanf:"cors"`
	Database  DatabaseConfig  `koanf:"database"`
	Cache     CacheConfig     `koanf:"cache"`
	Storage   StorageConfig   `koanf:"storage"`
	Auth      AuthConfig      `koanf:"auth"`
	Bootstrap BootstrapConfig `koanf:"bootstrap"`
	Audit     AuditConfig     `koanf:"audit"`
	Log       LogConfig       `koanf:"log"`
}

// AppConfig defines application environment metadata.
type AppConfig struct {
	Env  string `koanf:"env"`
	Name string `koanf:"name"`
}

// ServerConfig defines HTTP server timeouts and proxy trust boundaries.
type ServerConfig struct {
	Addr            string        `koanf:"addr"`
	ReadTimeout     time.Duration `koanf:"read_timeout"`
	WriteTimeout    time.Duration `koanf:"write_timeout"`
	IdleTimeout     time.Duration `koanf:"idle_timeout"`
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout"`
	TrustedProxies  []string      `koanf:"trusted_proxies"`
}

// CORSConfig configures optional explicit cross-origin access.
type CORSConfig struct {
	Enabled        bool     `koanf:"enabled"`
	AllowedOrigins []string `koanf:"allowed_origins"`
}

// DatabaseConfig defines PostgreSQL pool and auto-migration settings.
type DatabaseConfig struct {
	DSN             string        `koanf:"dsn"`
	AutoMigrate     bool          `koanf:"auto_migrate"`
	MaxConns        int32         `koanf:"max_conns"`
	MinConns        int32         `koanf:"min_conns"`
	MaxConnLifetime time.Duration `koanf:"max_conn_lifetime"`
}

// CacheConfig defines the cache backend (memory default or redis).
type CacheConfig struct {
	Driver        string `koanf:"driver"`
	RedisAddr     string `koanf:"redis_addr"`
	RedisPassword string `koanf:"redis_password"`
	RedisDB       int    `koanf:"redis_db"`
}

// StorageConfig defines object storage driver and size constraints.
type StorageConfig struct {
	Driver             string `koanf:"driver"`
	LocalRoot          string `koanf:"local_root"`
	MaxFileSizeBytes   int64  `koanf:"max_file_size_bytes"`
	MaxAvatarSizeBytes int64  `koanf:"max_avatar_size_bytes"`
}

// AuthConfig defines JWT and refresh token parameters.
type AuthConfig struct {
	JWTSecret       string        `koanf:"jwt_secret"`
	AccessTokenTTL  time.Duration `koanf:"access_token_ttl"`
	RefreshTokenTTL time.Duration `koanf:"refresh_token_ttl"`
	CookieSecure    bool          `koanf:"cookie_secure"`
}

// BootstrapConfig configures optional unattended initial super-admin creation via environment variables.
// When Username, Email, and Password are all set on an uninitialized database, the server seeds the
// initial super administrator automatically on startup and bypasses the web /setup wizard.
type BootstrapConfig struct {
	AdminUsername    string `koanf:"admin_username"`
	AdminEmail       string `koanf:"admin_email"`
	AdminDisplayName string `koanf:"admin_display_name"`
	AdminPassword    string `koanf:"admin_password"`
}

// Enabled reports whether unattended initial administrator bootstrap credentials are fully configured.
func (b BootstrapConfig) Enabled() bool {
	return strings.TrimSpace(b.AdminUsername) != "" &&
		strings.TrimSpace(b.AdminEmail) != "" &&
		b.AdminPassword != ""
}

// AuditConfig defines audit log retention policy.
type AuditConfig struct {
	RetentionDays int `koanf:"retention_days"`
}

// LogConfig defines structured logging verbosity.
type LogConfig struct {
	Level string `koanf:"level"`
}

// Default returns a baseline configuration suitable for local development.
func Default() Config {
	return Config{
		App: AppConfig{
			Env:  "development",
			Name: "tianshu",
		},
		Server: ServerConfig{
			Addr:            ":8080",
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     60 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
		Database: DatabaseConfig{
			DSN:             "postgres://tianshu:tianshu@localhost:5432/tianshu?sslmode=disable",
			AutoMigrate:     true,
			MaxConns:        25,
			MinConns:        2,
			MaxConnLifetime: 30 * time.Minute,
		},
		Cache: CacheConfig{
			Driver: "memory",
		},
		Storage: StorageConfig{
			Driver:             "local",
			LocalRoot:          "./data/uploads",
			MaxFileSizeBytes:   10 << 20,
			MaxAvatarSizeBytes: 2 << 20,
		},
		Auth: AuthConfig{
			JWTSecret:       defaultDevJWTSecret,
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
			CookieSecure:    false,
		},
		Audit: AuditConfig{
			RetentionDays: 180,
		},
		Log: LogConfig{
			Level: "info",
		},
	}
}

// Load reads configuration from optional candidate paths and overrides with TIANSHU_* environment variables.
func Load(paths ...string) (Config, error) {
	return LoadWithExplicit("", paths)
}

// LoadWithExplicit loads explicitPath strictly when non-empty (failing if missing);
// otherwise it loads any existing files from fallbackPaths in order, then applies TIANSHU_* overrides.
func LoadWithExplicit(explicitPath string, fallbackPaths []string) (Config, error) {
	k := koanf.New(".")

	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return Config{}, fmt.Errorf("stat explicit config %s: %w", explicitPath, err)
		}
		if err := k.Load(file.Provider(explicitPath), toml.Parser()); err != nil {
			return Config{}, fmt.Errorf("load config %s: %w", explicitPath, err)
		}
	} else {
		for _, p := range fallbackPaths {
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return Config{}, fmt.Errorf("stat config %s: %w", p, err)
			}
			if err := k.Load(file.Provider(p), toml.Parser()); err != nil {
				return Config{}, fmt.Errorf("load config %s: %w", p, err)
			}
		}
	}

	if err := k.Load(env.Provider(".", env.Opt{
		Prefix: "TIANSHU_",
		TransformFunc: func(key, val string) (string, any) {
			trimmed := strings.TrimPrefix(key, "TIANSHU_")
			normalized := strings.ToLower(strings.ReplaceAll(trimmed, "__", "."))
			return normalized, val
		},
	}), nil); err != nil {
		return Config{}, fmt.Errorf("load env config: %w", err)
	}

	cfg := Default()
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	// Support concise flat env aliases for unattended initial administrator bootstrap.
	applyEnvFallback(&cfg.Bootstrap.AdminUsername, "TIANSHU_ADMIN_USERNAME")
	applyEnvFallback(&cfg.Bootstrap.AdminEmail, "TIANSHU_ADMIN_EMAIL")
	applyEnvFallback(&cfg.Bootstrap.AdminDisplayName, "TIANSHU_ADMIN_DISPLAY_NAME")
	applyEnvFallback(&cfg.Bootstrap.AdminPassword, "TIANSHU_ADMIN_PASSWORD")

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnvFallback(dst *string, envKey string) {
	if *dst != "" {
		return
	}
	if v := os.Getenv(envKey); v != "" {
		*dst = v
	}
}

// IsProduction reports whether the service runs in production mode.
func (c Config) IsProduction() bool {
	return strings.EqualFold(c.App.Env, "production")
}

// Validate enforces security and operational invariants before startup.
func (c Config) Validate() error {
	switch c.App.Env {
	case "development", "production", "test":
	default:
		return fmt.Errorf("invalid app.env %q", c.App.Env)
	}

	if c.Server.Addr == "" {
		return errors.New("server.addr must not be empty")
	}

	if len(c.Auth.JWTSecret) < minJWTSecretLength {
		return fmt.Errorf("auth.jwt_secret must be at least %d bytes", minJWTSecretLength)
	}
	if c.IsProduction() && c.Auth.JWTSecret == defaultDevJWTSecret {
		return errors.New("auth.jwt_secret must not use the default development secret in production")
	}

	hasAnyBootstrap := strings.TrimSpace(c.Bootstrap.AdminUsername) != "" ||
		strings.TrimSpace(c.Bootstrap.AdminEmail) != "" ||
		c.Bootstrap.AdminPassword != ""
	if hasAnyBootstrap && !c.Bootstrap.Enabled() {
		return errors.New("bootstrap admin requires admin_username, admin_email, and admin_password together")
	}

	if c.CORS.Enabled {
		for _, origin := range c.CORS.AllowedOrigins {
			if strings.TrimSpace(origin) == "*" {
				return errors.New("cors.allowed_origins must not contain '*' when credentials are enabled")
			}
		}
	}

	if c.Audit.RetentionDays < 0 {
		return errors.New("audit.retention_days must be >= 0")
	}

	return nil
}
