package core

import "testing"

// Таблица chunk-распределения из референса (go_client/dispatcher.go):
// крупные пакеты группируются покрупнее, мелкие — по одному/тройкам.
func TestChunkSizeFor(t *testing.T) {
	cases := []struct {
		size int
		want int
	}{
		{0, 1}, {60, 1}, {100, 1}, // ≤100 → 1
		{101, 3}, {200, 3}, {300, 3}, // 101..300 → 3
		{301, 8}, {500, 8}, {700, 8}, // 301..700 → 8
		{701, 24}, {900, 24}, {1100, 24}, // 701..1100 → 24
		{1101, 64}, {1300, 64}, {1400, 64}, // >1100 → 64
	}
	for _, c := range cases {
		if got := chunkSizeFor(c.size); got != c.want {
			t.Errorf("chunkSizeFor(%d) = %d, ожидалось %d", c.size, got, c.want)
		}
	}
}

// Приоритетные каналы и dwell-константы — величины из референса,
// их изменение ломает подобранные производительностью значения.
func TestDispatcherTuningConstants(t *testing.T) {
	if prioThreshold != 128 {
		t.Errorf("prioThreshold = %d, ожидалось 128", prioThreshold)
	}
	if maxDwellMS != 15 {
		t.Errorf("maxDwellMS = %d, ожидалось 15", maxDwellMS)
	}
	if prioBuf != 32 {
		t.Errorf("prioBuf = %d, ожидалось 32", prioBuf)
	}
	if returnChBuf != 512 {
		t.Errorf("returnChBuf = %d, ожидалось 512", returnChBuf)
	}
	if allocateGateInterval.String() != "100ms" {
		t.Errorf("allocateGateInterval = %v, ожидалось 100ms", allocateGateInterval)
	}
	if workerStartDelay.String() != "75ms" {
		t.Errorf("workerStartDelay = %v, ожидалось 75ms", workerStartDelay)
	}
	if keepaliveInterval.String() != "10s" {
		t.Errorf("keepaliveInterval = %v, ожидалось 10s", keepaliveInterval)
	}
	if keepaliveMinSize != 25 {
		t.Errorf("keepaliveMinSize = %d, ожидалось 25", keepaliveMinSize)
	}
	if writeDeadline.String() != "3s" {
		t.Errorf("writeDeadline = %v, ожидалось 3s", writeDeadline)
	}
}
