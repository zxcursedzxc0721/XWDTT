package backend

import (
	"context"
)

const Version = "1.7.1"

// App — главный объект приложения (консольная версия, без Wails).
// События Bridge пробрасываются в onEvent — его задаёт CLI
// (печать в stdout с цветами и статистикой).
type App struct {
	bridge  *Bridge
	store   *Store
	onEvent func(name string, args ...any)
	release func() // освобождение блокировки единственного экземпляра
}

// NewApp создаёт App. onEvent — приёмник событий ("log", "state_changed",
// "stats", "error", "captcha_required", "event"); nil означает тихий режим.
func NewApp(onEvent func(name string, args ...any)) *App {
	if onEvent == nil {
		onEvent = func(string, ...any) {}
	}
	return &App{
		store:   NewStore(),
		onEvent: onEvent,
	}
}

// Init поднимает Bridge и выполняет стартовую работу.
// Вызывать только из команд, которым нужен мост (connect, интерактивный режим):
// остальным командам незачем трогать маршруты и лог-файлы.
//
// Возвращает ошибку, если уже работает другой экземпляр XWDTT (два туннеля
// делят один wintun-адаптер и таблицу маршрутов) — второй обязан завершиться.
func (a *App) Init() error {
	release, err := acquireInstanceLock()
	if err != nil {
		return err
	}
	a.release = release

	a.bridge = NewBridge(context.Background(), a.store, a.onEvent)

	// Уборка маршрутов, оставшихся от краша прошлого запуска. Синхронно:
	// горутина могла выполниться уже после подъёма нового туннеля и снести
	// его маршруты.
	CleanupStaleExcludeRoutes(func(msg string) {
		a.onEvent("log", "INFO", "[WG] "+msg)
	})
	return nil
}

// Close отключает туннель и освобождает ресурсы. Вызывать defer'ом после Init.
func (a *App) Close() {
	if a.bridge != nil {
		a.bridge.Disconnect()
	}
	if a.release != nil {
		a.release()
		a.release = nil
	}
}

// Store возвращает хранилище (профили/настройки) — используется CLI напрямую.
func (a *App) Store() *Store { return a.store }

// ═══════════════════════════════════════════════════
// CONNECTION
// ═══════════════════════════════════════════════════

func (a *App) Connect(params ConnectParams) error {
	return a.bridge.Connect(params)
}

func (a *App) Disconnect() {
	a.bridge.Disconnect()
}

func (a *App) Pause() {
	a.bridge.Pause()
}

func (a *App) Resume() {
	a.bridge.Resume()
}

func (a *App) IsRunning() bool {
	return a.bridge.IsRunning()
}

func (a *App) GetVersion() string {
	return Version
}

// ═══════════════════════════════════════════════════
// PROFILES
// ═══════════════════════════════════════════════════

func (a *App) GetProfile(name string) (*ProfileData, error) {
	return a.store.LoadProfile(name)
}

func (a *App) SaveProfile(name string, p ProfileData) error {
	return a.store.SaveProfile(name, p)
}

func (a *App) DeleteProfile(name string) error {
	return a.store.DeleteProfile(name)
}

func (a *App) ListProfiles() map[string]ProfileData {
	return a.store.ListProfiles()
}

// ═══════════════════════════════════════════════════
// SETTINGS
// ═══════════════════════════════════════════════════

func (a *App) GetAutoStart() bool {
	return a.store.LoadSettings().AutoStart
}

func (a *App) SetAutoStart(v bool) error {
	// Сначала применяем, потом сохраняем: иначе при провале schtasks
	// настройка расходилась с фактическим состоянием задачи.
	if err := setAutoStart(v); err != nil {
		return err
	}
	settings := a.store.LoadSettings()
	settings.AutoStart = v
	return a.store.SaveSettings(settings)
}

func (a *App) GetObfsMode() string {
	return a.store.LoadSettings().ObfsMode
}

func (a *App) SetObfsMode(mode string) error {
	settings := a.store.LoadSettings()
	settings.ObfsMode = mode
	return a.store.SaveSettings(settings)
}

func (a *App) GetObfsAccepted() bool {
	return a.store.LoadSettings().ObfsAccepted
}

func (a *App) SetObfsAccepted(v bool) error {
	settings := a.store.LoadSettings()
	settings.ObfsAccepted = v
	return a.store.SaveSettings(settings)
}

func (a *App) CheckUpdate() *UpdateInfo {
	info, err := CheckUpdate(Version)
	if err != nil {
		return &UpdateInfo{Available: false}
	}
	return info
}
