//go:build !embed

// Package web exposes the built React client (web/dist) for embedding into gem-server.
package web

import "io/fs"

// Dist returns nil: build with -tags embed (see Makefile) to include web/dist.
func Dist() fs.FS { return nil }
