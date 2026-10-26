//go:build windows

package backend

import (
	"fmt"
	"os/exec"
	"strings"
)

// blockIPv6ViaTunnel уводит ::/0 через туннель-интерфейс. Пакеты попадают в
// wintun, где WireGuard их отбрасывает (IPv6 не входит в allowedips) — вместо
// утечки наружу получаем «чёрную дыру». store=active: маршрут живёт до
// перезагрузки и не залипает в постоянном хранилище.
func blockIPv6ViaTunnel() error {
	// Метрика 1 и на интерфейсе, и на маршруте: иначе провайдерский ::/0
	// может победить по сумме метрик и утечка сохранится.
	_ = runCmdWindows("netsh", "interface", "ipv6", "set", "interface",
		"interface="+wgIface, "metric=1", "store=active")
	if err := runCmdWindows("netsh", "interface", "ipv6", "add", "route",
		"prefix=::/0", "interface="+wgIface, "metric=1", "store=active"); err != nil {
		return err
	}
	// netsh на части ошибок возвращает код 0 — проверяем, что маршрут реально
	// появился (строка вывода содержит и ::/0, и имя интерфейса).
	out, err := netshCombinedOutput("interface", "ipv6", "show", "route")
	if err != nil {
		return fmt.Errorf("проверка маршрута ::/0: %w", err)
	}
	if !routeLineExists(out, "::/0", wgIface) {
		return fmt.Errorf("маршрут ::/0 через %s не появился", wgIface)
	}
	return nil
}

// unblockIPv6ViaTunnel снимает свой ::/0 при остановке туннеля (best-effort:
// маршрут пропадает и вместе с удалением интерфейса).
func unblockIPv6ViaTunnel() {
	_ = runCmdWindows("netsh", "interface", "ipv6", "delete", "route",
		"prefix=::/0", "interface="+wgIface, "store=active")
}

func netshCombinedOutput(args ...string) (string, error) {
	cmd := exec.Command("netsh", args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// routeLineExists ищет строку, содержащую и префикс, и имя интерфейса
// (заголовки в выводе netsh локализованы, но имена и префиксы — ASCII).
func routeLineExists(out, prefix, ifaceName string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, prefix) && strings.Contains(line, ifaceName) {
			return true
		}
	}
	return false
}
