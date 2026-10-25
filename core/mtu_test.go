package core

// XW-14: patchMTU не должен добавлять второй MTU, если ключ уже есть в любом
// написании.

import (
	"strings"
	"testing"
)

func countMTU(conf string) int {
	n := 0
	for _, line := range strings.Split(conf, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "mtu") {
			n++
		}
	}
	return n
}

func TestPatchMTU_NoDuplicate(t *testing.T) {
	for _, conf := range []string{
		"[Interface]\nMTU = 1280\nPrivateKey = x\n[Peer]\n",
		"[Interface]\nMTU=1400\nPrivateKey = x\n[Peer]\n",
		"[Interface]\nmtu =1400\nPrivateKey = x\n[Peer]\n",
		"[Interface]\n  MTU\t=\t1420  \nPrivateKey = x\n[Peer]\n",
	} {
		got := patchMTU(conf)
		if got != conf {
			t.Errorf("конфиг изменён, хотя MTU уже был:\n%s\n--- стало ---\n%s", conf, got)
		}
		if n := countMTU(got); n != 1 {
			t.Errorf("MTU встречается %d раз, ожидался 1:\n%s", n, got)
		}
	}
}

func TestPatchMTU_Adds(t *testing.T) {
	conf := "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\n"
	got := patchMTU(conf)
	if n := countMTU(got); n != 1 {
		t.Errorf("MTU не добавлен (найдено %d):\n%s", n, got)
	}
	if !strings.Contains(got, "MTU = 1280") {
		t.Errorf("ожидался MTU = 1280:\n%s", got)
	}
}
