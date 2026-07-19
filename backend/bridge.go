package backend

import (
	"context"
	"fmt"
	"log"
	"sync"

	core "wg-turn-client"
)

// Bridge — мост между App.go и ядром.
type Bridge struct {
	ctx     context.Context
	store   *Store
	onEvent func(name string, args ...any)
	mu      sync.Mutex
	core    *core.Core
	running bool
	logFile *LogFile // полный лог сессии
}

func NewBridge(ctx context.Context, store *Store, onEvent func(string, ...any)) *Bridge {
	return &Bridge{ctx: ctx, store: store, onEvent: onEvent}
}

// Connect подключается к серверу.
func (b *Bridge) Connect(params ConnectParams) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("already running")
	}
	// Сразу помечаем как running чтобы заблокировать параллельные вызовы
	b.running = true
	b.mu.Unlock()

	hashes := params.Hashes
	if len(hashes) == 0 {
		return fmt.Errorf("нет хешей VK")
	}

	workers := params.Workers
	if workers <= 0 {
		workers = 24
	}

	deviceID := params.DeviceID
	if deviceID == "" {
		deviceID = "unknown"
	}

	cfg := core.Config{
		PeerAddr:    params.PeerAddr,
		Password:    params.Password,
		Hashes:      hashes,
		DeviceID:    deviceID,
		Workers:     workers,
		CaptchaMode: params.CaptchaMode,
		ObfsMode:    params.ObfsMode,
		Fingerprint: params.Fingerprint,
	}

	c := core.New(cfg)
	events, err := c.Start()
	if err != nil {
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
		return fmt.Errorf("core start: %w", err)
	}

	b.mu.Lock()
	b.core = c
	b.running = true
	b.logFile = b.store.OpenLogFile()
	b.mu.Unlock()

	go b.forwardEvents(events)
	return nil
}

func (b *Bridge) Disconnect() {
	b.mu.Lock()
	c := b.core
	b.mu.Unlock()

	// Tear down WireGuard interface immediately — don't wait for core shutdown
	wg.Teardown()
	log.Printf("[BRIDGE] WG интерфейс снят по запросу пользователя")

	if c != nil {
		c.Stop()
	}
}

func (b *Bridge) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

func (b *Bridge) SendCaptchaResult(token string) {
	b.mu.Lock()
	c := b.core
	b.mu.Unlock()
	if c != nil {
		c.SolveCaptcha(token)
	}
}

// forwardEvents читает канал событий ядра и пробрасывает в Wails.
func (b *Bridge) forwardEvents(events <-chan core.Event) {
	for ev := range events {
		switch ev.Type {
		case core.EventState:
			b.onEvent("state_changed", ev.Status)

		case core.EventLog:
			// Пишем ВСЁ в файл (включая отфильтрованное для UI)
			b.mu.Lock()
			if b.logFile != nil {
				b.logFile.Write(ev.Level, ev.Message)
			}
			b.mu.Unlock()
			// В UI — только отфильтрованное
			if ev.Level != "SKIP" {
				b.onEvent("log", ev.Level, ev.Message)
			}

		case core.EventStats:
			b.onEvent("stats", map[string]any{
				"active":     ev.Active,
				"bytes_up":   ev.BytesUp,
				"bytes_down": ev.BytesDown,
			})

		case core.EventError:
			b.onEvent("error", ev.Message)

		case core.EventEvent:
			switch ev.Name {
			case "wg_config":
				b.onEvent("log", "INFO", "[WG] Применение конфига...")
				wgLogf := func(msg string) {
					b.onEvent("log", "INFO", "[WG] "+msg)
					b.mu.Lock()
					if b.logFile != nil {
						b.logFile.Write("INFO", "[WG] "+msg)
					}
					b.mu.Unlock()
				}
				if err := wg.Apply(ev.Data, nil, wgLogf); err != nil {
					b.onEvent("error", fmt.Sprintf("[WG] Ошибка: %v", err))
				} else {
					b.onEvent("log", "INFO", "[WG] Конфиг применён, туннель активен")
					b.onEvent("state_changed", "connected")
				}
			case "captcha_required":
				b.onEvent("captcha_required", ev.Data)
			case "ready":
				b.onEvent("log", "INFO", "[ЯДРО] Туннель готов к работе")
			default:
				b.onEvent("event", ev.Name)
			}
		}
	}

	b.mu.Lock()
	b.running = false
	b.core = nil
	if b.logFile != nil {
		b.logFile.Close()
		b.logFile = nil
	}
	b.mu.Unlock()

	b.onEvent("state_changed", "disconnected")
	log.Printf("[BRIDGE] Ядро завершилось")
}
