//go:build windows

package backend

import (
	"encoding/base64"
	"os"
	"os/exec"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func metricConf() string {
	fill := func(b byte) string {
		raw := make([]byte, 32)
		for i := range raw {
			raw[i] = b
		}
		return base64.StdEncoding.EncodeToString(raw)
	}
	return "[Interface]\nAddress = 10.99.0.2/32\nPrivateKey = " + fill(7) +
		"\nMTU = 1280\n\n[Peer]\nPublicKey = " + fill(9) +
		"\nAllowedIPs = 0.0.0.0/0\nEndpoint = 127.0.0.1:9000\n"
}

func setRow(t *testing.T, name string, mutate func(*windows.MibIpInterfaceRow)) {
	t.Helper()
	iface, err := ifaceByAlias("xwdtt-wg")
	if err != nil {
		t.Fatal(err)
	}
	row := windows.MibIpInterfaceRow{Family: windows.AF_INET, InterfaceLuid: iface.Luid}
	if err := windows.GetIpInterfaceEntry(&row); err != nil {
		t.Fatalf("%s: Get: %v", name, err)
	}
	before := row.Metric
	mutate(&row)
	ret, _, _ := procSetIpInterfaceEntry.Call(uintptr(unsafe.Pointer(&row)))
	after, auto, _ := ifaceGetMetric(iface)
	t.Logf("%-40s ret=%d err=%v before=%d after=%d auto=%v size=%d",
		name, ret, windows.Errno(ret), before, after, auto, unsafe.Sizeof(row))
}

func TestLiveMetricDiagnose(t *testing.T) {
	if os.Getenv("XWDTT_LIVE_SPLIT") != "1" {
		t.Skip("set XWDTT_LIVE_SPLIT=1")
	}
	dll, err := os.ReadFile("../assets/wintun.dll")
	if err != nil {
		t.Fatal(err)
	}
	InitWintun(dll)
	policy := RoutePolicy{Mode: RouteModeSplit, SplitCIDRs: []string{"203.0.113.0/24"}}
	if aerr := wg.Apply(metricConf(), nil, policy, func(string) {}); aerr != nil {
		t.Fatalf("Apply: %v", aerr)
	}
	defer wg.Teardown()

	setRow(t, "auto=0, metric=5 (как было)", func(r *windows.MibIpInterfaceRow) {
		r.UseAutomaticMetric = 0
	})
	setRow(t, "auto=1, metric=1", func(r *windows.MibIpInterfaceRow) {
		r.UseAutomaticMetric = 1
		r.Metric = 1
	})
	setRow(t, "auto=0, metric=1", func(r *windows.MibIpInterfaceRow) {
		r.UseAutomaticMetric = 0
		r.Metric = 1
	})
	setRow(t, "auto=0, metric=1, index=0", func(r *windows.MibIpInterfaceRow) {
		r.UseAutomaticMetric = 0
		r.Metric = 1
		r.InterfaceIndex = 0
	})

	// Вариант с InitializeIpInterfaceEntry (как в winipcfg).
	iface, _ := ifaceByAlias("xwdtt-wg")
	var fresh windows.MibIpInterfaceRow
	modIphlpapi.NewProc("InitializeIpInterfaceEntry").Call(uintptr(unsafe.Pointer(&fresh)))
	fresh.Family = windows.AF_INET
	fresh.InterfaceLuid = iface.Luid
	fresh.InterfaceIndex = iface.Index
	fresh.UseAutomaticMetric = 0
	fresh.Metric = 1
	ret, _, _ := procSetIpInterfaceEntry.Call(uintptr(unsafe.Pointer(&fresh)))
	m1, a1, _ := ifaceGetMetric(iface)
	t.Logf("Initialize+Set ret=%d err=%v after=%d auto=%v", ret, windows.Errno(ret), m1, a1)

	// netsh со store=active
	out0, _ := exec.Command("netsh", "interface", "ipv4", "set", "interface", "xwdtt-wg", "metric=7", "store=active").CombinedOutput()
	m2, a2, _ := ifaceGetMetric(iface)
	t.Logf("netsh store=active: out=%q after=%d auto=%v", string(out0), m2, a2)

	// Правильный синтаксис netsh (позиционное имя интерфейса).
	out, _ := exec.Command("netsh", "interface", "ipv4", "set", "interface", "xwdtt-wg", "metric=1").CombinedOutput()
	after, auto, _ := ifaceGetMetric(iface)
	t.Logf("netsh positional metric=1: out=%q after=%d auto=%v", string(out), after, auto)

	// Возврат в авторежим через PowerShell.
	ps, _ := exec.Command("powershell", "-NoProfile", "-Command",
		"Set-NetIPInterface -InterfaceAlias xwdtt-wg -AddressFamily IPv4 -AutomaticMetric Enabled").CombinedOutput()
	m3, a3, _ := ifaceGetMetric(iface)
	t.Logf("ps auto: err=%q after=%d auto=%v", string(ps), m3, a3)
}
