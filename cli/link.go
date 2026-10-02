package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"pwdtt/backend"
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

// upsertFromLink — импорт ссылки идемпотентен: если профиль с тем же адресом
// и паролем уже есть, он обновляется (хеши/порт/воркеры), иначе создаётся новый
// с авто-именем как в GUI («Сервер 1», «Name 2», ...). Возвращает имя и флаг обновления.
func upsertFromLink(st *backend.Store, link *WdttLink) (string, bool) {
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
			_ = st.SaveProfile(name, p)
		}
		return name, true
	}

	name := autoName(profiles, link.Name)
	_ = st.SaveProfile(name, backend.ProfileData{
		PeerAddr: link.Host,
		Password: link.Password,
		Hashes:   link.Hashes,
		TurnPort: link.Port,
		Workers:  link.Workers,
	})
	return name, false
}

// autoName — логика авто-имени из GUI: база + счётчик, пока имя свободно.
func autoName(existing map[string]backend.ProfileData, linkName string) string {
	base := linkName
	if base == "" || base == "Server" {
		base = "Сервер"
	}
	for counter := 1; ; counter++ {
		cand := fmt.Sprintf("%s %d", base, counter)
		if _, ok := existing[cand]; !ok {
			return cand
		}
	}
}
