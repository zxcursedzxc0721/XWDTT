//go:build !windows

package backend

// IsElevated — на Linux/macOS права берутся поштучно (sudo по каждой команде,
// osascript при подключении), поэтому процесс не требует элевации целиком.
func IsElevated() bool { return true }

// ElevateAndExit — на этих платформах ничего перезапускать не нужно.
func ElevateAndExit() error { return nil }
