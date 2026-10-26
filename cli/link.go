package cli

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"xwdtt/backend"
)

// WdttLink — разобранная ссылка на сервер (wdtt:// или qwdtt://config?...).
type WdttLink struct {
	Name     string
	Host     string   // ip:dtlsPort
	Password string
	Hashes   []string
	Workers  int
	Port     string // переопределение порта TURN (только qwdtt)
}

// isWdttLink — похоже ли значение на ссылку-импорт.
func isWdttLink(s string) bool {
	return strings.HasPrefix(s, "wdtt://") || strings.HasPrefix(s, "qwdtt://")
}

// ParseWdttLink разбирает оба формата ссылок (порт из wdttLink.ts GUI).
//
//	qwdtt://config?name=&peer=&pass=&hashes=&workers=&port=
//	wdtt://IP:ДТЛС:WG:ПРОКСИ:ПАРОЛЬ[:ХЕШИ][#название]
func ParseWdttLink(raw string) (*WdttLink, bool) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "qwdtt://config?") {
		return parseQwdtt(s)
	}
	if strings.HasPrefix(s, "wdtt://") {
		return parseLegacy(s)
	}
	return nil, false
}

func parseQwdtt(raw string) (*WdttLink, bool) {
	q, err := url.ParseQuery(raw[len("qwdtt://config?"):])
	if err != nil {
		return nil, false
	}
	link := &WdttLink{
		Name:     q.Get("name"),
		Host:     q.Get("peer"),
		Password: q.Get("pass"),
		Port:     q.Get("port"),
	}
	if link.Name == "" {
		link.Name = "Server"
	}
	for _, h := range strings.Split(q.Get("hashes"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			link.Hashes = append(link.Hashes, h)
		}
	}
	if w := q.Get("workers"); w != "" {
		link.Workers, _ = strconv.Atoi(w)
	}
	if link.Host == "" || link.Password == "" {
		return nil, false
	}
	return link, true
}

func parseLegacy(raw string) (*WdttLink, bool) {
	parts := strings.Split(strings.TrimPrefix(raw, "wdtt://"), ":")
	if len(parts) < 5 {
		return nil, false
	}
	ip, dtlsPort := parts[0], parts[1]
	tail := strings.Join(parts[4:], ":") // ПАРОЛЬ[:ХЕШИ][#название]

	name := "Server"
	if idx := strings.LastIndex(tail, "#"); idx != -1 {
		if cand := strings.TrimSpace(tail[idx+1:]); cand != "" {
			name = cand
		}
		tail = tail[:idx]
	}

	password := tail
	var hashes []string
	if cidx := strings.LastIndex(tail, ":"); cidx != -1 {
		password = tail[:cidx]
		for _, h := range strings.Split(tail[cidx+1:], ",") {
			if h = strings.TrimSpace(h); h != "" {
				hashes = append(hashes, h)
			}
		}
	}

	if ip == "" || dtlsPort == "" || password == "" {
		return nil, false
	}
	return &WdttLink{
		Name:     name,
		Host:     ip + ":" + dtlsPort,
		Password: password,
		Hashes:   hashes,
	}, true
}

// validatePortOverride — переопределение порта (TURN/direct/raw): пусто =
// «не задано», иначе целое 1..65535. Раньше port=abc сохранялся как есть и
// диагностировался только сетевой ошибкой на connect/ping.
func validatePortOverride(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("порт должен быть целым 1..65535 (получено %q)", s)
	}
	return nil
}

// upsertFromLink — импорт ссылки идемпотентен: если профиль с тем же адресом
// и паролем уже есть, он обновляется (хеши/порт/воркеры), иначе создаётся новый
// с авто-именем как в GUI («Сервер 1», «Name 2», ...).
// Ошибка сохранения НЕ скрывается: иначе CLI печатал «Профиль сохранён», а
// на диске ничего не было.
func upsertFromLink(st *backend.Store, link *WdttLink) (string, bool, error) {
	if err := validatePeerAddr(link.Host); err != nil {
		return "", false, err
	}
	if err := validatePortOverride(link.Port); err != nil {
		return "", false, fmt.Errorf("порт TURN из ссылки: %w", err)
	}
	profiles := st.ListProfiles()
	for name, p := range profiles {
		if p.PeerAddr != link.Host || p.Password != link.Password {
			continue
		}
		changed := false
		if len(link.Hashes) > 0 && !sameStrings(p.Hashes, link.Hashes) {
			p.Hashes = link.Hashes
			changed = true
		}
		if link.Port != "" && link.Port != p.TurnPort {
			p.TurnPort = link.Port
			changed = true
		}
		if link.Workers > 0 && link.Workers != p.Workers {
			p.Workers = link.Workers
			changed = true
		}
		if changed {
			if err := st.SaveProfile(name, p); err != nil {
				return name, true, fmt.Errorf("обновить профиль %q: %w", name, err)
			}
		}
		return name, true, nil
	}

	name := autoName(st, link.Name)
	if err := st.SaveProfileNew(name, backend.ProfileData{
		PeerAddr: link.Host,
		Password: link.Password,
		Hashes:   link.Hashes,
		TurnPort: link.Port,
		Workers:  link.Workers,
	}); err != nil {
		return name, false, err
	}
	return name, false, nil
}

// autoName — логика авто-имени из GUI: база + счётчик, пока имя свободно.
// Проверка идёт по САНИТИЗИРОВАННОМУ имени (именно оно станет файлом):
// раньше «#» в ссылке давал «### 1», который сохранялся как «1.json» и
// затирал чужой профиль.
func autoName(st *backend.Store, linkName string) string {
	// Имя сразу приводим к тому виду, в котором оно попадёт на диск: иначе
	// пользователь видел «Профиль сохранён: ### 1», а файл назывался иначе.
	base := backend.CanonicalProfileName(linkName)
	if base == "" || base == "Server" {
		base = "Сервер"
	}
	for counter := 1; counter <= 100000; counter++ {
		cand := fmt.Sprintf("%s %d", base, counter)
		if !st.ProfileExists(cand) {
			return cand
		}
	}
	return fmt.Sprintf("%s %d", base, 100001)
}

// validatePeerAddr проверяет peer-адрес из ссылки/флага: host:port, где
// host — IPv4 или DNS-имя, порт 1..65535. Раньше принималось всё, что не
// пустое, и нерабочий профиль выяснялся только на connect.
func validatePeerAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("адрес %q: ожидается host:port (%v)", addr, err)
	}
	if host == "" {
		return fmt.Errorf("адрес %q: пустой хост", addr)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("адрес %q: порт должен быть 1..65535", addr)
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if !backend.IsDomainName(host) {
		return fmt.Errorf("адрес %q: %q — не IP и не доменное имя", addr, host)
	}
	return nil
}
