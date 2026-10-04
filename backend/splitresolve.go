package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Split tunneling (Этап 3): нормализация записей списка и резолв доменов.
//
// Список хранит три вида записей: CIDR (10.0.0.0/8), IP (1.2.3.4/32) и домены
// (vk.com, *.ozon.ru). Домены резолвятся в /32 при каждом подключении — с
// кэшем на диск (dnscache.json), TTL 10 минут, при сбое DNS берётся stale из
// кэша (референс: BypassRoutes.kt resolveHosts, строки 304–338).

// Лимиты. Стартовые значения (100 записей / 400 префиксов — как MAX_RULES и
// MAX_EXCLUDE_PREFIXES в BypassRoutes.kt) были рассчитаны на ручной ввод и
// поштучный netsh. Импорт больших списков ip (тысячи адресов из ip-list.json)
// потребовал поднять оба: маршруты применяются батчем одним процессом netsh -f
// (см. applyTunnelRoutes — ~4000 команд/сек), поэтому ограничение теперь
// только по разумному размеру списка, а не по скорости ОС.
const (
	MaxSplitRules    = 65536 // записей в списке (лимит CLI ips add/import)
	maxSplitPrefixes = 65536 // /32 после резолва доменов
	splitResolveTTL     = 10 * time.Minute
	splitResolveTimeout = 5 * time.Second
)

// LookupIPv4 вынесено в переменную для тестируемости (без реальной сети).
var LookupIPv4 = func(host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), splitResolveTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	return out, nil
}

// ═══════════════════════════════════════════════════
// НОРМАЛИЗАЦИЯ ЗАПИСЕЙ (при добавлении в список)
// ═══════════════════════════════════════════════════

// NormalizeSplitEntry приводит запись к каноническому виду:
//
//	1.2.3.4          → 1.2.3.4/32
//	10.1.2.3/8       → 10.0.0.0/8        (нормализация сети)
//	https://a.ru/x   → a.ru              (схема и путь отбрасываются)
//	VK.COM.          → vk.com            (нижний регистр, без концевой точки)
//	*.Ozon.ru        → *.ozon.ru         (wildcard: apex + www при резолве)
//
// Ошибка — запись не похожа ни на IP, ни на CIDR, ни на домен (CLI печатает
// предупреждение и пропускает запись, это не фатально).
func NormalizeSplitEntry(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("пустая запись")
	}

	// URL → только хост (https://whoer.net/ru → whoer.net)
	if lc := strings.ToLower(s); strings.HasPrefix(lc, "http://") || strings.HasPrefix(lc, "https://") {
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return "", fmt.Errorf("не удалось выделить хост из URL %q", raw)
		}
		s = u.Hostname()
	}

	// CIDR — до IP, чтобы «10.1.2.3/8» ушёл в нормализацию сети
	if strings.Contains(s, "/") {
		_, network, err := net.ParseCIDR(s)
		if err != nil {
			return "", fmt.Errorf("некорректный CIDR %q", raw)
		}
		if network.IP.To4() == nil {
			return "", fmt.Errorf("IPv6 не поддерживается: %q", raw)
		}
		return network.String(), nil
	}

	// Отдельный IPv6-случай (без слэша)
	if ip := net.ParseIP(s); ip != nil {
		if ip.To4() == nil {
			return "", fmt.Errorf("IPv6 не поддерживается: %q", raw)
		}
		return ip.String() + "/32", nil
	}

	// Wildcard-домен: *.ozon.ru — резолвит apex и www
	if strings.HasPrefix(strings.ToLower(s), "*.") {
		host := strings.ToLower(strings.TrimSuffix(s[2:], "."))
		if !isDomainName(host) {
			return "", fmt.Errorf("некорректный wildcard-домен %q", raw)
		}
		return "*." + host, nil
	}

	// Обычный домен
	host := strings.ToLower(strings.TrimSuffix(s, "."))
	if isDomainName(host) {
		return host, nil
	}
	return "", fmt.Errorf("не IP, CIDR и не домен: %q", raw)
}

// SplitEntryIsDomain — нужен ли резолв (запись доменная).
func SplitEntryIsDomain(entry string) bool {
	return !strings.Contains(entry, "/")
}

// isDomainName — строгая проверка хоста (порт из BypassRoutes.kt,
// строки 293–302): есть точка, TLD с буквой, лейблы валидны.
func isDomainName(host string) bool {
	if len(host) < 1 || len(host) > 253 {
		return false
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	if !strings.Contains(host, ".") {
		return false
	}
	labels := strings.Split(host, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 {
			return false
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			if !isAlnum(l[i]) && l[i] != '-' {
				return false
			}
		}
	}
	// TLD должен содержать букву
	tld := labels[len(labels)-1]
	hasLetter := false
	for i := 0; i < len(tld); i++ {
		if tld[i] >= 'a' && tld[i] <= 'z' {
			hasLetter = true
			break
		}
	}
	return hasLetter
}

func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// ═══════════════════════════════════════════════════
// РЕЗОЛВ С КЭШЕМ (при подключении)
// ═══════════════════════════════════════════════════

// dnsCacheEntry — одна доменная запись кэша.
type dnsCacheEntry struct {
	IPs []string `json:"ips"` // последние успешные A-записи
	TS  int64    `json:"ts"`  // unix-секунды последнего успешного резолва
}

// ResolveSplitRoutes превращает записи списка в CIDR-ы для RoutePolicy:
// IP/CIDR проходят как есть, домены резолвятся (с кэшем на диск).
// Возвращает nil для пустого списка. logf может быть nil.
func ResolveSplitRoutes(entries []string, logf func(string)) []string {
	logf2 := func(msg string) { if logf != nil { logf(msg) } }
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > MaxSplitRules {
		logf2(fmt.Sprintf("split: список длиннее %d записей, хвост игнорируется", MaxSplitRules))
		entries = entries[:MaxSplitRules]
	}

	cachePath := filepath.Join(configDir(), "dnscache.json")
	cache := loadDNSCache(cachePath)
	now := time.Now().Unix()
	cacheDirty := false

	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !SplitEntryIsDomain(entry) {
			out = append(out, entry)
			continue
		}
		hosts := domainLookupHosts(entry)
		for _, host := range hosts {
			prev, hadCache := cache[host]
			ips, stale, err := resolveHostCached(host, cache, now)
			if err != nil {
				logf2(fmt.Sprintf("split: DNS %s: %v", host, err))
				continue
			}
			if stale || !hadCache || !sameStringsEqual(prev.IPs, ips) {
				cache[host] = dnsCacheEntry{IPs: ips, TS: now}
				cacheDirty = true
			}
			for _, ip := range ips {
				out = append(out, ip+"/32")
			}
			if len(out) >= maxSplitPrefixes {
				logf2(fmt.Sprintf("split: достигнут лимит %d маршрутов, хвост списка игнорируется", maxSplitPrefixes))
				if cacheDirty {
					saveDNSCache(cachePath, cache)
				}
				return dedupe(out)
			}
		}
	}
	if cacheDirty {
		saveDNSCache(cachePath, cache)
	}
	return dedupe(out)
}

// domainLookupHosts — домен → хосты для DNS: обычный домен сам по себе,
// wildcard (*.x.ru) → apex + www (как в референсе).
func domainLookupHosts(entry string) []string {
	if strings.HasPrefix(entry, "*.") {
		apex := entry[2:]
		return []string{apex, "www." + apex}
	}
	return []string{entry}
}

// resolveHostCached — резолв одного хоста через кэш.
// Возвращает (IPs, staleUsed, error). При сбое DNS берётся stale из кэша
// (частая ситуация: DNS уже ушёл в поднимающийся туннель).
func resolveHostCached(host string, cache map[string]dnsCacheEntry, now int64) ([]string, bool, error) {
	if c, ok := cache[host]; ok && c.TS > 0 && len(c.IPs) > 0 && now-c.TS < int64(splitResolveTTL.Seconds()) {
		return c.IPs, false, nil // свежий кэш — сеть не трогаем
	}

	ips, err := LookupIPv4(host)
	if err == nil && len(ips) > 0 {
		return ips, false, nil
	}

	// Сбой/пусто → stale из кэша (порт логики референса, строки 322–330)
	if c, ok := cache[host]; ok && len(c.IPs) > 0 {
		return c.IPs, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return nil, false, fmt.Errorf("нет A-записей")
}

// sameStringsEqual — равенство двух списков IP (для проверки изменений кэша).
func sameStringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dnsCacheFile — формат dnscache.json.
type dnsCacheFile struct {
	Entries map[string]dnsCacheEntry `json:"entries"`
}

func loadDNSCache(path string) map[string]dnsCacheEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]dnsCacheEntry{}
	}
	var f dnsCacheFile
	if err := json.Unmarshal(data, &f); err != nil || f.Entries == nil {
		return map[string]dnsCacheEntry{}
	}
	return f.Entries
}

func saveDNSCache(path string, entries map[string]dnsCacheEntry) {
	data, err := json.MarshalIndent(dnsCacheFile{Entries: entries}, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = atomicWrite(path, data)
}
