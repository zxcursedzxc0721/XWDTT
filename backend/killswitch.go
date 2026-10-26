package backend

// Kill-switch full-режима (маршрутная версия).
//
// WFP-фильтры давали бы полную блокировку, но их ошибка = навсегда мёртвая
// сеть (фильтры переживают процесс). Маршрутный вариант проще и обратим:
// пока туннель жив, прямой default-маршрут физического адаптера удалён —
// при обрыве сессии трафик просто некуда идти напрямую (TURN/DNS-исключения
// работают по своим on-link маршрутам, сессия переподнимается). Teardown
// возвращает default, а после краша — файл-бэкап чинит сеть при следующем
// старте xwdtt (а перезагрузку DHCP-маршрут восстанавливает сам).

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
)

// physDefaultBackup — сохранённый физический default-маршрут.
type physDefaultBackup struct {
	Prefix  string `json:"prefix"` // всегда "0.0.0.0/0"
	NextHop string `json:"next_hop"`
	Luid    uint64 `json:"luid"`
	Index   uint32 `json:"index"`
	Metric  uint32 `json:"metric"`
}

func physDefBackupPath() string {
	return filepath.Join(configDir(), "physdef-backup.json")
}

// killSwitchRemovePhysDef сохраняет физический default в файл и удаляет его.
// Без записанного бэкапа маршрут НЕ удаляется: лучше утечка, чем навсегда
// потерянная сеть.
func killSwitchRemovePhysDef(def ipRoute, logf func(string)) {
	path := physDefBackupPath()
	b := physDefaultBackup{
		Prefix:  "0.0.0.0/0",
		NextHop: def.NextHop.String(),
		Luid:    def.Luid,
		Index:   def.Index,
		Metric:  def.Metric,
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		logf("kill-switch: бэкап: " + err.Error())
		return
	}
	if err := atomicWrite(path, data); err != nil {
		logf("kill-switch: бэкап не записан, прямой default НЕ убираю: " + err.Error())
		return
	}
	if err := routeDelete(def); err != nil {
		// Маршрут остался — файл бы врал, что его нет, и teardown добавил бы
		// дубль. Убираем файл, оставляем сеть как есть.
		_ = os.Remove(path)
		logf("kill-switch: прямой default не удалился: " + err.Error())
		return
	}
	logf("Kill-switch: прямой доступ закрыт — только туннель (восстановится при отключении)")
}

// restorePhysDef возвращает физический default из файла бэкапа и удаляет
// файл. Если файла нет — тихий no-op (split-режим ничего не сохранял).
func restorePhysDef(logf func(string)) bool {
	path := physDefBackupPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_ = os.Remove(path)
	var b physDefaultBackup
	if err := json.Unmarshal(data, &b); err != nil {
		if logf != nil {
			logf("kill-switch: бэкап повреждён: " + err.Error())
		}
		return false
	}
	nextHop := net.ParseIP(b.NextHop)
	if nextHop == nil {
		nextHop = net.IPv4zero
	}
	r, err := newRoute(b.Prefix, nextHop, ipIface{Luid: b.Luid, Index: b.Index}, b.Metric)
	if err != nil {
		if logf != nil {
			logf("kill-switch: восстановить default: " + err.Error())
		}
		return false
	}
	if err := routeAdd(r); err != nil {
		if logf != nil {
			logf("kill-switch: вернуть прямой default: " + err.Error())
		}
		return false
	}
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
