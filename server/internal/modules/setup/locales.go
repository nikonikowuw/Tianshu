package setup

import "embed"

// LocalesFS embeds setup module translations.
//
//go:embed locales/*.toml
var LocalesFS embed.FS
