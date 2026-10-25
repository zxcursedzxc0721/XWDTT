package cli

// Выбор порта пира по режиму подключения.
//
// На сервере три независимых листенера (server/main.go):
//
//	-listen       56000 — DTLS (классический режим)
//	-listen-direct 56002 — RTP-obfs без DTLS (флаг -notls)
//	-listen-raw     56003 — raw-IP без WireGuard (флаг -raw)
//
// Клиент обязан слать GETCONF/GETCONF_RAW на СВОЙ порт: пакет, пришедший на
// DTLS-порт без DTLS-hs, сервер молча отбрасывает → таймаут конфига и
// «туннель готов, но IP не меняется». Тот же приём использует мобильный
// клиент (TunnelService.kt: effectiveServerPort = raw→serverRawPort,
// notls→serverDirectPort, иначе serverDtlsPort).

import (
	"net"
	"strings"
)

// Дефолтные порты прямых режимов — совпадают с дефолтами деплоя сервера
// (app/src/main/assets/deploy.sh: WDTT_DIRECT_PORT/WDTT_RAW_PORT) и
// мобильного клиента (SettingsStore.kt: 56002/56003).
const (
	defaultDTLSPort   = "56000"
	defaultDirectPort = "56002"
	defaultRawPort    = "56003"
)

// PeerAddrForMode возвращает адрес пира с портом, соответствующим режиму
// подключения: обычный режим — порт профиля (DTLS), -notls — direct-порт,
// -raw — raw-порт. directPort/rawPort из профиля; пусто = дефолт.
func PeerAddrForMode(peer string, noTLS, raw bool, directPort, rawPort string) string {
	trimmed := strings.TrimSpace(peer)
	host, _, err := net.SplitHostPort(trimmed)

	if !noTLS && !raw {
		if err == nil {
			return trimmed
		}
		// Легаси-профили могли сохраниться как «1.2.3.4» без порта — ядро
		// такой адрес не резолвит («missing port in address»), а DTLS-
		// листенер по умолчанию 56000.
		if trimmed == "" {
			return trimmed
		}
		return net.JoinHostPort(trimmed, defaultDTLSPort)
	}

	port := strings.TrimSpace(directPort)
	if raw {
		port = strings.TrimSpace(rawPort)
	}
	if port == "" {
		if raw {
			port = defaultRawPort
		} else {
			port = defaultDirectPort
		}
	}

	if trimmed == "" {
		return trimmed
	}
	// Без порта — считаем всю строку хостом (peer может быть «1.2.3.4»).
	if err != nil {
		host = trimmed
	}
	if host == "" {
		return trimmed
	}
	return net.JoinHostPort(host, port)
}

// peerPortOr — порт из профиля, а если пуст — дефолт (для вывода).
func peerPortOr(port, def string) string {
	if port = strings.TrimSpace(port); port != "" {
		return port
	}
	return def
}
