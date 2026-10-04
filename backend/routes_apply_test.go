package backend

// Батч-маршруты: фильтр вывода netsh и сводка для лога.

import (
	"strings"
	"testing"
)

func TestNetshWarnings_FiltersOkLines(t *testing.T) {
	out := "OK.\nOk.\n" + strings.Repeat("ОК.\r\n", 3) // латиница + UTF-8
	if got := netshWarnings(out); got != "" {
		t.Errorf("netshWarnings(только ОК) = %q, ожидалась пустая строка", got)
	}
	// Реальный вывод на русской локали Windows — cp1251 (CE CA 2E), не UTF-8
	cp1251OK := string([]byte{0xCE, 0xCA, '.'})
	if got := netshWarnings(cp1251OK + "\r\n" + cp1251OK + "\r\n"); got != "" {
		t.Errorf("netshWarnings(cp1251 ОК.) = %q, ожидалась пустая строка", got)
	}
	out = "ОК.\r\nОшибка при чтении файла, проверьте наличие ошибок при попытке выполнить команду.\r\nОК.\r\n"
	got := netshWarnings(out)
	if !strings.Contains(got, "Ошибка") {
		t.Errorf("netshWarnings = %q, ожидалась строка-ошибка", got)
	}
}

func TestNetshWarnings_Truncates(t *testing.T) {
	big := strings.Repeat("x", 1000)
	if got := netshWarnings(big); len(got) > 510 {
		t.Errorf("netshWarnings не обрезан: %d символов", len(got))
	}
}

func TestRouteSummary_TruncatesLongList(t *testing.T) {
	long := make([]string, 10832)
	for i := range long {
		long[i] = "1.2.3.4/32"
	}
	s := routeSummary(long)
	if len(s) > 300 {
		t.Errorf("routeSummary слишком длинный (%d символов)", len(s))
	}
	if !strings.Contains(s, "10824") { // 10832 - 8 показанных
		t.Errorf("routeSummary без счётчика: %s", s)
	}
}

func TestRouteSummary_ShortListAsIs(t *testing.T) {
	if got := routeSummary([]string{"0.0.0.0/0"}); got != "[0.0.0.0/0]" {
		t.Errorf("routeSummary = %q", got)
	}
}
