//go:build windows

package backend

import (
	"path/filepath"
	"strings"
	"testing"
)

// Честный вызов IP Helper API: без прав администратора читать можно.
func TestListPhysicalDNSExcluding(t *testing.T) {
	entries, err := listPhysicalDNSExcluding(wgIface)
	if err != nil {
		t.Fatalf("listPhysicalDNSExcluding: %v", err)
	}
	for _, e := range entries {
		if e.Alias == "" {
			t.Errorf("пустой alias у записи %+v", e)
		}
		if strings.EqualFold(e.Alias, wgIface) {
			t.Errorf("туннельный адаптер не должен попадать в бэкап: %q", e.Alias)
		}
		if !e.DHCP && len(e.Servers) == 0 {
			t.Errorf("бессмысленная запись %+v", e)
		}
		for _, s := range e.Servers {
			if s == "" {
				t.Errorf("пустой DNS-сервер в %+v", e)
			}
		}
	}
	t.Logf("адаптеров с DNS: %d", len(entries))
}

func TestDnsBackupPathIsInConfigDir(t *testing.T) {
	p := dnsBackupPath()
	if !filepath.IsAbs(p) || filepath.Base(p) != "dns-backup.json" {
		t.Errorf("неожиданный путь бэкапа: %q", p)
	}
	if !strings.Contains(strings.ToLower(p), "xwdtt") {
		t.Errorf("бэкап вне каталога xwdtt: %q", p)
	}
}
