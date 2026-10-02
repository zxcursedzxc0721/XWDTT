package core

import "testing"

// normalizeDoHEndpoint принимает только https:// и проставляет /dns-query.
func TestNormalizeDoHEndpoint(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://1.1.1.1", "https://1.1.1.1/dns-query", true},
		{"https://1.1.1.1/", "https://1.1.1.1/dns-query", true},
		{"https://dns.google/dns-query", "https://dns.google/dns-query", true},
		{"https://custom.example/path", "https://custom.example/path", true},
		{"http://1.1.1.1/dns-query", "", false},  // только https
		{"1.1.1.1", "", false},                    // без схемы
		{"", "", false},                           // пусто
		{"https:///nohost", "", false},            // нет хоста
	}
	for _, c := range cases {
		got, ok := normalizeDoHEndpoint(c.in)
		if ok != c.ok {
			t.Errorf("normalizeDoHEndpoint(%q) ok = %v, ожидалось %v", c.in, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("normalizeDoHEndpoint(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

// goDoHEndpointsForPreset: известные пресеты → свои эндпоинты, неизвестный → yandex.
func TestDoHEndpointsForPreset(t *testing.T) {
	if eps := goDoHEndpointsForPreset("doh-cloudflare"); len(eps) != 2 || eps[0] != "https://1.1.1.1/dns-query" {
		t.Errorf("doh-cloudflare: %v", eps)
	}
	if eps := goDoHEndpointsForPreset("doh-google"); eps[0] != "https://8.8.8.8/dns-query" {
		t.Errorf("doh-google: %v", eps)
	}
	if eps := goDoHEndpointsForPreset("doh-yandex"); eps[0] != "https://77.88.8.8/dns-query" {
		t.Errorf("doh-yandex: %v", eps)
	}
	if eps := goDoHEndpointsForPreset("чтото-невалидное"); eps[0] != "https://77.88.8.8/dns-query" {
		t.Errorf("дефолт: %v", eps)
	}
}

// goDoHEndpointsForArg: doh:URL,URL → валидные URL, иначе пресет.
func TestDoHEndpointsForArg(t *testing.T) {
	eps := goDoHEndpointsForArg("doh:https://a.example/dns-query, http://bad.example, https://b.example")
	if len(eps) != 2 {
		t.Fatalf("ожидалось 2 валидных эндпоинта, получено %d: %v", len(eps), eps)
	}
	if eps[0] != "https://a.example/dns-query" || eps[1] != "https://b.example/dns-query" {
		t.Errorf("эндпоинты: %v", eps)
	}
	// Ни один не прошёл → фолбэк на yandex.
	eps = goDoHEndpointsForArg("doh:http://bad")
	if eps[0] != "https://77.88.8.8/dns-query" {
		t.Errorf("фолбэк: %v", eps)
	}
}

// goDNSIsDoH различает DoH-аргументы и обычные пресеты :53.
func TestDNSIsDoH(t *testing.T) {
	for _, s := range []string{"doh:yandex", "doh-cloudflare", "DOH-Google", "  doh:x "} {
		if !goDNSIsDoH(s) {
			t.Errorf("goDNSIsDoH(%q) = false, ожидалось true", s)
		}
	}
	for _, s := range []string{"yandex", "cloudflare", "custom:1.1.1.1", ""} {
		if goDNSIsDoH(s) {
			t.Errorf("goDNSIsDoH(%q) = true, ожидалось false", s)
		}
	}
}

// dohHostHeader: для IP-эндпоинтов подставляет доменное имя (SNI).
func TestDoHHostHeader(t *testing.T) {
	if got := dohHostHeader("https://1.1.1.1/dns-query"); got != "cloudflare-dns.com" {
		t.Errorf("1.1.1.1 → %q", got)
	}
	if got := dohHostHeader("https://8.8.8.8/dns-query"); got != "dns.google" {
		t.Errorf("8.8.8.8 → %q", got)
	}
	if got := dohHostHeader("https://dns.google/dns-query"); got != "" {
		t.Errorf("доменный хост не должен получать подмену, получено %q", got)
	}
}

// stripDNSWireFrame / frameDNSForTCP — обратимая конвертация TCP-фрейма.
func TestDNSWireFrameRoundTrip(t *testing.T) {
	query := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01}
	framed := frameDNSForTCP(query)
	if len(framed) != len(query)+2 {
		t.Fatalf("frameDNSForTCP: длина %d", len(framed))
	}
	back := stripDNSWireFrame(framed)
	if len(back) != len(query) {
		t.Fatalf("stripDNSWireFrame: длина %d", len(back))
	}
	for i := range query {
		if back[i] != query[i] {
			t.Fatalf("байт %d не совпал", i)
		}
	}
	// Уже голый wire-формат не трогаем.
	if raw := stripDNSWireFrame(query); len(raw) != len(query) {
		t.Errorf("stripDNSWireFrame(gолый) = %d байт", len(raw))
	}
}
