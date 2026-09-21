//go:build bindings

package main

import (
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
)

// Wails bindings generation phase compiles this file separately with -tags bindings, without starting Gin/Database, to avoid dependency on local Postgres.
func main() {
	app := NewApp()
	_ = wails.Run(&options.App{
		Title: "WeKnora Lite",
		Bind:  []interface{}{app},
	})
}
