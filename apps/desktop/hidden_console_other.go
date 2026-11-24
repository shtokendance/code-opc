//go:build !windows

package main

// ensureHiddenConsole is a no-op on non-Windows platforms.
func ensureHiddenConsole() {}
