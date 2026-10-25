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
	"net"
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

	iface, ifaceErr := ifaceByAlias(wgIface)
	if ifaceErr != nil {
		w.teardownWindowsLocked()
		return nil, ifaceErr
	}
	w.activeIface = iface

	// Адрес /32 (как addAddress(ip, 32) на Android): шлюз не нужен —
	// хостовые маршруты ниже. Назначаем через IP Helper API: netsh для /32
	// возвращает успех, но адрес не назначает.
	if err := ifaceSetIPv4Address(iface, ip+"/32"); err != nil {
		w.teardownWindowsLocked()
		return nil, err
	}
	w.activeAddr = ip + "/32"
	logf(fmt.Sprintf("Адрес %s назначен на %s", ip, wgIface))

	if plan.HijackDefault {
		def, err := bestDefaultRoute(iface.Luid)
		if err != nil {
			w.teardownWindowsLocked()
			return nil, fmt.Errorf("full-режим невозможен: %w", err)
		}
		logf(fmt.Sprintf("Шлюз по умолчанию: %s (интерфейс %d)", def.NextHop, def.Index))

		// Метрика 1 только у своего интерфейса — физический default не трогаем.
		if m, _, merr := ifaceGetMetric(iface); merr == nil {
			w.savedMetric, w.hadMetric = m, true
		}
		if err := ifaceSetMetric(iface, 1); err != nil {
			logf("метрика интерфейса: " + err.Error())
		}

		defRoute, rerr := newRoute("0.0.0.0/0", nil, iface, 0)
		if rerr != nil {
			w.teardownWindowsLocked()
			return nil, rerr
		}
		if err := routeAdd(defRoute); err != nil {
			w.teardownWindowsLocked()
			return nil, err
		}
		w.activeWinRoute = append(w.activeWinRoute, defRoute)

		// DNS из RAWCONF назначаем только в full: в туннеле серверный
		// резолвер доступен, а в split адреса списка через него не идут
		// (маршрута к нему нет) — там остаёмся на DNS провайдера.
		var dnsList []string
		for _, s := range strings.Split(dnsCSV, ",") {
			if parsed := net.ParseIP(strings.TrimSpace(s)); parsed != nil && parsed.To4() != nil {
				dnsList = append(dnsList, parsed.String())
			}
		}
		if len(dnsList) > 0 {
			if err := ifaceSetDNS(iface.Alias, dnsList); err != nil {
				logf("DNS: " + err.Error())
			} else {
				w.activeDNS = true
				logf(fmt.Sprintf("DNS через туннель: %s", strings.Join(dnsList, ", ")))
			}
		}

		// Exclude routes — TURN/VK/DNS мимо туннеля (в split пусто).
		phys := ipIface{Luid: def.Luid, Index: def.Index}
		for _, cidr := range plan.ExcludeRoutes {
			r, rerr := newRoute(cidr, def.NextHop, phys, 0)
			if rerr != nil {
				continue
			}
			if err := routeAdd(r); err != nil {
				logf(fmt.Sprintf("исключение %s: %v", cidr, err))
				continue
			}
			w.activeWinExcl = append(w.activeWinExcl, r)
		}
	}

	// Маршруты через интерфейс: full — 0.0.0.0/0, split — адреса списка.
	tunnelCIDRs := plan.TunnelRoutes
	if plan.HijackDefault {
		tunnelCIDRs = withoutCIDR(tunnelCIDRs, "0.0.0.0/0")
	}
	tunnelRoutes := applyTunnelRoutes(tunnelCIDRs, iface, logf)
	w.activeWinRoute = append(w.activeWinRoute, tunnelRoutes...)

	logf(fmt.Sprintf("Raw туннель %s поднят (ip=%s), маршруты через него: %s", wgIface, ip, routeSummary(routeStrings(tunnelRoutes))))
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
