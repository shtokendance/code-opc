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
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// attachWebviewUIDelegate ensures the underlying WKWebView has a WKUIDelegate configured
// to handle <input type="file"> open panels (macOS WKWebView requires this).
func attachWebviewUIDelegate(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	handle, err := window.NativeWindowHandle()
	if err != nil || handle == 0 {
		return
	}
	C.SetupWebviewUIDelegate(unsafe.Pointer(handle))
}
