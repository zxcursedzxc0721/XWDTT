package backend_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"xwdtt/backend"
)

// ═══════════════════════════════════════════════════
// BuildRoutePlan
// ═══════════════════════════════════════════════════

func TestBuildRoutePlan_Full(t *testing.T) {
	allowed := []string{"0.0.0.0/0"}
	turn := []string{"1.2.3.4", "5.6.7.8"}

	plan := backend.BuildRoutePlan(allowed, turn, backend.RoutePolicy{Mode: backend.RouteModeFull})

	if !plan.HijackDefault {
		t.Error("full: HijackDefault должен быть true")
	}
	if len(plan.TunnelRoutes) != 1 || plan.TunnelRoutes[0] != "0.0.0.0/0" {
		t.Errorf("full: TunnelRoutes = %v, ожидался [0.0.0.0/0]", plan.TunnelRoutes)
	}
	// excludes обязаны содержать TURN /32 и все vkExcludeCIDRs
	if !contains(plan.ExcludeRoutes, "1.2.3.4/32") || !contains(plan.ExcludeRoutes, "5.6.7.8/32") {
		t.Errorf("full: в ExcludeRoutes нет TURN /32: %v", plan.ExcludeRoutes)
	}
	if !contains(plan.ExcludeRoutes, "90.156.0.0/16") {
		t.Errorf("full: в ExcludeRoutes нет vkExcludeCIDRs: %v", plan.ExcludeRoutes)
	}
	// первыми идут TURN, потом vk, потом DNS — порядок как в старом коде
	if plan.ExcludeRoutes[0] != "1.2.3.4/32" || plan.ExcludeRoutes[1] != "5.6.7.8/32" {
		t.Errorf("full: порядок ExcludeRoutes нарушен: %v", plan.ExcludeRoutes)
	}
}

func TestBuildRoutePlan_Full_EmptyPolicy(t *testing.T) {
	// Пустой policy (значение по умолчанию) = full
	plan := backend.BuildRoutePlan([]string{"0.0.0.0/0"}, nil, backend.RoutePolicy{})
	if !plan.HijackDefault {
		t.Error("пустой policy должен давать full-режим")
	}
}

func TestBuildRoutePlan_Split(t *testing.T) {
	allowed := []string{"0.0.0.0/0"} // сервер всегда шлёт это
	turn := []string{"1.2.3.4"}

	plan := backend.BuildRoutePlan(allowed, turn, backend.RoutePolicy{
		Mode:       backend.RouteModeSplit,
		SplitCIDRs: []string{"9.9.9.9/32", "10.0.0.0/8"},
	})

	if plan.HijackDefault {
		t.Error("split: дефолтный маршрут нельзя перехватывать")
	}
	if len(plan.ExcludeRoutes) != 0 {
		t.Errorf("split: ExcludeRoutes должны быть пусты, got %v", plan.ExcludeRoutes)
	}
	// Ключевой инвариант: 0.0.0.0/0 сервера НЕ попадает в туннель
	if contains(plan.TunnelRoutes, "0.0.0.0/0") {
		t.Errorf("split: AllowedIPs сервера не должен участвовать: %v", plan.TunnelRoutes)
	}
	want := []string{"9.9.9.9/32", "10.0.0.0/8"}
	if len(plan.TunnelRoutes) != 2 || plan.TunnelRoutes[0] != want[0] || plan.TunnelRoutes[1] != want[1] {
		t.Errorf("split: TunnelRoutes = %v, ожидалось %v", plan.TunnelRoutes, want)
	}
}

func TestBuildRoutePlan_Split_EmptyList(t *testing.T) {
	plan := backend.BuildRoutePlan([]string{"0.0.0.0/0"}, []string{"1.2.3.4"},
		backend.RoutePolicy{Mode: backend.RouteModeSplit})
	if len(plan.TunnelRoutes) != 0 {
		t.Errorf("split с пустым списком: TunnelRoutes = %v, ожидалось пусто", plan.TunnelRoutes)
	}
	if plan.HijackDefault || len(plan.ExcludeRoutes) != 0 {
		t.Error("split с пустым списком: никаких побочных эффектов")
	}
}

func TestBuildRoutePlan_Dedupe(t *testing.T) {
	plan := backend.BuildRoutePlan(
		[]string{"10.0.0.0/8", "10.0.0.0/8"},
		[]string{"1.2.3.4", "1.2.3.4"},
		backend.RoutePolicy{Mode: backend.RouteModeFull})
	if len(plan.TunnelRoutes) != 1 {
		t.Errorf("дубли TunnelRoutes: %v", plan.TunnelRoutes)
	}
	if c := count(plan.ExcludeRoutes, "1.2.3.4/32"); c != 1 {
		t.Errorf("дубли в ExcludeRoutes (1.2.3.4/32: %d шт): %v", c, plan.ExcludeRoutes)
	}
}

// ═══════════════════════════════════════════════════
// NormalizeSplitEntry
// ═══════════════════════════════════════════════════

func TestNormalizeSplitEntry(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1.2.3.4", "1.2.3.4/32"},
		{"10.0.0.0/8", "10.0.0.0/8"},
		{"10.1.2.3/8", "10.0.0.0/8"}, // нормализация сети
		{"vk.com", "vk.com"},
		{"VK.COM.", "vk.com"}, // регистр + концевая точка
		{"https://whoer.net/ru", "whoer.net"},
		{"*.Ozon.ru", "*.ozon.ru"},
		{"  8.8.8.8  ", "8.8.8.8/32"},
	}
	for _, c := range cases {
		got, err := backend.NormalizeSplitEntry(c.in)
		if err != nil {
			t.Errorf("NormalizeSplitEntry(%q): неожиданная ошибка %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeSplitEntry(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeSplitEntry_Invalid(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"не домен и не ip",
		"256.1.1.1",
		"10.0.0.0/99",
		"::1",        // IPv6
		"2001:db8::/32",
		"-.bad.com",  // лейбл с дефисом в начале
		"bad..com",   // пустой лейбл
		"localhost.", // нет точки в конце после trim... фактически "localhost" без точки → мусор
	}
	for _, s := range bad {
		if got, err := backend.NormalizeSplitEntry(s); err == nil {
			t.Errorf("NormalizeSplitEntry(%q) = %q, ожидалась ошибка", s, got)
		}
	}
}

func TestSplitEntryIsDomain(t *testing.T) {
	if !backend.SplitEntryIsDomain("vk.com") {
		t.Error("vk.com — домен")
	}
	if backend.SplitEntryIsDomain("10.0.0.0/8") {
		t.Error("10.0.0.0/8 — не домен")
	}
}

// ═══════════════════════════════════════════════════
// ResolveSplitRoutes — резолв с кэшем (без реальной сети)
// ═══════════════════════════════════════════════════

// withFakeResolver подменяет DNS-резолвер на время теста.
// newTestStore уже вызван тестом — конфиг уходит во временную папку.
func withFakeResolver(t *testing.T, fn func(host string) ([]string, error)) {
	t.Helper()
	orig := backend.LookupIPv4
	backend.LookupIPv4 = fn
	t.Cleanup(func() { backend.LookupIPv4 = orig })
}

func TestResolveSplitRoutes_Mixed(t *testing.T) {
	newTestStore(t)
	withFakeResolver(t, func(host string) ([]string, error) {
		if host == "vk.com" {
			return []string{"1.2.3.4", "5.6.7.8"}, nil
		}
		return nil, os.ErrDeadlineExceeded
	})

	got := backend.ResolveSplitRoutes([]string{"9.9.9.9/32", "vk.com"}, nil)
	if !contains(got, "9.9.9.9/32") {
		t.Errorf("CIDR должен проходить как есть: %v", got)
	}
	if !contains(got, "1.2.3.4/32") || !contains(got, "5.6.7.8/32") {
		t.Errorf("домен должен дать /32: %v", got)
	}
}

func TestResolveSplitRoutes_DomainFailureWithoutCache(t *testing.T) {
	newTestStore(t)
	withFakeResolver(t, func(host string) ([]string, error) {
		return nil, os.ErrDeadlineExceeded
	})
	var logs []string
	got := backend.ResolveSplitRoutes([]string{"dead.example"}, func(m string) { logs = append(logs, m) })
	if len(got) != 0 {
		t.Errorf("при сбое без кэша ничего не должно вернуться: %v", got)
	}
	if len(logs) == 0 {
		t.Error("сбой должен логироваться")
	}
}

func TestResolveSplitRoutes_StaleCacheFallback(t *testing.T) {
	newTestStore(t)
	// Просроченный кэш + недоступный DNS → берём stale (как BypassRoutes.kt)
	writeDNSCache(t, xwdttDir(t), `{"vk.com":{"ips":["8.8.8.8"],"ts":1}}`)

	withFakeResolver(t, func(host string) ([]string, error) {
		return nil, os.ErrDeadlineExceeded
	})
	got := backend.ResolveSplitRoutes([]string{"vk.com"}, nil)
	if !contains(got, "8.8.8.8/32") {
		t.Errorf("ожидался stale IP из кэша: %v", got)
	}
}

func TestResolveSplitRoutes_FreshCacheSkipsDNS(t *testing.T) {
	newTestStore(t)
	// Свежий кэш → резолв не нужен
	writeDNSCache(t, xwdttDir(t), freshCacheJSON("vk.com", "9.9.9.9"))

	withFakeResolver(t, func(host string) ([]string, error) {
		t.Error("DNS не должен спрашиваться при свежем кэше")
		return nil, nil
	})
	got := backend.ResolveSplitRoutes([]string{"vk.com"}, nil)
	if !contains(got, "9.9.9.9/32") {
		t.Errorf("ожидался IP из свежего кэша: %v", got)
	}
}

// ═══════════════════════════════════════════════════
// Настройки: RouteMode
// ═══════════════════════════════════════════════════

func TestLoadSettings_RouteModeDefault(t *testing.T) {
	s := newTestStore(t)
	if got := s.LoadSettings(); got.RouteMode != backend.RouteModeFull {
		t.Errorf("RouteMode по умолчанию = %q, ожидался full", got.RouteMode)
	}

	// Мусор в файле → full
	path := filepath.Join(xwdttDir(t), "config.json")
	os.WriteFile(path, []byte(`{"routeMode":"banana"}`), 0o644)
	if got := s.LoadSettings(); got.RouteMode != backend.RouteModeFull {
		t.Errorf("мусорный RouteMode = %q, ожидался full", got.RouteMode)
	}

	// split сохраняется
	os.WriteFile(path, []byte(`{"routeMode":"split","splitRoutes":["vk.com"]}`), 0o644)
	got := s.LoadSettings()
	if got.RouteMode != backend.RouteModeSplit {
		t.Errorf("RouteMode = %q, ожидался split", got.RouteMode)
	}
	if len(got.SplitRoutes) != 1 || got.SplitRoutes[0] != "vk.com" {
		t.Errorf("SplitRoutes = %v", got.SplitRoutes)
	}
}

func TestSplitRoutes_Roundtrip(t *testing.T) {
	s := newTestStore(t)
	settings := s.LoadSettings()
	settings.RouteMode = backend.RouteModeSplit
	settings.SplitRoutes = []string{"1.2.3.4/32", "vk.com"}
	if err := s.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	loaded := s.LoadSettings()
	if loaded.RouteMode != backend.RouteModeSplit {
		t.Errorf("RouteMode: %q", loaded.RouteMode)
	}
	if strings.Join(loaded.SplitRoutes, ",") != "1.2.3.4/32,vk.com" {
		t.Errorf("SplitRoutes: %v", loaded.SplitRoutes)
	}
}

// ═══════════════════════════════════════════════════
// helpers
// ═══════════════════════════════════════════════════

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func count(list []string, v string) int {
	n := 0
	for _, s := range list {
		if s == v {
			n++
		}
	}
	return n
}

// writeDNSCache пишет dnscache.json (готовое JSON-тело entries) по пути dir.
func writeDNSCache(t *testing.T, dir, entriesJSON string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	body := `{"entries":` + entriesJSON + `}`
	if err := os.WriteFile(filepath.Join(dir, "dnscache.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write dnscache: %v", err)
	}
}

// freshCacheJSON — запись кэша с текущей меткой времени (TTL не истёк).
func freshCacheJSON(host, ip string) string {
	return `{"` + host + `":{"ips":["` + ip + `"],"ts":` +
		strconv.FormatInt(time.Now().Unix(), 10) + `}}`
}
