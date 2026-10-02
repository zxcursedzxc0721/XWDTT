package core

import (
	"log"
	"time"
)

// rawDiagf логирует с миллисекундным timestamp (HH:mm:ss.SSS) —
// нужно для сопоставления моментов между процессами при разборе таймлайна
// подъёма Raw TUN, где он может подвисать.
func rawDiagf(format string, args ...any) {
	ts := time.Now().Format("15:04:05.000")
	log.Printf("[RAW-DIAG "+ts+"] "+format, args...)
}
