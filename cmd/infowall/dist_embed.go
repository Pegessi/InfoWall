//go:build !dev && embed_frontend
// +build !dev,embed_frontend

package main

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedFS embed.FS

func distFS() fs.FS { return embeddedFS }
