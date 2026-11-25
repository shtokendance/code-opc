package main

import (
	"os"
	"path/filepath"
	"sync"
)

const (
	appConfigDirName       = ".codeopc"
	legacyAppConfigDirName = ".multica"
)

var migrateConfigDirOnce sync.Once

// getAppConfigDir returns ~/.codeopc, migrating from ~/.multica once if ~/.codeopc
// does not yet exist.
func getAppConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	targetDir := filepath.Join(home, appConfigDirName)
	migrateConfigDirOnce.Do(func() {
		if _, err := os.Stat(targetDir); err == nil {
			return
		}
		legacyDir := filepath.Join(home, legacyAppConfigDirName)
		if info, err := os.Stat(legacyDir); err == nil && info.IsDir() {
			_ = os.Rename(legacyDir, targetDir)
		}
	})
	return targetDir
}
