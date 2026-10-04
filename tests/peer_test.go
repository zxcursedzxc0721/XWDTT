package backend_test

// Регрессия: в режимах -notls/-raw пакет GETCONF/GETCONF_RAW обязан уходить
// на порт своего листенера сервера (56002/56003), а не на DTLS-порт 56000 —
// иначе сервер молча отбрасывает пакет и конфиг не приходит вовсе
// («туннель готов, но IP не меняется»).

import (
	"testing"

	"pwdtt/cli"
)

func TestPeerAddrForMode_DTLSUnchanged(t *testing.T) {
	got := cli.PeerAddrForMode("1.2.3.4:56000", false, false, "", "")
	if got != "1.2.3.4:56000" {
		t.Errorf("обычный режим должен сохранять порт профиля: got %q", got)
	}
}

func TestPeerAddrForMode_NotlsDefaultsTo56002(t *testing.T) {
	got := cli.PeerAddrForMode("1.2.3.4:56000", true, false, "", "")
	if got != "1.2.3.4:56002" {
		t.Errorf("-notls: got %q, want 1.2.3.4:56002", got)
	}
}

func TestPeerAddrForMode_RawDefaultsTo56003(t *testing.T) {
	got := cli.PeerAddrForMode("1.2.3.4:56000", false, true, "", "")
	if got != "1.2.3.4:56003" {
		t.Errorf("-raw: got %q, want 1.2.3.4:56003", got)
	}
}

func TestPeerAddrForMode_ProfilePortsOverride(t *testing.T) {
	got := cli.PeerAddrForMode("1.2.3.4:56000", true, false, "7000", "7001")
	if got != "1.2.3.4:7000" {
		t.Errorf("-notls с портом профиля: got %q, want 1.2.3.4:7000", got)
	}
	got = cli.PeerAddrForMode("1.2.3.4:56000", true, true, "7000", "7001")
	if got != "1.2.3.4:7001" {
		t.Errorf("-raw с приоритетом над -notls: got %q, want 1.2.3.4:7001", got)
	}
}

func TestPeerAddrForMode_NoPortInPeer(t *testing.T) {
	got := cli.PeerAddrForMode("1.2.3.4", true, false, "", "")
	if got != "1.2.3.4:56002" {
		t.Errorf("peer без порта: got %q, want 1.2.3.4:56002", got)
	}
}
