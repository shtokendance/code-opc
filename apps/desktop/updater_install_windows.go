//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func installAndRestart(extractedDir string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("eval executable symlinks: %w", err)
	}

	// Locate .exe file inside extracted directory
	var newExePath string
	err = filepath.Walk(extractedDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".exe") {
			newExePath = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil || newExePath == "" {
		return fmt.Errorf("no .exe executable found in update package")
	}

	oldExe := currentExe + ".old"
	_ = os.Remove(oldExe)

	if err := os.Rename(currentExe, oldExe); err != nil {
		return fmt.Errorf("failed to rename current executable: %w", err)
	}

	if err := os.Rename(newExePath, currentExe); err != nil {
		// Fallback to copy if moving across partitions or volumes
		if copyErr := copyFileContents(newExePath, currentExe); copyErr != nil {
			_ = os.Rename(oldExe, currentExe)
			return fmt.Errorf("failed to copy new executable: %w", copyErr)
		}
	}

	cmd := exec.Command(currentExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to restart executable: %w", err)
	}

	return nil
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
