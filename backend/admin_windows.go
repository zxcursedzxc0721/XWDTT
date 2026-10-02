//go:build windows

package backend

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsElevated — есть ли у процесса права администратора.
// Без них route/netsh/wintun на Windows не работают.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

var (
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	procShellExecute = shell32.NewProc("ShellExecuteW")
)

// ElevateAndExit перезапускает процесс с запросом прав администратора
// (UAC-диалог) и завершает текущий. Если пользователь отказал — возвращает ошибку.
//
// Нужен потому, что раньше права задавал Wails-манифест (requireAdministrator);
// консольная версия собирается обычным go build без манифеста, поэтому
// запрашиваем права во время выполнения.
func ElevateAndExit() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("определить путь exe: %w", err)
	}
	args := strings.Join(os.Args[1:], " ")

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(args)
	if err != nil {
		return err
	}

	ret, _, _ := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		0,     // каталог наследуется
		1,     // SW_NORMAL
	)
	if ret <= 32 {
		return fmt.Errorf("запрос прав отклонён (код ShellExecute=%d)", ret)
	}
	os.Exit(0) // новый процесс запущен — этот больше не нужен
	return nil
}
