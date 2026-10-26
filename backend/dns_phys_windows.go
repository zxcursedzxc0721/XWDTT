//go:build windows

package backend

// DNS физических адаптеров на время full-сессии.
//
// Windows использует «smart multi-homed name resolution»: DNS-запросы уходят
// ПАРАЛЛЕЛЬНО по всем адаптерам, даже когда default-маршрут перехвачен
// туннелем — запросы к DNS провайдера утекают с реальным IP мимо туннеля.
// Фикс: на время full-сессии зануляем IPv4 DNS на всех физических
// адаптерах, а перед этим сохраняем их в файл — чтобы восстановить даже
// после краша процесса (netsh-правки персистентны, без восстановления
// машина осталась бы «без интернета»).
//
// В split-режиме не трогаем: там DNS провайдера — штатный путь.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const ipDhcpEnabledFlag = 0x4 // IP_ADAPTER_DHCP_ENABLED в IP_ADAPTER_ADDRESSES.Flags

// dnsBackupEntry — сохранённое состояние IPv4 DNS одного адаптера.
type dnsBackupEntry struct {
	Alias   string   `json:"alias"`           // имя подключения (для netsh)
	Servers []string `json:"servers,omitempty"` // статические DNS (когда DHCP=false)
	DHCP    bool     `json:"dhcp"`              // были от DHCP → source=dhcp при возврате
}

func dnsBackupPath() string {
	return filepath.Join(configDir(), "dns-backup.json")
}

// listPhysicalDNSExcluding читает IPv4 DNS всех адаптеров, кроме excludeAlias.
func listPhysicalDNSExcluding(excludeAlias string) ([]dnsBackupEntry, error) {
	var size uint32
	// Первый вызов — узнать размер буфера (ожидаемая ошибка buffer overflow).
	windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, nil, &size)
	if size == 0 {
		return nil, fmt.Errorf("GetAdaptersAddresses: нулевой размер")
	}
	buf := make([]byte, size)
	if err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0,
		(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size); err != nil {
		return nil, err
	}

	var out []dnsBackupEntry
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		alias := windows.UTF16PtrToString(a.FriendlyName)
		if alias == "" || strings.EqualFold(alias, excludeAlias) {
			continue
		}
		dhcp := a.Flags&ipDhcpEnabledFlag != 0
		var servers []string
		for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
			ip := d.Address.IP()
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				servers = append(servers, v4.String())
			}
		}
		if !dhcp && len(servers) == 0 {
			continue // DNS и так не задан — чистить и восстанавливать нечего
		}
		out = append(out, dnsBackupEntry{Alias: alias, Servers: servers, DHCP: dhcp})
	}
	return out, nil
}

// guardPhysicalDNS зануляет IPv4 DNS физических адаптеров (вызывается из
// full-ветки applyWindows/ApplyRaw после настройки DNS туннеля).
// Сначала пишет файл бэкапа: без него не чистит — лучше утечка, чем
// навсегда потерянный DNS.
func guardPhysicalDNS(logf func(string)) {
	path := dnsBackupPath()
	if _, err := os.Stat(path); err == nil {
		return // уже занулено (повторный apply без честного teardown)
	}
	entries, err := listPhysicalDNSExcluding(wgIface)
	if err != nil {
		logf("DNS физ. адаптеров: " + err.Error())
		return
	}
	if len(entries) == 0 {
		return
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		logf("DNS бэкап: " + err.Error())
		return
	}
	if err := atomicWrite(path, data); err != nil {
		logf("DNS бэкап не записан, физические DNS не трогаю: " + err.Error())
		return
	}
	cleared := 0
	for _, e := range entries {
		if err := ifaceClearDNS(e.Alias); err != nil {
			logf(fmt.Sprintf("DNS физ. адаптера %q: %v", e.Alias, err))
			continue
		}
		cleared++
	}
	if cleared > 0 {
		logf(fmt.Sprintf("DNS физических адаптеров отключён (%d из %d) — восстановится при отключении", cleared, len(entries)))
	}
}

// restorePhysicalDNS возвращает сохранённые DNS и удаляет файл бэкапа.
func restorePhysicalDNS(logf func(string)) bool {
	path := dnsBackupPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_ = os.Remove(path) // убираем ДО применения: повторный краш не оставит битый цикл
	var entries []dnsBackupEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		if logf != nil {
			logf("DNS бэкап повреждён: " + err.Error())
		}
		return false
	}
	ok := 0
	for _, e := range entries {
		var err error
		switch {
		case e.DHCP:
			err = runCmdWindows("netsh", "interface", "ipv4", "set", "dnsservers",
				"name="+e.Alias, "source=dhcp", "validate=no")
		case len(e.Servers) > 0:
			err = ifaceSetDNS(e.Alias, e.Servers)
		default:
			continue
		}
		if err != nil {
			if logf != nil {
				logf(fmt.Sprintf("вернуть DNS %q: %v", e.Alias, err))
			}
			continue
		}
		ok++
	}
	if logf != nil && len(entries) > 0 {
		logf(fmt.Sprintf("DNS физических адаптеров восстановлен: %d/%d", ok, len(entries)))
	}
	return ok > 0
}
