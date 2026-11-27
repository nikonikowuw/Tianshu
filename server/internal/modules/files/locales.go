package files

import "embed"

// LocalesFS embeds the files module translation files for app-level i18n registration.
//
//go:embed locales/*.toml
var LocalesFS embed.FS
