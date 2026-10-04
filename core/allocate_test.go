package core

// Регрессия: "TURN Allocate: all retransmissions failed" (временный сетевой
// сбой) НЕ должен классифицироваться как FATAL — иначе воркеры умирают
// навсегда и часть трафика перестаёт идти, хотя туннель «активен».
// Мобильный клиент (go_client/group.go) все такие ошибки ретраит.

import (
	"errors"
	"testing"
)

func TestClassifyAllocateError_TemporaryNetworkFailureIsRetry(t *testing.T) {
	err := errors.New("all retransmissions failed for P3Gp+2bgbcDNszNy")
	if got := classifyAllocateError(err); got != SessionErrorRetry {
		t.Errorf("retransmissions failed: got %q, want %q", got, SessionErrorRetry)
	}
}

func TestClassifyAllocateError_QuotaIsQuotaNotBan(t *testing.T) {
	// Квота (486) — лимит АККАУНТА VK, а не мёртвость адреса: банить
	// глобальный TURN-адрес нельзя (другие хеши на нём работают).
	for _, msg := range []string{
		"TURN Allocate: quota exceeded",
		"relay error 486",
		"Allocate error response (error 486: Allocation Quota Reached)",
	} {
		if got := classifyAllocateError(errors.New(msg)); got != SessionErrorQuota {
			t.Errorf("%q: got %q, want %q", msg, got, SessionErrorQuota)
		}
	}
}

func TestClassifyAllocateError_UnreachableIsAddressDead(t *testing.T) {
	for _, msg := range []string{
		"udp: connect: connection refused",
		"network unreachable",
		"i/o timeout",
		"no route to host",
	} {
		if got := classifyAllocateError(errors.New(msg)); got != SessionErrorAddressDead {
			t.Errorf("%q: got %q, want %q", msg, got, SessionErrorAddressDead)
		}
	}
}

func TestClassifyAllocateError_NeverFatalForUnknown(t *testing.T) {
	// Любая незнакомая ошибка Allocate — тоже ретрай, а не смерть воркера:
	// FATAL из Allocate убивал воркеры без возможности восстановления.
	got := classifyAllocateError(errors.New("some brand-new pion/turn error"))
	if got != SessionErrorRetry {
		t.Errorf("неизвестная ошибка: got %q, want %q", got, SessionErrorRetry)
	}
}
