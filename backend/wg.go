package backend

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// WG — управление WireGuard интерфейсом.
var wg = &WG{}

const wgIface = "xwdtt-wg"

// Статический GUID для Wintun задан в wg_windows_init.go (windows-only),
// чтобы не плодить адаптеры (xwdtt-wg 2, xwdtt-wg 3) при крашах приложения.

// ═══════════════════════════════════════════════════
// PUBLIC API
// ═══════════════════════════════════════════════════

type WG struct {
	// Linux: маршруты в виде строк ("dev:10.0.0.0/8" — через интерфейс,
	// иначе — через шлюз).
	activeRoutes   []string
	activeRoutesMu sync.Mutex

	// stateMu сериализует Apply/Teardown и защищает поля ниже:
	// Apply вызывается из горутины forwardEvents, Teardown — из потока
	// команды CLI (disconnect/Ctrl+C), одновременный доступ = гонка.
	stateMu sync.Mutex

	// Windows/macOS userspace-specific
	activeDevice *device.Device
	activeTun    tun.Device

	// Windows: состояние сессии для точного снятия туннеля.
	// Маршруты хранятся целиком (интерфейс + шлюз + префикс): удаление по
	// одним CIDR-строкам оставляло чужие маршруты и снимало не свои.
	activeIface    ipIface
	activeAddr     string // адрес, назначенный интерфейсу ("10.8.0.2/32")
	activeWinRoute []ipRoute
	activeWinExcl  []ipRoute
	activeDNS      bool   // интерфейсу назначались DNS-серверы
	savedMetric    uint32 // метрика интерфейса до нашего вмешательства
	hadMetric      bool
	activeIPv6Route bool // ::/0 уведён через туннель (антиутечка IPv6, full)

	// macOS: exclude-маршруты, которые снимет helper по команде "down".
	activeExcludeRoutes []string

	// macOS-specific: соединение с привилегированным helper-процессом
	helperConn *net.UnixConn
}

type wgLogFunc func(msg string)

func (w *WG) Apply(conf string, turnIPs []string, policy RoutePolicy, logf wgLogFunc) error {
	if logf == nil {
		logf = func(msg string) { log.Printf("[WG] %s", msg) }
	}
	switch runtime.GOOS {
	case "linux":
		return w.applyLinux(conf, turnIPs, policy, logf)
	case "windows":
		return w.applyWindows(conf, turnIPs, policy, logf)
	case "darwin":
		return w.applyDarwin(conf, turnIPs, policy, logf)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// CleanupStaleExcludeRoutes убирает маршруты, оставшиеся после краша
// прошлого запуска (когда приложение не успело сделать уборку). Вызывать при
// старте приложения ДО подключения; на платформах без такой проблемы — no-op.
func CleanupStaleExcludeRoutes(logf wgLogFunc) {
	if logf == nil {
		logf = func(msg string) { log.Printf("[WG] %s", msg) }
	}
	switch runtime.GOOS {
	case "darwin":
		cleanupStaleExcludeRoutesDarwin(logf)
	case "windows":
		cleanupStaleRoutesWindows(logf)
	}
}

func (w *WG) Teardown() {
	switch runtime.GOOS {
	case "linux":
		w.teardownLinux()
	case "windows":
		w.teardownWindows()
	case "darwin":
		w.teardownDarwin()
	}
}

// ═══════════════════════════════════════════════════
// COMMON — парсинг конфига
// ═══════════════════════════════════════════════════

var wgQuickOnlyFields = map[string]bool{
	"address": true, "dns": true, "mtu": true,
	"preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true,
}

var vkExcludeCIDRs = []string{
	"87.240.128.0/18", "87.240.192.0/19", "90.156.0.0/16",
	"93.186.224.0/21", "95.142.192.0/21", "95.163.0.0/16",
	"95.213.0.0/18", "155.212.192.0/20", "185.16.28.0/22",
	"194.67.64.0/18", "195.82.146.0/23", "213.180.193.0/24",
	"77.88.0.0/18", "8.8.8.0/24", "1.1.1.0/24",
}

// repairHijackedDefaultMetric возвращает метрику маршрутов по умолчанию,
// равную 9999 (наследие старого full-режима), к штатному значению 0.
// Маршруты через наш собственный интерфейс не трогает.
func repairHijackedDefaultMetric(ownLuid uint64) (int, error) {
	routes, err := listIPv4Routes()
	if err != nil {
		return 0, err
	}
	repaired := 0
	for _, r := range routes {
		if r.PrefixLen != 0 || r.Luid == ownLuid || r.Metric != 9999 {
			continue
		}
		if len(r.NextHop) == 0 || r.NextHop.Equal(net.IPv4zero) {
			continue // маршрут без шлюза чинить нечего
		}
		fixed := r
		fixed.Metric = 0
		if err := routeDelete(r); err != nil {
			continue
		}
		if err := routeAdd(fixed); err != nil {
			continue
		}
		repaired++
	}
	return repaired, nil
}

// parseDNSServers достаёт IPv4-серверы из строки DNS = ... WG-конфига.
// Используется только в full-режиме: сервер выдаёт резолвер внутри туннеля.
func parseDNSServers(conf string) []string {
	var out []string
	for _, line := range strings.Split(conf, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 || strings.ToLower(strings.TrimSpace(parts[0])) != "dns" {
			continue
		}
		for _, s := range strings.Split(parts[1], ",") {
			if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil && ip.To4() != nil {
				out = append(out, ip.String())
			}
		}
	}
	return out
}

func parseWGConfig(conf string) (addr, mtu string, allowedIPs []string, wgConf string) {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(conf))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := strings.TrimSpace(parts[1])
			switch key {
			case "address":
				addr = val
				continue
			case "mtu":
				mtu = val
				continue
			case "allowedips":
				for _, cidr := range strings.Split(val, ",") {
					if c := strings.TrimSpace(cidr); c != "" {
						allowedIPs = append(allowedIPs, c)
					}
				}
			default:
				if wgQuickOnlyFields[key] {
					continue
				}
			}
		}
		out.WriteString(line + "\n")
	}
	wgConf = out.String()
	return
}

// ═══════════════════════════════════════════════════
// LINUX — ip/wg команды через sudo
// ═══════════════════════════════════════════════════

func (w *WG) applyLinux(conf string, turnIPs []string, policy RoutePolicy, logf wgLogFunc) error {
	w.teardownLinux()

	addr, mtu, allowedIPs, wgConf := parseWGConfig(conf)
	if addr == "" {
		return fmt.Errorf("Address not found in wg config")
	}

	plan := BuildRoutePlan(allowedIPs, turnIPs, policy)

	tmp, err := os.CreateTemp("", "wg-turn-*.conf")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(wgConf); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	// wgConf содержит приватный ключ — файл конфига только для владельца.
	os.Chmod(tmpName, 0o600)

	if err := runCmdLinux("ip", "link", "add", wgIface, "type", "wireguard"); err != nil {
		return fmt.Errorf("ip link add: %w", err)
	}
	if err := runCmdLinux("wg", "setconf", wgIface, tmpName); err != nil {
		return fmt.Errorf("wg setconf: %w", err)
	}
	_ = runCmdLinux("ip", "addr", "flush", "dev", wgIface)
	if err := runCmdLinux("ip", "addr", "add", addr, "dev", wgIface); err != nil {
		return fmt.Errorf("ip addr add: %w", err)
	}
	if mtu != "" {
		_ = runCmdLinux("ip", "link", "set", wgIface, "mtu", mtu)
	}
	if err := runCmdLinux("ip", "link", "set", wgIface, "up"); err != nil {
		return fmt.Errorf("ip link set up: %w", err)
	}

	var routes []string
	gw := defaultGatewayLinux()
	if gw != "" {
		for _, cidr := range plan.ExcludeRoutes {
			if runCmdLinux("ip", "route", "add", cidr, "via", gw) == nil {
				routes = append(routes, cidr)
			}
		}
	}
	for _, cidr := range plan.TunnelRoutes {
		if runCmdLinux("ip", "route", "add", cidr, "dev", wgIface) == nil {
			routes = append(routes, "dev:"+cidr)
		}
	}

	w.activeRoutesMu.Lock()
	w.activeRoutes = routes
	w.activeRoutesMu.Unlock()
	return nil
}

func (w *WG) teardownLinux() {
	w.activeRoutesMu.Lock()
	routes := w.activeRoutes
	w.activeRoutes = nil
	w.activeRoutesMu.Unlock()

	for _, entry := range routes {
		if strings.HasPrefix(entry, "dev:") {
			_ = runCmdLinux("ip", "route", "del", strings.TrimPrefix(entry, "dev:"), "dev", wgIface)
		} else {
			_ = runCmdLinux("ip", "route", "del", entry)
		}
	}
	_ = runCmdLinux("ip", "link", "del", wgIface)
}

func runCmdLinux(name string, args ...string) error {
	cmd := exec.Command("sudo", append([]string{"-n", name}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func defaultGatewayLinux() string {
	cmd := exec.Command("ip", "route", "show", "default")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func localDNSServers() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(fields[1])
		if ip == nil || ip.IsLoopback() {
			continue
		}
		result = append(result, fields[1])
	}
	return result
}

// ═══════════════════════════════════════════════════
// WINDOWS — wintun + netsh + route
// ═══════════════════════════════════════════════════

var wintunDLLData []byte

// InitWintun устанавливает wintun.dll (вызывается из main_windows.go).
func InitWintun(dll []byte) { wintunDLLData = dll }

func (w *WG) applyWindows(conf string, turnIPs []string, policy RoutePolicy, logf wgLogFunc) error {
	w.stateMu.Lock()
	defer w.stateMu.Unlock()
	w.teardownWindowsLocked()

	if err := extractWintun(); err != nil {
		return fmt.Errorf("extract wintun.dll: %w", err)
	}

	addr, mtuStr, allowedIPs, wgConf := parseWGConfig(conf)
	if addr == "" {
		return fmt.Errorf("Address not found in wg config")
	}

	plan := BuildRoutePlan(allowedIPs, turnIPs, policy)

	mtu := 1300
	if mtuStr != "" {
		fmt.Sscanf(mtuStr, "%d", &mtu)
	}

	// Create wintun TUN interface
	logf(fmt.Sprintf("Создание TUN интерфейса %q (mtu=%d)", wgIface, mtu))
	tunDev, err := tun.CreateTUN(wgIface, mtu)
	if err != nil {
		return fmt.Errorf("create TUN: %w", err)
	}
	w.activeTun = tunDev

	// Create userspace WireGuard device
	logger := &device.Logger{
		Verbosef: func(format string, args ...interface{}) {},
		Errorf:   func(format string, args ...interface{}) { logf(fmt.Sprintf(format, args...)) },
	}
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)
	w.activeDevice = dev

	logf("Применение WireGuard конфига (uapi)")
	uapiStr, uapiErr := uapiConf(wgConf)
	if uapiErr != nil {
		w.teardownWindowsLocked()
		return fmt.Errorf("uapi conf: %w", uapiErr)
	}
	if err := dev.IpcSetOperation(strings.NewReader(uapiStr)); err != nil {
		w.teardownWindowsLocked()
		return fmt.Errorf("IpcSet: %w", err)
	}

	if err := dev.Up(); err != nil {
		w.teardownWindowsLocked()
		return fmt.Errorf("device up: %w", err)
	}
	logf("WireGuard устройство поднято")

	iface, err := ifaceByAlias(wgIface)
	if err != nil {
		w.teardownWindowsLocked()
		return err
	}
	w.activeIface = iface

	// Адрес назначаем через IP Helper API: netsh для /32 на живой Windows
	// возвращает код 0, но адрес НЕ назначает — туннель поднят и мёртв.
	if err := ifaceSetIPv4Address(iface, addr); err != nil {
		w.teardownWindowsLocked()
		return err
	}
	w.activeAddr = addr
	logf(fmt.Sprintf("Адрес %s назначен на %s", addr, wgIface))

	// Split: дефолтный маршрут не перехватываем — через туннель идут только
	// адреса списка.
	if plan.HijackDefault {
		def, err := bestDefaultRoute(iface.Luid)
		if err != nil {
			// Без шлюза нельзя ни перехватить default, ни увести исключения
			// мимо туннеля: молча продолжать — значит получить нерабочий
			// туннель и утечку трафика.
			w.teardownWindowsLocked()
			return fmt.Errorf("full-режим невозможен: %w", err)
		}
		logf(fmt.Sprintf("Шлюз по умолчанию: %s (интерфейс %d)", def.NextHop, def.Index))

		// Свою метрику фиксируем, физический default не трогаем вовсе:
		// маршрут 0.0.0.0/0 через туннель побеждает за счёт метрики 1.
		if m, _, err := ifaceGetMetric(iface); err == nil {
			w.savedMetric, w.hadMetric = m, true
		}
		if err := ifaceSetMetric(iface, 1); err != nil {
			logf("метрика интерфейса: " + err.Error())
		}

		defRoute, err := newRoute("0.0.0.0/0", nil, iface, 0)
		if err != nil {
			w.teardownWindowsLocked()
			return err
		}
		if err := routeAdd(defRoute); err != nil {
			w.teardownWindowsLocked()
			return err
		}
		w.activeWinRoute = append(w.activeWinRoute, defRoute)

		// DNS из конфига сервера (10.x внутри туннеля). Без этого система
		// шлёт запросы на DNS провайдера, который ушёл в туннель и
		// недоступен с сервера — «подключено», но не открывается ничего.
		if dns := parseDNSServers(conf); len(dns) > 0 {
			if err := ifaceSetDNS(iface.Alias, dns); err != nil {
				logf("DNS: " + err.Error())
			} else {
				w.activeDNS = true
				logf(fmt.Sprintf("DNS через туннель: %s", strings.Join(dns, ", ")))
			}
		}

		// Exclude routes — TURN/VK/DNS мимо туннеля через физический шлюз.
		phys := ipIface{Luid: def.Luid, Index: def.Index}
		for _, cidr := range plan.ExcludeRoutes {
			r, rerr := newRoute(cidr, def.NextHop, phys, 0)
			if rerr != nil {
				continue // IPv6 и мусор в списке исключений пропускаем
			}
			if err := routeAdd(r); err != nil {
				logf(fmt.Sprintf("исключение %s: %v", cidr, err))
				continue
			}
			w.activeWinExcl = append(w.activeWinExcl, r)
		}
		if len(w.activeWinExcl) > 0 {
			logf(fmt.Sprintf("Мимо туннеля (%d): %s", len(w.activeWinExcl),
				routeSummary(routeStrings(w.activeWinExcl))))
		}
	}

	// IPv6-утечка: маршрутизация IPv4-only, глобальный IPv6 провайдера
	// мимо туннеля не иначе как явным перехватом (full) или предупреждением.
	w.guardIPv6Leak(plan.HijackDefault, logf)

	// Add routes via the WG interface (в split — только адреса списка).
	// 0.0.0.0/0 уже добавлен выше как перехват default — не дублируем.
	tunnelCIDRs := plan.TunnelRoutes
	if plan.HijackDefault {
		tunnelCIDRs = withoutCIDR(tunnelCIDRs, "0.0.0.0/0")
	}
	tunnelRoutes := applyTunnelRoutes(tunnelCIDRs, iface, logf)
	w.activeWinRoute = append(w.activeWinRoute, tunnelRoutes...)

	// В full-режиме перехват default не попадает в tunnelRoutes — иначе лог
	// показывал бы «маршруты через него: []» при полностью перехваченном трафике.
	summary := routeSummary(routeStrings(tunnelRoutes))
	if plan.HijackDefault {
		summary = routeSummary(append(routeStrings(tunnelRoutes), "0.0.0.0/0"))
	}
	logf(fmt.Sprintf("Туннель %s поднят, маршруты через него: %s", wgIface, summary))
	return nil
}

func (w *WG) teardownWindows() {
	w.stateMu.Lock()
	defer w.stateMu.Unlock()
	w.teardownWindowsLocked()
}

func (w *WG) teardownWindowsLocked() {
	if w.activeDevice == nil && w.activeTun == nil && len(w.activeWinRoute) == 0 && len(w.activeWinExcl) == 0 && !w.activeIPv6Route {
		return
	}

	// Свой IPv6 ::/0 снимаем первым: он не должен пережить закрытие TUN.
	if w.activeIPv6Route {
		unblockIPv6ViaTunnel()
		w.activeIPv6Route = false
	}

	// Удаляем ровно свои маршруты: по LUID + префиксу + next hop.
	// Чужие default-маршруты (в т.ч. WARP) не затрагиваются — мы их и не меняли.
	for _, r := range w.activeWinExcl {
		if err := routeDelete(r); err != nil {
			log.Printf("[WG] %v", err)
		}
	}
	w.activeWinExcl = nil
	for _, r := range w.activeWinRoute {
		if err := routeDelete(r); err != nil {
			log.Printf("[WG] %v", err)
		}
	}
	w.activeWinRoute = nil

	// Возвращаем метрику интерфейса до нашей.
	if w.hadMetric && w.activeIface.Luid != 0 {
		if err := ifaceSetMetric(w.activeIface, w.savedMetric); err != nil {
			log.Printf("[WG] вернуть метрику интерфейса: %v", err)
		}
	}
	w.hadMetric = false

	if w.activeDNS && w.activeIface.Alias != "" {
		if err := ifaceClearDNS(w.activeIface.Alias); err != nil {
			log.Printf("[WG] сбросить DNS интерфейса: %v", err)
		}
	}
	w.activeDNS = false

	// Снимаем адрес: он переживает пересоздание адаптера и мешает split-сессии.
	if w.activeAddr != "" {
		if ip, _, err := parseAddrCIDR(w.activeAddr); err == nil {
			if derr := ifaceDeleteIPv4Address(w.activeIface, ip); derr != nil {
				log.Printf("[WG] %v", derr)
			}
		}
		w.activeAddr = ""
	}
	w.activeIface = ipIface{}

	// Close WireGuard device
	if w.activeDevice != nil {
		w.activeDevice.Close()
		w.activeDevice = nil
	}

	// Close TUN device (releases wintun adapter, tunnel routes disappear with it)
	if w.activeTun != nil {
		w.activeTun.Close()
		w.activeTun = nil
	}
}

// cleanupStaleRoutesWindows снимает маршруты, оставшиеся от прошлого запуска
// (краш/выключение без teardown) и возвращает метрику интерфейса в авторежим.
// Автоматические маршруты самого адаптера (адрес, on-link) не трогает.
func cleanupStaleRoutesWindows(logf wgLogFunc) {
	iface, err := ifaceByAlias(wgIface)
	if err != nil {
		return // адаптера нет — чистить нечего
	}
	routes, err := routesThroughIface(iface.Luid)
	if err != nil {
		logf(fmt.Sprintf("очистка старых маршрутов: %v", err))
		return
	}
	removed := 0
	for _, r := range routes {
		// Наши маршруты (MIB_IPPROTO_NETMGMT) и статические от netsh
		// (10006/10007). Локальные маршруты адаптера не удаляем.
		if r.Protocol != protoNetMgmt && r.Protocol != 10006 && r.Protocol != 10007 {
			continue
		}
		if err := routeDelete(r); err != nil {
			logf(fmt.Sprintf("удалить зависший маршрут %s: %v", r, err))
			continue
		}
		removed++
	}
	if removed > 0 {
		logf(fmt.Sprintf("удалены «зависшие» маршруты прошлой сессии: %d", removed))
	}
	if _, automatic, err := ifaceGetMetric(iface); err == nil && !automatic {
		if err := ifaceSetAutomaticMetric(iface); err == nil {
			logf("метрика интерфейса возвращена в автоматический режим")
		}
	}

	// 9999 — магическая метрика, которой старая версия «поднимала»
	// физический default-маршрут. После краха она остаётся и ломает
	// приоритет маршрутов. Восстанавливаем 0 (штатное значение DHCP-маршрута,
	// к которому добавляется метрика интерфейса).
	repaired, rerr := repairHijackedDefaultMetric(iface.Luid)
	if rerr != nil {
		logf(fmt.Sprintf("проверка метрики default-маршрута: %v", rerr))
	} else if repaired > 0 {
		logf(fmt.Sprintf("восстановлена метрика default-маршрута: %d шт. (было 9999)", repaired))
	}
}

// extractWintun кладёт встроенный wintun.dll рядом с exe и сверяет SHA-256
// существующего файла со встроенным. Раньше любой уже лежащий файл
// использовался без проверки: подмена DLL в каталоге, доступном на запись,
// давала выполнение кода в процессе с правами администратора.
func extractWintun() error {
	if wintunDLLData == nil {
		return fmt.Errorf("wintun.dll not embedded")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(filepath.Dir(exe), "wintun.dll")
	want := sha256.Sum256(wintunDLLData)
	if data, err := os.ReadFile(dst); err == nil {
		if sha256.Sum256(data) == want {
			return nil
		}
		log.Printf("[WG] wintun.dll рядом с exe не совпадает со встроенным — перезаписываю")
	}
	if err := os.WriteFile(dst, wintunDLLData, 0o644); err != nil {
		return err
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		return fmt.Errorf("проверить записанный wintun.dll: %w", err)
	}
	if sha256.Sum256(data) != want {
		return fmt.Errorf("wintun.dll повреждён после записи")
	}
	return nil
}

func uapiConf(wgConf string) (string, error) {
	var sb strings.Builder
	inPeer := false
	for _, line := range strings.Split(wgConf, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "[Interface]" {
			inPeer = false
			continue
		}
		if trimmed == "[Peer]" {
			if inPeer {
				sb.WriteString("\n")
			}
			inPeer = true
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "privatekey":
			h, err := toHex(val)
			if err != nil {
				return "", fmt.Errorf("PrivateKey: %w", err)
			}
			sb.WriteString("private_key=" + h + "\n")
		case "listenport":
			sb.WriteString("listen_port=" + val + "\n")
		case "publickey":
			h, err := toHex(val)
			if err != nil {
				return "", fmt.Errorf("PublicKey: %w", err)
			}
			sb.WriteString("public_key=" + h + "\n")
		case "presharedkey":
			h, err := toHex(val)
			if err != nil {
				return "", fmt.Errorf("PresharedKey: %w", err)
			}
			sb.WriteString("preshared_key=" + h + "\n")
		case "endpoint":
			sb.WriteString("endpoint=" + val + "\n")
		case "allowedips":
			for _, cidr := range strings.Split(val, ",") {
				if c := strings.TrimSpace(cidr); c != "" {
					sb.WriteString("allowed_ip=" + c + "\n")
				}
			}
		case "persistentkeepalive":
			sb.WriteString("persistent_keepalive_interval=" + val + "\n")
		}
	}
	sb.WriteString("\n")
	return sb.String(), nil
}

// toHex конвертирует base64-ключ WireGuard в hex для uapi. Раньше при
// ошибке декода возвращалась исходная строка и IpcSet молча получал мусор —
// теперь ошибка пробрасывается наверх, туннель падает сразу и понятно.
// Значение ключа в ошибку не включается (это секрет).
func toHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("некорректный base64-ключ: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func runCmdWindows(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}



func parseCIDR(cidr string) (ip, mask string, err error) {
	parts := strings.SplitN(cidr, "/", 2)
	if len(parts) != 2 {
		return cidr, "255.255.255.255", nil
	}
	ip = parts[0]
	var prefix int
	if _, e := fmt.Sscanf(parts[1], "%d", &prefix); e != nil || prefix < 0 || prefix > 32 {
		return "", "", fmt.Errorf("invalid prefix %q", parts[1])
	}
	var m uint32
	if prefix > 0 {
		m = ^uint32(0) << (32 - prefix)
	}
	mask = fmt.Sprintf("%d.%d.%d.%d", m>>24, (m>>16)&0xff, (m>>8)&0xff, m&0xff)
	return ip, mask, nil
}

// ═══════════════════════════════════════════════════
// Exported wrappers for testing
// ═══════════════════════════════════════════════════

func ParseWGConfig(conf string) (addr, mtu string, allowedIPs []string, wgConf string) {
	return parseWGConfig(conf)
}

func LocalDNSServers() []string {
	return localDNSServers()
}

func DefaultGatewayLinux() string {
	return defaultGatewayLinux()
}

func UapiConf(wgConf string) (string, error) {
	return uapiConf(wgConf)
}

func ToHex(b64 string) (string, error) {
	return toHex(b64)
}

func ParseCIDR(cidr string) (ip, mask string, err error) {
	return parseCIDR(cidr)
}
