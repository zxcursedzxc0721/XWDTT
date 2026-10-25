//go:build !windows

package backend

// На Linux/macOS единый экземпляр не блокируется: там нет общего wintun-
// адаптера и метрики default-маршрута, а состояние сессии своё у каждого
// процесса. Реализация для Windows — instance_windows.go.
func acquireInstanceLock() (func(), error) { return func() {}, nil }
