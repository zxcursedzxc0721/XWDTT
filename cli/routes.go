package cli

import (
	"fmt"
	"os"
	"strings"

	"xwdtt/backend"
)

// runRoute — показать/сменить режим маршрутизации.
//
//	xwdtt route           → текущий режим
//	xwdtt route full|split
func runRoute(args []string) int {
	st := backend.NewStore()

	if len(args) == 0 {
		printRoute(st.LoadSettings())
		return 0
	}
	if len(args) != 1 {
		routeUsage()
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		routeUsage()
		return 0
	case backend.RouteModeFull, backend.RouteModeSplit:
		s := st.LoadSettings()
		s.RouteMode = args[0]
		if err := st.SaveSettings(s); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		printRoute(s)
		if args[0] == backend.RouteModeSplit && len(s.SplitRoutes) == 0 {
			fmt.Println("Внимание: список пуст — через VPN не пойдёт ничего.")
			fmt.Println("Добавьте адреса: xwdtt ips add vk.com")
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "неизвестный режим: %s (нужен full или split)\n\n", args[0])
		routeUsage()
		return 2
	}
}

func printRoute(s backend.AppSettings) {
	mode := s.RouteMode
	if mode == "" {
		mode = backend.RouteModeFull
	}
	switch mode {
	case backend.RouteModeSplit:
		fmt.Printf("Режим: split — через VPN только адреса из списка (%d шт.), остальное напрямую\n", len(s.SplitRoutes))
	default:
		fmt.Println("Режим: full — весь трафик через VPN")
	}
}

// runIps — управление глобальным списком адресов split-режима.
//
//	xwdtt ips list
//	xwdtt ips add <ip|cidr|домен>...
//	xwdtt ips import <файл.json>
//	xwdtt ips rm <ip|cidr|домен>...
func runIps(args []string) int {
	return ipsCmd(backend.NewStore(), args)
}

func ipsCmd(st *backend.Store, args []string) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "-h", "--help", "help":
		ipsUsage()
		return 0
	case "list", "ls":
		return ipsList(st)
	case "add":
		return ipsAdd(st, args[1:])
	case "import":
		return ipsImport(st, args[1:])
	case "rm", "delete", "remove":
		return ipsRm(st, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "неизвестная подкоманда: %s\n\n", args[0])
		ipsUsage()
		return 2
	}
}

func ipsList(st *backend.Store) int {
	s := st.LoadSettings()
	if len(s.SplitRoutes) == 0 {
		fmt.Println("Список пуст. Добавьте: xwdtt ips add vk.com")
		return 0
	}
	fmt.Printf("Список split-маршрутов (%d шт.)%s:\n", len(s.SplitRoutes), routeModeSuffix(s))
	for i, e := range s.SplitRoutes {
		kind := "домен — резолвится при подключении"
		if !backend.SplitEntryIsDomain(e) {
			kind = "сеть"
		}
		fmt.Printf("  %2d. %-24s (%s)\n", i+1, e, kind)
	}
	return 0
}

func routeModeSuffix(s backend.AppSettings) string {
	if s.RouteMode == backend.RouteModeSplit {
		return ", режим split включён"
	}
	return ", режим split выключен"
}

func ipsAdd(st *backend.Store, args []string) int {
	if len(args) == 0 {
		ipsUsage()
		return 2
	}
	s := st.LoadSettings()
	existing := make(map[string]bool, len(s.SplitRoutes))
	for _, e := range s.SplitRoutes {
		existing[e] = true
	}

	added, warned := 0, 0
	for _, raw := range args {
		entry, err := backend.NormalizeSplitEntry(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "предупреждение: пропущено — %v\n", err)
			warned++
			continue
		}
		if existing[entry] {
			fmt.Printf("уже в списке: %s\n", entry)
			continue
		}
		if len(s.SplitRoutes) >= backend.MaxSplitRules {
			fmt.Fprintf(os.Stderr, "список полон (максимум %d записей)\n", backend.MaxSplitRules)
			break
		}
		s.SplitRoutes = append(s.SplitRoutes, entry)
		existing[entry] = true
		added++
		fmt.Printf("добавлено: %s\n", entry)
	}

	if added == 0 {
		if warned > 0 {
			return 1
		}
		return 0
	}
	if err := st.SaveSettings(s); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	printRouteHint(s)
	return 0
}

// ipsImport — пакетный импорт адресов из JSON-файла
// ({"сервис": ["ip", ...]} или просто ["ip", ...]).
//
// В отличие от ips add не печатает построчно (файлы на тысячи записей),
// а показывает сводку.
func ipsImport(st *backend.Store, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "использование: xwdtt ips import <файл.json>")
		return 2
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось прочитать файл: %v\n", err)
		return 1
	}
	entries, skipped, err := backend.ParseSplitIPList(data)
	if err != nil {
		// Код 2 — ошибка формата/содержимого файла, а не сбой ввода-вывода.
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	if skipped > 0 {
		fmt.Printf("пропущено некорректных записей: %d\n", skipped)
	}

	s := st.LoadSettings()
	existing := make(map[string]bool, len(s.SplitRoutes))
	for _, e := range s.SplitRoutes {
		existing[e] = true
	}
	added := 0
	for _, entry := range entries {
		if existing[entry] {
			continue
		}
		if len(s.SplitRoutes) >= backend.MaxSplitRules {
			fmt.Fprintf(os.Stderr, "список полон (максимум %d записей), остальное не добавлено\n", backend.MaxSplitRules)
			break
		}
		s.SplitRoutes = append(s.SplitRoutes, entry)
		existing[entry] = true
		added++
	}

	fmt.Printf("в файле: %d записей (%d уникальных), добавлено новых: %d, всего в списке: %d\n",
		len(entries)+skipped, len(entries), added, len(s.SplitRoutes))
	if added == 0 {
		return 0
	}
	if err := st.SaveSettings(s); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	printRouteHint(s)
	return 0
}

func ipsRm(st *backend.Store, args []string) int {
	if len(args) == 0 {
		ipsUsage()
		return 2
	}
	s := st.LoadSettings()

	// Нормализуем удаляемое, чтобы «1.2.3.4» совпало с хранящимся «1.2.3.4/32»
	targets := make(map[string]bool, len(args))
	for _, raw := range args {
		if entry, err := backend.NormalizeSplitEntry(raw); err == nil {
			targets[entry] = true
		} else {
			targets[strings.ToLower(strings.TrimSpace(raw))] = true // попытка «как есть»
		}
	}

	var kept []string
	removed := 0
	for _, e := range s.SplitRoutes {
		if targets[e] {
			fmt.Printf("удалено: %s\n", e)
			removed++
			continue
		}
		kept = append(kept, e)
	}
	if removed == 0 {
		fmt.Fprintln(os.Stderr, "ни одна запись не найдена в списке. Текущий список:")
		ipsList(st)
		return 1
	}
	s.SplitRoutes = kept
	if err := st.SaveSettings(s); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	printRouteHint(s)
	return 0
}

func printRouteHint(s backend.AppSettings) {
	if s.RouteMode == backend.RouteModeSplit {
		fmt.Println("Изменения вступят в силу при следующем подключении.")
	} else {
		fmt.Println("Список сохранён (действует в режиме split: xwdtt route split).")
	}
}

func routeUsage() {
	fmt.Fprint(os.Stderr, `Использование: xwdtt route [full|split]

  full   весь трафик через VPN (по умолчанию)
  split  через VPN только адреса из списка (xwdtt ips)
`)
}

func ipsUsage() {
	fmt.Fprint(os.Stderr, `Использование: xwdtt ips <подкоманда>

  list                    показать список
  add <ip|cidr|домен>...  добавить (домены резолвятся при подключении)
  import <файл.json>      импортировать список из JSON-файла
  rm <ip|cidr|домен>...   удалить

Примеры:
  xwdtt ips add 1.2.3.4 10.0.0.0/8 vk.com
  xwdtt ips import "C:\Downloads\ip-list.json"
  xwdtt route split
`)
}
