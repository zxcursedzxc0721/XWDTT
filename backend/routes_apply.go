package backend

// Применение маршрутов через интерфейс.
//
// Малые списки (<= batchRouteThreshold) ставятся поштучно через IP Helper API:
// ошибка видна поимённо, маршрут создаётся только активным.
//
// Большие списки (тысячи /32 после `ips import`) несовместимы с поштучным
// применением: netsh умеет выполнять скрипт одним процессом (netsh -f), это
// ~4000 команд/сек (10 832 адреса — около 3 секунд). Скрипт пишется с
// store=active: без него netsh складывает маршруты в постоянное хранилище
// (registry), откуда они не исчезают после краха и перезагрузки и уводят
// трафик мимо/в туннель навсегда.
//
// Ограничения скрипта netsh (проверено эмпирически):
//   - файл должен быть без BOM (UTF-8 с BOM даёт «неверная директива»);
//   - netsh ПРОДОЛЖАЕТ после ошибочной команды — частичные сбои не валят скрипт;
//   - успешная команда печатает «ОК.»/«Ok.» — мусорить таким выводом в лог нельзя,
//     поэтому он перехватывается и в лог идут только строки-исключения.
//
// Поэтому после батча фактическое состояние сверяется с таблицей маршрутизации:
// в activeTunnelRoutes попадают только реально существующие маршруты.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// batchRouteThreshold — до скольких маршрутов применять их поштучно (ошибки
// видны поимённо). Выше — батч одним процессом netsh.
const batchRouteThreshold = 32

// applyTunnelRoutes добавляет маршруты через интерфейс iface и возвращает
// фактически добавленные (для точного снятия в teardown). logf может быть nil.
func applyTunnelRoutes(cidrs []string, iface ipIface, logf wgLogFunc) []ipRoute {
	logf2 := func(msg string) {
		if logf != nil {
			logf(msg)
		}
	}
	if len(cidrs) == 0 {
		return nil
	}

	// Нормализуем и отбрасываем дубли/невалидные/IPv6.
	want := make([]ipRoute, 0, len(cidrs))
	seen := make(map[string]bool, len(cidrs))
	for _, cidr := range cidrs {
		r, err := newRoute(cidr, nil, iface, 0)
		if err != nil {
			logf2(fmt.Sprintf("маршрут пропущен: %v", err))
			continue
		}
		key := r.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		want = append(want, r)
	}
	if len(want) == 0 {
		return nil
	}

	if len(want) <= batchRouteThreshold {
		added := make([]ipRoute, 0, len(want))
		for _, r := range want {
			if err := routeAdd(r); err != nil {
				logf2(fmt.Sprintf("маршрут %s не добавлен: %v", r, err))
				continue
			}
			added = append(added, r)
		}
		return added
	}

	warnings, err := runNetshRouteScript("add", routeStrings(want), iface.Alias)
	if err != nil {
		logf2(fmt.Sprintf("netsh batch add route (%d шт.): %v %s", len(want), err, warnings))
	} else if warnings != "" {
		logf2(fmt.Sprintf("netsh batch add route (%d шт.): часть команд не выполнена: %s",
			len(want), warnings))
	}

	added := filterExistingRoutes(iface, want)
	if missing := len(want) - len(added); missing > 0 {
		logf2(fmt.Sprintf("через туннель не встали %d из %d маршрутов", missing, len(want)))
	}
	return added
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
		// CRLF — как в проверенных рабочих скриптах netsh;
		// store=active — не оставлять маршруты в реестре.
		fmt.Fprintf(w, "interface ipv4 %s route %s \"%s\" store=active\r\n", action, cidr, iface)
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

// withoutCIDR убирает точное совпадение CIDR из списка (для 0.0.0.0/0,
// который в full-режиме добавляется отдельно как перехват default).
func withoutCIDR(cidrs []string, drop string) []string {
	out := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		if c == drop {
			continue
		}
		out = append(out, c)
	}
	return out
}
