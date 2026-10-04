package backend

// ConnectParams — параметры подключения из frontend.
type ConnectParams struct {
	PeerAddr    string   `json:"peerAddr"`             // адрес:порт сервера
	Password    string   `json:"password"`             // пароль подключения
	Hashes      []string `json:"hashes"`               // хеши VK
	DeviceID    string   `json:"deviceId,omitempty"`   // ID устройства
	Workers     int      `json:"workers,omitempty"`    // воркеры (кратно 9)
	CaptchaMode string   `json:"captchaMode,omitempty"` // auto/rjs/wv
	ObfsMode    string   `json:"obfsMode,omitempty"`   // audio/video
	Fingerprint string   `json:"fingerprint,omitempty"` // chrome/android/ios/safari/firefox
	TurnTCP     bool     `json:"turnTcp,omitempty"`    // использовать TCP транспорт
	DoH         string   `json:"doh,omitempty"`        // DNS режим (пусто = yandex, см. core/dns.go)
	NoDTLS      bool     `json:"notls,omitempty"`      // без DTLS: RTP-obfs напрямую поверх TURN
	Raw         bool     `json:"raw,omitempty"`        // raw-IP без WireGuard (сервер -listen-raw)
}

// ProfileData — данные сервера, хранятся в ~/.config/pwdtt/servers/<name>.json.
type ProfileData struct {
	PeerAddr string   `json:"peer"`      // адрес:порт VPS сервера
	Password string   `json:"password"`  // пароль подключения
	Hashes   []string `json:"hashes"`    // хеши VK-звонков
	Listen   string   `json:"listen"`    // локальный адрес (по умолчанию 127.0.0.1:9000)
	TurnHost string   `json:"turn"`      // переопределение IP TURN
	TurnPort string   `json:"port"`      // переопределение порта TURN
	DeviceID string   `json:"device_id"` // уникальный ID устройства
	TurnTCP  bool     `json:"turn_tcp"`  // использовать TCP транспорт
	Workers  int      `json:"workers,omitempty"` // воркеры (0 = авто), приходят из qwdtt-ссылки

	// Порты прямых режимов: на сервере -listen-direct и -listen-raw живут на
	// ОТДЕЛЬНЫХ портах от DTLS (-listen), поэтому в режимах -notls/-raw порт
	// пира подменяется (как в мобильном клиенте: TunnelService.kt,
	// serverDirectPort/serverRawPort). Пусто = дефолты деплоя 56002/56003.
	DirectPort string `json:"direct_port,omitempty"` // порт -listen-direct (режим -notls)
	RawPort    string `json:"raw_port,omitempty"`    // порт -listen-raw (режим -raw)
}

// AppSettings — настройки приложения, хранятся в ~/.config/pwdtt/config.json.
type AppSettings struct {
	AutoStart    bool   `json:"autoStart"`    // автозапуск при старте системы
	ObfsMode     string `json:"obfsMode"`     // audio/video
	ObfsAccepted bool   `json:"obfsAccepted"` // пользователь принял предупреждение об обфускации
	TurnTCP      bool   `json:"turnTcp"`      // использовать TCP транспорт
	Workers      int    `json:"workers,omitempty"`     // воркеры по умолчанию (0 = авто)
	Fingerprint  string `json:"fingerprint,omitempty"` // chrome/android/ios/safari/firefox
	DoH          string `json:"doh"`                   // DNS для резолва VK: yandex|cloudflare|google|doh-*|custom:IP|doh:URL ("" = yandex)

	// Split tunneling (Этап 3): режим маршрутизации и глобальный список
	// адресов, идущих через VPN в режиме split. Список общий для всех серверов.
	RouteMode   string   `json:"routeMode"`   // RouteModeFull (по умолчанию) | RouteModeSplit
	SplitRoutes []string `json:"splitRoutes"` // IP/CIDR/домены, нормализованы (см. NormalizeSplitEntry)
}
