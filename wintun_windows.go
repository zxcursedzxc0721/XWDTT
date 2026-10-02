//go:build windows

package main

import (
	_ "embed"

	"pwdtt/backend"
)

//go:embed assets/wintun.dll
var wintunDLL []byte

func init() {
	backend.InitWintun(wintunDLL)
}
