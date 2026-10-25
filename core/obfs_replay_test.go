package core

import (
	"encoding/binary"
	"testing"
)

// buildTestRTP — минимальный RTP-кадр для replay window (12 байт заголовка).
func buildTestRTP(ssrc uint32, seq uint16, ts uint32) []byte {
	w := make([]byte, 13)
	w[0] = 0x80
	w[1] = 96
	binary.BigEndian.PutUint16(w[2:4], seq)
	binary.BigEndian.PutUint32(w[4:8], ts)
	binary.BigEndian.PutUint32(w[8:12], ssrc)
	return w
}

// Повтор того же кадра отбрасывается, свежий — принимается.
func TestReplayWindow_DuplicateRejected(t *testing.T) {
	var w replayWindow
	pkt := buildTestRTP(0x12345678, 1, 960)
	if !w.accept(pkt) {
		t.Fatal("первый пакет должен быть принят")
	}
	if w.accept(pkt) {
		t.Error("повтор того же пакета должен быть отброшен")
	}
	// Следующий по seq — принимается.
	next := buildTestRTP(0x12345678, 2, 1920)
	if !w.accept(next) {
		t.Error("свежий пакет должен быть принят")
	}
}

// Пакет за пределами окна (слишком старый timestamp) отбрасывается.
func TestReplayWindow_TooOldRejected(t *testing.T) {
	var w replayWindow
	if !w.accept(buildTestRTP(1, 1, 10_000_000)) {
		t.Fatal("первый пакет должен быть принят")
	}
	// На replayWindowSpan старше самого нового — за окном.
	old := buildTestRTP(1, 2, uint32(10_000_000-replayWindowSpan-1))
	if w.accept(old) {
		t.Error("слишком старый пакет должен быть отброшен")
	}
}

// Смена SSRC перезапускает окно (переподключение к тому же relay): новый
// поток принимается, а его повтор — нет. Раньше кадр отклонялся, окно не
// сбрасывалось, и все последующие пакеты терялись до перезапуска процесса.
func TestReplayWindow_SSRCChangeRestartsWindow(t *testing.T) {
	var w replayWindow
	if !w.accept(buildTestRTP(1, 1, 960)) {
		t.Fatal("первый пакет должен быть принят")
	}
	first := buildTestRTP(2, 1, 960)
	if !w.accept(first) {
		t.Fatal("первый кадр нового SSRC должен быть принят")
	}
	if w.accept(first) {
		t.Error("повтор кадра нового SSRC должен быть отброшен")
	}
	if !w.accept(buildTestRTP(2, 2, 1920)) {
		t.Error("следующий кадр нового SSRC должен быть принят")
	}
}

// Короткий кадр (< 12 байт) не принимается и не паникует.
func TestReplayWindow_ShortWire(t *testing.T) {
	var w replayWindow
	if w.accept([]byte{0x80, 1, 2}) {
		t.Error("короткий кадр должен быть отброшен")
	}
}
