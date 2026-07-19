package backend_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"pwdtt/backend"
)

func TestSetAutoStartLinux_Create(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := backend.SetAutoStartLinux(true); err != nil {
		t.Fatalf("SetAutoStartLinux(true): %v", err)
	}

	desktopPath := filepath.Join(dir, ".config", "autostart", "pwdtt.desktop")
	data, err := os.ReadFile(desktopPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "[Desktop Entry]") {
		t.Error("missing [Desktop Entry] header")
	}
	if !strings.Contains(content, "Type=Application") {
		t.Error("missing Type=Application")
	}
	if !strings.Contains(content, "Name=PWDTT") {
		t.Error("missing Name=PWDTT")
	}
	if !strings.Contains(content, "X-GNOME-Autostart-enabled=true") {
		t.Error("missing X-GNOME-Autostart-enabled=true")
	}
	if !strings.Contains(content, "Exec=") {
		t.Error("missing Exec= path")
	}
}

func TestSetAutoStartLinux_Remove(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Сначала создаём
	backend.SetAutoStartLinux(true)
	desktopPath := filepath.Join(dir, ".config", "autostart", "pwdtt.desktop")
	if _, err := os.Stat(desktopPath); err != nil {
		t.Fatal("desktop file should exist after SetAutoStartLinux(true)")
	}

	// Удаляем
	if err := backend.SetAutoStartLinux(false); err != nil {
		t.Fatalf("SetAutoStartLinux(false): %v", err)
	}

	if _, err := os.Stat(desktopPath); !os.IsNotExist(err) {
		t.Error("desktop file should be removed after SetAutoStartLinux(false)")
	}
}

func TestSetAutoStartLinux_RemoveNonExistent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Удаление несуществующего файла не должно паниковать
	if err := backend.SetAutoStartLinux(false); err != nil {
		t.Fatalf("SetAutoStartLinux(false) on missing file: %v", err)
	}
}

func TestSetAutoStartLinux_ContentFormat(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	backend.SetAutoStartLinux(true)

	desktopPath := filepath.Join(dir, ".config", "autostart", "pwdtt.desktop")
	data, _ := os.ReadFile(desktopPath)
	content := string(data)

	// Проверяем что Exec указывает на текущий бинарник
	execPath, _ := os.Executable()
	if !strings.Contains(content, "Exec="+execPath) {
		t.Errorf("Exec path mismatch: content has %q, expected contain %q", content, execPath)
	}

	// Проверяем что файл заканчивается переводом строки
	if !strings.HasSuffix(content, "\n") {
		t.Error("desktop file should end with newline")
	}
}

func TestSetAutoStartLinux_Idempotent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Двойной вызов true не должен паниковать
	if err := backend.SetAutoStartLinux(true); err != nil {
		t.Fatalf("first SetAutoStartLinux(true): %v", err)
	}
	if err := backend.SetAutoStartLinux(true); err != nil {
		t.Fatalf("second SetAutoStartLinux(true): %v", err)
	}

	desktopPath := filepath.Join(dir, ".config", "autostart", "pwdtt.desktop")
	if _, err := os.Stat(desktopPath); err != nil {
		t.Fatal("desktop file should exist after double SetAutoStartLinux(true)")
	}
}
