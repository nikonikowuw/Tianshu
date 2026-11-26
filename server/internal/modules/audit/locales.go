package audit

import "embed"

// LocalesFS embeds audit module translations.
//
//go:embed locales/*.toml
var LocalesFS embed.FS
