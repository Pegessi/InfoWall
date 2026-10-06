//go:build !dev && !embed_frontend
// +build !dev,!embed_frontend

package main

import (
	"embed"
	"io/fs"
)

//go:embed all:frontend_stub
var embeddedFS embed.FS

func distFS() fs.FS {
	root, err := fs.Sub(embeddedFS, "frontend_stub")
	if err != nil {
		panic("embedded frontend stub is unavailable: " + err.Error())
	}
	return root
}
