package identity

import "embed"

// LocalesFS embeds identity module translations.
//
//go:embed locales/*.toml
var LocalesFS embed.FS
