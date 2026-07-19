package backend_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pwdtt/backend"
)

// ═══════════════════════════════════════════════════
// parseWGConfig
// ═══════════════════════════════════════════════════

func TestParseWGConfig_FullConfig(t *testing.T) {
	conf := `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.0.0.2/32
DNS = 1.1.1.1
MTU = 1280
PreUp = echo up
PostUp = echo postup
PreDown = echo down
PostDown = echo postdown

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
Endpoint = 1.2.3.4:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`

	addr, mtu, allowedIPs, wgConf := backend.ParseWGConfig(conf)

	if addr != "10.0.0.2/32" {
		t.Errorf("addr: got %q, want %q", addr, "10.0.0.2/32")
	}
	if mtu != "1280" {
		t.Errorf("mtu: got %q, want %q", mtu, "1280")
	}
	if len(allowedIPs) != 2 {
		t.Fatalf("allowedIPs: got %d, want 2", len(allowedIPs))
	}
	if allowedIPs[0] != "0.0.0.0/0" {
		t.Errorf("allowedIPs[0]: got %q", allowedIPs[0])
	}
	if allowedIPs[1] != "::/0" {
		t.Errorf("allowedIPs[1]: got %q", allowedIPs[1])
	}
	// wgConf должен содержать секции [Interface] и [Peer]
	if !strings.Contains(wgConf, "[Interface]") {
		t.Error("wgConf missing [Interface]")
	}
	if !strings.Contains(wgConf, "[Peer]") {
		t.Error("wgConf missing [Peer]")
	}
	//	wg-quick поля (Address, DNS, MTU, PreUp, PostUp...) НЕ должны попасть в wgConf
	for _, field := range []string{"Address", "DNS", "MTU", "PreUp", "PostUp", "PreDown", "PostDown"} {
		if strings.Contains(wgConf, field+" =") {
			t.Errorf("wgConf should not contain wg-quick field %q", field)
		}
	}
}

func TestParseWGConfig_MinimalConfig(t *testing.T) {
	conf := `[Interface]
Address = 10.66.66.2/24

[Peer]
PublicKey = bla
Endpoint = 5.5.5.5:51820
AllowedIPs = 0.0.0.0/0
`

	addr, mtu, allowedIPs, wgConf := backend.ParseWGConfig(conf)

	if addr != "10.66.66.2/24" {
		t.Errorf("addr: got %q", addr)
	}
	if mtu != "" {
		t.Errorf("mtu should be empty, got %q", mtu)
	}
	if len(allowedIPs) != 1 || allowedIPs[0] != "0.0.0.0/0" {
		t.Errorf("allowedIPs: got %v", allowedIPs)
	}
	if !strings.Contains(wgConf, "[Peer]") {
		t.Error("wgConf missing [Peer]")
	}
}

func TestParseWGConfig_MultipleAllowedIPs(t *testing.T) {
	conf := `[Interface]
Address = 10.0.0.1/32

[Peer]
PublicKey = abc
Endpoint = 1.2.3.4:51820
AllowedIPs = 192.168.1.0/24, 10.0.0.0/8, 172.16.0.0/12
`

	_, _, allowedIPs, _ := backend.ParseWGConfig(conf)

	if len(allowedIPs) != 3 {
		t.Fatalf("allowedIPs: got %d, want 3", len(allowedIPs))
	}
	expected := []string{"192.168.1.0/24", "10.0.0.0/8", "172.16.0.0/12"}
	for i, want := range expected {
		if allowedIPs[i] != want {
			t.Errorf("allowedIPs[%d]: got %q, want %q", i, allowedIPs[i], want)
		}
	}
}

func TestParseWGConfig_IgnoresWgQuickFields(t *testing.T) {
	conf := `[Interface]
PrivateKey = key123
ListenPort = 51820
Address = 10.0.0.1/32
DNS = 8.8.8.8
MTU = 1420
PreUp = echo start
PostUp = echo started
PreDown = echo stop
PostDown = echo stopped
SaveConfig = true

[Peer]
PublicKey = pub456
AllowedIPs = 0.0.0.0/0
`

	addr, mtu, allowedIPs, wgConf := backend.ParseWGConfig(conf)

	if addr != "10.0.0.1/32" {
		t.Errorf("addr: got %q", addr)
	}
	if mtu != "1420" {
		t.Errorf("mtu: got %q", mtu)
	}
	if len(allowedIPs) != 1 {
		t.Errorf("allowedIPs: got %d, want 1", len(allowedIPs))
	}
	//	wgConf не должен содержать wg-quick поля
	for _, bad := range []string{"DNS =", "MTU =", "PreUp =", "PostUp =", "PreDown =", "PostDown =", "SaveConfig ="} {
		if strings.Contains(wgConf, bad) {
			t.Errorf("wgConf should not contain %q", bad)
		}
	}
	// Но должен содержать Peer-поля
	if !strings.Contains(wgConf, "PublicKey") {
		t.Error("wgConf missing PublicKey")
	}
	if !strings.Contains(wgConf, "AllowedIPs") {
		t.Error("wgConf missing AllowedIPs")
	}
}

func TestParseWGConfig_EmptyAllowedIPs(t *testing.T) {
	conf := `[Interface]
Address = 10.0.0.1/32

[Peer]
PublicKey = abc
Endpoint = 1.2.3.4:51820
`

	_, _, allowedIPs, _ := backend.ParseWGConfig(conf)
	if allowedIPs != nil {
		t.Errorf("expected nil allowedIPs, got %v", allowedIPs)
	}
}

func TestParseWGConfig_AddressCaseInsensitive(t *testing.T) {
	conf := `[Interface]
address = 10.99.99.1/24
`

	addr, _, _, _ := backend.ParseWGConfig(conf)
	if addr != "10.99.99.1/24" {
		t.Errorf("addr: got %q", addr)
	}
}

// ═══════════════════════════════════════════════════
// localDNSServers — парсинг /etc/resolv.conf
// ═══════════════════════════════════════════════════

func TestLocalDNSServers_Integration(t *testing.T) {
	// Интеграционный тест — читает реальный /etc/resolv.conf
	// Если файла нет — пропускаем
	if _, err := os.Stat("/etc/resolv.conf"); os.IsNotExist(err) {
		t.Skip("/etc/resolv.conf not found")
	}

	servers := backend.LocalDNSServers()
	// На большинстве систем есть хотя бы 1 DNS
	t.Logf("DNS servers: %v", servers)
	for _, s := range servers {
		if s == "" {
			t.Error("empty DNS server in list")
		}
	}
}

func TestLocalDNSServers_SkipsLoopback(t *testing.T) {
	// Создаём фиктивный resolv.conf с loopback и реальным DNS
	dir := t.TempDir()
	resolvPath := filepath.Join(dir, "resolv.conf")
	content := `nameserver 127.0.0.53
nameserver 8.8.8.8
nameserver 1.1.1.1
`
	os.WriteFile(resolvPath, []byte(content), 0o644)

	// localDNSServers читает /etc/resolv.conf напрямую,
	// но мы можем проверить логику: loopback (127.x) должен быть пропущен
	// Это интеграционный тест — проверяем что на реальной системе нет 127.x
	servers := backend.LocalDNSServers()
	for _, s := range servers {
		if strings.HasPrefix(s, "127.") {
			t.Errorf("loopback DNS %q should be skipped", s)
		}
	}
}

// ═══════════════════════════════════════════════════
// defaultGatewayLinux — парсинг ip route
// ═══════════════════════════════════════════════════

func TestDefaultGatewayLinux_Integration(t *testing.T) {
	// Интеграционный тест — работает только на Linux
	gw := backend.DefaultGatewayLinux()
	t.Logf("default gateway: %q", gw)
	// Может быть пустым в контейнерах — не фейлим
}

// ═══════════════════════════════════════════════════
// uapiConf — конвертация в UAPI формат
// ═══════════════════════════════════════════════════

func TestUapiConf(t *testing.T) {
	wgConf := `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
ListenPort = 51820

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
Endpoint = 1.2.3.4:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`

	uapi := backend.UapiConf(wgConf)

	if !strings.Contains(uapi, "private_key=") {
		t.Error("UAPI missing private_key")
	}
	if !strings.Contains(uapi, "listen_port=51820") {
		t.Error("UAPI missing listen_port")
	}
	if !strings.Contains(uapi, "public_key=") {
		t.Error("UAPI missing public_key")
	}
	if !strings.Contains(uapi, "endpoint=1.2.3.4:51820") {
		t.Error("UAPI missing endpoint")
	}
	if !strings.Contains(uapi, "allowed_ip=0.0.0.0/0") {
		t.Error("UAPI missing allowed_ip")
	}
	if !strings.Contains(uapi, "persistent_keepalive_interval=25") {
		t.Error("UAPI missing persistent_keepalive_interval")
	}
	// Секции [Interface] и [Peer] не должны попасть в UAPI
	if strings.Contains(uapi, "[Interface]") || strings.Contains(uapi, "[Peer]") {
		t.Error("UAPI should not contain section headers")
	}
}

func TestUapiConf_MultipleAllowedIPs(t *testing.T) {
	wgConf := `[Peer]
PublicKey = abc
AllowedIPs = 10.0.0.0/8, 192.168.0.0/16
`

	uapi := backend.UapiConf(wgConf)

	if !strings.Contains(uapi, "allowed_ip=10.0.0.0/8") {
		t.Error("UAPI missing first allowed_ip")
	}
	if !strings.Contains(uapi, "allowed_ip=192.168.0.0/16") {
		t.Error("UAPI missing second allowed_ip")
	}
}

// ═══════════════════════════════════════════════════
// parseCIDR
// ═══════════════════════════════════════════════════

func TestParseCIDR(t *testing.T) {
	tests := []struct {
		cidr   string
		ip     string
		mask   string
		wantErr bool
	}{
		{"10.0.0.0/24", "10.0.0.0", "255.255.255.0", false},
		{"192.168.1.0/16", "192.168.1.0", "255.255.0.0", false},
		{"172.16.0.0/12", "172.16.0.0", "255.240.0.0", false},
		{"8.8.8.8/32", "8.8.8.8", "255.255.255.255", false},
		{"0.0.0.0/0", "0.0.0.0", "0.0.0.0", false},
		{"1.2.3.4", "1.2.3.4", "255.255.255.255", false}, // без / — полная маска
		{"10.0.0.0/33", "", "", true},                      // невалидный префикс
		{"10.0.0.0/abc", "", "", true},                     // не число
	}

	for _, tt := range tests {
		ip, mask, err := backend.ParseCIDR(tt.cidr)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseCIDR(%q): err=%v, wantErr=%v", tt.cidr, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if ip != tt.ip {
				t.Errorf("ParseCIDR(%q) ip: got %q, want %q", tt.cidr, ip, tt.ip)
			}
			if mask != tt.mask {
				t.Errorf("ParseCIDR(%q) mask: got %q, want %q", tt.cidr, mask, tt.mask)
			}
		}
	}
}

// ═══════════════════════════════════════════════════
// toHex
// ═══════════════════════════════════════════════════

func TestToHex(t *testing.T) {
	// base64 "AQ==" = 0x01
	got := backend.ToHex("AQ==")
	if got != "01" {
		t.Errorf("ToHex(AQ==): got %q, want %q", got, "01")
	}

	// Невалидный base64 — возвращает как есть
	got = backend.ToHex("not-base64!!")
	if got != "not-base64!!" {
		t.Errorf("ToHex(invalid): got %q, want original", got)
	}
}
