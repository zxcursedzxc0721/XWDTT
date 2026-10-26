//go:build !windows

package backend

// Заглушки IP Helper API: реальная реализация только для Windows
// (winip_windows.go). На Linux/macOS маршруты ставятся через ip/osascript,
// эти функции не вызываются, но нужны для компиляции общих файлов.

import (
	"fmt"
	"net"
)

var errWinOnly = fmt.Errorf("управление маршрутами через IP Helper API доступно только на Windows")

func ifaceByAlias(string) (ipIface, error)              { return ipIface{}, errWinOnly }
func ifaceSetIPv4Address(ipIface, string) error         { return errWinOnly }
func ifaceDeleteIPv4Address(ipIface, net.IP) error      { return errWinOnly }
func ifaceIPv4Addresses(ipIface) []net.IP               { return nil }
func ifaceHasIPv4(ipIface, net.IP) bool                 { return false }
func ifaceGetMetric(ipIface) (uint32, bool, error)      { return 0, false, errWinOnly }
func ifaceSetMetric(ipIface, uint32) error              { return errWinOnly }
func ifaceSetAutomaticMetric(ipIface) error             { return errWinOnly }
func listIPv4Routes() ([]ipRoute, error)                { return nil, errWinOnly }
func routeAdd(ipRoute) error                            { return errWinOnly }
func routeDelete(ipRoute) error                         { return errWinOnly }
func bestDefaultRoute(uint64) (ipRoute, error)          { return ipRoute{}, errWinOnly }
func routesThroughIface(uint64) ([]ipRoute, error)      { return nil, errWinOnly }
func filterExistingRoutes(ipIface, []ipRoute) []ipRoute { return nil }
func ifaceSetDNS(string, []string) error                { return errWinOnly }
func ifaceClearDNS(string) error                        { return errWinOnly }

// DNS физических адаптеров (dns_phys_windows.go) — только Windows.
func guardPhysicalDNS(func(string))        {}
func restorePhysicalDNS(func(string)) bool { return false }
