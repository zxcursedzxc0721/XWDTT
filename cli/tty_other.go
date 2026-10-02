//go:build !windows

package cli

// На Linux/macOS ANSI работает в терминале из коробки.
func enableVT() {}
