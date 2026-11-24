//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	user32                    = syscall.NewLazyDLL("user32.dll")
	procAllocConsole          = kernel32.NewProc("AllocConsole")
	procGetConsoleWnd         = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procShowWindow            = user32.NewProc("ShowWindow")
)

const swHide = 0

// ensureHiddenConsole guarantees the desktop GUI application process owns a
// hidden console on Windows. Because GUI applications compiled with -H=windowsgui
// start without an attached console, any subprocess (agent CLIs, git, cmd shims,
// node) would otherwise cause Windows to allocate and display a visible console
// window (conhost.exe) that flashes briefly on screen.
//
// By allocating a console and hiding it immediately at application startup,
// every child and grandchild process inherits this hidden console automatically,
// eliminating visible console popups and CMD window flashes completely.
func ensureHiddenConsole() {
	if hwnd, _, _ := procGetConsoleWnd.Call(); hwnd != 0 {
		if consoleProcessCount() == 1 {
			procShowWindow.Call(hwnd, swHide)
		}
		return
	}
	if r, _, _ := procAllocConsole.Call(); r == 0 {
		return
	}
	if hwnd, _, _ := procGetConsoleWnd.Call(); hwnd != 0 {
		procShowWindow.Call(hwnd, swHide)
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
