//go:build !darwin

package backend

import "fmt"

func (w *WG) applyDarwin(conf string, turnIPs []string, logf wgLogFunc) error {
	return fmt.Errorf("darwin not supported on this platform")
}

func (w *WG) teardownDarwin() {}
