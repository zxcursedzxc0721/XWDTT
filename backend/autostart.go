package backend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func setAutoStart(v bool) error {
	switch runtime.GOOS {
	case "linux":
		return setAutoStartLinux(v)
	case "windows":
		return setAutoStartWindows(v)
	default:
		return fmt.Errorf("unsupported: %s", runtime.GOOS)
	}
}

func setAutoStartLinux(v bool) error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Join(os.Getenv("HOME"), ".config", "autostart")
	path := filepath.Join(dir, "pwdtt.desktop")
	if !v {
		os.Remove(path)
		return nil
	}
	os.MkdirAll(dir, 0o755)
	content := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=PWDTT\nExec=%s\nX-GNOME-Autostart-enabled=true\n", execPath)
	return os.WriteFile(path, []byte(content), 0o644)
}

func setAutoStartWindows(v bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !v {
		cmd := exec.Command("reg", "delete", runKey, "/v", "PWDTT", "/f")
		hideWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil && !strings.Contains(string(out), "не удалось найти") {
			return fmt.Errorf("reg delete: %w — %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	cmd := exec.Command("reg", "add", runKey, "/v", "PWDTT", "/t", "REG_SZ", "/d", exe, "/f")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg add: %w — %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetAutoStartLinux — exported wrapper for testing.
func SetAutoStartLinux(v bool) error {
	return setAutoStartLinux(v)
}
