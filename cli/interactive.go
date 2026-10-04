package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"pwdtt/backend"
)

// runInteractive — режим по умолчанию: промпт pwdtt> с командами.
// Логи печатаются поверх промпта; строка ввода перерисовывается после событий.
func runInteractive() int {
	// Права: интерактивный режим умеет connect/pause — запрашиваем UAC один раз
	// при старте (до приветствия, чтобы при перезапуске не печатать его дважды).
	if code := ensureElevated(); code != 0 {
		return code
	}

	p := newPrinter(true)
	app := backend.NewApp(p.OnEvent)
	app.Init()
	defer app.Close()

	installSignalWatcher(app, p)

	fmt.Printf("PWDTT %s — интерактивный режим. Команда help — справка, quit — выход.\n", backend.Version)

	sc := bufio.NewScanner(os.Stdin)
	for {
		p.showPrompt()
		if !sc.Scan() {
			fmt.Println()
			break // EOF
		}
		p.promptDone()
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		args := SplitCommandLine(line)
		if quit := dispatchInteractive(app, p, args); quit {
			break
		}
	}

	// Close() (defer) снимет туннель, если он ещё поднят.
	p.clearLine()
	return 0
}

// dispatchInteractive выполняет команду промпта. Возвращает true для выхода.
func dispatchInteractive(app *backend.App, p *printer, args []string) bool {
	switch args[0] {
	case "connect":
		if len(args) < 2 {
			connectUsage()
			return false
		}
		startTunnel(app, p, args[1:])
	case "disconnect":
		if !app.IsRunning() {
			p.line("не подключено.")
			return false
		}
		p.line(p.c(cYellow, "Отключение..."))
		app.Disconnect()
		// Ждём фактического завершения ядра: иначе набранный сразу connect
		// упирается в "running" ещё останавливающейся сессии.
		waitStopped(app, 15*time.Second)
	case "pause":
		if !app.IsRunning() {
			p.line("нет активного туннеля.")
			return false
		}
		app.Pause()
		p.line(p.c(cYellow, "Пауза: воркеры остановлены, интерфейс поднят."))
	case "resume":
		if !app.IsRunning() {
			p.line("нет активного туннеля.")
			return false
		}
		app.Resume()
		p.line(p.c(cGreen, "Возобновлено."))
	case "servers":
		serversCmd(app.Store(), args[1:])
	case "settings":
		settingsCmd(app.Store(), app, args[1:])
	case "route":
		runRoute(args[1:])
	case "ips":
		ipsCmd(app.Store(), args[1:])
	case "ping":
		runPing(args[1:])
	case "stats":
		p.printSnapshot()
	case "logs":
		logsCmd(app.Store())
	case "update":
		cmdUpdate()
	case "report":
		cmdReport(app)
	case "version":
		cmdVersion()
	case "help", "?":
		interactiveHelp()
	case "quit", "exit":
		if app.IsRunning() {
			p.line(p.c(cYellow, "Отключение..."))
			app.Disconnect()
			waitStopped(app, 15*time.Second)
		}
		return true
	default:
		p.line("неизвестная команда: " + args[0] + " (help — справка)")
	}
	return false
}

// waitStopped ждёт фактического завершения ядра (Bridge.Disconnect только
// запускает остановку): без ожидания набранный сразу connect упирается в
// "already running" ещё останавливающейся сессии.
func waitStopped(app *backend.App, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for app.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	return !app.IsRunning()
}

// SplitCommandLine разбивает строку по пробелам с учётом двойных кавычек,
// чтобы wdtt-ссылки с #название с пробелами не резались.
func SplitCommandLine(line string) []string {
	var args []string
	var cur strings.Builder
	inQuote := false
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' || r == '\t':
			if inQuote {
				cur.WriteRune(r)
			} else if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}
