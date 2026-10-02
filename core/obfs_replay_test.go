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

// Смена SSRC между кадрами не принимается (новая потоковая сессия).
func TestReplayWindow_SSRCChangeRejected(t *testing.T) {
	var w replayWindow
	if !w.accept(buildTestRTP(1, 1, 960)) {
		t.Fatal("первый пакет должен быть принят")
	}
	if w.accept(buildTestRTP(2, 1, 960)) {
		t.Error("кадр с другим SSRC должен быть отброшен")
	}
}

// Короткий кадр (< 12 байт) не принимается и не паникует.
func TestReplayWindow_ShortWire(t *testing.T) {
	var w replayWindow
	if w.accept([]byte{0x80, 1, 2}) {
		t.Error("короткий кадр должен быть отброшен")
	}
}
