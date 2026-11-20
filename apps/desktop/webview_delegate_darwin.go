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
	"net/url"
	"strings"
	"unsafe"

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

func dispatchCustomProtocolURL(app *application.App, rawURL string) {
	if rawURL == "" || app == nil {
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	if u.Scheme != "multica" {
		return
	}
	// multica://auth/callback?token=<jwt>
	if u.Host == "auth" && strings.HasPrefix(u.Path, "/callback") {
		token := u.Query().Get("token")
		if token != "" {
			app.EmitEvent("auth:token", token)
		}
		return
	}
	// multica://open/issue?url=...
	if u.Host == "open" && strings.HasPrefix(u.Path, "/issue") {
		issueURL := u.Query().Get("url")
		if issueURL != "" {
			app.EmitEvent("issue:open", issueURL)
		}
		return
	}
	// multica://invite/<invitationId>
	if u.Host == "invite" {
		invitationID := strings.TrimPrefix(u.Path, "/")
		if invitationID != "" {
			app.EmitEvent("invite:open", invitationID)
		}
		return
	}
}

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

func setupDarwinURLHandler(app *application.App) {
	globalApp = app
	C.SetupDarwinURLHandler()
}
