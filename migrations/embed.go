// Package migrations embeds the SQL migrations into the compiled binaries.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
