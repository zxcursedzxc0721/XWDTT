//go:build linux

package backend

import "os/exec"

func hideWindow(_ *exec.Cmd) {}
