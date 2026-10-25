//go:build windows

package backend

// Единственный экземпляр для команд, поднимающих туннель (XW-20).
//
// Без блокировки два процесса могли одновременно создавать/закрывать один и
// тот же адаптер xwdtt-wg, менять маршруты и снимать туннель друг друга:
// второй экземпляр при teardown ломал сеть первому.
//
// Используется named mutex: ОС освобождает его автоматически при завершении
// процесса (в т.ч. аварийном), поэтому «зависших» блокировок не бывает.

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const instanceMutexName = `Local\xwdtt-tunnel-singleton`

var instanceMutex windows.Handle

// acquireInstanceLock берёт блокировку единственного экземпляра.
// Возвращает функцию освобождения.
func acquireInstanceLock() (func(), error) {
	name, err := windows.UTF16PtrFromString(instanceMutexName)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if err == windows.ERROR_ALREADY_EXISTS {
			return nil, fmt.Errorf("уже запущен другой экземпляр XWDTT (туннель может быть только один)")
		}
		return nil, fmt.Errorf("CreateMutex: %w", err)
	}
	// CreateMutex возвращает ERROR_ALREADY_EXISTS, если объект существовал.
	// Проверяем фактическое владение: WaitForSingleObject с нулевым таймаутом.
	wait, werr := windows.WaitForSingleObject(h, 0)
	if werr != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("ожидание мьютекса: %w", werr)
	}
	if wait != windows.WAIT_OBJECT_0 {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("уже запущен другой экземпляр XWDTT (туннель может быть только один)")
	}
	instanceMutex = h
	return func() {
		if instanceMutex != 0 {
			_ = windows.ReleaseMutex(instanceMutex)
			windows.CloseHandle(instanceMutex)
			instanceMutex = 0
		}
	}, nil
}
