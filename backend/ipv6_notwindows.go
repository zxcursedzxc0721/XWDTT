//go:build !windows

package backend

import "fmt"

// Перехват IPv6 реализован только на Windows (netsh): на прочих платформах
// guardIPv6Leak вызывается только из веток applyWindows/ApplyRaw, сюда код
// попадает лишь для компоновки.
func blockIPv6ViaTunnel() error {
	return fmt.Errorf("перехват IPv6 не поддерживается на %s", "этой платформе")
}

func unblockIPv6ViaTunnel() {}
