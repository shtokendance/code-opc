package main

import (
	"context"
	"embed"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/embeddeddaemon"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:out/renderer
var assets embed.FS

//go:embed build/icon.png
var appIcon []byte

// spaAssetHandler serves static files and /wails/runtime.js from embedded FS,
// and falls back to index.html for client-side SPA routing (avoiding 404 on reload or deep links).
func spaAssetHandler(assetsFS embed.FS) http.Handler {
	bundledHandler := application.BundledAssetFileServer(assetsFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path
		// Handle nested relative asset paths from sub-route windows (e.g., /slug/issues/assets/foo.js -> /assets/foo.js)
		if idx := strings.Index(reqPath, "/assets/"); idx > 0 {
			r.URL.Path = reqPath[idx:]
			bundledHandler.ServeHTTP(w, r)
			return
		}
		// Direct assets, wails runtime (/wails/runtime.js), or files with standard extensions
		if strings.HasPrefix(reqPath, "/wails/") || strings.HasPrefix(reqPath, "/assets/") || path.Ext(reqPath) != "" {
			bundledHandler.ServeHTTP(w, r)
			return
		}
		// SPA route navigation: rewrite path to / so index.html is returned
		r.URL.Path = "/"
		bundledHandler.ServeHTTP(w, r)
	})
}

func cleanupOldExecutable() {
	if exe, err := os.Executable(); err == nil {
		if clean, err := filepath.EvalSymlinks(exe); err == nil {
			_ = os.Remove(clean + ".old")
			if runtime.GOOS == "darwin" {
				macosDir := filepath.Dir(clean)
				contentsDir := filepath.Dir(macosDir)
				bundleDir := filepath.Dir(contentsDir)
				if strings.HasSuffix(bundleDir, ".app") {
					_ = os.RemoveAll(bundleDir + ".old")
				}
			}
		}
	}
}

func main() {
	ensureHiddenConsole()
	cleanupOldExecutable()
	fixProcessPath()
	service := NewAppService()

	app := application.New(application.Options{
		Name:        "CodeOPC",
		Description: "CodeOPC Desktop Native Client",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: spaAssetHandler(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	service.SetApp(app)
	setupDarwinURLHandler(app)

	// Clean shutdown on quit
	app.OnShutdown(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		embeddeddaemon.DefaultManager.StopAll(ctx)
		service.Shutdown()
	})

	// Setup macOS native Application & Edit menu to enable standard clipboard shortcuts (⌘C, ⌘V, ⌘A, ⌘Z)
	menu := app.Menu.New()
	if runtime.GOOS == "darwin" {
		menu.AddRole(application.AppMenu)
	}
	menu.AddRole(application.FileMenu)
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)
	app.Menu.SetApplicationMenu(menu)

	// Create primary workspace tab window
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:                      "CodeOPC",
		Width:                      1280,
		Height:                     900,
		MinWidth:                   900,
		MinHeight:                  600,
		DefaultContextMenuDisabled: true,
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 50,
		},
	})

	mainWindow.OnWindowEvent(events.Common.WindowShow, func(e *application.WindowEvent) {
		attachWebviewUIDelegate(mainWindow)
	})
	mainWindow.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(e *application.WindowEvent) {
		attachWebviewUIDelegate(mainWindow)
	})

	mainWindow.Show()
	attachWebviewUIDelegate(mainWindow)
	service.SetMainWindow(mainWindow)

	err := app.Run()
	if err != nil {
		println("Error:", err.Error())
	}
}
