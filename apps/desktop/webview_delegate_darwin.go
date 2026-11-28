//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
#include "webview_delegate_darwin.h"
*/
import "C"
import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

var globalApp *application.App

//export handleMacURL
func handleMacURL(cURL *C.char) {
	if cURL == nil || globalApp == nil {
		return
	}
	rawURL := C.GoString(cURL)
	dispatchCustomProtocolURL(globalApp, rawURL)
}

// attachWebviewUIDelegate ensures the underlying WKWebView has a WKUIDelegate configured
// to handle <input type="file"> open panels (macOS WKWebView requires this).
func attachWebviewUIDelegate(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	handle := window.NativeWindow()
	if handle == nil {
		return
	}
	C.SetupWebviewUIDelegate(handle)
}

func setupDarwinURLHandler(app *application.App) {
	globalApp = app
	C.SetupDarwinURLHandler()
}
