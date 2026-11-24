//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func installAndRestart(extractedDir string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("in-place update not supported on %s", runtime.GOOS)
	}

	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("eval executable symlinks: %w", err)
	}

	// In macOS, currentExe is typically <App>.app/Contents/MacOS/<Executable>
	macosDir := filepath.Dir(currentExe)
	contentsDir := filepath.Dir(macosDir)
	bundleDir := filepath.Dir(contentsDir)

	if !strings.HasSuffix(bundleDir, ".app") {
		return fmt.Errorf("current executable is not running from a .app bundle: %s", bundleDir)
	}

	// Locate .app bundle inside extracted directory
	var newAppBundle string
	entries, err := os.ReadDir(extractedDir)
	if err != nil {
		return fmt.Errorf("read extracted directory: %w", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".app") {
			newAppBundle = filepath.Join(extractedDir, e.Name())
			break
		}
	}
	if newAppBundle == "" {
		return fmt.Errorf("no .app bundle found in update package")
	}

	oldBundle := bundleDir + ".old"
	_ = os.RemoveAll(oldBundle)

	if err := os.Rename(bundleDir, oldBundle); err != nil {
		return fmt.Errorf("failed to backup current .app bundle: %w", err)
	}

	if err := os.Rename(newAppBundle, bundleDir); err != nil {
		_ = os.Rename(oldBundle, bundleDir)
		return fmt.Errorf("failed to move new .app bundle: %w", err)
	}

	// Relaunch using macOS open command
	cmd := exec.Command("open", "-n", bundleDir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to relaunch updated app: %w", err)
	}

	return nil
}
