package backend

import (
	"os/exec"
	"syscall"
)

const _createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: _createNoWindow,
	}
}
