//go:build embed

// Package web exposes the built React client (web/dist) for embedding into gem-server.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built client, or nil when not embedded.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}
