// Package migrations exposes versioned SQL migrations embedded in the binary.
// The SQL files in this directory are the single source of truth for schema changes.
package migrations

import "embed"

// FS contains all *.up.sql and *.down.sql files next to this source file.
//
//go:embed *.sql
var FS embed.FS
