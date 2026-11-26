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
