package backend

import "net"

// IPv6-утечка.
//
// Вся маршрутизация туннеля (routeplan, winip, netsh) — IPv4-only: маршруты
// 0.0.0.0/0 и список split не покрывают IPv6 вообще. Если у провайдера есть
// глобальный IPv6, в full-режиме весь IPv6-трафик уходил напрямую в интернет —
// пользователь думает, что он за VPN, а его реальный IP (v6) утекает.

// hasGlobalIPv6 сообщает, есть ли на системе глобальный IPv6-адрес провайдера.
// Только диапазон 2000::/3 действительно утекает наружу: link-local (fe80::/10)
// и ULA (fc00::/7) интернетом не маршрутизируются.
func hasGlobalIPv6() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if isGlobalIPv6(ipn.IP) {
				return true
			}
		}
	}
	return false
}

// isGlobalIPv6 true для адресов из 2000::/3 (глобальные unicast IANA).
func isGlobalIPv6(ip net.IP) bool {
	ip = ip.To16()
	if ip == nil || ip.To4() != nil {
		return false
	}
	return ip[0]&0xe0 == 0x20
}

// guardIPv6Leak закрывает IPv6-утечку при поднятии туннеля:
//   - full: ::/0 уводится через туннель-интерфейс — WireGuard отбрасывает
//     IPv6 (его нет в allowedips), вместо утечки наружу получаем «чёрную
//     дыру»; IPv4-сайты при этом работают по A-записям как обычно;
//   - split: ::/0 через туннель сломал бы легитимный direct-трафик
//     неперечисленных сайтов, поэтому только предупреждаем — AAAA-записи
//     перечисленных адресов идут мимо туннеля.
func (w *WG) guardIPv6Leak(full bool, logf wgLogFunc) {
	if !hasGlobalIPv6() {
		return
	}
	if !full {
		logf("ВНИМАНИЕ: обнаружен глобальный IPv6 провайдера — в split-режиме IPv6-трафик идёт мимо туннеля (AAAA-записи обходят список). Отключите IPv6 на адаптере, если нужна полная приватность.")
		return
	}
	if err := blockIPv6ViaTunnel(); err != nil {
		logf("ВНИМАНИЕ: не удалось перехватить IPv6 — возможна утечка реального IP: " + err.Error())
		return
	}
	w.activeIPv6Route = true
	logf("IPv6 перехвачен через туннель: утечка по IPv6/AAAA закрыта")
}
