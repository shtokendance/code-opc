//go:build !windows

package repocache

import "os/exec"

func hideSubprocessWindow(_ *exec.Cmd) {}
