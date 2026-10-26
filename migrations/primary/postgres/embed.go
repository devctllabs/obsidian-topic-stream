package postgresmigrations

import (
	"embed"
)

// Files contains the immutable golang-migrate up/down migration set.
//
//go:embed *.sql
var Files embed.FS
