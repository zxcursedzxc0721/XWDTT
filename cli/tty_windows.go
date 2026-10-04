//go:build windows

package cli

// Включение VT-режима (ANSI-цвета) для консоли Windows.
// Без этого старая conhost печатает escape-последовательности как мусор.
//
// DISABLE_NEWLINE_AUTO_RETURN сюда НЕ ставим: он отключает возврат каретки при
// записи "\n", и каждая следующая строка начинается с колонки окончания
// предыдущей — help/логи в интерактиве печатались «лесенкой» (регрессия:
// cli/tty_windows_test.go).

import "golang.org/x/sys/windows"

func enableVT() {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return // не консоль (пайп/файл) — цвета и так отключены
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
