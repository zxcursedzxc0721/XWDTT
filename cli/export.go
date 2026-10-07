package cli

// Экспорт-обёртки для тестов пакета tests/ (см. tests/help_test.go).
// В проде подкоманды вызываются через Run/dispatchInteractive.

import "xwdtt/backend"

func ServersCmdForTest(st *backend.Store, args []string) int {
	return serversCmd(st, args)
}

func SettingsCmdForTest(st *backend.Store, app *backend.App, args []string) int {
	return settingsCmd(st, app, args)
}

func IpsCmdForTest(st *backend.Store, args []string) int {
	return ipsCmd(st, args)
}
