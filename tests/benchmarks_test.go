package backend_test

import (
	"strings"
	"testing"

	"pwdtt/backend"
)

// ═══════════════════════════════════════════════════
// parseWGConfig
// ═══════════════════════════════════════════════════

const sampleWGConfig = `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.0.0.2/32
DNS = 1.1.1.1
MTU = 1280
PreUp = echo up
PostUp = echo postup

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
Endpoint = 1.2.3.4:51820
AllowedIPs = 0.0.0.0/0, ::/0, 192.168.0.0/16
PersistentKeepalive = 25
`

func BenchmarkParseWGConfig(b *testing.B) {
	for b.Loop() {
		backend.ParseWGConfig(sampleWGConfig)
	}
}

func BenchmarkParseWGConfig_Minimal(b *testing.B) {
	minimal := `[Interface]
Address = 10.0.0.1/32
[Peer]
PublicKey = abc
Endpoint = 1.2.3.4:51820
AllowedIPs = 0.0.0.0/0
`
	for b.Loop() {
		backend.ParseWGConfig(minimal)
	}
}

func BenchmarkParseWGConfig_LargeAllowedIPs(b *testing.B) {
	cidrs := make([]string, 50)
	for i := range cidrs {
		cidrs[i] = "10.0.0.0/8"
	}
	large := `[Interface]
Address = 10.0.0.1/32
[Peer]
PublicKey = abc
Endpoint = 1.2.3.4:51820
AllowedIPs = ` + strings.Join(cidrs, ", ") + `
`
	for b.Loop() {
		backend.ParseWGConfig(large)
	}
}

// ═══════════════════════════════════════════════════
// parseCIDR
// ═══════════════════════════════════════════════════

func BenchmarkParseCIDR(b *testing.B) {
	for b.Loop() {
		backend.ParseCIDR("192.168.1.0/24")
	}
}

func BenchmarkParseCIDR_NoPrefix(b *testing.B) {
	for b.Loop() {
		backend.ParseCIDR("10.0.0.1")
	}
}

// ═══════════════════════════════════════════════════
// uapiConf
// ═══════════════════════════════════════════════════

func BenchmarkUapiConf(b *testing.B) {
	wgConf := `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
ListenPort = 51820
[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
Endpoint = 1.2.3.4:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`
	for b.Loop() {
		backend.UapiConf(wgConf)
	}
}

// ═══════════════════════════════════════════════════
// toHex
// ═══════════════════════════════════════════════════

func BenchmarkToHex(b *testing.B) {
	for b.Loop() {
		backend.ToHex("yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=")
	}
}

func BenchmarkToHex_Invalid(b *testing.B) {
	for b.Loop() {
		backend.ToHex("not-base64!!")
	}
}
