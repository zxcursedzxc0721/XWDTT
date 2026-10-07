package backend

// Батч-применение маршрутов интерфейса.
//
// split-списки после импорта больших ip-list (тысячи /32) несовместимы с
// поштучным запуском: каждый netsh — отдельный процесс (~100мс), 10832 адреса
// давали бы ~18 минут подъёма и столько же снятия туннеля. netsh умеет
// выполнять скрипт одним процессом (netsh -f), это ~4000 команд/сек:
// те же 10832 маршрута — около 3 секунд.
//
// Ограничения скрипта netsh (проверено эмпирически):
//   - файл должен быть без BOM (UTF-8 с BOM даёт «неверная директива»);
//   - netsh ПРОДОЛЖАЕТ после ошибочной команды — частичные сбои не валят скрипт;
//   - успешная команда печатает «ОК.»/«Ok.» — мусорить такой вывод в лог нельзя,
//     поэтому он перехватывается и в лог идут только строки-исключения.

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// batchRouteThreshold — до скольких маршрутов держать старое поштучное
// поведение (ошибки видны поимённо в логе). Выше — батч одним процессом.
const batchRouteThreshold = 32

// applyTunnelRoutes добавляет маршруты через интерфейс iface и возвращает
// фактически добавленные (для учёта в teardown). logf может быть nil.
func applyTunnelRoutes(cidrs []string, iface string, logf wgLogFunc) []string {
	logf2 := func(msg string) { if logf != nil { logf(msg) } }
	if len(cidrs) == 0 {
		return nil
	}

	if len(cidrs) <= batchRouteThreshold {
		var added []string
		for _, cidr := range cidrs {
			if err := runCmdWindows("netsh", "interface", "ip", "add", "route", cidr, iface); err != nil {
				logf2(fmt.Sprintf("netsh add route %s failed: %v", cidr, err))
				continue
			}
			added = append(added, cidr)
		}
		return added
	}

	warnings, err := runNetshRouteScript("add", cidrs, iface)
	if err != nil {
		logf2(fmt.Sprintf("netsh batch add route (%d шт.): %v %s", len(cidrs), err, warnings))
	} else if warnings != "" {
		logf2(fmt.Sprintf("netsh batch add route (%d шт.): часть команд не выполнена: %s",
			len(cidrs), warnings))
	}
	return cidrs
}

// deleteTunnelRoutes удаляет маршруты, добавленные applyTunnelRoutes
// (best-effort: отсутствие маршрута не ошибка снятия туннеля).
func deleteTunnelRoutes(cidrs []string, iface string) {
	if len(cidrs) == 0 {
		return
	}
	if len(cidrs) <= batchRouteThreshold {
		for _, cidr := range cidrs {
			_ = runCmdWindows("netsh", "interface", "ip", "delete", "route", cidr, iface)
		}
		return
	}
	if warnings, err := runNetshRouteScript("delete", cidrs, iface); err == nil && warnings != "" {
		log.Printf("netsh batch delete route (%d шт.): %s", len(cidrs), warnings)
	}
}

// runNetshRouteScript выполняет netsh -f со скриптом из cidrs (action:
// "add"|"delete"). Возвращает отфильтрованный вывод (строки-исключения из
// потока «ОК.») и ошибку запуска/выхода процесса.
func runNetshRouteScript(action string, cidrs []string, iface string) (string, error) {
	if len(cidrs) == 0 {
		return "", nil
	}
	f, err := os.CreateTemp("", "xwdtt-routes-*.txt")
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer os.Remove(path)

	w := bufio.NewWriter(f)
	for _, cidr := range cidrs {
		// CRLF — как в проверенных рабочих скриптах netsh
		fmt.Fprintf(w, "interface ip %s route %s %s\r\n", action, cidr, iface)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	cmd := exec.Command("netsh", "-f", path)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return netshWarnings(string(out)), err
}

// netshWarnings оставляет из вывода netsh только строки-исключения:
// успешные команды печатают «ОК.»/«Ok.» — их в лог не тащим.
// Возвращает не больше ~500 символов.
func netshWarnings(out string) string {
	var msgs []string
	for _, line := range strings.Split(out, "\n") {
		s := strings.TrimSpace(line)
		if netshOKLine(s) {
			continue
		}
		if s == "" {
			continue
		}
		msgs = append(msgs, s)
	}
	if len(msgs) == 0 {
		return ""
	}
	res := strings.Join(msgs, "; ")
	if len(res) > 500 {
		res = res[:500] + "…"
	}
	return res
}

// netshOKLine — строка-подтверждение «ОК.» из вывода netsh. Консольные
// утилиты Windows печатают в локали процесса (для русской — cp1251, байты
// CE CA 2E), в тестах может встретиться UTF-8 (D0 9E D0 9A 2E) и латиница
// (Ok.), поэтому проверяется побайтово по объединённому набору символов.
// Любая другая строка (ошибки и пр.) содержит байты вне набора.
func netshOKLine(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == 'o' || c == 'k' || c == 'O' || c == 'K', // латиница
			c == '.' || c == '!' || c == ' ' || c == '\t',
			c == 0xCE || c == 0xCA, // «О»/«К» в cp1251
			c == 0xD0 || c == 0x9E || c == 0x9A: // «О»/«К» в UTF-8
		default:
			return false
		}
	}
	return true
}

// routeSummary — маршруты для лога. split-список в тысячи адресов целиком
// заливал консоль и файл лога — печатаем начало и счётчик.
func routeSummary(routes []string) string {
	const show = 8
	if len(routes) <= show {
		return fmt.Sprintf("%v", routes)
	}
	return fmt.Sprintf("%v … и ещё %d", routes[:show], len(routes)-show)
}
