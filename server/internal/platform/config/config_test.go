package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"tianshu/internal/platform/config"
)

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
	t.Setenv("TIANSHU_ADMIN_USERNAME", "rootadmin")
	t.Setenv("TIANSHU_ADMIN_EMAIL", "root@example.com")
	t.Setenv("TIANSHU_ADMIN_PASSWORD", "pass1234")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Bootstrap.Enabled() || cfg.Bootstrap.AdminUsername != "rootadmin" {
		t.Fatalf("expected enabled bootstrap config, got %+v", cfg.Bootstrap)
	}
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
		cfg.Auth.JWTSecret = "super-strong-production-secret-at-least-32-bytes"
		cfg.Storage.Driver = "s3"
		cfg.Storage.S3.Bucket = "prod-bucket"
		cfg.Storage.S3.AccessKey = "tianshu"
		cfg.Storage.S3.SecretKey = "tianshu123456"
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected production validation error for default s3 credentials")
		}

		cfg.Storage.S3.AccessKey = "prod-access-key"
		cfg.Storage.S3.SecretKey = "prod-secret-key"
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
		cfg.Auth.JWTSecret = "super-strong-production-secret-at-least-32-bytes"
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

	t.Run("rejects non-positive login_rate_limit", func(t *testing.T) {
		for _, limit := range []int{-1, 0} {
			cfg := config.Default()
			cfg.Auth.LoginRateLimit = limit
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected validation error for auth.login_rate_limit=%d", limit)
			}
		}
	})
}
