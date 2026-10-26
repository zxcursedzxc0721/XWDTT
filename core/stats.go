package core

import (
	"sync/atomic"
	"time"
)

type Stats struct {
	TotalBytesUp      atomic.Int64
	TotalBytesDown    atomic.Int64
	ActiveConnections atomic.Int32
	// DroppedUp/DroppedDown — пакеты, потерянные на пути через диспетчер
	// (нет свободных воркеров / ошибка записи в TUN). Раньше дропы видны
	// были только в отладочных rawDiagf-логах и не попадали в статистику.
	DroppedUp   atomic.Int64
	DroppedDown atomic.Int64
}

func NewStats() *Stats {
	return &Stats{}
}

// RunLoop отправляет статистику каждые 3 секунды.
// onLog вызывается для лог-сообщений, onStats — для статистики.
func (s *Stats) RunLoop(shutdown <-chan struct{},
	onLog func(level, msg string),
	onStats func(rx, tx int64, workers int32)) {

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	var prevDrops int64 // дропы на прошлом тике — в лог пишем только прирост

	for {
		select {
		case <-shutdown:
			return
		case <-ticker.C:
			active := s.ActiveConnections.Load()
			up := s.TotalBytesUp.Load()
			down := s.TotalBytesDown.Load()
			totalMB := float64(up+down) / (1024.0 * 1024.0)

			if onLog != nil {
				msg := "[STATS] Активных: " + itoa(int(active)) + ", Трафик: " + ftoa(totalMB) + " МБ"
				// Дропы: показываем только изменившийся прирост — иначе
				// суммарный счётчик превратил бы каждую строку в тревогу.
				droppedUp := s.DroppedUp.Load()
				droppedDown := s.DroppedDown.Load()
				if delta := droppedUp + droppedDown - prevDrops; delta > 0 {
					msg += ", ДРОПЫ: +" + itoa(int(delta)) +
						" (↑" + itoa(int(droppedUp)) +
						" ↓" + itoa(int(droppedDown)) + " всего)"
				}
				prevDrops = droppedUp + droppedDown
				onLog("INFO", msg)
			}
			if onStats != nil {
				onStats(up, down, active)
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func ftoa(f float64) string {
	// простая конвертация без fmt
	intPart := int(f)
	decPart := int((f - float64(intPart)) * 100)
	return itoa(intPart) + "." + itoa(decPart)
}
