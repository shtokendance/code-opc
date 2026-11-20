//go:build !darwin

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

func attachWebviewUIDelegate(window *application.WebviewWindow) {
	// No-op on Windows/Linux (handled natively by Webview2 / WebKitGTK)
}

func setupDarwinURLHandler(app *application.App) {
	// No-op on Windows/Linux
}
