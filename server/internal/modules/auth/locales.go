package auth

import "embed"

// LocalesFS embeds auth module translations.
//
//go:embed locales/*.toml
var LocalesFS embed.FS
