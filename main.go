package main

import (
	"os"

	"xwdtt/backend"
	"xwdtt/cli"
)

func main() {
	backend.MigrateLegacyConfigDir()
	// Краш прошлой full-сессии мог оставить сеть без DNS/прямого default
	// (dns-backup.json / physdef-backup.json) — возвращаем, пока туннель
	// не поднят.
	backend.RestoreStaleFullMode(func(s string) { println("[xwdtt] " + s) })
	os.Exit(cli.Run(os.Args[1:]))
}
