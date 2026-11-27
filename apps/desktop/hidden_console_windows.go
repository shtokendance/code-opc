//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	user32                    = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWnd         = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procShowWindow            = user32.NewProc("ShowWindow")
)

const swHide = 0

// ensureHiddenConsole hides an attached console window if the GUI process
// somehow inherited one, without calling AllocConsole (which flashes or spawns
// a visible Windows Terminal window on Windows 11). Subprocesses suppress their
// own console windows via CREATE_NO_WINDOW.
func ensureHiddenConsole() {
	if hwnd, _, _ := procGetConsoleWnd.Call(); hwnd != 0 {
		if consoleProcessCount() == 1 {
			procShowWindow.Call(hwnd, swHide)
		}
	}
}

func consoleProcessCount() uint32 {
	var buf [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	return uint32(n)
}
