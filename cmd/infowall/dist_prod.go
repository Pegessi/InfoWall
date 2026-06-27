//go:build !dev
// +build !dev

package main

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedFS embed.FS

func distFS() fs.FS { return embeddedFS }
