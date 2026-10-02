package cli

// check (Этап 4.5): проверка VK-хешей профиля — перенос runHashChecks из
// proxy-turn-vk-android/go_client/main.go (строки 59–107). Статусы
// ok/captcha/dead/limited/network печатаются в stdout (не форматом HASH_CHECK|).

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"pwdtt/backend"
	core "wg-turn-client"
)

// serversCheck — `pwdtt servers check <имя>`: по каждому хешу запрашивает
// креды у VK и печатает статус. Права администратора не нужны (без маршрутов).
func serversCheck(st *backend.Store, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "использование: pwdtt servers check <имя>")
		return 2
	}
	prof, err := st.LoadProfile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "профиль %q не найден.\n", args[0])
		listProfileNames(st, os.Stderr)
		return 1
	}

	hashes := nonEmpty(prof.Hashes)
	if len(hashes) == 0 {
		fmt.Fprintf(os.Stderr, "в профиле %q нет хешей. Добавьте: pwdtt servers set-hashes %q h1,h2,...\n", args[0], args[0])
		return 1
	}

	fmt.Printf("Проверка хешей (%s): %d\n", args[0], len(hashes))

	failed := 0
	for i, hash := range hashes {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		_, _, turnURLs, err := core.GetCreds(ctx, hash, 9000+i)
		cancel()

		status, message := classifyHashCheckError(err)
		if err == nil {
			status = "ok"
			message = fmt.Sprintf("TURN urls=%d", len(turnURLs))
		}
		mark := "OK "
		if status != "ok" {
			mark = "FAIL"
			failed++
		}
		fmt.Printf("  [%s] %2d. %s  %s: %s\n", mark, i+1, core.ShortHash(hash), status, message)
	}

	if failed > 0 {
		fmt.Printf("Итог: %d из %d хешей не прошли\n", failed, len(hashes))
		return 1
	}
	fmt.Println("Итог: все хеши в порядке")
	return 0
}

// classifyHashCheckError — перенос runHashChecks/classifyHashCheckError:
// текст ошибки VK → человекочитаемый статус. Экспорт — для тестов.
func ClassifyHashCheckError(err error) (string, string) {
	return classifyHashCheckError(err)
}

func classifyHashCheckError(err error) (string, string) {
	if err == nil {
		return "ok", ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "captcha_required") || strings.Contains(text, "captcha_wait_required"):
		return "captcha", "VK просит капчу"
	case strings.Contains(text, "call not found") ||
		strings.Contains(text, "joinconversationbylink") ||
		strings.Contains(text, "missing turn_server") ||
		strings.Contains(text, "9000") ||
		strings.Contains(text, "callunavailable"):
		return "dead", "Звонок не найден или закрыт"
	case strings.Contains(text, "flood") || strings.Contains(text, "rate limit") || strings.Contains(text, "error_code:29"):
		return "limited", "VK временно ограничил запросы"
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline") || strings.Contains(text, "lookup") || strings.Contains(text, "network"):
		return "network", "Сетевая ошибка"
	default:
		return "error", err.Error()
	}
}
