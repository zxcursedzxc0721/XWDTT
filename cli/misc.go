package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"xwdtt/backend"
)

func cmdVersion() int {
	fmt.Printf("XWDTT %s (%s/%s, %s)\n", backend.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	return 0
}

// cmdUpdate — проверка обновлений; args[0] == "--verify" | "verify" —
// сверка скачанного файла с SHA256SUMS последнего релиза.
// args == nil — из интерактивного режима (только проверка).
func cmdUpdate(args []string) int {
	if len(args) >= 1 && (args[0] == "--verify" || args[0] == "verify") {
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "использование: xwdtt update --verify <скачанный файл>\n")
			return 2
		}
		return cmdVerifyUpdate(args[1])
	}
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
	// Печатаем ожидаемый хеш — пользователь должен сверять его после
	// скачивания, иначе ассет можно подменить без следа.
	if info.SumsURL != "" && info.AssetName != "" {
		sum, err := backend.FetchExpectedSHA256(info.SumsURL, info.AssetName)
		if err != nil {
			fmt.Printf("SHA256SUMS недоступен: %v\n", err)
		} else {
			fmt.Printf("SHA-256: %s\n", sum)
			fmt.Printf("Проверить: xwdtt update --verify <скачанный файл>\n")
		}
	}
	if body := strings.TrimSpace(info.Body); body != "" {
		fmt.Printf("\n%s\n", body)
	}
	return 0
}

// cmdVerifyUpdate сверяет локальный файл с хешем из SHA256SUMS релиза.
// Возвращает 0 — совпал, 1 — не совпало/ошибка.
func cmdVerifyUpdate(path string) int {
	info, err := backend.CheckUpdate(backend.Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Не получить релиз для проверки: %v\n", err)
		return 1
	}
	assetName := info.AssetName
	if base := filepath.Base(path); base != "" {
		// Файл мог быть сохранён под другим именем («(1).exe» и т.п.) —
		// сначала ищем его имя в SHA256SUMS, иначе — имя ассета текущей ОС.
		assetName = base
	}
	sum, err := backend.FetchExpectedSHA256(info.SumsURL, assetName)
	if err != nil && assetName != info.AssetName && info.AssetName != "" {
		sum, err = backend.FetchExpectedSHA256(info.SumsURL, info.AssetName)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Не удалось получить ожидаемый хеш: %v\n", err)
		return 1
	}
	got, err := backend.VerifyFileSHA256(path, sum)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ПРОВЕРКА ПРОВАЛЕНА: %v\n", err)
		if got != "" {
			fmt.Fprintf(os.Stderr, "файл %s, хеш %s\n", path, got)
		}
		return 1
	}
	fmt.Printf("OK: %s\nSHA-256: %s — совпадает с релизом %s\n", path, got, info.Version)
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
