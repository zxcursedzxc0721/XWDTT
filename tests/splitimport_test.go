package backend_test

// Импорт ip-list (Этап 6): разбор JSON-файлов и поднятые лимиты списка.

import (
	"fmt"
	"testing"

	"xwdtt/backend"
)

func TestParseSplitIPList_ObjectByService(t *testing.T) {
	data := []byte(`{
		"claude.ai": ["100.24.112.119", "104.192.142.21"],
		"cursor.com": ["107.20.126.66"],
		"discord.media": ["1.2.3.4", "1.2.3.4"]
	}`)
	entries, skipped, err := backend.ParseSplitIPList(data)
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, ожидалось 0", skipped)
	}
	// дубль 1.2.3.4 убран, IP нормализованы в /32
	want := []string{"100.24.112.119/32", "104.192.142.21/32", "107.20.126.66/32", "1.2.3.4/32"}
	if len(entries) != len(want) {
		t.Fatalf("entries = %v (len=%d), ожидалось %v", entries, len(entries), want)
	}
	seen := make(map[string]bool)
	for _, e := range entries {
		seen[e] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("нет ожидаемой записи %s в %v", w, entries)
		}
	}
}

func TestParseSplitIPList_PlainArray(t *testing.T) {
	entries, skipped, err := backend.ParseSplitIPList([]byte(`["9.9.9.9", "vk.com", "10.0.0.0/8"]`))
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if skipped != 0 || len(entries) != 3 {
		t.Errorf("entries=%v skipped=%d, ожидались 3 записи без пропусков", entries, skipped)
	}
}

func TestParseSplitIPList_BadEntriesSkipped(t *testing.T) {
	entries, skipped, err := backend.ParseSplitIPList([]byte(`["1.2.3.4", "не-ip-и-не-домен", ""]`))
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, ожидалось 2", skipped)
	}
	if len(entries) != 1 || entries[0] != "1.2.3.4/32" {
		t.Errorf("entries = %v, ожидалось [1.2.3.4/32]", entries)
	}
}

func TestParseSplitIPList_UnknownFormat(t *testing.T) {
	if _, _, err := backend.ParseSplitIPList([]byte(`{"a": "not-an-array"}`)); err == nil {
		t.Error("ожидалась ошибка для неизвестного формата")
	}
	if _, _, err := backend.ParseSplitIPList([]byte(`это не json`)); err == nil {
		t.Error("ожидалась ошибка для мусора")
	}
}

// Список длиннее старого лимита (100 записей) не должен обрезаться:
// Regression — раньше ResolveSplitRoutes молча отбрасывал хвост.
func TestResolveSplitRoutes_BigListNotTruncated(t *testing.T) {
	const n = 5000
	entries := make([]string, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, fmt.Sprintf("198.51.%d.%d/32", i/256, i%256))
	}
	got := backend.ResolveSplitRoutes(entries, nil)
	if len(got) < n {
		t.Errorf("ResolveSplitRoutes обрезал список: %d из %d", len(got), n)
	}
}

func TestMaxSplitRules_CoversIPListImport(t *testing.T) {
	if backend.MaxSplitRules < 11000 {
		t.Errorf("MaxSplitRules = %d, импорт списка на ~10800 записей не пройдёт", backend.MaxSplitRules)
	}
}
