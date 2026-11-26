package access

import "embed"

// LocalesFS embeds access module translations (role/permission display keys and domain error messages).
//
//go:embed locales/*.toml
var LocalesFS embed.FS
