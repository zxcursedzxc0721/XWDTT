package backend

import (
	"net"
	"testing"
)

func TestIsGlobalIPv6(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"2a00:1450:4001:80f::200e", true},  // глобальный unicast провайдера
		{"2606:4700::1111", true},           // Cloudflare
		{"fe80::1", false},                  // link-local
		{"fd00::1", false},                  // ULA
		{"fdd3:f9be:2ecd::1", false},        // ULA (как на машине тестов)
		{"::1", false},                      // loopback
		{"192.168.1.67", false},             // IPv4
		{"::ffff:192.168.1.67", false},      // IPv4-mapped
	}
	for _, c := range cases {
		if got := isGlobalIPv6(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("isGlobalIPv6(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestHasGlobalIPv6DoesNotPanic(t *testing.T) {
	// Значение зависит от машины — проверяем только, что детекция работает
	// и не падает без сети/IPv6.
	_ = hasGlobalIPv6()
}
