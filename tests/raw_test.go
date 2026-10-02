package backend_test

import (
	"runtime"
	"strings"
	"testing"

	"pwdtt/backend"
)

// ═══════════════════════════════════════════════════
// parseRAWConf (Этап 4.4: RAW-режим)
// ═══════════════════════════════════════════════════

func TestParseRAWConfOK(t *testing.T) {
	ip, dns, mtu, err := backend.ParseRAWConf("RAWCONF:10.70.0.5|8.8.8.8,1.1.1.1|1300")
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if ip != "10.70.0.5" || dns != "8.8.8.8,1.1.1.1" || mtu != 1300 {
		t.Errorf("получено ip=%q dns=%q mtu=%d", ip, dns, mtu)
	}

	// Пустой DNS допустим (сервер может его не выдавать).
	ip, dns, mtu, err = backend.ParseRAWConf("RAWCONF:10.70.0.6||1280")
	if err != nil || ip != "10.70.0.6" || dns != "" || mtu != 1280 {
		t.Errorf("пустой dns: ip=%q dns=%q mtu=%d err=%v", ip, dns, mtu, err)
	}
}

func TestParseRAWConfErrors(t *testing.T) {
	bad := []string{
		"",
		"WGCONF:10.70.0.5||1300",       // не RAWCONF
		"RAWCONF:10.70.0.5|8.8.8.8",     // 2 поля вместо 3
		"RAWCONF:10.70.0.5|8.8.8.8|abc", // не число MTU
		"RAWCONF:10.70.0.5|8.8.8.8|0",   // нулевой MTU
		"RAWCONF:|8.8.8.8|1300",         // пустой IP
	}
	for _, s := range bad {
		if _, _, _, err := backend.ParseRAWConf(s); err == nil {
			t.Errorf("ParseRAWConf(%q): ожидалась ошибка", s)
		}
	}
}

// applyRaw на не-Windows возвращает понятную ошибку, а не паникует.
func TestApplyRawUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("тест для не-Windows платформ")
	}
	var w backend.WG
	_, err := w.ApplyRaw("RAWCONF:10.70.0.5||1300", nil, backend.RoutePolicy{}, nil)
	if err == nil || !strings.Contains(err.Error(), "Windows") {
		t.Errorf("ожидалась ошибка про Windows, получено: %v", err)
	}
}
