package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	pathMarker         = "__MULTICA_PATH__"
	shellPathTimeout   = 5 * time.Second
	shellPathWaitDelay = 500 * time.Millisecond
)

// fixProcessPath ensures the GUI app process inherits the user's real shell
// PATH on macOS/Linux (where Finder/Launchpad/desktop entries launch with a
// minimal PATH like /usr/bin:/bin:/usr/sbin:/sbin).
//
// Without this, node/python-based agent CLIs (e.g. codex, pi, opencode) whose
// shebang is `#!/usr/bin/env node` fail `--version` probes and execution with
// `env: node: No such file or directory`, even when resolveAgentsViaLoginShell
// locates the CLI binary itself.
func fixProcessPath() {
	current := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		os.Setenv("PATH", fixWindowsPath(current))
		return
	}

	recovered := resolveLoginShellPATH()
	fallbacks := unixFallbackPathDirs()
	merged := mergePathEntries(recovered, current, fallbacks, string(os.PathListSeparator))
	if merged != "" {
		os.Setenv("PATH", merged)
	}
}

func fixWindowsPath(current string) string {
	var fallbacks []string
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		npmDir := filepath.Join(appData, "npm")
		if info, err := os.Stat(npmDir); err == nil && info.IsDir() {
			fallbacks = append(fallbacks, npmDir)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		localBin := filepath.Join(home, ".local", "bin")
		if info, err := os.Stat(localBin); err == nil && info.IsDir() {
			fallbacks = append(fallbacks, localBin)
		}
	}
	return mergePathEntries(current, "", fallbacks, string(os.PathListSeparator))
}

func unixFallbackPathDirs() []string {
	dirs := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".cargo", "bin"),
		)
	}
	return dirs
}

// mergePathEntries combines recovered login-shell PATH, current process PATH,
// and fallback directories in order, deduplicating entries while preserving
// precedence (login-shell PATH first, fallbacks appended last so nvm/fnm/volta
// are never shadowed by system /usr/local/bin).
func mergePathEntries(primary, secondary string, fallbacks []string, sep string) string {
	seen := make(map[string]struct{})
	var out []string
	add := func(entry string) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return
		}
		if _, exists := seen[entry]; exists {
			return
		}
		seen[entry] = struct{}{}
		out = append(out, entry)
	}

	for _, p := range strings.Split(primary, sep) {
		add(p)
	}
	for _, p := range strings.Split(secondary, sep) {
		add(p)
	}
	for _, p := range fallbacks {
		add(p)
	}
	return strings.Join(out, sep)
}

func resolveLoginShellPATH() string {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		if runtime.GOOS == "darwin" {
			shell = "/bin/zsh"
		} else {
			shell = "/bin/sh"
		}
	}

	base := filepath.Base(shell)
	var script string
	switch base {
	case "zsh", "bash", "sh", "ksh", "mksh", "ash", "dash":
		script = `printf "` + pathMarker + `%s` + pathMarker + `\n" "$PATH"`
	case "fish":
		script = `printf "` + pathMarker + `%s` + pathMarker + `\n" (string join ":" $PATH)`
	default:
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), shellPathTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-ilc", script)
	cmd.WaitDelay = shellPathWaitDelay
	raw, err := cmd.Output()
	if err != nil && len(raw) == 0 {
		return ""
	}
	return extractMarkedPATH(string(raw))
}

func extractMarkedPATH(output string) string {
	start := strings.LastIndex(output, pathMarker)
	if start == -1 {
		return ""
	}
	// Find the matching opening marker before the closing marker.
	prefix := output[:start]
	open := strings.LastIndex(prefix, pathMarker)
	if open == -1 {
		return ""
	}
	return strings.TrimSpace(prefix[open+len(pathMarker):])
}
