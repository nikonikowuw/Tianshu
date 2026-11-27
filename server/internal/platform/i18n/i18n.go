package i18n

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	// LangZhCN is the default Simplified Chinese locale.
	LangZhCN = "zh-CN"
	// LangEnUS is the supported English locale.
	LangEnUS = "en-US"
)

var supportedLanguages = []string{LangZhCN, LangEnUS}

//go:embed locales/*.toml
var baseLocalesFS embed.FS

type ctxKey struct{}

// Bundle aggregates base and module TOML translation catalogs.
//
// Concurrency contract: all RegisterFS and Validate calls must complete during
// application startup before concurrent Translate calls begin. After startup,
// the catalog map is immutable and safe for concurrent reads without locking.
type Bundle struct {
	messages map[string]map[string]string
}

// NewBundle creates a Bundle preloaded with base platform translations.
func NewBundle() (*Bundle, error) {
	b := &Bundle{
		messages: make(map[string]map[string]string, len(supportedLanguages)),
	}
	for _, lang := range supportedLanguages {
		b.messages[lang] = make(map[string]string)
	}
	if err := b.RegisterFS("platform", baseLocalesFS, "locales"); err != nil {
		return nil, err
	}
	return b, nil
}

// RegisterFS loads zh-CN.toml and en-US.toml from dir inside fsys, rejecting duplicate keys.
// Must only be invoked during single-threaded startup initialization.
func (b *Bundle) RegisterFS(source string, fsys fs.FS, dir string) error {
	for _, lang := range supportedLanguages {
		filePath := lang + ".toml"
		if dir != "" && dir != "." {
			filePath = dir + "/" + filePath
		}
		raw, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return fmt.Errorf("i18n register %s (%s): %w", source, filePath, err)
		}

		parsed := make(map[string]string)
		if err := toml.Unmarshal(raw, &parsed); err != nil {
			return fmt.Errorf("i18n decode %s (%s): %w", source, filePath, err)
		}

		target := b.messages[lang]
		for key, val := range parsed {
			if _, exists := target[key]; exists {
				return fmt.Errorf("i18n duplicate key %q in locale %s (source %s)", key, lang, source)
			}
			target[key] = val
		}
	}
	return nil
}

// Validate verifies that every supported locale contains the exact same set of keys.
func (b *Bundle) Validate() error {
	base := b.messages[LangZhCN]
	for _, lang := range supportedLanguages {
		if lang == LangZhCN {
			continue
		}
		other := b.messages[lang]
		var missing []string
		for k := range base {
			if _, ok := other[k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			slices.Sort(missing)
			return fmt.Errorf("i18n locale %s missing keys: %s", lang, strings.Join(missing, ", "))
		}

		var extra []string
		for k := range other {
			if _, ok := base[k]; !ok {
				extra = append(extra, k)
			}
		}
		if len(extra) > 0 {
			slices.Sort(extra)
			return fmt.Errorf("i18n locale %s has extra keys missing in %s: %s", lang, LangZhCN, strings.Join(extra, ", "))
		}
	}
	return nil
}

// Translate resolves key for the given locale with fallback to zh-CN and simple {{param}} substitution.
func (b *Bundle) Translate(lang, key string, params map[string]any) string {
	normalized := NormalizeLocale(lang)
	msg, ok := b.messages[normalized][key]
	if !ok {
		msg, ok = b.messages[LangZhCN][key]
		if !ok {
			return key
		}
	}
	if len(params) == 0 {
		return msg
	}
	for k, v := range params {
		msg = strings.ReplaceAll(msg, "{{"+k+"}}", fmt.Sprint(v))
	}
	return msg
}

// ParseAcceptLanguage resolves the highest-priority supported locale from an Accept-Language header,
// honoring RFC 9110 quality weights (q-values) and subtag boundaries.
func ParseAcceptLanguage(header string) string {
	if strings.TrimSpace(header) == "" {
		return LangZhCN
	}

	bestLang := LangZhCN
	bestQ := -1.0

	for part := range strings.SplitSeq(header, ",") {
		segments := strings.Split(part, ";")
		tag := strings.ToLower(strings.TrimSpace(segments[0]))
		if tag == "" {
			continue
		}

		q := 1.0
		validQ := true
		for _, param := range segments[1:] {
			key, val, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
			if err != nil || parsed < 0 || parsed > 1 {
				validQ = false
				break
			}
			q = parsed
		}
		if !validQ || q == 0 {
			continue
		}

		var candidate string
		switch {
		case tag == "en" || strings.HasPrefix(tag, "en-") || strings.HasPrefix(tag, "en_"):
			candidate = LangEnUS
		case tag == "zh" || strings.HasPrefix(tag, "zh-") || strings.HasPrefix(tag, "zh_"):
			candidate = LangZhCN
		default:
			continue
		}

		if q > bestQ {
			bestQ = q
			bestLang = candidate
		}
	}
	return bestLang
}

// NormalizeLocale maps arbitrary language tags onto supported locales.
func NormalizeLocale(lang string) string {
	return ParseAcceptLanguage(lang)
}

// WithLocale stores the resolved request locale in ctx.
func WithLocale(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxKey{}, NormalizeLocale(lang))
}

// LocaleFromContext retrieves the resolved locale from ctx, defaulting to zh-CN.
func LocaleFromContext(ctx context.Context) string {
	if ctx == nil {
		return LangZhCN
	}
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return v
	}
	return LangZhCN
}
