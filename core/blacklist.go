package core

import (
	"sync"
	"time"
)

// TurnBlacklist — глобальный блек-лист для TURN-адресов.
// Если адрес вернул ошибку (486, unreachable, timeout и т.д.),
// он блокируется на указанное время, чтобы не создавать лишнюю нагрузку.
type TurnBlacklist struct {
	mu    sync.RWMutex
	bans  map[string]time.Time // addr -> banUntil
	ttl   time.Duration        // время блокировки
}

// NewTurnBlacklist создает новый блек-лист с заданным TTL.
func NewTurnBlacklist(ttl time.Duration) *TurnBlacklist {
	return &TurnBlacklist{
		bans: make(map[string]time.Time),
		ttl:  ttl,
	}
}

// IsBanned проверяет, забанен ли адрес.
// Если время бана истекло — адрес автоматически разбанивается.
func (b *TurnBlacklist) IsBanned(addr string) bool {
	b.mu.RLock()
	banUntil, ok := b.bans[addr]
	b.mu.RUnlock()

	if !ok {
		return false
	}

	// Если срок бана истек — удаляем запись и возвращаем false
	if time.Now().After(banUntil) {
		b.mu.Lock()
		delete(b.bans, addr)
		b.mu.Unlock()
		return false
	}

	return true
}

// Ban добавляет адрес в блек-лист на TTL.
// Если адрес уже забанен — продлевает бан.
func (b *TurnBlacklist) Ban(addr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bans[addr] = time.Now().Add(b.ttl)
}

// Clear сбрасывает весь блек-лист (для тестов или форсированного сброса).
func (b *TurnBlacklist) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bans = make(map[string]time.Time)
}

// Len возвращает количество забаненных адресов

func (b *TurnBlacklist) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.bans)
}

// GetBanned возвращает список всех забаненных адресов (для логов).
func (b *TurnBlacklist) GetBanned() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	addrs := make([]string, 0, len(b.bans))
	for addr := range b.bans {
		addrs = append(addrs, addr)
	}
	return addrs
}

// TTL = 5 минут
var GlobalBlacklist = NewTurnBlacklist(5 * time.Minute)
