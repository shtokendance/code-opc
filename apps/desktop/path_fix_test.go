package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestExtractMarkedPATH(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "clean output",
			output: "__MULTICA_PATH__/opt/homebrew/bin:/usr/bin__MULTICA_PATH__\n",
			want:   "/opt/homebrew/bin:/usr/bin",
		},
		{
			name:   "noisy shell rc banner before and after",
			output: "Welcome to zsh!\n__MULTICA_PATH__/Users/u/.nvm/bin:/usr/bin__MULTICA_PATH__\nbye\n",
			want:   "/Users/u/.nvm/bin:/usr/bin",
		},
		{
			name:   "missing markers",
			output: "/usr/bin:/bin",
			want:   "",
		},
		{
			name:   "single marker only",
			output: "__MULTICA_PATH__/usr/bin",
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractMarkedPATH(tc.output); got != tc.want {
				t.Fatalf("extractMarkedPATH(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

func TestMergePathEntries(t *testing.T) {
	got := mergePathEntries(
		"/Users/u/.nvm/bin:/usr/local/bin:/Users/u/.nvm/bin",
		"/usr/bin:/bin:/usr/local/bin",
		[]string{"/opt/homebrew/bin", "/usr/local/bin", "/Users/u/.local/bin"},
		":",
	)
	want := "/Users/u/.nvm/bin:/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin:/Users/u/.local/bin"
	if got != want {
		t.Fatalf("mergePathEntries = %q, want %q", got, want)
	}
}

func TestFixProcessPathUnderMinimalGUIEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS/Linux launchd minimal PATH test")
	}

	origPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", origPath)
	})

	// Simulate Finder / Launchpad minimal GUI PATH.
	_ = os.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	fixProcessPath()

	recovered := os.Getenv("PATH")
	if !strings.Contains(recovered, "/opt/homebrew/bin") || !strings.Contains(recovered, "/usr/bin") {
		t.Fatalf("expected recovered PATH to include fallback and system dirs, got %q", recovered)
	}

	// If codex or pi is installed via nvm/fnm/npm on this machine, its
	// `#!/usr/bin/env node` shebang must now resolve and execute `--version`.
	for _, cli := range []string{"codex", "pi"} {
		path, err := exec.LookPath(cli)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, "--version").CombinedOutput()
		if err != nil {
			t.Fatalf("%s --version failed after fixProcessPath (PATH=%q): %v\noutput: %s", cli, recovered, err, string(out))
		}
		t.Logf("%s (%s) --version => %s", cli, path, strings.TrimSpace(string(out)))
	}
}
