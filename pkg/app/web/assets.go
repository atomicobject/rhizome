package web

import (
	"embed"
	"io/fs"
)

//go:embed assets/dist/*
var embeddedAssets embed.FS

//go:embed openapi.yaml
var embeddedOpenAPIYAML []byte

// Assets returns the embedded UI assets filesystem.
func Assets() fs.FS {
	sub, err := fs.Sub(embeddedAssets, "assets/dist")
	if err != nil {
		return embeddedAssets
	}
	return sub
}
