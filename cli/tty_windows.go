//go:build windows

package cli

// Включение VT-режима (ANSI-цвета) для консоли Windows.
// Без этого старая conhost печатает escape-последовательности как мусор.

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
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN)
}
