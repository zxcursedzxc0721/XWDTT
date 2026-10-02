package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"pwdtt/backend"
)

// validFingerprints — значения, которые понимает core/profiles.go.
var validFingerprints = map[string]bool{
	"chrome": true, "firefox": true, "safari": true,
	"android": true, "ios": true,
}

// settingsKeys — отсортированный список поддерживаемых ключей (для сообщений).
var settingsKeys = []string{
	"autostart", "fingerprint", "obfs", "obfs-accepted", "turn-tcp", "workers",
}

func runSettings(args []string) int {
	return settingsCmd(backend.NewStore(), nil, args)
}

// settingsCmd — get/set настроек. autostart дополнительно трогает ОС,
// поэтому ему нужен app (см. backend.App.SetAutoStart).
func settingsCmd(st *backend.Store, app *backend.App, args []string) int {
	if len(args) == 0 {
		settingsGet(st)
		return 0
	}
	switch args[0] {
	case "get":
		settingsGet(st)
		return 0
	case "set":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "использование: pwdtt settings set <ключ> <значение>")
			settingsUsage()
			return 2
		}
		return settingsSet(st, app, args[1], args[2])
	default:
		fmt.Fprintf(os.Stderr, "неизвестная подкоманда: %s\n\n", args[0])
		settingsUsage()
		return 2
	}
}

func settingsGet(st *backend.Store) {
	s := st.LoadSettings()
	fmt.Printf("%-14s %s\n", "autostart", onOff(s.AutoStart))
	fmt.Printf("%-14s %s\n", "obfs", orDash(s.ObfsMode))
	fmt.Printf("%-14s %s\n", "obfs-accepted", onOff(s.ObfsAccepted))
	fmt.Printf("%-14s %s\n", "turn-tcp", onOff(s.TurnTCP))
	fmt.Printf("%-14s %s\n", "workers", workersStr(s.Workers))
	fmt.Printf("%-14s %s\n", "fingerprint", orDash(s.Fingerprint))
}

func settingsSet(st *backend.Store, app *backend.App, key, value string) int {
	s := st.LoadSettings()
	save := func() int {
		if err := st.SaveSettings(s); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		fmt.Printf("%s = %s\n", key, value)
		return 0
	}

	switch key {
	case "autostart":
		v, err := parseBool(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if app == nil {
			app = backend.NewApp(nil)
		}
		if err := app.SetAutoStart(v); err != nil {
			fmt.Fprintf(os.Stderr, "автозапуск: %v\n", err)
			return 1
		}
		s.AutoStart = v
		return save()
	case "obfs":
		if value != "audio" && value != "video" {
			fmt.Fprintln(os.Stderr, "значение: audio|video")
			return 2
		}
		if value == "video" && !s.ObfsAccepted {
			fmt.Fprintln(os.Stderr, "режим video требует принятия предупреждения об обфускации:")
			fmt.Fprintln(os.Stderr, "  pwdtt settings set obfs-accepted on")
			return 1
		}
		s.ObfsMode = value
		return save()
	case "obfs-accepted":
		v, err := parseBool(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		s.ObfsAccepted = v
		return save()
	case "turn-tcp":
		v, err := parseBool(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		s.TurnTCP = v
		return save()
	case "workers":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			fmt.Fprintln(os.Stderr, "ожидается целое >= 0 (0 = авто)")
			return 2
		}
		s.Workers = n
		return save()
	case "fingerprint":
		if !validFingerprints[value] {
			fmt.Fprintln(os.Stderr, "значение: chrome|firefox|safari|android|ios")
			return 2
		}
		s.Fingerprint = value
		return save()
	default:
		fmt.Fprintf(os.Stderr, "неизвестный ключ: %s\n\n", key)
		settingsUsage()
		return 2
	}
}

func settingsUsage() {
	fmt.Fprintf(os.Stderr, "Ключи: %s\n\n", strings.Join(settingsKeys, ", "))
	fmt.Fprint(os.Stderr, `Использование: pwdtt settings get
       pwdtt settings set <ключ> <значение>

Значения:
  autostart      on|off
  obfs           audio|video
  obfs-accepted  on|off   (обязательно для video)
  turn-tcp       on|off
  workers        целое >= 0 (0 = авто)
  fingerprint    chrome|firefox|safari|android|ios
`)
}

// parseBool — on/off, true/false, yes/no, 1/0.
func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("ожидается on|off (получено %q)", v)
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func yesNo(v bool) string {
	if v {
		return "да"
	}
	return "нет"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func workersStr(n int) string {
	if n == 0 {
		return "auto"
	}
	return strconv.Itoa(n)
}
