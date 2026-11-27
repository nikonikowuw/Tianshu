package i18n_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"tianshu/internal/platform/i18n"
)

func TestBundleBaseValidAndTranslates(t *testing.T) {
	b, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	gotZH := b.Translate(i18n.LangZhCN, "VALIDATION_MIN", map[string]any{"min": 12})
	if !strings.Contains(gotZH, "12") {
		t.Fatalf("zh translation = %q, want param 12", gotZH)
	}

	gotEN := b.Translate(i18n.LangEnUS, "VALIDATION_MIN", map[string]any{"min": 12})
	if !strings.Contains(gotEN, "12") {
		t.Fatalf("en translation = %q, want param 12", gotEN)
	}
}

func TestBundleRejectsDuplicateAndIncompleteKeys(t *testing.T) {
	t.Run("duplicate key fails registration", func(t *testing.T) {
		b, err := i18n.NewBundle()
		if err != nil {
			t.Fatalf("NewBundle() error = %v", err)
		}
		dup := fstest.MapFS{
			"locales/zh-CN.toml": {Data: []byte(`INTERNAL_ERROR = "dup"`)},
			"locales/en-US.toml": {Data: []byte(`INTERNAL_ERROR = "dup"`)},
		}
		if err := b.RegisterFS("dup", dup, "locales"); err == nil {
			t.Fatal("expected duplicate key error, got nil")
		}
	})

	t.Run("missing translation fails validation", func(t *testing.T) {
		b, err := i18n.NewBundle()
		if err != nil {
			t.Fatalf("NewBundle() error = %v", err)
		}
		incomplete := fstest.MapFS{
			"locales/zh-CN.toml": {Data: []byte(`USERS_NOT_FOUND = "用户不存在"`)},
			"locales/en-US.toml": {Data: []byte(``)},
		}
		if err := b.RegisterFS("users", incomplete, "locales"); err != nil {
			t.Fatalf("RegisterFS() error = %v", err)
		}
		if err := b.Validate(); err == nil {
			t.Fatal("expected incomplete locale error, got nil")
		}
	})
}

func TestParseAcceptLanguage(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"", i18n.LangZhCN},
		{"en-US,en;q=0.9,zh-CN;q=0.8", i18n.LangEnUS},
		{"zh-CN,zh;q=0.9", i18n.LangZhCN},
		{"zh-CN;q=0.5, en-US;q=0.9", i18n.LangEnUS},
		{"en-US;q=0, zh-CN;q=1.0", i18n.LangZhCN},
		{"en-US;q=0", i18n.LangZhCN},
		{"en-US;q=1.5, zh-CN;q=0.8", i18n.LangZhCN},
		{"fr-FR,en;q=0.8", i18n.LangEnUS},
		{"english,zhx", i18n.LangZhCN},
		{"en_US", i18n.LangEnUS},
		{"zh_CN", i18n.LangZhCN},
		{"ja-JP", i18n.LangZhCN},
	}
	for _, tc := range tests {
		if got := i18n.ParseAcceptLanguage(tc.header); got != tc.want {
			t.Errorf("ParseAcceptLanguage(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}
