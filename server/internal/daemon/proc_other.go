//go:build !windows

package daemon

import "os/exec"

func hideSubprocessWindow(_ *exec.Cmd) {}
