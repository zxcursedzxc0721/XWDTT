package core

// DNS-дилер для резолва VK — перенос из
// proxy-turn-vk-android/go_client/creds.go (setupGlobalResolver и хелперы).
//
// Выбор режима задаётся аргументом (Config.DNS):
//
//	yandex|cloudflare|google        — обычный DNS :53 (UDP с TCP-фолбэком)
//	doh-yandex|doh-cloudflare|doh-google — DoH (см. doh.go)
//	custom:IP[,IP...]               — свой список DNS :53
//	doh:https://host/dns-query,...   — свой DoH-эндпоинт
//
// Пустой аргумент = "yandex" (поведение до Этапа 4).

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

// goDNSServersForPreset — серверы для известных пресетов :53.
func goDNSServersForPreset(preset string) []string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "cloudflare":
		return []string{"1.1.1.1:53", "1.0.0.1:53"}
	case "google":
		return []string{"8.8.8.8:53", "8.8.4.4:53"}
	default:
		return []string{"77.88.8.8:53", "77.88.8.1:53"}
	}
}

// goDNSServersForArg — "custom:IP,IP" → свой список, иначе пресет.
func goDNSServersForArg(arg string) []string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "custom:") {
		raw := strings.TrimPrefix(arg, "custom:")
		var out []string
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(part, ":") {
				part += ":53"
			}
			out = append(out, part)
		}
		if len(out) > 0 {
			return out
		}
		return goDNSServersForPreset("yandex")
	}
	return goDNSServersForPreset(arg)
}

// isLoopbackDNSAddress — локальный резолвер (systemd-resolved и т.п.)
// не трогаем: он и так нормально работает и не подвержен DPI.
func isLoopbackDNSAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost" || host == "0.0.0.0"
}

// goDNSLabel — человекочитаемое имя режима для лога.
func goDNSLabel(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "custom:") {
		return "Свой DNS"
	}
	if strings.HasPrefix(arg, "doh:") {
		return "Свой DoH"
	}
	switch strings.ToLower(arg) {
	case "cloudflare":
		return "Cloudflare"
	case "google":
		return "Google DNS"
	case "doh-cloudflare":
		return "Cloudflare DoH"
	case "doh-google":
		return "Google DoH"
	case "doh-yandex":
		return "Яндекс DoH"
	default:
		return "Яндекс DNS"
	}
}

func formatGoDNSServers(servers []string) string {
	parts := make([]string, 0, len(servers))
	for _, s := range servers {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			parts = append(parts, s)
			continue
		}
		if port == "53" {
			parts = append(parts, host)
		} else {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// setupGlobalResolver — настройка net.DefaultResolver по аргументу.
func setupGlobalResolver(arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		arg = "yandex" // поведение до Этапа 4
	}
	if goDNSIsDoH(arg) {
		setupDoHResolver(arg, goDoHEndpointsForArg(arg))
		return
	}

	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	servers := goDNSServersForArg(arg)
	log.Printf(
		"[КЛИЕНТ] DNS для VK: %s (%s) — UDP/TCP :53",
		goDNSLabel(arg),
		formatGoDNSServers(servers),
	)

	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var lastErr error
			for _, dns := range servers {
				conn, err := dialer.DialContext(ctx, "udp", dns)
				if err == nil {
					return conn, nil
				}
				lastErr = err
				conn, err = dialer.DialContext(ctx, "tcp", dns)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}

			address = strings.TrimSpace(address)
			if address != "" && !isLoopbackDNSAddress(address) {
				conn, err := dialer.DialContext(ctx, network, address)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("no DNS servers available for %q", arg)
			}
			return nil, lastErr
		},
	}
}
