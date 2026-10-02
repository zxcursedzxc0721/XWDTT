package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"pwdtt/backend"
)

// runServers — отдельный вызов (без моста): только хранилище.
func runServers(args []string) int {
	return serversCmd(backend.NewStore(), args)
}

func serversCmd(st *backend.Store, args []string) int {
	if len(args) == 0 {
		return serversList(st)
	}
	switch args[0] {
	case "list", "ls":
		return serversList(st)
	case "add":
		return serversAdd(st, args[1:])
	case "show":
		return serversShow(st, args[1:])
	case "rm", "delete", "remove":
		return serversRm(st, args[1:])
	case "set-hashes":
		return serversSetHashes(st, args[1:])
	case "edit":
		return serversEdit(st, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "неизвестная подкоманда: %s\n\n", args[0])
		serversUsage()
		return 2
	}
}

func serversList(st *backend.Store) int {
	profiles := st.ListProfiles()
	if len(profiles) == 0 {
		fmt.Println("Серверов нет. Добавьте: pwdtt servers add \"wdtt://...\"")
		return 0
	}
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ИМЯ\tАДРЕС\tХЕШИ\tTCP\tВОРКЕРЫ")
	for _, n := range names {
		p := profiles[n]
		workers := "-"
		if p.Workers > 0 {
			workers = fmt.Sprint(p.Workers)
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", n, p.PeerAddr, len(nonEmpty(p.Hashes)), yesNo(p.TurnTCP), workers)
	}
	w.Flush()
	return 0
}

func serversAdd(st *backend.Store, args []string) int {
	if len(args) != 1 {
		serversUsage()
		return 2
	}
	link, ok := ParseWdttLink(args[0])
	if !ok {
		fmt.Fprintln(os.Stderr, "не удалось разобрать ссылку. Ожидается wdtt://... или qwdtt://config?...")
		return 2
	}
	name, reused := upsertFromLink(st, link)
	if reused {
		fmt.Printf("Профиль обновлён: %s\n", name)
	} else {
		fmt.Printf("Профиль сохранён: %s\n", name)
	}
	return 0
}

func serversShow(st *backend.Store, args []string) int {
	if len(args) != 1 {
		serversUsage()
		return 2
	}
	p, err := st.LoadProfile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "профиль %q не найден.\n", args[0])
		listProfileNames(st, os.Stderr)
		return 1
	}
	fmt.Printf("Имя:      %s\n", args[0])
	fmt.Printf("Адрес:    %s\n", p.PeerAddr)
	fmt.Printf("Пароль:   %s\n", p.Password)
	fmt.Printf("Хеши (%d): %s\n", len(nonEmpty(p.Hashes)), strings.Join(nonEmpty(p.Hashes), ", "))
	if p.Listen != "" {
		fmt.Printf("Listen:   %s\n", p.Listen)
	}
	if p.TurnHost != "" || p.TurnPort != "" {
		fmt.Printf("TURN:     %s:%s\n", p.TurnHost, p.TurnPort)
	}
	fmt.Printf("TCP:      %s\n", yesNo(p.TurnTCP))
	if p.Workers > 0 {
		fmt.Printf("Воркеры:  %d\n", p.Workers)
	}
	fmt.Printf("DeviceID: %s\n", p.DeviceID)
	return 0
}

func serversRm(st *backend.Store, args []string) int {
	if len(args) != 1 {
		serversUsage()
		return 2
	}
	if err := st.DeleteProfile(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		listProfileNames(st, os.Stderr)
		return 1
	}
	fmt.Printf("Удалён: %s\n", args[0])
	return 0
}

func serversSetHashes(st *backend.Store, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "использование: pwdtt servers set-hashes <имя> h1,h2,...")
		return 2
	}
	p, err := st.LoadProfile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		listProfileNames(st, os.Stderr)
		return 1
	}
	// Принимаем и «h1,h2», и «h1 h2» — склеиваем и разбиваем по запятым.
	var hashes []string
	for _, h := range strings.Split(strings.Join(args[1:], ","), ",") {
		if h = strings.TrimSpace(h); h != "" {
			hashes = append(hashes, h)
		}
	}
	if len(hashes) == 0 {
		fmt.Fprintln(os.Stderr, "нужен хотя бы один хеш")
		return 2
	}
	p.Hashes = hashes
	if err := st.SaveProfile(args[0], *p); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Printf("Хеши обновлены в %q: %d шт.\n", args[0], len(hashes))
	return 0
}

func serversEdit(st *backend.Store, args []string) int {
	flagArgs, posArgs := splitArgs(args, editValueFlags)
	if len(posArgs) != 1 {
		serversUsage()
		return 2
	}
	name := posArgs[0]
	p, err := st.LoadProfile(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		listProfileNames(st, os.Stderr)
		return 1
	}

	np, newName, code := parseEditFlags(flagArgs, p, name)
	if code != 0 {
		return code
	}

	if err := st.SaveProfile(newName, *np); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if newName != name {
		_ = st.DeleteProfile(name)
		fmt.Printf("Сохранено и переименовано: %s → %s\n", name, newName)
	} else {
		fmt.Printf("Сохранено: %s\n", newName)
	}
	return 0
}

func parseEditFlags(flagArgs []string, p *backend.ProfileData, name string) (*backend.ProfileData, string, int) {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	peer := fs.String("peer", p.PeerAddr, "адрес сервера")
	password := fs.String("password", p.Password, "пароль")
	listen := fs.String("listen", p.Listen, "локальный адрес")
	turn := fs.String("turn", p.TurnHost, "переопределение IP TURN")
	port := fs.String("port", p.TurnPort, "переопределение порта TURN")
	turnTCP := fs.Bool("turn-tcp", p.TurnTCP, "TCP-транспорт TURN")
	workers := fs.Int("workers", p.Workers, "воркеры")
	newName := fs.String("name", name, "новое имя профиля")

	if err := fs.Parse(flagArgs); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка флагов: %v\n", err)
		serversUsage()
		return nil, "", 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "лишние аргументы: %s\n", strings.Join(fs.Args(), " "))
		serversUsage()
		return nil, "", 2
	}

	np := *p
	np.PeerAddr = *peer
	np.Password = *password
	np.Listen = *listen
	np.TurnHost = *turn
	np.TurnPort = *port
	np.TurnTCP = *turnTCP
	np.Workers = *workers

	if strings.TrimSpace(np.PeerAddr) == "" {
		fmt.Fprintln(os.Stderr, "адрес не может быть пустым")
		return nil, "", 2
	}
	if strings.TrimSpace(*newName) == "" {
		fmt.Fprintln(os.Stderr, "имя не может быть пустым")
		return nil, "", 2
	}
	return &np, strings.TrimSpace(*newName), 0
}

func serversUsage() {
	fmt.Fprint(os.Stderr, `Использование: pwdtt servers <подкоманда>

  list                                список серверов
  add <wdtt://...>                    добавить/обновить по ссылке
  show <имя>                          показать профиль
  rm <имя>                            удалить профиль
  set-hashes <имя> h1,h2,...          заменить хеши VK
  edit <имя> [-peer A] [-password P] [-name N] [-listen L]
        [-turn H] [-port P] [-turn-tcp] [-workers N]
`)
}

// listProfileNames печатает доступные имена профилей.
func listProfileNames(st *backend.Store, w io.Writer) {
	profiles := st.ListProfiles()
	if len(profiles) == 0 {
		fmt.Fprintln(w, "Доступных серверов нет.")
		return
	}
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "Доступные серверы:")
	for _, n := range names {
		fmt.Fprintf(w, "  %s (%s)\n", n, profiles[n].PeerAddr)
	}
}
