package core

// Тесты статистики дропов (п.7 анализа: дропы были видны только в
// отладочных rawDiagf-логах и не попадали в статистику).

import (
	"strings"
	"testing"
	"time"
)

// TestStatsRunLoopReportsDrops: прирост дропов попадает в лог-строку,
// а без дропов строка остаётся чистой (без ложных тревог).
func TestStatsRunLoopReportsDrops(t *testing.T) {
	s := NewStats()
	shutdown := make(chan struct{})
	defer close(shutdown)

	logs := make(chan string, 16)
	go s.RunLoop(shutdown,
		func(level, msg string) {
			select {
			case logs <- msg:
			default:
			}
		},
		nil,
	)

	// RunLoop тикает каждые 3с — ждём первый тик (чистый), потом дропаем.
	select {
	case msg := <-logs:
		if strings.Contains(msg, "ДРОПЫ") {
			t.Fatalf("до дропов в логе не должно быть ДРОПОВ: %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("первый тик RunLoop не пришёл за 5с")
	}

	s.DroppedUp.Add(3)
	s.DroppedDown.Add(1)

	select {
	case msg := <-logs:
		if !strings.Contains(msg, "ДРОПЫ: +4") {
			t.Fatalf("ожидали прирост +4 в логе, got %q", msg)
		}
		if !strings.Contains(msg, "↑3") || !strings.Contains(msg, "↓1") {
			t.Fatalf("в логе нет разбивки по направлениям: %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("второй тик RunLoop не пришёл за 5с")
	}

	// Новых дропов нет — тревога не повторяется (дельта = 0).
	select {
	case msg := <-logs:
		if strings.Contains(msg, "ДРОПЫ") {
			t.Fatalf("повторная тревога без новых дропов: %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("третий тик RunLoop не пришёл за 5с")
	}
}

// TestStatsDropsCounters: счётчики независимы и начинаются с нуля.
func TestStatsDropsCounters(t *testing.T) {
	s := NewStats()
	if s.DroppedUp.Load() != 0 || s.DroppedDown.Load() != 0 {
		t.Fatal("счётчики дропов должны начинаться с нуля")
	}
	s.DroppedUp.Add(5)
	s.DroppedDown.Add(2)
	if s.DroppedUp.Load() != 5 || s.DroppedDown.Load() != 2 {
		t.Fatalf("got ↑%d ↓%d", s.DroppedUp.Load(), s.DroppedDown.Load())
	}
}
