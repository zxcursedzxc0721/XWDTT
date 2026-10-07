//go:build windows

package main

import (
	_ "embed"

	"xwdtt/backend"
)

//go:embed assets/wintun.dll
var wintunDLL []byte

func init() {
	backend.InitWintun(wintunDLL)
}
