package backend

// Общие (платформенно-нейтральные) типы для работы с интерфейсами и
// маршрутами IPv4. Реализация — в winip_windows.go (IP Helper API) и
// winip_notwindows.go (заглушки).

import (
	"fmt"
	"net"
	"strings"
)

// ipIface — ссылка на сетевой интерфейс ОС.
// Luid — стабильный идентификатор (меняется только при пересоздании адаптера),
// Index — LUID-производный индекс, который ОС может менять при
// отключении/включении адаптера.
type ipIface struct {
	Alias string
	Luid  uint64
	Index uint32
}

// ipRoute — запись таблицы маршрутизации IPv4.
type ipRoute struct {
	Dest      net.IP // 4 байта, сеть (не хост)
	PrefixLen uint8
	NextHop   net.IP // 0.0.0.0 = on-link
	Luid      uint64
	Index     uint32
	Metric    uint32
	Protocol  uint32 // источник маршрута (MIB_IPPROTO_*)
}

// protoNetMgmt — MIB_IPPROTO_NETMGMT: маршрут добавлен административно
// (наш или netsh-статический). Автоматические маршруты адаптера не трогаем.
const protoNetMgmt = 3

// String — «1.2.3.0/24» для сообщений и сводок.
func (r ipRoute) String() string {
	return fmt.Sprintf("%s/%d", r.Dest, r.PrefixLen)
}

// parseAddrCIDR разбирает адрес интерфейса: "10.8.0.2/32", "10.8.0.2/24"
// или просто "10.8.0.2" (→ /32).
func parseAddrCIDR(cidr string) (net.IP, uint8, error) {
	if !strings.Contains(cidr, "/") {
		cidr += "/32"
	}
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, 0, fmt.Errorf("некорректный адрес %q: %w", cidr, err)
	}
	v4 := ip.To4()
	if v4 == nil {
		return nil, 0, fmt.Errorf("адрес %q не IPv4", cidr)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 || ones < 0 {
		return nil, 0, fmt.Errorf("некорректная маска в %q", cidr)
	}
	return v4, uint8(ones), nil
}

// cidrToRoute разбирает «1.2.3.4/24» в адрес сети и длину префикса.
func cidrToRoute(cidr string) (net.IP, uint8, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, 0, fmt.Errorf("некорректный CIDR %q: %w", cidr, err)
	}
	v4 := ip.To4()
	if v4 == nil {
		return nil, 0, fmt.Errorf("IPv6 не поддерживается: %q", cidr)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, 0, fmt.Errorf("некорректная маска %q", cidr)
	}
	return ipnet.IP.To4(), uint8(ones), nil
}

// newRoute собирает маршрут в интерфейс iface.
// nextHop == nil (или 0.0.0.0) — маршрут on-link (через сам интерфейс).
func newRoute(cidr string, nextHop net.IP, iface ipIface, metric uint32) (ipRoute, error) {
	dest, prefix, err := cidrToRoute(cidr)
	if err != nil {
		return ipRoute{}, err
	}
	if nextHop != nil && nextHop.To4() == nil {
		return ipRoute{}, fmt.Errorf("некорректный next hop: %v", nextHop)
	}
	return ipRoute{
		Dest:      dest,
		PrefixLen: prefix,
		NextHop:   nextHop,
		Luid:      iface.Luid,
		Index:     iface.Index,
		Metric:    metric,
	}, nil
}

// routeStrings — представление маршрутов для лога/сводки.
func routeStrings(routes []ipRoute) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.String())
	}
	return out
}
