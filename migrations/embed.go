package migrations

import (
	"embed"
	"io/fs"
)

//go:embed metadata/*.up.sql
var files embed.FS

// Metadata berisi migration database platform, dengan akar di folder metadata.
var Metadata, _ = fs.Sub(files, "metadata")
