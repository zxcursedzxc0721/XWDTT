package backend

// RAW-режим (Этап 4.4): сырой IP-туннель без WireGuard-устройства.
//
// Сервер с -listen-raw отвечает на GETCONF_RAW строкой "RAWCONF:ip|dns|mtu"
// (см. proxy-turn-vk-android/server/raw.go handleConnRaw). Клиент поднимает
// wintun-интерфейс БЕЗ device.NewDevice (пакеты читаются/пишутся напрямую
// через tun.Device — StartSession уже вызван в tun.CreateTUN), назначает
// IP и маршруты теми же netsh/route-командами, что и applyWindows, и
// отдаёт обёртку диспетчеру через core.Core.AttachTUN.
//
// Параметры адресации — как на Android (RawTunVpnService.kt): адрес /32,
// в full — дефолт через туннель, в split — только адреса списка.

import (
	"fmt"
	"log"
	"runtime"
	"strings"

	"golang.zx2c4.com/wireguard/tun"
)

// RawTUN — интерфейс raw-устройства для передачи в ядро (структурно
// совпадает с core.RawTUN: Read/Write/Close).
type RawTUN interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
}

// parseRAWConf разбирает "RAWCONF:ip|dns|mtu" (dns — CSV серверных
// DNS-резолверов, может быть пустым).
func parseRAWConf(rawConf string) (ip, dnsCSV string, mtu int, err error) {
	if !strings.HasPrefix(rawConf, "RAWCONF:") {
		return "", "", 0, fmt.Errorf("неожиданный raw-конфиг: %q", rawConf)
	}
	parts := strings.Split(strings.TrimPrefix(rawConf, "RAWCONF:"), "|")
	if len(parts) != 3 {
		return "", "", 0, fmt.Errorf("некорректный RAWCONF: %q", rawConf)
	}
	ip = strings.TrimSpace(parts[0])
	dnsCSV = strings.TrimSpace(parts[1])
	if _, convErr := fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &mtu); convErr != nil || mtu <= 0 {
		return "", "", 0, fmt.Errorf("некорректный MTU в RAWCONF: %q", parts[2])
	}
	if ip == "" {
		return "", "", 0, fmt.Errorf("пустой IP в RAWCONF: %q", rawConf)
	}
	return ip, dnsCSV, mtu, nil
}

// ApplyRaw поднимает raw-интерфейс по RAWCONF и возвращает устройство
// для core.AttachTUN. turnIPs/policy — тот же план маршрутов, что и в
// applyWindows: full = 0.0.0.0/0 через туннель + исключения (TURN/VK/DNS)
// мимо него, split = только адреса списка.
func (w *WG) ApplyRaw(rawConf string, turnIPs []string, policy RoutePolicy, logf wgLogFunc) (RawTUN, error) {
	if logf == nil {
		logf = func(msg string) { log.Printf("[WG] %s", msg) }
	}
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("raw-режим пока поддерживается только на Windows")
	}

	w.stateMu.Lock()
	defer w.stateMu.Unlock()
	w.teardownWindowsLocked()

	if err := extractWintun(); err != nil {
		return nil, fmt.Errorf("extract wintun.dll: %w", err)
	}

	ip, dnsCSV, mtu, err := parseRAWConf(rawConf)
	if err != nil {
		return nil, err
	}

	// AllowedIPs в raw-режиме нет — в full весь трафик идёт через туннель,
	// поэтому просим план с дефолтом (в split он всё равно игнорируется).
	plan := BuildRoutePlan([]string{"0.0.0.0/0"}, turnIPs, policy)

	logf(fmt.Sprintf("Создание raw TUN интерфейса %q (ip=%s, mtu=%d)", wgIface, ip, mtu))
	tunDev, err := tun.CreateTUN(wgIface, mtu)
	if err != nil {
		return nil, fmt.Errorf("create TUN: %w", err)
	}
	w.activeTun = tunDev

	// Адрес /32 (как addAddress(ip, 32) на Android): шлюз не нужен —
	// хостовые маршруты ниже.
	host, mask, cidrErr := parseCIDR(ip + "/32")
	if cidrErr != nil {
		w.teardownWindowsLocked()
		return nil, fmt.Errorf("IP %q: %w", ip, cidrErr)
	}
	if err := runCmdWindows("netsh", "interface", "ip", "set", "address",
		"name="+wgIface, "source=static", host, mask); err != nil {
		logf(fmt.Sprintf("netsh set address failed: %v", err))
	}

	gw := defaultGatewayWindows()

	if plan.HijackDefault {
		// Интерфейс с метрикой 1 побеждает дефолтный маршрут провайдера
		// (тот поднимается до 9999) — как в applyWindows.
		_ = runCmdWindows("netsh", "interface", "ip", "set", "interface",
			"interface="+wgIface, "metric=1")
		logf(fmt.Sprintf("Default gateway: %s", gw))
		if gw != "" {
			w.physGW = gw
			_ = runCmdWindows("route", "change", "0.0.0.0", "mask", "0.0.0.0", gw, "metric", "9999")
		}

		// DNS из RAWCONF назначаем только в full: в туннеле серверный
		// резолвер доступен, а в split адреса списка через него не идут
		// (маршрута к нему нет) — там остаёмся на DNS провайдера.
		if dnsCSV != "" {
			for i, srv := range strings.Split(dnsCSV, ",") {
				srv = strings.TrimSpace(srv)
				if srv == "" {
					continue
				}
				if i == 0 {
					if dnsErr := runCmdWindows("netsh", "interface", "ip", "set", "dnsservers",
						"name="+wgIface, "static", srv, "primary", "validate=no"); dnsErr != nil {
						logf(fmt.Sprintf("netsh set dnsservers %s: %v", srv, dnsErr))
					}
					continue
				}
				_ = runCmdWindows("netsh", "interface", "ip", "add", "dnsservers",
					"name="+wgIface, srv, fmt.Sprintf("index=%d", i+1), "validate=no")
			}
			logf(fmt.Sprintf("DNS: %s", dnsCSV))
		}
	}

	// Exclude routes — TURN/VK/DNS мимо туннеля (в split пусто).
	var excludes []string
	if gw != "" {
		excludes = plan.ExcludeRoutes
		for _, cidr := range excludes {
			ex, exMask, exErr := parseCIDR(cidr)
			if exErr != nil {
				continue
			}
			_ = runCmdWindows("route", "add", ex, "mask", exMask, gw)
		}
	}
	w.activeExcludeRoutes = excludes

	// Маршруты через интерфейс: full — 0.0.0.0/0, split — адреса списка.
	var tunnelRoutes []string
	for _, cidr := range plan.TunnelRoutes {
		if routeErr := runCmdWindows("netsh", "interface", "ip", "add", "route", cidr, wgIface); routeErr != nil {
			logf(fmt.Sprintf("netsh add route %s failed: %v", cidr, routeErr))
			continue
		}
		tunnelRoutes = append(tunnelRoutes, cidr)
	}
	w.activeRoutes = tunnelRoutes

	logf(fmt.Sprintf("Raw туннель %s поднят (ip=%s), маршруты через него: %v", wgIface, ip, tunnelRoutes))
	return newRawTunAdapter(tunDev), nil
}

// ParseRAWConf — обёртка для тестов (см. parseRAWConf).
func ParseRAWConf(rawConf string) (ip, dnsCSV string, mtu int, err error) {
	return parseRAWConf(rawConf)
}

// rawTunAdapter — однопакетная обёртка над tun.Device (batch-интерфейс
// wireguard-go) для core.RawTUN. readLoop и writeLoop работают параллельно,
// поэтому срезы-обёртки локальны в каждом вызове (гонки на общем поле нет).
type rawTunAdapter struct {
	dev tun.Device
}

func newRawTunAdapter(dev tun.Device) *rawTunAdapter {
	return &rawTunAdapter{dev: dev}
}

func (a *rawTunAdapter) Read(p []byte) (int, error) {
	bufs := [][]byte{p}
	sizes := make([]int, 1)
	for {
		n, err := a.dev.Read(bufs, sizes, 0)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue // батч без пакетов — не должно случаться, но не возвращаем 0 без ошибки
		}
		return sizes[0], nil
	}
}

func (a *rawTunAdapter) Write(p []byte) (int, error) {
	if _, err := a.dev.Write([][]byte{p}, 0); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (a *rawTunAdapter) Close() error { return a.dev.Close() }
