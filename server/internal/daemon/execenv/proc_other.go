//go:build !windows

package execenv

import "os/exec"

func hideSubprocessWindow(_ *exec.Cmd) {}
