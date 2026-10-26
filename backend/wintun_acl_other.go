//go:build !windows

package backend

// restrictWintunACL — заглушка для не-Windows платформ (см. wintun_acl_windows.go).
func restrictWintunACL(path string) error { return nil }
