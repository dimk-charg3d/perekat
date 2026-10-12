// Package ui holds the interface shared by the desktop window and the phone app.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:frontend
var embedded embed.FS

// Assets returns the files of the interface, with index.html at the root.
func Assets() fs.FS {
	sub, err := fs.Sub(embedded, "frontend")
	if err != nil {
		panic(err)
	}
	return sub
}
