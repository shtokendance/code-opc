//go:build windows

package localenv

import (
	"os/exec"
	"syscall"
)

const supported = false

func shellCommand(script string) *exec.Cmd {
	cmd := exec.Command("cmd", "/C", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	return cmd
}

func setProcessGroup(*exec.Cmd) {}

func signalGroup(int, syscall.Signal) error { return ErrUnsupported }

func signalProcess(int, syscall.Signal) error { return ErrUnsupported }

func groupAlive(int) bool { return false }

func processGroup(int) int { return 0 }

func processSession(int) int { return 0 }
