// Package migrations embeds the SQL schema migrations into the binary so
// the container image is self-contained and applies them at startup.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
