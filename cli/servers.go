package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"xwdtt/backend"
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
	case "-h", "--help", "help":
		serversUsage()
		return 0
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
	case "check":
		return serversCheck(st, args[1:])
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
	broken := st.ListBrokenProfiles()
	if len(profiles) == 0 && len(broken) == 0 {
		fmt.Println("Серверов нет. Добавьте: xwdtt servers add \"wdtt://...\"")
		return 0
	}
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	if len(names) > 0 {
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
	}

	// Битые профили раньше не были видны нигде, а rm удалял их молча.
	if len(broken) > 0 {
		sort.Strings(broken)
		fmt.Println("\nПовреждённые профили (файл есть, JSON не читается):")
		for _, n := range broken {
			fmt.Printf("  %s — показать: servers show %q, удалить с копией: servers rm %q --force\n", n, n, n)
		}
	}
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
	name, reused, err := upsertFromLink(st, link)
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось сохранить профиль: %v\n", err)
		return 1
	}
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
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if !st.ProfileExists(args[0]) {
			listProfileNames(st, os.Stderr)
		}
		return 1
	}
	fmt.Printf("Имя:      %s\n", backend.CanonicalProfileName(args[0]))
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
	if p.DirectPort != "" || p.RawPort != "" {
		fmt.Printf("Прямые:   -notls→порт %s   -raw→порт %s\n",
			peerPortOr(p.DirectPort, defaultDirectPort), peerPortOr(p.RawPort, defaultRawPort))
	}
	fmt.Printf("DeviceID: %s\n", p.DeviceID)
	return 0
}

func serversRm(st *backend.Store, args []string) int {
	force := false
	var positional []string
	for _, a := range args {
		if a == "-f" || a == "--force" {
			force = true
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) != 1 {
		serversUsage()
		return 2
	}
	name := positional[0]

	_, loadErr := st.LoadProfile(name)
	if loadErr != nil {
		if !st.ProfileExists(name) {
			fmt.Fprintf(os.Stderr, "профиль %q не найден.\n", name)
			listProfileNames(st, os.Stderr)
			return 1
		}
		// Файл есть, но не читается — не удаляем молча то, что может быть
		// ещё пригодно для ручного восстановления.
		if !force {
			fmt.Fprintf(os.Stderr, "профиль %q повреждён: %v\n", name, loadErr)
			fmt.Fprintf(os.Stderr, "Удалить с резервной копией: xwdtt servers rm %q --force\n", name)
			return 1
		}
		backup, berr := st.BackupProfile(name)
		if berr != nil {
			fmt.Fprintf(os.Stderr, "не удалось сделать резервную копию: %v\n", berr)
			return 1
		}
		if err := st.DeleteProfile(name); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		fmt.Printf("Удалён повреждённый профиль: %s (копия: %s)\n", name, backup)
		return 0
	}

	if err := st.DeleteProfile(name); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		listProfileNames(st, os.Stderr)
		return 1
	}
	fmt.Printf("Удалён: %s\n", name)
	return 0
}

func serversSetHashes(st *backend.Store, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "использование: xwdtt servers set-hashes <имя> h1,h2,...")
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
	// --help до всех проверок: справка должна работать и без валидного имени.
	for _, a := range args {
		if a == "-h" || a == "--help" {
			serversUsage()
			return 0
		}
	}
	// --force разрешает перезапись существующего целевого профиля.
	force := false
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "-f" || a == "--force" {
			force = true
			continue
		}
		filtered = append(filtered, a)
	}

	flagArgs, posArgs := splitArgs(filtered, editValueFlags)
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
	if np == nil {
		// nil → либо ошибка флагов (code=2), либо --help (code=0)
		return code
	}

	// Старое и новое имя после санитизации — одно и то же имя файла
	// (в т.ч. отличаются только регистром: на Windows это один файл).
	// Раньше save+delete в таком случае удалял только что записанный профиль.
	oldCanon := backend.CanonicalProfileName(name)
	newCanon := backend.CanonicalProfileName(newName)
	if newCanon == "" {
		fmt.Fprintln(os.Stderr, "имя после очистки стало пустым")
		return 2
	}
	if newCanon == oldCanon || strings.EqualFold(newCanon, oldCanon) {
		if err := st.SaveProfile(oldCanon, *np); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		fmt.Printf("Сохранено: %s\n", oldCanon)
		return 0
	}

	// Переименование поверх чужого профиля — только по явному --force.
	if st.ProfileExists(newCanon) && !force {
		fmt.Fprintf(os.Stderr, "профиль %q уже существует — перезаписать его: xwdtt servers edit %q -name %q --force\n",
			newCanon, oldCanon, newCanon)
		return 1
	}

	if err := st.SaveProfile(newCanon, *np); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if err := st.DeleteProfile(oldCanon); err != nil {
		fmt.Fprintf(os.Stderr, "сохранён как %q, но старый профиль не удалён: %v\n", newCanon, err)
		return 1
	}
	fmt.Printf("Сохранено и переименовано: %s → %s\n", oldCanon, newCanon)
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
	directPort := fs.String("direct-port", p.DirectPort, "порт -listen-direct (режим -notls, дефолт 56002)")
	rawPort := fs.String("raw-port", p.RawPort, "порт -listen-raw (режим -raw, дефолт 56003)")
	newName := fs.String("name", name, "новое имя профиля")

	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			serversUsage()
			return nil, "", 0
		}
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
	np.TurnPort = strings.TrimSpace(*port)
	np.TurnTCP = *turnTCP
	np.Workers = *workers
	np.DirectPort = strings.TrimSpace(*directPort)
	np.RawPort = strings.TrimSpace(*rawPort)

	// Порты-переопределения валидируются при сохранении, а не сетевой
	// ошибкой на connect: раньше `-port abc` молча сохранялся.
	for label, val := range map[string]string{
		"port": np.TurnPort, "direct-port": np.DirectPort, "raw-port": np.RawPort,
	} {
		if err := validatePortOverride(val); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
			return nil, "", 2
		}
	}

	if strings.TrimSpace(np.PeerAddr) == "" {
		fmt.Fprintln(os.Stderr, "адрес не может быть пустым")
		return nil, "", 2
	}
	if err := validatePeerAddr(strings.TrimSpace(np.PeerAddr)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, "", 2
	}
	// 0 — авто (ядро само приведёт к кратному 9, максимум 108).
	if np.Workers < 0 || np.Workers > 108 {
		fmt.Fprintln(os.Stderr, "воркеры: 0 (авто) или 1..108")
		return nil, "", 2
	}
	if strings.TrimSpace(*newName) == "" {
		fmt.Fprintln(os.Stderr, "имя не может быть пустым")
		return nil, "", 2
	}
	return &np, strings.TrimSpace(*newName), 0
}

func serversUsage() {
	fmt.Fprint(os.Stderr, `Использование: xwdtt servers <подкоманда>

  list                                список серверов
  add <wdtt://...>                    добавить/обновить по ссылке
  show <имя>                          показать профиль
  rm <имя> [--force]                  удалить профиль (--force — и повреждённый,
                                      с копией в backups/)
  set-hashes <имя> h1,h2,...          заменить хеши VK
  check <имя>                         проверить хеши VK (статусы ok/captcha/dead/...)
  edit <имя> [-peer A] [-password P] [-name N] [-listen L]
        [-turn H] [-port P] [-turn-tcp] [-workers N]
        [-direct-port N] [-raw-port N] [--force]
        (--force — разрешить перезапись существующего имени)
`)
}

// listProfileNames печатает доступные имена профилей.
func listProfileNames(st *backend.Store, w io.Writer) {
	profiles := st.ListProfiles()
	broken := st.ListBrokenProfiles()
	if len(profiles) == 0 && len(broken) == 0 {
		fmt.Fprintln(w, "Доступных серверов нет.")
		return
	}
	if len(profiles) == 0 {
		fmt.Fprintf(w, "Читаемых профилей нет; повреждённых: %d (servers list).\n", len(broken))
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
