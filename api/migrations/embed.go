// Package migrations holds the SQL migrations, embedded so the server and the tests use the same files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
