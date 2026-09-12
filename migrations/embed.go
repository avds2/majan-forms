// Package migrations embeds forward-only database migrations in the binary.
package migrations

import "embed"

// FS contains all numbered SQL migrations.
//
//go:embed *.sql
var FS embed.FS
