//go:build windows

package backend

// Работа с сетевыми интерфейсами и маршрутами Windows через IP Helper API
// (iphlpapi/netioapi), а не через netsh/route.
//
// Причины: netsh молча «успешно» применяет неверные команды. Проверено на
// живой Windows 11: `netsh interface ip set address name=xwdtt-wg
// source=static 10.99.0.2/32 none` падает с «недопустимый параметр mask
// (none)», а `... 10.99.0.2 255.255.255.255` возвращает код 0, но адрес НЕ
// назначает. В результате TUN-интерфейс остаётся без IPv4-адреса, маршруты
// в него добавляются, WireGuard-устройство поднято — и ни один пакет не
// ходит: снаружи это выглядит как «подключился, но ничего не работает».
//
// Дополнительно `netsh ... add route` по умолчанию пишет маршруты в
// постоянное хранилище (registry), откуда они не исчезают после краха и
// перезагрузки. API создаёт только активные маршруты, привязанные к LUID
// интерфейса, и позволяет точно проверять/удалять их.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ipDadStatePreferred  = 4 // IpDadStatePreferred: адрес считается готовым сразу
	ipPrefixOriginManual = 2 // IpPrefixOriginManual
	ipSuffixOriginManual = 2 // IpSuffixOriginManual
	infiniteLifetime     = 0xffffffff
)

var (
	modIphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

	procConvertInterfaceAliasToLuid     = modIphlpapi.NewProc("ConvertInterfaceAliasToLuid")
	procInitializeUnicastIpAddressEntry = modIphlpapi.NewProc("InitializeUnicastIpAddressEntry")
	procCreateUnicastIpAddressEntry     = modIphlpapi.NewProc("CreateUnicastIpAddressEntry")
	procDeleteUnicastIpAddressEntry     = modIphlpapi.NewProc("DeleteUnicastIpAddressEntry")
	procInitializeIpForwardEntry        = modIphlpapi.NewProc("InitializeIpForwardEntry")
	procCreateIpForwardEntry2           = modIphlpapi.NewProc("CreateIpForwardEntry2")
	procDeleteIpForwardEntry2           = modIphlpapi.NewProc("DeleteIpForwardEntry2")
	procInitializeIpInterfaceEntry      = modIphlpapi.NewProc("InitializeIpInterfaceEntry")
	procSetIpInterfaceEntry             = modIphlpapi.NewProc("SetIpInterfaceEntry")
)

// ═══════════════════════════════════════════════════
// SOCKADDR / LUID
// ═══════════════════════════════════════════════════

// ipv4Sockaddr упаковывает IPv4 в SOCKADDR_INET. nil → 0.0.0.0 (on-link).
func ipv4Sockaddr(ip net.IP) windows.RawSockaddrInet {
	var sa windows.RawSockaddrInet
	sa.Family = windows.AF_INET
	if v4 := ip.To4(); v4 != nil {
		sa.Data[0] = binary.LittleEndian.Uint32(v4)
	}
	return sa
}

// sockaddrIPv4 разбирает SOCKADDR_INET. Не IPv4 → nil.
func sockaddrIPv4(sa windows.RawSockaddrInet) net.IP {
	if sa.Family != windows.AF_INET {
		return nil
	}
	v := sa.Data[0]
	return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).To4()
}

// MIB_UNICASTIPADDRESS_ROW хранит адрес в union SOCKADDR_INET, объявленном в
// x/sys как RawSockaddrInet6: у IPv4-адреса байты лежат по смещению 4, то
// есть на месте поля Flowinfo.
func setUnicastSockaddr(dst *windows.RawSockaddrInet6, ip net.IP) {
	dst.Family = windows.AF_INET
	dst.Port = 0
	dst.Flowinfo = 0
	dst.Addr = [16]byte{}
	dst.Scope_id = 0
	if v4 := ip.To4(); v4 != nil {
		dst.Flowinfo = binary.LittleEndian.Uint32(v4)
	}
}

func unicastSockaddrIPv4(sa windows.RawSockaddrInet6) net.IP {
	if sa.Family != windows.AF_INET {
		return nil
	}
	v := sa.Flowinfo
	return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).To4()
}

// ifaceByAlias находит интерфейс по имени подключения (например, "xwdtt-wg").
func ifaceByAlias(alias string) (ipIface, error) {
	name, err := windows.UTF16PtrFromString(alias)
	if err != nil {
		return ipIface{}, err
	}
	var luid uint64
	if ret, _, _ := procConvertInterfaceAliasToLuid.Call(
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&luid))); ret != 0 {
		return ipIface{}, fmt.Errorf("интерфейс %q не найден: %w", alias, windows.Errno(ret))
	}
	// Индекс нужен части функций API; LUID остаётся основным идентификатором.
	var index uint32
	if ret, _, _ := windows.NewLazySystemDLL("iphlpapi.dll").
		NewProc("ConvertInterfaceLuidToIndex").
		Call(uintptr(unsafe.Pointer(&luid)), uintptr(unsafe.Pointer(&index))); ret != 0 {
		index = 0
	}
	return ipIface{Alias: alias, Luid: luid, Index: index}, nil
}

// ═══════════════════════════════════════════════════
// АДРЕСА
// ═══════════════════════════════════════════════════

// ifaceSetIPv4Address назначает адрес (формат "10.8.0.2/32" или "10.8.0.2").
// После вызова проверяет, что адрес действительно появился: netsh в этом
// месте врал, и молчаливая неудача стоила неработающего туннеля.
func ifaceSetIPv4Address(iface ipIface, cidr string) error {
	ip, prefix, err := parseAddrCIDR(cidr)
	if err != nil {
		return err
	}

	var row windows.MibUnicastIpAddressRow
	procInitializeUnicastIpAddressEntry.Call(uintptr(unsafe.Pointer(&row)))
	setUnicastSockaddr(&row.Address, ip)
	row.InterfaceLuid = iface.Luid
	row.InterfaceIndex = iface.Index
	row.OnLinkPrefixLength = prefix
	row.PrefixOrigin = ipPrefixOriginManual
	row.SuffixOrigin = ipSuffixOriginManual
	row.ValidLifetime = infiniteLifetime
	row.PreferredLifetime = infiniteLifetime
	row.DadState = ipDadStatePreferred

	if ret, _, _ := procCreateUnicastIpAddressEntry.Call(uintptr(unsafe.Pointer(&row))); ret != 0 {
		// Уже назначен — не ошибка (повторное подключение к тому же адаптеру).
		if windows.Errno(ret) != windows.ERROR_OBJECT_ALREADY_EXISTS {
			return fmt.Errorf("назначить адрес %s: %w", cidr, windows.Errno(ret))
		}
	}

	if !ifaceHasIPv4(iface, ip) {
		return fmt.Errorf("адрес %s не назначен на интерфейс %q (Windows отклонил его молча)", cidr, iface.Alias)
	}
	return nil
}

// ifaceDeleteIPv4Address снимает адрес с интерфейса.
func ifaceDeleteIPv4Address(iface ipIface, ip net.IP) error {
	_, prefix, err := parseAddrCIDR(ip.String() + "/32")
	if err != nil {
		return err
	}
	var row windows.MibUnicastIpAddressRow
	setUnicastSockaddr(&row.Address, ip)
	row.InterfaceLuid = iface.Luid
	row.InterfaceIndex = iface.Index
	row.OnLinkPrefixLength = prefix
	if ret, _, _ := procDeleteUnicastIpAddressEntry.Call(uintptr(unsafe.Pointer(&row))); ret != 0 {
		if windows.Errno(ret) == windows.ERROR_NOT_FOUND || windows.Errno(ret) == windows.ERROR_FILE_NOT_FOUND {
			return nil
		}
		return fmt.Errorf("снять адрес %s: %w", ip, windows.Errno(ret))
	}
	return nil
}

// ifaceIPv4Addresses — все IPv4-адреса интерфейса.
func ifaceIPv4Addresses(iface ipIface) []net.IP {
	var table *windows.MibUnicastIpAddressTable
	if err := windows.GetUnicastIpAddressTable(windows.AF_INET, &table); err != nil {
		return nil
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	rows := unsafe.Slice(&table.Table[0], int(table.NumEntries))
	var out []net.IP
	for _, row := range rows {
		if row.InterfaceLuid != iface.Luid {
			continue
		}
		if ip := unicastSockaddrIPv4(row.Address); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

func ifaceHasIPv4(iface ipIface, want net.IP) bool {
	for _, ip := range ifaceIPv4Addresses(iface) {
		if ip.Equal(want) {
			return true
		}
	}
	return false
}

// ═══════════════════════════════════════════════════
// МЕТРИКА ИНТЕРФЕЙСА
// ═══════════════════════════════════════════════════

// ifaceGetMetric возвращает базовую метрику интерфейса и признак
// автоматического расчёта.
func ifaceGetMetric(iface ipIface) (metric uint32, automatic bool, err error) {
	row := windows.MibIpInterfaceRow{Family: windows.AF_INET, InterfaceLuid: iface.Luid}
	if err := windows.GetIpInterfaceEntry(&row); err != nil {
		return 0, false, fmt.Errorf("GetIpInterfaceEntry(%q): %w", iface.Alias, err)
	}
	return row.Metric, row.UseAutomaticMetric != 0, nil
}

// ifaceSetMetric задаёт фиксированную метрику интерфейса.
func ifaceSetMetric(iface ipIface, metric uint32) error {
	return ifaceApplyMetric(iface, false, metric)
}

// ifaceSetAutomaticMetric возвращает интерфейсу автоматический расчёт метрики.
func ifaceSetAutomaticMetric(iface ipIface) error {
	return ifaceApplyMetric(iface, true, 0)
}

// ifaceApplyMetric выставляет метрику через SetIpInterfaceEntry.
//
// Важно: строку обязательно инициализировать InitializeIpInterfaceEntry.
// setipinterfaceentry с полями из GetIpInterfaceEntry на живом wintun-
// адаптере возвращает ERROR_INVALID_PARAMETER (проверено: ошибка 87 на
// всех вариантах), а с инициализированной строкой — код 0.
func ifaceApplyMetric(iface ipIface, automatic bool, metric uint32) error {
	var row windows.MibIpInterfaceRow
	procInitializeIpInterfaceEntry.Call(uintptr(unsafe.Pointer(&row)))
	row.Family = windows.AF_INET
	row.InterfaceLuid = iface.Luid
	row.InterfaceIndex = iface.Index
	if automatic {
		row.UseAutomaticMetric = 1
		row.Metric = 0
	} else {
		row.UseAutomaticMetric = 0
		row.Metric = metric
	}
	if ret, _, _ := procSetIpInterfaceEntry.Call(uintptr(unsafe.Pointer(&row))); ret != 0 {
		return fmt.Errorf("SetIpInterfaceEntry(%q): %w", iface.Alias, windows.Errno(ret))
	}

	// Проверяем фактический результат: Windows умеет вернуть успех, ничего не изменив.
	got, auto, err := ifaceGetMetric(iface)
	if err != nil {
		return err
	}
	if automatic {
		if !auto {
			return fmt.Errorf("автоматическая метрика %q не включилась (метрика %d)", iface.Alias, got)
		}
		return nil
	}
	if auto || got != metric {
		return fmt.Errorf("метрика %q не применилась: получено %d (авто=%v), ожидалось %d", iface.Alias, got, auto, metric)
	}
	return nil
}

// ═══════════════════════════════════════════════════
// МАРШРУТЫ
// ═══════════════════════════════════════════════════

// listIPv4Routes читает активную таблицу маршрутизации IPv4.
func listIPv4Routes() ([]ipRoute, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_INET, &table); err != nil {
		return nil, fmt.Errorf("GetIpForwardTable2: %w", err)
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))

	rows := unsafe.Slice(&table.Table[0], int(table.NumEntries))
	out := make([]ipRoute, 0, len(rows))
	for _, row := range rows {
		dest := sockaddrIPv4(row.DestinationPrefix.Prefix)
		if dest == nil {
			continue
		}
		out = append(out, ipRoute{
			Dest:      dest,
			PrefixLen: row.DestinationPrefix.PrefixLength,
			NextHop:   sockaddrIPv4(row.NextHop),
			Luid:      row.InterfaceLuid,
			Index:     row.InterfaceIndex,
			Metric:    row.Metric,
			Protocol:  row.Protocol,
		})
	}
	return out, nil
}

// routeAdd добавляет активный маршрут (не постоянный: после перезагрузки и
// удаления интерфейса он не остаётся в registry).
func routeAdd(r ipRoute) error {
	var row windows.MibIpForwardRow2
	procInitializeIpForwardEntry.Call(uintptr(unsafe.Pointer(&row)))
	row.InterfaceLuid = r.Luid
	row.InterfaceIndex = r.Index
	row.DestinationPrefix.Prefix = ipv4Sockaddr(r.Dest)
	row.DestinationPrefix.PrefixLength = r.PrefixLen
	row.NextHop = ipv4Sockaddr(r.NextHop)
	row.Metric = r.Metric
	row.Protocol = protoNetMgmt
	row.ValidLifetime = infiniteLifetime
	row.PreferredLifetime = infiniteLifetime
	if ret, _, _ := procCreateIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row))); ret != 0 {
		return fmt.Errorf("добавить маршрут %s: %w", r, windows.Errno(ret))
	}
	return nil
}

// routeDelete удаляет маршрут. Отсутствующий маршрут — не ошибка.
func routeDelete(r ipRoute) error {
	var row windows.MibIpForwardRow2
	row.InterfaceLuid = r.Luid
	row.InterfaceIndex = r.Index
	row.DestinationPrefix.Prefix = ipv4Sockaddr(r.Dest)
	row.DestinationPrefix.PrefixLength = r.PrefixLen
	row.NextHop = ipv4Sockaddr(r.NextHop)
	if ret, _, _ := procDeleteIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row))); ret != 0 {
		if err := windows.Errno(ret); err == windows.ERROR_NOT_FOUND || err == windows.ERROR_FILE_NOT_FOUND {
			return nil
		}
		return fmt.Errorf("удалить маршрут %s: %w", r, windows.Errno(ret))
	}
	return nil
}

// bestDefaultRoute выбирает действующий маршрут по умолчанию: минимальная
// суммарная метрика (маршрут + интерфейс), кроме интерфейса excludeLuid.
func bestDefaultRoute(excludeLuid uint64) (ipRoute, error) {
	routes, err := listIPv4Routes()
	if err != nil {
		return ipRoute{}, err
	}
	ifaceMetrics := make(map[uint64]uint32)
	var best ipRoute
	bestTotal := uint32(0)
	found := false
	for _, r := range routes {
		if r.PrefixLen != 0 || r.Luid == excludeLuid {
			continue
		}
		if len(r.NextHop) == 0 || r.NextHop.Equal(net.IPv4zero) {
			continue // маршрут по умолчанию без шлюза не даёт выхода в сеть
		}
		metric, ok := ifaceMetrics[r.Luid]
		if !ok {
			if m, _, merr := ifaceGetMetric(ipIface{Luid: r.Luid}); merr == nil {
				metric = m
			}
			ifaceMetrics[r.Luid] = metric
		}
		total := r.Metric + metric
		if !found || total < bestTotal {
			best, bestTotal, found = r, total, true
		}
	}
	if !found {
		return ipRoute{}, errors.New("в системе нет маршрута по умолчанию с шлюзом")
	}
	return best, nil
}

// allDefaultRoutes возвращает ВСЕ физические default-маршруты с шлюзом
// (кроме интерфейса excludeLuid). Multi-homed системы (Ethernet+Wi-Fi)
// держат несколько 0.0.0.0/0 — kill-switch обязан снять каждый, иначе
// трафик течёт мимо туннеля через второй аплинк.
func allDefaultRoutes(excludeLuid uint64) ([]ipRoute, error) {
	routes, err := listIPv4Routes()
	if err != nil {
		return nil, err
	}
	var out []ipRoute
	for _, r := range routes {
		if r.PrefixLen != 0 || r.Luid == excludeLuid {
			continue
		}
		if len(r.NextHop) == 0 || r.NextHop.Equal(net.IPv4zero) {
			continue // маршрут по умолчанию без шлюза не даёт выхода в сеть
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, errors.New("в системе нет маршрута по умолчанию с шлюзом")
	}
	return out, nil
}

// routesThroughIface — активные маршруты, привязанные к интерфейсу.
func routesThroughIface(luid uint64) ([]ipRoute, error) {
	routes, err := listIPv4Routes()
	if err != nil {
		return nil, err
	}
	var out []ipRoute
	for _, r := range routes {
		if r.Luid == luid {
			out = append(out, r)
		}
	}
	return out, nil
}

// filterExistingRoutes оставляет из want только те маршруты, которые реально
// есть в таблице через интерфейс iface (проверка после пакетного netsh -f).
func filterExistingRoutes(iface ipIface, want []ipRoute) []ipRoute {
	existing := make(map[string]bool)
	if routes, err := routesThroughIface(iface.Luid); err == nil {
		for _, r := range routes {
			existing[r.String()] = true
		}
	}
	out := make([]ipRoute, 0, len(want))
	for _, r := range want {
		if existing[r.String()] {
			out = append(out, r)
		}
	}
	return out
}

// ═══════════════════════════════════════════════════
// DNS ИНТЕРФЕЙСА
// ═══════════════════════════════════════════════════

// ifaceSetDNS назначает DNS-серверы интерфейса (первый — основной).
// netsh здесь достаточен: ошибка видна по коду возврата, а DNS не влияет
// на маршрутизацию.
func ifaceSetDNS(alias string, servers []string) error {
	for i, srv := range servers {
		var err error
		if i == 0 {
			err = runCmdWindows("netsh", "interface", "ipv4", "set", "dnsservers",
				"name="+alias, "source=static", "address="+srv, "register=primary", "validate=no")
		} else {
			err = runCmdWindows("netsh", "interface", "ipv4", "add", "dnsservers",
				"name="+alias, "address="+srv, fmt.Sprintf("index=%d", i+1), "validate=no")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ifaceClearDNS убирает DNS-серверы интерфейса (чтобы настройки прошлой
// full-сессии не влияли на split-сессию).
func ifaceClearDNS(alias string) error {
	return runCmdWindows("netsh", "interface", "ipv4", "delete", "dnsservers",
		"name="+alias, "address=all")
}
