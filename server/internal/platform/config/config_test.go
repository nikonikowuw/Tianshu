package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tianshu/internal/platform/config"
)

const testProdDatabaseDSN = "postgres://tianshu:prod-strong-db-password-99@db.internal:5432/tianshu?sslmode=require"
const testProdJWTSecret = "super-strong-production-secret-at-least-32-bytes"

func TestLoadWithExplicitAndPrecedence(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "config.toml")
	localPath := filepath.Join(dir, "config.local.toml")

	if err := os.WriteFile(basePath, []byte(`
[server]
addr = ":9000"
[log]
level = "warn"
`), 0o600); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	if err := os.WriteFile(localPath, []byte(`
[log]
level = "debug"
`), 0o600); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	cfg, err := config.LoadWithExplicit("", []string{basePath, localPath})
	if err != nil {
		t.Fatalf("LoadWithExplicit() error = %v", err)
	}
	if cfg.Server.Addr != ":9000" {
		t.Fatalf("Server.Addr = %q, want :9000", cfg.Server.Addr)
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("Log.Level = %q, want debug (local override)", cfg.Log.Level)
	}

	// Explicit missing file must fail loudly instead of silently falling back.
	if _, err := config.LoadWithExplicit(filepath.Join(dir, "missing.toml"), nil); err == nil {
		t.Fatal("expected error for missing explicit config file")
	}
}

func TestBootstrapAdminEnvAliasesAndPartialValidation(t *testing.T) {
	t.Run("full flat env aliases enable bootstrap", func(t *testing.T) {
		t.Setenv("TIANSHU_ADMIN_USERNAME", "rootadmin")
		t.Setenv("TIANSHU_ADMIN_EMAIL", "root@example.com")
		t.Setenv("TIANSHU_ADMIN_PASSWORD", "pass1234")

		cfg, err := config.LoadWithExplicit("", nil)
		if err != nil {
			t.Fatalf("LoadWithExplicit() error = %v", err)
		}
		if !cfg.Bootstrap.Enabled() || cfg.Bootstrap.AdminUsername != "rootadmin" {
			t.Fatalf("expected enabled bootstrap config, got %+v", cfg.Bootstrap)
		}
	})

	t.Run("partial bootstrap env fails validation", func(t *testing.T) {
		t.Setenv("TIANSHU_ADMIN_USERNAME", "rootadmin")
		if _, err := config.LoadWithExplicit("", nil); err == nil {
			t.Fatal("expected error when only partial bootstrap credentials are provided")
		}
	})
}

func TestConfigValidateBranches(t *testing.T) {
	t.Run("rejects invalid app.env and empty server.addr", func(t *testing.T) {
		cfg := config.Default()
		cfg.App.Env = "staging"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for invalid app.env")
		}

		cfg = config.Default()
		cfg.Server.Addr = ""
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for empty server.addr")
		}
	})

	t.Run("rejects empty, invalid, and production weak database.dsn", func(t *testing.T) {
		cfg := config.Default()
		cfg.Database.DSN = ""
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for empty database.dsn")
		}

		cfg = config.Default()
		cfg.Database.DSN = "://bad-dsn"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for unparseable database.dsn")
		}

		// Production rejects default dev DSN.
		cfg = config.Default()
		cfg.App.Env = "production"
		cfg.Auth.JWTSecret = testProdJWTSecret
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected production validation error for default database.dsn")
		}

		// Production rejects weak/empty DB passwords even with custom host/db.
		for _, weakDSN := range []string{
			"postgres://tianshu@db.internal:5432/tianshu?sslmode=require",
			"postgres://tianshu:postgres@db.internal:5432/tianshu?sslmode=require",
			"postgres://tianshu:tianshu@db.internal:5432/tianshu?sslmode=require",
		} {
			cfg.Database.DSN = weakDSN
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected production validation error for weak DSN %q", weakDSN)
			}
		}

		cfg.Database.DSN = testProdDatabaseDSN
		cfg.Auth.CookieSecure = true
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected valid production config, got %v", err)
		}

		// Production must reject a non-Secure refresh cookie.
		cfg.Auth.CookieSecure = false
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected production validation error for auth.cookie_secure=false")
		}
		cfg.Auth.CookieSecure = true
	})

	t.Run("rejects weak/whitespace jwt_secret, prod default secrets, wildcard CORS, negative audit retention, and unknown cache driver", func(t *testing.T) {
		cfg := config.Default()
		cfg.Auth.JWTSecret = strings.Repeat(" ", 40)
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for whitespace-only jwt_secret")
		}

		cfg = config.Default()
		cfg.Auth.JWTSecret = "short"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for short jwt_secret")
		}

		cfg = config.Default()
		cfg.App.Env = "production"
		cfg.Database.DSN = testProdDatabaseDSN
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected production validation error for default jwt_secret")
		}

		cfg.Auth.JWTSecret = testProdJWTSecret
		cfg.Storage.SignedURLSecret = config.Default().Auth.JWTSecret
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected production validation error for default signed_url_secret")
		}

		cfg = config.Default()
		cfg.CORS.Enabled = true
		cfg.CORS.AllowedOrigins = []string{"*"}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for wildcard CORS origin")
		}

		cfg = config.Default()
		cfg.Audit.RetentionDays = -1
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for negative audit.retention_days")
		}

		cfg = config.Default()
		cfg.Cache.Driver = "redis"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for unsupported cache.driver")
		}
	})
}

func TestStorageValidation(t *testing.T) {
	t.Run("rejects unknown driver", func(t *testing.T) {
		cfg := config.Default()
		cfg.Storage.Driver = "gcs"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for unknown storage driver")
		}
	})

	t.Run("s3 requires bucket and credentials", func(t *testing.T) {
		cfg := config.Default()
		cfg.Storage.Driver = "s3"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error when s3 bucket/credentials are missing")
		}

		cfg.Storage.S3.Bucket = "tianshu"
		cfg.Storage.S3.AccessKey = "tianshu"
		cfg.Storage.S3.SecretKey = "tianshu123456"
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected dev s3 config to validate, got %v", err)
		}
	})

	t.Run("production rejects default s3 credentials", func(t *testing.T) {
		cfg := config.Default()
		cfg.App.Env = "production"
		cfg.Database.DSN = testProdDatabaseDSN
		cfg.Auth.JWTSecret = testProdJWTSecret
		cfg.Storage.Driver = "s3"
		cfg.Storage.S3.Bucket = "prod-bucket"
		cfg.Storage.S3.AccessKey = "tianshu"
		cfg.Storage.S3.SecretKey = "tianshu123456"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected production validation error for default s3 credentials")
		}

		cfg.Storage.S3.AccessKey = "prod-access-key"
		cfg.Storage.S3.SecretKey = "prod-secret-key"
		cfg.Auth.CookieSecure = true
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected strong production s3 config to validate, got %v", err)
		}
	})

	t.Run("signed url secret must be strong when configured", func(t *testing.T) {
		cfg := config.Default()
		cfg.Storage.SignedURLSecret = "short"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for short signed_url_secret")
		}
		cfg.Storage.SignedURLSecret = "dedicated-signed-url-secret-at-least-32-bytes"
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected strong signed_url_secret to validate, got %v", err)
		}
	})

	t.Run("production rejects weak s3 credentials even when default driver is local", func(t *testing.T) {
		cfg := config.Default()
		cfg.App.Env = "production"
		cfg.Database.DSN = testProdDatabaseDSN
		cfg.Auth.JWTSecret = testProdJWTSecret
		cfg.Storage.Driver = "local"
		cfg.Storage.S3.Bucket = "legacy-bucket"
		cfg.Storage.S3.AccessKey = "minioadmin"
		cfg.Storage.S3.SecretKey = "minioadmin"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected production validation error when weak s3 credentials are configured under local driver")
		}
	})
}

func TestAuthSessionRetentionValidation(t *testing.T) {
	t.Run("rejects negative session_retention_days", func(t *testing.T) {
		cfg := config.Default()
		cfg.Auth.SessionRetentionDays = -1
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for negative auth.session_retention_days")
		}
	})

	t.Run("accepts zero to disable automatic session purge", func(t *testing.T) {
		cfg := config.Default()
		cfg.Auth.SessionRetentionDays = 0
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected auth.session_retention_days=0 to validate, got %v", err)
		}
	})

	t.Run("accepts positive session_retention_days", func(t *testing.T) {
		cfg := config.Default()
		cfg.Auth.SessionRetentionDays = 14
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected auth.session_retention_days=14 to validate, got %v", err)
		}
	})
}

func TestAuthLoginRateLimitConfig(t *testing.T) {
	t.Run("rejects non-positive login_rate_limit", func(t *testing.T) {
		for _, limit := range []int{-1, 0} {
			cfg := config.Default()
			cfg.Auth.LoginRateLimit = limit
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected validation error for auth.login_rate_limit=%d", limit)
			}
		}
	})

	t.Run("supports env override for positive login_rate_limit", func(t *testing.T) {
		t.Setenv("TIANSHU_AUTH__LOGIN_RATE_LIMIT", "30")
		cfg, err := config.LoadWithExplicit("", nil)
		if err != nil {
			t.Fatalf("LoadWithExplicit() error = %v", err)
		}
		if cfg.Auth.LoginRateLimit != 30 {
			t.Fatalf("Auth.LoginRateLimit = %d, want 30", cfg.Auth.LoginRateLimit)
		}
	})

	t.Run("rejects zero login_rate_limit via env override", func(t *testing.T) {
		t.Setenv("TIANSHU_AUTH__LOGIN_RATE_LIMIT", "0")
		if _, err := config.LoadWithExplicit("", nil); err == nil {
			t.Fatal("expected LoadWithExplicit() validation error for TIANSHU_AUTH__LOGIN_RATE_LIMIT=0")
		}
	})
}

func TestServerRateLimitAndBodyLimitConfig(t *testing.T) {
	t.Run("rejects negative api_rate_limit and max_request_body_bytes", func(t *testing.T) {
		cfg := config.Default()
		cfg.Server.APIRateLimit = -1
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for negative server.api_rate_limit")
		}

		cfg = config.Default()
		cfg.Server.MaxRequestBodyBytes = -1
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error for negative server.max_request_body_bytes")
		}
	})

	t.Run("accepts zero to disable global limit and supports env overrides", func(t *testing.T) {
		t.Setenv("TIANSHU_SERVER__API_RATE_LIMIT", "0")
		t.Setenv("TIANSHU_SERVER__MAX_REQUEST_BODY_BYTES", "2097152")
		cfg, err := config.LoadWithExplicit("", nil)
		if err != nil {
			t.Fatalf("LoadWithExplicit() error = %v", err)
		}
		if cfg.Server.APIRateLimit != 0 {
			t.Fatalf("Server.APIRateLimit = %d, want 0", cfg.Server.APIRateLimit)
		}
		if cfg.Server.MaxRequestBodyBytes != 2097152 {
			t.Fatalf("Server.MaxRequestBodyBytes = %d, want 2097152", cfg.Server.MaxRequestBodyBytes)
		}
	})
}
