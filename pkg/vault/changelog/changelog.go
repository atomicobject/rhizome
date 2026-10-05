// Package changelog embeds and serves CHANGELOG.md content.
package changelog

import (
	_ "embed"
)

//go:embed CHANGELOG.md
var Content string
