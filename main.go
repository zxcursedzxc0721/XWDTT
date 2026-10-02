package main

import (
	"os"

	"pwdtt/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
