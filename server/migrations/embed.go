package migrations

import "embed"

// FS embeds all versioned SQL migration files for Goose auto-migration.
//
//go:embed *.sql
var FS embed.FS
