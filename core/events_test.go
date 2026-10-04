package core

import (
	"testing"
	"time"
)

// Регрессия: событие "ready" должно уходить ОДИН раз на сессию, а не на
// каждый воркер — иначе в консоль вываливалось N одинаковых строк
// «Туннель готов к работе» (по числу воркеров, см. session.go emitReady).
func TestEmitReadyOncePerSession(t *testing.T) {
	c := New(Config{
		PeerAddr: "1.27.0.0.1:1",
		Password: "x",
		Hashes:   []string{"h"},
	})

	// 27 воркеров вызвали ready — дошло должно быть ровно одно событие.
	for i := 0; i < 27; i++ {
		c.emitReady()
	}

	select {
	case ev := <-c.events:
		if ev.Type != EventEvent || ev.Name != "ready" {
			t.Fatalf("ожидалось событие ready, получено: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("событие ready не пришло")
	}

	select {
	case ev := <-c.events:
		t.Fatalf("дубликат события ready: %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// дубликатов нет — это и ожидается
	}
}
