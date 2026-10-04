package core

import (
	"encoding/json"
	"fmt"
	"log"
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
