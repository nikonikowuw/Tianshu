package dashboard

import "embed"

// LocalesFS embeds dashboard module translations.
//
//go:embed locales/*.toml
var LocalesFS embed.FS
