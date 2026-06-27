//go:build dev
// +build dev

package main

import "io/fs"

func distFS() fs.FS { return nil }
