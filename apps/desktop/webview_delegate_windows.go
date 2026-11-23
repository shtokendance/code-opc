//go:build windows

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

//go:embed build/icon.png
var winAppIcon []byte

func attachWebviewUIDelegate(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	handle, err := window.NativeWindowHandle()
	if err != nil || handle == 0 {
		return
	}
	hwnd := w32.HWND(handle)

	// Set small icon (16x16 / 32x32 for title bar and system menu)
	if smallIcon, err := w32.CreateSmallHIconFromImage(winAppIcon); err == nil && smallIcon != 0 {
		w32.SendMessage(hwnd, w32.WM_SETICON, w32.ICON_SMALL, uintptr(smallIcon))
	}

	// Set big icon (32x32 / 48x48 / 256x256 for taskbar and Alt+Tab)
	if bigIcon, err := w32.CreateLargeHIconFromImage(winAppIcon); err == nil && bigIcon != 0 {
		w32.SendMessage(hwnd, w32.WM_SETICON, w32.ICON_BIG, uintptr(bigIcon))
	}
}

func setupDarwinURLHandler(app *application.App) {
	// No-op on Windows
}
