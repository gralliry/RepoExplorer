package app

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails backend implementation. main.App embeds this type so the
// JavaScript binding can stay under window.go.main.App.
type App struct {
	ctx context.Context

	// Authentication (see auth.go). The token is persisted to
	// %APPDATA%\RepoExplorer\auth.json.
	authMu sync.Mutex
	auth   authConfig

	loginMu     sync.Mutex
	loginCancel context.CancelFunc
}

func New() *App {
	return &App{}
}

// Startup is called by Wails once the app is ready; we keep the context so the
// download goroutines can talk to the runtime (progress events, dialogs).
func Startup(a *App, ctx context.Context) {
	a.ctx = ctx
	a.loadAuth()
}

// PickFolder opens the native directory chooser and returns the chosen path
// ("" when the user cancels).
func (a *App) PickFolder() string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择下载目录",
	})
	if err != nil {
		return ""
	}
	return dir
}
