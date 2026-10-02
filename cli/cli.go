// Package cli — консольный интерфейс PWDTT.
// Подкоманды + режим по умолчанию (без аргументов — интерактивный промпт).
package cli

import (
	"fmt"
	"os"

	"pwdtt/backend"
)

// Run — точка входа CLI, возвращает exit code.
func Run(args []string) int {
	if len(args) == 0 {
		return runInteractive()
	}
	switch args[0] {
	case "connect":
		return runConnect(args[1:])
	case "servers":
		return runServers(args[1:])
	case "settings":
		return runSettings(args[1:])
	case "route":
		return runRoute(args[1:])
	case "ips":
		return runIps(args[1:])
	case "pause", "resume", "disconnect":
		// Процесс-модель: туннель живёт в процессе `connect` или интерактивного
		// режима — из отдельного процесса им управлять нечем.
		fmt.Fprintf(os.Stderr, "%s работает в процессе с туннелем: интерактивный режим или Ctrl+C в окне connect.\n", args[0])
		return 1
	case "update":
		return cmdUpdate()
	case "report":
		return cmdReport(nil)
	case "version", "--version", "-v":
		return cmdVersion()
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда: %s\n\n", args[0])
		usage()
		return 2
	}
}

// ensureElevated — на Windows без прав администратора перезапускает процесс
// с UAC-запросом (route/netsh/wintun требуют прав). На Linux/macOS права
// берутся поштучно (sudo/osascript) — всегда OK.
func ensureElevated() int {
	if backend.IsElevated() {
		return 0
	}
	fmt.Fprintln(os.Stderr, "Требуются права администратора — запрос у Windows...")
	if err := backend.ElevateAndExit(); err != nil {
		fmt.Fprintf(os.Stderr, "Не удалось получить права: %v\n", err)
		return 1
	}
	return 0 // сюда не доходим: успешный перезапуск делает os.Exit
}

func usage() {
	fmt.Print(`PWDTT — консольный VPN-клиент

Использование:
  pwdtt                                    интерактивный режим
  pwdtt connect [флаги] <имя|wdtt://ссылка>  подключить (Ctrl+C — отключить)
  pwdtt servers <подкоманда>               управление серверами
  pwdtt settings get|set <ключ> [значение] настройки
  pwdtt route [full|split]                 режим маршрутизации (тюнелирование)
  pwdtt ips list|add ...|rm ...            список адресов split-режима
  pwdtt update | report | version | help

Серверы:
  pwdtt servers list
  pwdtt servers add "wdtt://IP:ДТЛС:WG:ПРОКСИ:ПАРОЛЬ[:ХЕШИ][#название]"
  pwdtt servers show <имя>
  pwdtt servers rm <имя>
  pwdtt servers set-hashes <имя> h1,h2,...
  pwdtt servers edit <имя> [-peer A] [-password P] [-name N] ...

Настройки (pwdtt settings set <ключ> <значение>):
  autostart on|off        автозапуск при входе в систему
  obfs audio|video        режим обфускации
  obfs-accepted on|off    принять предупреждение (нужно для video)
  turn-tcp on|off         TCP-транспорт TURN
  workers N               воркеров (0 = авто: max(9, хеши*9))
  fingerprint ИМЯ         chrome|firefox|safari|android|ios

Флаги connect:
  -workers N  -obfs audio|video  -fingerprint ИМЯ  -turn-tcp
`)
}

func interactiveHelp() {
	fmt.Print(`Команды:
  connect <имя|wdtt://ссылка> [флаги]      подключить туннель
  disconnect                               отключить
  pause | resume                           пауза / продолжить
  servers list|add|show|rm|set-hashes|edit  серверы
  settings get|set <ключ> <значение>       настройки
  route full|split                         режим маршрутизации
  ips list|add ...|rm ...                  список адресов split-режима
  stats                                    статистика сессии
  logs                                     последний лог-файл
  update | report | version                обновление / отчёт / версия
  help                                     эта справка
  quit                                     выход (туннель отключится)
`)
}
