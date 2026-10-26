package core

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// emitError отправляет ошибку в канал событий.
func (c *Core) emitError(code, message string, fatal bool) {
	c.emit(Event{
		Type:    EventError,
		Name:    code,
		Message: message,
	})
	if fatal {
		log.Printf("[ЯДРО] FATAL: %s — %s", code, message)
	}
}

// emitReady отправляет событие "туннель готов" — один раз на сессию.
// Каждый воркер вызывает ready после авторизации; без Once в консоль
// вываливалось N одинаковых строк «Туннель готов к работе» (по числу воркеров).
func (c *Core) emitReady() {
	c.readyOnce.Do(func() {
		c.emit(Event{Type: EventEvent, Name: "ready"})
	})
}

// startLiveness — «живость» после ready (п.6 анализа). Если через минуту
// после объявления готовности есть исходящий трафик, но ни одного входящего
// пакета — сессия с мёртвой линией: консоль при этом уже пишет «готово»,
// а данные не идут (симптом «подключено, но ничего не работает»).
// Только логирует — трафик и так продолжает работать/переподниматься.
func (c *Core) startLiveness(stats *Stats) {
	if stats == nil {
		return
	}
	c.livenessOnce.Do(func() {
		go func() {
			const (
				checkEvery = 15 * time.Second
				silentWarn = 4 // минута тишины подряд → первое предупреждение
			)
			t := time.NewTicker(checkEvery)
			defer t.Stop()
			var silent int
			for {
				select {
				case <-c.ctx.Done():
					return
				case <-t.C:
					up := stats.TotalBytesUp.Load()
					down := stats.TotalBytesDown.Load()
					if down > 0 || up == 0 {
						// Входящие есть — всё живо; или юзер просто сидит
						// без дела (up==0) — тревогу не поднимаем.
						silent = 0
						continue
					}
					silent++
					if silent == silentWarn {
						log.Printf("[LIVENESS] Есть исходящий трафик, но ни одного входящего пакета минуту — туннель объявлен готовым, данных нет. Проверьте маршруты и не мешают ли другие VPN (Cloudflare WARP и т.п.).")
					} else if silent%16 == 0 { // каждые ~4 минуты
						log.Printf("[LIVENESS] Всё ещё нет входящих данных (%d мин) — связь с сервером не подтверждена.", silent/4)
					}
				}
			}
		}()
	})
}

// emitCaptchaRequest отправляет запрос на решение капчи.
func (c *Core) emitCaptchaRequest(mode, redirectURI, sessionToken string) {
	data, _ := json.Marshal(map[string]string{
		"mode":          mode,
		"redirect_uri":  redirectURI,
		"session_token": sessionToken,
	})
	c.emit(Event{Type: EventEvent, Name: "captcha_required", Data: string(data)})
}

// emitCaptchaDone отправляет результат решения капчи.
func (c *Core) emitCaptchaDone(success bool, errStr string) {
	payload := map[string]any{"success": success}
	if errStr != "" {
		payload["error"] = errStr
	}
	data, _ := json.Marshal(payload)
	c.emit(Event{Type: EventEvent, Name: "captcha_done", Data: string(data)})
}

// logf логирует и отправляет в канал событий.
func (c *Core) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Println(msg)
	c.emit(Event{Type: EventLog, Level: "INFO", Message: msg})
}
