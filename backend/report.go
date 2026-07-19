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

	sb.WriteString("=== PWDTT Bug Report ===\n\n")

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
