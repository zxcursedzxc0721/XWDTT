package backend

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// LogEntry — структура лога (аналог LogEntry из frontend).
type LogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Time    string `json:"time"`
	Count   int    `json:"count"`
}

// GenerateReport собирает отчёт:
// - Информация об устройстве
// - Полные логи из файла (все строки)
func (a *App) GenerateReport(_ []LogEntry) string {
	var sb strings.Builder

	sb.WriteString("=== XWDTT Bug Report ===\n\n")

	// === Устройство ===
	sb.WriteString("## Device\n")
	sb.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("- Go: %s\n", runtime.Version()))
	sb.WriteString(fmt.Sprintf("- App Version: %s\n", Version))

	if hostname, err := os.Hostname(); err == nil {
		sb.WriteString(fmt.Sprintf("- Hostname: %s\n", hostname))
	}

	sb.WriteString(fmt.Sprintf("- Config: %s\n", configDir()))
	sb.WriteString("\n")

	// === Настройки (без чувствительных данных) ===
	// README/шаблон баг-репорта обещают «состояние настроек» — раньше секции
	// не было вовсе. Секретов здесь нет: только режимные флаги.
	if a.store != nil {
		s := a.store.LoadSettings()
		sb.WriteString("## Settings\n")
		sb.WriteString(fmt.Sprintf("- RouteMode: %s\n", s.RouteMode))
		sb.WriteString(fmt.Sprintf("- ObfsMode: %s\n", s.ObfsMode))
		sb.WriteString(fmt.Sprintf("- ObfsAccepted: %v\n", s.ObfsAccepted))
		sb.WriteString(fmt.Sprintf("- TurnTCP: %v\n", s.TurnTCP))
		sb.WriteString(fmt.Sprintf("- Workers: %d\n", s.Workers))
		sb.WriteString(fmt.Sprintf("- Fingerprint: %s\n", s.Fingerprint))
		sb.WriteString(fmt.Sprintf("- DoH: %s\n", s.DoH))
		sb.WriteString(fmt.Sprintf("- AutoStart: %v\n", s.AutoStart))
		sb.WriteString(fmt.Sprintf("- SplitRoutes: %d записей\n", len(s.SplitRoutes)))
		sb.WriteString("\n")
	}

	// === Полные логи из файла ===
	sb.WriteString("## Logs\n")
	logContent := a.store.ReadLatestLog()
	if logContent == "" {
		sb.WriteString("(no logs)\n")
	} else {
		sb.WriteString("```\n")
		sb.WriteString(logContent)
		sb.WriteString("```\n")
	}

	return sb.String()
}
