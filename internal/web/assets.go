package web

import (
	"embed"
	"io/fs"
)

//go:embed ui/*
var embeddedUI embed.FS

var uiFS fs.FS

func init() {
	sub, err := fs.Sub(embeddedUI, "ui")
	if err != nil {
		panic(err)
	}
	uiFS = sub
}
