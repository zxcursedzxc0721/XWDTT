package backend

// Split tunneling (Этап 3): режимы маршрутизации и планировщик маршрутов.
//
// RoutePlan — чистое описание «что делать с маршрутами», без выполнения:
// тестируется без root и без сети. Три apply-функции в wg.go лишь применяют
// план к ОС. Каждый режим гарантирует собственные инварианты:
//
//	full  — как до Этапа 3, байт-в-байт: весь трафик в туннель, исключения
//	        (TURN/VK/DNS) мимо него, дефолтный маршрут перехвачен;
//	split — в туннель ТОЛЬКО адреса из списка, исключений нет (незачем:
//	        всё остальное и так идёт напрямую), дефолт не трогаем.

// Режимы маршрутизации (AppSettings.RouteMode).
const (
	RouteModeFull  = "full"  // весь трафик через VPN (поведение по умолчанию)
	RouteModeSplit = "split" // через VPN только адреса из SplitRoutes
)

// RoutePolicy — что просит вызывающая сторона (Bridge при событии wg_config).
type RoutePolicy struct {
	Mode       string   // RouteModeFull | RouteModeSplit (пусто = full)
	SplitCIDRs []string // резолвнутый список split-маршрутов (нормализованные CIDR)
}

// RoutePlan — готовое распределение маршрутов для apply-функций.
type RoutePlan struct {
	TunnelRoutes  []string // через wg-интерфейс
	ExcludeRoutes []string // мимо туннеля, через физический шлюз (только full)
	HijackDefault bool     // менять ли метрику дефолтного маршрута (только full)
}

// BuildRoutePlan строит план по AllowedIPs из WG-конфига, TURN-адресам и
// политики режима. Обе ветки детерминированы: split не зависит от конфига
// сервера (AllowedIPs = 0.0.0.0/0 в split не разворачивается — см. 3.4 плана).
func BuildRoutePlan(allowedIPs []string, turnIPs []string, p RoutePolicy) RoutePlan {
	if p.Mode == RouteModeSplit {
		// Маршруты только из пользовательского списка; 0.0.0.0/0 из конфига
		// сервера в split не участвует, дефолтный маршрут не перехватываем.
		return RoutePlan{
			TunnelRoutes:  dedupe(p.SplitCIDRs),
			ExcludeRoutes: nil, // исключения не нужны: остальное и так напрямую
			HijackDefault: false,
		}
	}

	// full: как всегда — всё в туннель, TURN/VK/DNS — мимо него.
	excludes := make([]string, 0, len(turnIPs)+len(vkExcludeCIDRs)+4)
	for _, ip := range turnIPs {
		excludes = append(excludes, ip+"/32")
	}
	excludes = append(excludes, vkExcludeCIDRs...)
	for _, dns := range localDNSServers() {
		excludes = append(excludes, dns+"/32")
	}
	return RoutePlan{
		TunnelRoutes:  dedupe(allowedIPs),
		ExcludeRoutes: dedupe(excludes),
		HijackDefault: true,
	}
}

// IsSplit — режим split с учётом нормализации пустого значения.
func (p RoutePolicy) IsSplit() bool { return p.Mode == RouteModeSplit }

// dedupe убирает дубли, сохраняя порядок (порядок маршрутов предсказуем).
func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
