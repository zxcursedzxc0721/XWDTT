package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"xwdtt/backend"
)

func cmdVersion() int {
	fmt.Printf("XWDTT %s (%s/%s, %s)\n", backend.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	return 0
}

func cmdUpdate() int {
	info, err := backend.CheckUpdate(backend.Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Не удалось проверить обновления: %v\n", err)
		return 1
	}
	if !info.Available {
		fmt.Printf("Обновлений нет — используется последняя версия (%s).\n", backend.Version)
		return 0
	}
	fmt.Printf("Доступно обновление: %s\n", info.Version)
	if info.URL != "" {
		fmt.Printf("Скачать: %s\n", info.URL)
	}
	if body := strings.TrimSpace(info.Body); body != "" {
		fmt.Printf("\n%s\n", body)
	}
	return 0
}

func cmdReport(app *backend.App) int {
	if app == nil {
		app = backend.NewApp(nil)
	}
	fmt.Print(app.GenerateReport(nil))
	return 0
}

func logsCmd(st *backend.Store) int {
	logs := st.ReadLatestLog()
	if logs == "" {
		fmt.Println("Логов пока нет.")
		return 0
	}
	fmt.Print(logs)
	if !strings.HasSuffix(logs, "\n") {
		fmt.Println()
	}
	return 0
}

// ═══════════════════════════════════════════════════
// Мелкие помощники
// ═══════════════════════════════════════════════════

// nonEmpty отфильтровывает пустые хеши (строки попадаются после редактирования).
func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// formatBytes — человекочитаемый размер (Б/КБ/МБ/ГБ).
func formatBytes(n int64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
		gb = 1 << 30
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f ГБ", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.1f МБ", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.1f КБ", float64(n)/kb)
	default:
		return fmt.Sprintf("%d Б", n)
	}
}
