package backend

// Kill-switch full-режима (маршрутная версия).
//
// WFP-фильтры давали бы полную блокировку, но их ошибка = навсегда мёртвая
// сеть (фильтры переживают процесс). Маршрутный вариант проще и обратим:
// пока туннель жив, прямые default-маршруты физических адаптеров удалены —
// при обрыве сессии трафику просто некуда идти напрямую (TURN/DNS-исключения
// работают по своим on-link маршрутам, сессия переподнимается). Teardown
// возвращает default'ы, а после краша — файл-бэкап чинит сеть при следующем
// старте xwdtt (а перезагрузку DHCP-маршрут восстанавливает сам).

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// physRouteBackup — один сохранённый физический default-маршрут.
type physRouteBackup struct {
	Prefix  string `json:"prefix"` // всегда "0.0.0.0/0"
	NextHop string `json:"next_hop"`
	Luid    uint64 `json:"luid"`
	Index   uint32 `json:"index"`
	Metric  uint32 `json:"metric"`
}

// physDefaultBackup — все физические default-маршруты. Multi-homed системы
// (Ethernet+Wi-Fi) держат несколько 0.0.0.0/0: снятие только лучшего
// оставляло второй аплинк живым — kill-switch тек мимо туннеля.
type physDefaultBackup struct {
	Routes []physRouteBackup `json:"routes"`
}

// legacyPhysDefaultBackup — формат бэкапа до multi-route (один маршрут);
// нужен, чтобы бэкап, оставшийся от прошлой версии, всё равно восстановился.
type legacyPhysDefaultBackup struct {
	Prefix  string `json:"prefix"`
	NextHop string `json:"next_hop"`
	Luid    uint64 `json:"luid"`
	Index   uint32 `json:"index"`
	Metric  uint32 `json:"metric"`
}

func physDefBackupPath() string {
	return filepath.Join(configDir(), "physdef-backup.json")
}

// killSwitchRemovePhysDef сохраняет физические default-маршруты в файл и
// удаляет их. Без записанного бэкапа маршруты НЕ удаляются: лучше утечка,
// чем навсегда потерянная сеть.
func killSwitchRemovePhysDef(defs []ipRoute, logf func(string)) {
	if len(defs) == 0 {
		return
	}
	path := physDefBackupPath()
	b := physDefaultBackup{}
	for _, def := range defs {
		b.Routes = append(b.Routes, physRouteBackup{
			Prefix:  "0.0.0.0/0",
			NextHop: def.NextHop.String(),
			Luid:    def.Luid,
			Index:   def.Index,
			Metric:  def.Metric,
		})
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		logf("kill-switch: бэкап: " + err.Error())
		return
	}
	if err := atomicWrite(path, data); err != nil {
		logf("kill-switch: бэкап не записан, прямые default'ы НЕ убираю: " + err.Error())
		return
	}
	for _, def := range defs {
		if err := routeDelete(def); err != nil {
			// Маршрут остался — файл врёт, что его нет, и teardown добавил бы
			// дубль. Но удалять файл целиком из-за одного из нескольких
			// маршрутов нельзя: остальные уже сняты. дубль при restore
			// безвреден (routeAdd вернёт ошибку).
			logf("kill-switch: прямой default не удалился: " + err.Error())
		}
	}
	if len(defs) > 1 {
		logf(fmt.Sprintf("Kill-switch: прямых default-маршрутов снято: %d (multi-homed)", len(defs)))
	} else {
		logf("Kill-switch: прямой доступ закрыт — только туннель (восстановится при отключении)")
	}
}

// restorePhysDef возвращает физические default-маршруты из файла бэкапа и
// удаляет файл. Если файла нет — тихий no-op (split-режим ничего не сохранял).
func restorePhysDef(logf func(string)) bool {
	path := physDefBackupPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var b physDefaultBackup
	if err := json.Unmarshal(data, &b); err != nil || len(b.Routes) == 0 {
		// Возможно, это старый одно-маршрутный формат.
		var old legacyPhysDefaultBackup
		if err2 := json.Unmarshal(data, &old); err2 == nil && old.Prefix != "" {
			b.Routes = []physRouteBackup{{
				Prefix: old.Prefix, NextHop: old.NextHop,
				Luid: old.Luid, Index: old.Index, Metric: old.Metric,
			}}
		} else {
			_ = os.Remove(path) // бэкап не разбирается — оставить его только мешает
			if logf != nil {
				logf("kill-switch: бэкап повреждён: " + err.Error())
			}
			return false
		}
	}
	restored, failed := 0, 0
	for _, rb := range b.Routes {
		nextHop := net.ParseIP(rb.NextHop)
		if nextHop == nil {
			nextHop = net.IPv4zero
		}
		r, err := newRoute(rb.Prefix, nextHop, ipIface{Luid: rb.Luid, Index: rb.Index}, rb.Metric)
		if err != nil {
			failed++
			if logf != nil {
				logf("kill-switch: восстановить default: " + err.Error())
			}
			continue
		}
		if err := routeAdd(r); err != nil {
			failed++
			if logf != nil {
				logf("kill-switch: вернуть прямой default: " + err.Error())
			}
			continue
		}
		restored++
	}
	if failed > 0 {
		// Файл НЕ удаляем: это единственная копия параметров физического
		// default, а RestoreStaleFullMode при следующем старте повторит
		// попытку. Уже восстановленные маршруты при повторе дадут ошибку
		// routeAdd — она безвредна.
		if logf != nil {
			logf(fmt.Sprintf("kill-switch: восстановлено %d из %d default-маршрутов, повтор при следующем старте", restored, len(b.Routes)))
		}
		return restored > 0
	}
	_ = os.Remove(path)
	if logf != nil {
		logf("Прямой доступ восстановлен (kill-switch снят)")
	}
	return true
}

// RestoreStaleFullMode — вход из main(): чинит состояние после краша
// full-сессии (DNS физ. адаптеров + прямой default). Работает elevated
// и только когда туннельного адаптера нет.
func RestoreStaleFullMode(logf func(string)) {
	if !IsElevated() {
		return
	}
	if _, err := ifaceByAlias(wgIface); err == nil {
		return // туннель жив — его teardown сам всё восстановит
	}
	restorePhysDef(logf)
	restorePhysicalDNS(logf)
}
