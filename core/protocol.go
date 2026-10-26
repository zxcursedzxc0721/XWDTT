package core

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

func RequestConfig(conn net.Conn, localPort, deviceID, password string) (string, error) {
	payload := fmt.Sprintf("GETCONF:%s|%s|%s", localPort, deviceID, password)
	if _, err := conn.Write([]byte(payload)); err != nil {
		return "", fmt.Errorf("отправка GETCONF: %w", err)
	}

	// Ответ конфига — один датаграммный Read: буфер должен быть на
	// максимальный размер UDP/DTLS-записи (64 КБ), иначе длинный
	// конфиг молча обрезается на 4096 и устройство получает кривой key.
	b := make([]byte, 65536)
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return "", fmt.Errorf("установка дедлайна: %w", err)
	}
	n, err := conn.Read(b)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return "", fmt.Errorf("чтение ответа конфига: %w", err)
	}

	resp := string(b[:n])
	if resp == "NOCONF" {
		return "", nil
	}

	if strings.HasPrefix(resp, "DENIED:") {
		reason := strings.TrimPrefix(resp, "DENIED:")
		switch reason {
		case "wrong_password":
			return "", fmt.Errorf("FATAL_AUTH: неверный пароль подключения")
		case "expired":
			return "", fmt.Errorf("FATAL_AUTH: срок действия пароля истёк")
		case "device_mismatch":
			return "", fmt.Errorf("FATAL_AUTH: пароль привязан к другому устройству")
		default:
			return "", fmt.Errorf("FATAL_AUTH: доступ запрещён (%s)", reason)
		}
	}

	// Валидация формата (п.4 анализа): ответ должен выглядеть как
	// WireGuard-конфиг ([Interface]/[Peer] или key=value с известным ключом).
	// Мусорный или неизвестный ответ (повреждение, несовместимая версия
	// протокола) раньше принимался как конфиг и «тихо» становился пустым
	// uapiConf — туннель поднимался без адреса и падал уже в applyWindows
	// с менее понятной ошибкой.
	if !looksLikeWGConfig(resp) {
		snippet := resp
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		return "", fmt.Errorf("неожиданный ответ GETCONF (не похож на WireGuard-конфиг): %q", snippet)
	}

	return resp, nil
}

// looksLikeWGConfig — похож ли ответ сервера на INI-конфиг WireGuard.
// Достаточно одной узнаваемой секции или ключа (регистр и пробелы не важны).
func looksLikeWGConfig(resp string) bool {
	const interfaceSec, peerSec = "[Interface]", "[Peer]"
	known := map[string]bool{
		"address": true, "privatekey": true, "publickey": true,
		"presharedkey": true, "dns": true, "mtu": true,
		"allowedips": true, "endpoint": true, "listenport": true,
		"persistentkeepalive": true,
	}
	for _, line := range strings.Split(resp, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if t == interfaceSec || t == peerSec {
			return true
		}
		idx := strings.IndexByte(t, '=')
		if idx <= 0 {
			continue
		}
		if known[strings.ToLower(strings.TrimSpace(t[:idx]))] {
			return true
		}
	}
	return false
}

// SendAuth авторизует рабочую DTLS-сессию, которая не запрашивает WireGuard-конфиг.
func SendAuth(conn net.Conn, deviceID, password string) error {
	payload := fmt.Sprintf("AUTH:%s|%s", deviceID, password)
	if _, err := conn.Write([]byte(payload)); err != nil {
		return fmt.Errorf("отправка AUTH: %w", err)
	}
	return nil
}

// RequestRawConfig запрашивает у сервера конфигурацию для raw-IP режима
// (без WireGuard) — сервер отвечает "RAWCONF:ip|dns|mtu" (см. server/raw.go
// handleConnRaw). ip пусто на первый вызов, если сервер ещё не назначил его.
// Перенос из proxy-turn-vk-android/go_client/protocol.go.
func RequestRawConfig(conn net.Conn, deviceID, password string) (ip, dnsCSV string, mtu int, err error) {
	payload := fmt.Sprintf("GETCONF_RAW:%s|%s", deviceID, password)
	if _, err = conn.Write([]byte(payload)); err != nil {
		return "", "", 0, fmt.Errorf("отправка GETCONF_RAW: %w", err)
	}

	b := make([]byte, 65536)
	if err = conn.SetReadDeadline(time.Now().Add(45 * time.Second)); err != nil {
		return "", "", 0, fmt.Errorf("установка дедлайна: %w", err)
	}
	n, readErr := conn.Read(b)
	_ = conn.SetReadDeadline(time.Time{})
	if readErr != nil {
		return "", "", 0, fmt.Errorf("чтение ответа RAWCONF: %w", readErr)
	}

	resp := string(b[:n])
	if resp == "NOCONF" {
		return "", "", 0, nil
	}
	if strings.HasPrefix(resp, "DENIED:") {
		reason := strings.TrimPrefix(resp, "DENIED:")
		switch reason {
		case "wrong_password":
			return "", "", 0, fmt.Errorf("FATAL_AUTH: неверный пароль подключения")
		case "expired":
			return "", "", 0, fmt.Errorf("FATAL_AUTH: срок действия пароля истёк")
		case "device_mismatch":
			return "", "", 0, fmt.Errorf("FATAL_AUTH: пароль привязан к другому устройству")
		default:
			return "", "", 0, fmt.Errorf("FATAL_AUTH: доступ запрещён (%s)", reason)
		}
	}
	if !strings.HasPrefix(resp, "RAWCONF:") {
		return "", "", 0, fmt.Errorf("неожиданный ответ RAWCONF: %q", resp)
	}

	parts := strings.Split(strings.TrimPrefix(resp, "RAWCONF:"), "|")
	if len(parts) != 3 {
		return "", "", 0, fmt.Errorf("некорректный формат RAWCONF: %q", resp)
	}
	// Валидация полей: раньше принималось всё подряд, и мусорный ip/dns
	// уходил в TUN (netsh падал уже там, с менее понятной ошибкой),
	// а mtu=0/отрицательный ломал интерфейс.
	ipVal := strings.TrimSpace(parts[0])
	dnsVal := strings.TrimSpace(parts[1])
	if ipVal != "" && net.ParseIP(ipVal) == nil {
		return "", "", 0, fmt.Errorf("некорректный IP в RAWCONF: %q", parts[0])
	}
	for _, resolver := range strings.Split(dnsVal, ",") {
		resolver = strings.TrimSpace(resolver)
		if resolver == "" {
			continue
		}
		if net.ParseIP(resolver) == nil {
			return "", "", 0, fmt.Errorf("некорректный DNS в RAWCONF: %q", parts[1])
		}
	}
	mtuVal, convErr := strconv.Atoi(strings.TrimSpace(parts[2]))
	if convErr != nil {
		return "", "", 0, fmt.Errorf("некорректный MTU в RAWCONF: %q", parts[2])
	}
	if mtuVal < 68 || mtuVal > 65535 {
		return "", "", 0, fmt.Errorf("MTU вне диапазона 68..65535 в RAWCONF: %q", parts[2])
	}
	// Кламп до 1500: приёмные буферы даунлинка рассчитаны на кадры ~1600-2128
	// байт; серверный MTU 65535 давал молча обрезанные кадры → AEAD auth fail
	// → тихий дроп всего даунлинка.
	if mtuVal > 1500 {
		mtuVal = 1500
	}
	return ipVal, dnsVal, mtuVal, nil
}
