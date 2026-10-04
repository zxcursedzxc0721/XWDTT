package cli

// ping (Этап 4.5): замер RTT до TURN-сервера без поднятия туннеля.
// Перенос -ping-only из proxy-turn-vk-android/go_client/main.go (строки 285–305).

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"pwdtt/backend"
	core "wg-turn-client"
)

var pingValueFlags = map[string]bool{
	"-obfs": true, "--obfs": true,
}

func runPing(args []string) int {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	obfsMode := fs.String("obfs", "", "audio|video")
	notls := fs.Bool("notls", false, "прямой режим без DTLS")
	raw := fs.Bool("raw", false, "raw-режим (GETCONF_RAW)")
	turnTCP := fs.Bool("turn-tcp", false, "TCP-транспорт TURN")

	flagArgs, posArgs := splitArgs(args, pingValueFlags)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			pingUsage()
			return 0
		}
		fmt.Fprintf(os.Stderr, "ошибка флагов: %v\n", err)
		pingUsage()
		return 2
	}
	if len(posArgs) > 1 {
		pingUsage()
		return 2
	}

	// Ping — только чтение хранилища: App.Init (bridge, уборка маршрутов)
	// тут не нужен.
	st := backend.NewStore()
	settings := st.LoadSettings()

	// Резолв профиля: либо имя, либо (если ровно один) он сам.
	var prof *backend.ProfileData
	var profName string
	if len(posArgs) == 1 {
		if isWdttLink(posArgs[0]) {
			fmt.Fprintln(os.Stderr, "ping принимает имя профиля, а не ссылку. Сохраните сервер: pwdtt servers add <wdtt://...>")
			return 2
		}
		profName = posArgs[0]
		var err error
		if prof, err = st.LoadProfile(profName); err != nil {
			fmt.Fprintf(os.Stderr, "профиль %q не найден.\n", profName)
			listProfileNames(st, os.Stderr)
			return 1
		}
	} else {
		profiles := st.ListProfiles()
		switch len(profiles) {
		case 0:
			fmt.Fprintln(os.Stderr, "нет сохранённых серверов. Добавьте: pwdtt servers add \"wdtt://...\"")
			return 1
		case 1:
			for n, pr := range profiles {
				profName, prof = n, &pr
			}
		default:
			fmt.Fprintln(os.Stderr, "несколько профилей — укажите имя:")
			listProfileNames(st, os.Stderr)
			return 2
		}
	}

	hashes := nonEmpty(prof.Hashes)
	if len(hashes) == 0 {
		fmt.Fprintf(os.Stderr, "в профиле %q нет хешей VK.\n", profName)
		return 1
	}

	peer, err := net.ResolveUDPAddr("udp", strings.TrimSpace(prof.PeerAddr))
	if err != nil {
		fmt.Fprintf(os.Stderr, "резолв пира %q: %v\n", prof.PeerAddr, err)
		return 1
	}

	wrapKey, err := core.DeriveWrapKey(prof.Password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wrap-ключ: %v\n", err)
		return 1
	}

	mode := *obfsMode
	if mode == "" {
		mode = settings.ObfsMode
	}
	if mode == "" {
		mode = "audio"
	}

	tp := &core.TurnParams{
		Host:         prof.TurnHost,
		Port:         prof.TurnPort,
		WrapKey:      wrapKey,
		ObfsMode:     mode,
		NoDTLS:       *notls,
		RawMode:      *raw,
		TCPTransport: *turnTCP || settings.TurnTCP || prof.TurnTCP,
	}

	fmt.Printf("Профиль:     %s (пир %s)\n", profName, prof.PeerAddr)

	// Первый успешный хеш — как в -ping-only (перебор по всем хешам).
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var lastErr error
	for i, hash := range hashes {
		user, pass, turnURLs, credErr := core.GetCreds(ctx, hash, 999)
		if credErr != nil {
			lastErr = fmt.Errorf("хеш %d: %w", i+1, credErr)
			fmt.Printf("  хеш %d: ошибка кредов: %v\n", i+1, credErr)
			continue
		}
		rtt, pingErr := core.RunPing(ctx, tp, peer, user, pass, turnURLs, prof.DeviceID, prof.Password)
		if pingErr != nil {
			lastErr = fmt.Errorf("хеш %d: %w", i+1, pingErr)
			fmt.Printf("  хеш %d: ping: %v\n", i+1, pingErr)
			continue
		}
		fmt.Printf("RTT:         %d мс (хеш %d из %d)\n", rtt, i+1, len(hashes))
		return 0
	}

	fmt.Fprintf(os.Stderr, "все хеши не ответили: %v\n", lastErr)
	return 1
}

func pingUsage() {
	fmt.Fprint(os.Stderr, `Использование: pwdtt ping [флаги] [имя]

Замер RTT до TURN-сервера (аллокация + хендшейк), туннель не поднимается.

Флаги:
  -obfs audio|video   режим обфускации
  -turn-tcp           TCP-транспорт TURN
  -notls              прямой режим без DTLS
  -raw                raw-режим (GETCONF_RAW)

Примеры:
  pwdtt ping
  pwdtt ping "Мой сервер"
`)
}
