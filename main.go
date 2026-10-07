package main

import (
	"os"

	"xwdtt/backend"
	"xwdtt/cli"
)

func main() {
	backend.MigrateLegacyConfigDir()
	os.Exit(cli.Run(os.Args[1:]))
}
