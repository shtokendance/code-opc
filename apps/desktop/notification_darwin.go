//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework UserNotifications
#include <stdlib.h>
#include "notification_darwin.h"
*/
import "C"
import (
	"encoding/json"
	"sync"
	"unsafe"
)

var (
	notificationClickMu      sync.RWMutex
	notificationClickHandler func(map[string]interface{})
)

//export multicaOnNotificationClicked
func multicaOnNotificationClicked(payloadJSON *C.char) {
	raw := ""
	if payloadJSON != nil {
		raw = C.GoString(payloadJSON)
	}
	payload := map[string]interface{}{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &payload)
	}
	notificationClickMu.RLock()
	cb := notificationClickHandler
	notificationClickMu.RUnlock()
	if cb != nil {
		go cb(payload)
	}
}

func initNativeNotifications(onClick func(map[string]interface{})) {
	notificationClickMu.Lock()
	notificationClickHandler = onClick
	notificationClickMu.Unlock()
	C.InitMacNotifications()
}

func showNativeNotification(title, body string, payload map[string]interface{}) {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	cBody := C.CString(body)
	defer C.free(unsafe.Pointer(cBody))

	payloadBytes, _ := json.Marshal(payload)
	cPayload := C.CString(string(payloadBytes))
	defer C.free(unsafe.Pointer(cPayload))

	C.ShowMacNotification(cTitle, cBody, cPayload)
}
