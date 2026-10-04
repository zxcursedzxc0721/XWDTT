package backend_test

import (
	"testing"

	"pwdtt/backend"
	"pwdtt/cli"
)

// ═══════════════════════════════════════════════════
// --help подкоманд → 0, неизвестная подкоманда → 2
// ═══════════════════════════════════════════════════

func TestSubcommandHelpExitCodes(t *testing.T) {
	// Изоляция конфига тестами (могут читать settings)
	t.Setenv("APPDATA", t.TempDir())

	helpArgs := []string{"-h"}
	cases := []struct {
		name string
		run  func() int
	}{
		{"servers", func() int { return cli.ServersCmdForTest(backend.NewStore(), helpArgs) }},
		{"settings", func() int { return cli.SettingsCmdForTest(backend.NewStore(), nil, helpArgs) }},
		{"ips", func() int { return cli.IpsCmdForTest(backend.NewStore(), helpArgs) }},
		{"route", func() int { return cli.Run([]string{"route", "-h"}) }},
		{"ping", func() int { return cli.Run([]string{"ping", "-h"}) }},
	}
	for _, c := range cases {
		if code := c.run(); code != 0 {
			t.Errorf("%s --help: код %d, ожидалось 0", c.name, code)
		}
	}

	// Неизвестная подкоманда → 2 (usage в stderr)
	if code := cli.ServersCmdForTest(backend.NewStore(), []string{"bogus"}); code != 2 {
		t.Errorf("servers bogus: код %d, ожидалось 2", code)
	}
	if code := cli.SettingsCmdForTest(backend.NewStore(), nil, []string{"bogus"}); code != 2 {
		t.Errorf("settings bogus: код %d, ожидалось 2", code)
	}
	if code := cli.IpsCmdForTest(backend.NewStore(), []string{"bogus"}); code != 2 {
		t.Errorf("ips bogus: код %d, ожидалось 2", code)
	}
}
