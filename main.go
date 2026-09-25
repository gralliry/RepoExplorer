package main

import (
	"context"
	"embed"

	backend "github.com/gralliry/RepoExplorer/internal/app"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// App is a thin binding wrapper kept in package main so the generated Wails
// JavaScript namespace remains window.go.main.App.
type App struct{ *backend.App }

func newApp() *App { return &App{App: backend.New()} }

func (a *App) startup(ctx context.Context) { backend.Startup(a.App, ctx) }

func main() {
	app := newApp()

	err := wails.Run(&options.App{
		Title:     "RepoExplorer",
		Width:     1180,
		Height:    760,
		MinWidth:  900,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 245, G: 246, B: 248, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
