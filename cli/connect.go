package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"pwdtt/backend"
)

// connectValueFlags — флаги connect, поглощающие значение (для разбора
// аргументов в произвольном порядке: `connect имя --workers 18`).
var connectValueFlags = map[string]bool{
	"-workers": true, "--workers": true,
	"-obfs": true, "--obfs": true,
	"-fingerprint": true, "--fingerprint": true,
}

var editValueFlags = map[string]bool{
	"-peer": true, "--peer": true,
	"-password": true, "--password": true,
	"-listen": true, "--listen": true,
	"-turn": true, "--turn": true,
	"-port": true, "--port": true,
	"-name": true, "--name": true,
	"-workers": true, "--workers": true,
}

// splitArgs разделяет аргументы на флаговые и позиционные, чтобы работал
// любой порядок (флаги после цели). Значения «значимых» флагов забираются
// вместе с флагом; логические флаги значения не берут.
func splitArgs(args []string, valueFlags map[string]bool) (flagArgs, posArgs []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			posArgs = append(posArgs, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flagArgs = append(flagArgs, a)
			if !strings.Contains(a, "=") && valueFlags[a] && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		posArgs = append(posArgs, a)
	}
	return
}

// runConnect — неблокирующая часть подключения (флаги, резолв профиля, запуск ядра).
// Возвращает 0, если туннель запущен.
func startTunnel(app *backend.App, p *printer, args []string) int {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workers := fs.Int("workers", 0, "число воркеров (0 = авто)")
	obfs := fs.String("obfs", "", "audio|video")
	fingerprint := fs.String("fingerprint", "", "chrome|firefox|safari|android|ios")
	turnTCP := fs.Bool("turn-tcp", false, "TCP-транспорт TURN")
	noDTLS := fs.Bool("notls", false, "без DTLS: RTP-obfs AEAD напрямую (сервер с -listen-direct)")
	raw := fs.Bool("raw", false, "raw-IP без WireGuard (сервер с -listen-raw)")

	flagArgs, posArgs := splitArgs(args, connectValueFlags)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			connectUsage()
			return codeHelpShown
		}
		fmt.Fprintf(os.Stderr, "ошибка флагов: %v\n", err)
		connectUsage()
		return 2
	}
	if len(posArgs) != 1 {
		connectUsage()
		return 2
	}
	target := posArgs[0]

	if *obfs != "" && *obfs != "audio" && *obfs != "video" {
		fmt.Fprintf(os.Stderr, "недопустимый --obfs %q (audio|video)\n", *obfs)
		return 2
	}
	if *fingerprint != "" && !validFingerprints[*fingerprint] {
		fmt.Fprintf(os.Stderr, "недопустимый --fingerprint %q (chrome|firefox|safari|android|ios)\n", *fingerprint)
		return 2
	}
	if *workers < 0 {
		fmt.Fprintln(os.Stderr, "--workers не может быть отрицательным")
		return 2
	}

	settings := app.Store().LoadSettings()

	var prof *backend.ProfileData
	var profName string
	if isWdttLink(target) {
		link, ok := ParseWdttLink(target)
		if !ok {
			fmt.Fprintln(os.Stderr, "не удалось разобрать ссылку. Формат: wdtt://IP:ДТЛС:WG:ПРОКСИ:ПАРОЛЬ[:ХЕШИ][#название]")
			return 2
		}
		var reused bool
		profName, reused = upsertFromLink(app.Store(), link)
		var err error
		if prof, err = app.GetProfile(profName); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if reused {
			fmt.Printf("Профиль обновлён: %s\n", profName)
		} else {
			fmt.Printf("Профиль сохранён: %s\n", profName)
		}
	} else {
		profName = target
		var err error
		if prof, err = app.GetProfile(target); err != nil {
			fmt.Fprintf(os.Stderr, "профиль %q не найден.\n", target)
			listProfileNames(app.Store(), os.Stderr)
			fmt.Fprintln(os.Stderr, `Добавьте сервер: pwdtt servers add "wdtt://IP:ДТЛС:WG:ПРОКСИ:ПАРОЛЬ"`)
			return 1
		}
	}

	hashes := nonEmpty(prof.Hashes)
	if len(hashes) == 0 {
		fmt.Fprintf(os.Stderr, "в профиле %q нет хешей VK. Добавьте: pwdtt servers set-hashes %q h1,h2,...\n", profName, profName)
		return 1
	}

	w := *workers
	if w <= 0 {
		w = prof.Workers
	}
	if w <= 0 {
		w = settings.Workers
	}
	if w <= 0 {
		w = maxInt(9, len(hashes)*9)
	}

	obfsMode := *obfs
	if obfsMode == "" {
		obfsMode = settings.ObfsMode
	}
	if obfsMode == "" {
		obfsMode = "audio"
	}

	fp := *fingerprint
	if fp == "" {
		fp = settings.Fingerprint
	}

	turn := *turnTCP || settings.TurnTCP || prof.TurnTCP

	fmt.Printf("Сервер:      %s (профиль %q)\n", prof.PeerAddr, profName)
	fmt.Printf("Воркеры:     %d   обфускация: %s   TCP: %s", w, obfsMode, yesNo(turn))
	if fp != "" {
		fmt.Printf("   fingerprint: %s", fp)
	}
	if *noDTLS || *raw {
		fmt.Printf("   режим: ")
		if *raw {
			fmt.Printf("raw")
		} else {
			fmt.Printf("no-dtls")
		}
	}
	fmt.Println()

	// Права: перезапуск с UAC только после всех проверок, чтобы ошибки
	// (профиль не найден, нет хешей) показывались без UAC-окна. Повторный
	// проход идемпотентен: upsert профиля ничего не дублирует.
	if code := ensureElevated(); code != 0 {
		return code
	}

	err := app.Connect(backend.ConnectParams{
		PeerAddr:    prof.PeerAddr,
		Password:    prof.Password,
		Hashes:      hashes,
		DeviceID:    prof.DeviceID,
		Workers:     w,
		CaptchaMode: "auto",
		ObfsMode:    obfsMode,
		Fingerprint: fp,
		TurnTCP:     turn,
		DoH:         settings.DoH,
		NoDTLS:      *noDTLS,
		Raw:         *raw,
	})
	if err != nil {
		p.errorLine(err.Error())
		return 1
	}
	if p.interactive {
		p.line(p.c(cGray, "Туннель запущен. Ctrl+C — выход (туннель отключится)."))
	} else {
		p.line(p.c(cGray, "Ctrl+C — отключить."))
	}
	return 0
}

// codeHelpShown — startTunnel показал справку (--help): это успех, но не
// «туннель запущен», поэтому код 0 здесь означал бы вечное ожидание в runConnect.
const codeHelpShown = -1

// runConnect — неблокирующая часть подключения (флаги, резолв, запуск ядра).
// Возвращает код выхода; 0 = туннель запущен и работает.
func runConnect(args []string) int {
	p := newPrinter(false)
	app := backend.NewApp(p.OnEvent)
	app.Init()
	defer app.Close()

	// Сигналы — до запуска, чтобы Ctrl+C в первые миллисекунды не убил процесс
	// без снятия маршрутов.
	installSignalWatcher(app, p)

	if code := startTunnel(app, p, args); code != 0 {
		if code == codeHelpShown {
			return 0
		}
		return code
	}

	// Ждём либо завершения сессии самим ядром, либо Ctrl+C
	// (обработчик сигнала сам делает Disconnect и os.Exit).
	<-p.doneCh
	p.clearLine()
	if p.sawError() {
		return 1
	}
	return 0
}

// installSignalWatcher — первый Ctrl+C: отключение и выход 0; второй: принудительно.
var signalWatcherOnce sync.Once

func installSignalWatcher(app *backend.App, p *printer) {
	signalWatcherOnce.Do(func() {
		sig := make(chan os.Signal, 2)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		go func() {
			n := 0
			for range sig {
				n++
				if n > 1 {
					os.Exit(130)
				}
				p.clearLine()
				fmt.Println(p.c(cYellow, "Отключение... (ещё раз Ctrl+C — принудительно)"))
				go func() {
					app.Disconnect()
					deadline := time.Now().Add(15 * time.Second)
					for app.IsRunning() && time.Now().Before(deadline) {
						time.Sleep(100 * time.Millisecond)
					}
					os.Exit(0)
				}()
			}
		}()
	})
}

func connectUsage() {
	fmt.Fprint(os.Stderr, `Использование: pwdtt connect [флаги] <имя|wdtt://ссылка>

Флаги:
  -workers N            число воркеров (по умолчанию: max(9, хеши*9))
  -obfs audio|video     режим обфускации
  -fingerprint ИМЯ      chrome|firefox|safari|android|ios
  -turn-tcp             TCP-транспорт TURN
  -notls                без DTLS: RTP-obfs напрямую поверх TURN
                        (сервер с -listen-direct)
  -raw                  raw-IP без WireGuard (сервер с -listen-raw)

Примеры:
  pwdtt connect "Мой сервер"
  pwdtt connect "wdtt://1.2.3.4:8443:51820:1080:пароль#Мой сервер" -workers 27
`)
}
