# PWDTT — План переделки (инструкция для ИИ)

> **Назначение файла:** этот документ — полный handoff. Если работа прервалась (закончились токены,
> новая сессия), ИИ должен прочитать этот файл целиком и продолжать по этапам, не спрашивая заново
> контекст. Все решения ниже уже согласованы с пользователем — **не переобсуждать их**.
>
> **Язык общения с пользователем:** русский.
>
> **Статус работ:** анализ завершён, код ещё не менялся. Начинать с Этапа 1.

---

## 1. Что за проект

**PWDTT** — десктопный VPN-клиент (Go), форк `proxy-turn-vk-android`. Поднимает локальный
WireGuard-интерфейс и гонит трафик через TURN/DTLS серверы VK, маскируя пакеты под RTP
(ChaCha20-Poly1305) — для провайдера это выглядит как зашифрованный медиатрафик звонка.

```
Приложение → WireGuard → ChaCha20/RTP → VK TURN/DTLS → wdtt-server (VPS) → интернет
```

### Структура репозитория

| Путь | Что это | Судьба |
|---|---|---|
| `frontend/` | React UI для Wails | **Удалить (Этап 1)** |
| `backend/` | Wails-биндинги, Bridge, Store, WG-роутинг, autostart/update/report | Оставить, убрать Wails |
| `core/` | Ядро (модуль `wg-turn-client`, `replace => ./core`) — TURN/DTLS, воркеры, обфускация | Обновить (Этап 4) |
| `main_linux.go`, `main_windows.go`, `main_darwin.go` | Точки входа Wails | **Заменить на один `main.go` (Этап 1)** |
| `tests/` | 78 юнит-тестов, пакет `backend_test` | Оставить зелёными |
| `assets/wintun.dll` | Драйвер TUN для Windows | Оставить (embed в windows-файле) |
| `assets/install-sudoers.sh` | sudoers для Linux (`ip`, `wg`) | Оставить |
| `build/` | Wails-артефакты сборки | Удалить Wails-специфичное, `icon.ico` оставить |
| `wails.json` | Конфиг Wails | **Удалить** |
| `.github/workflows/build.yml`, `release.yml` | CI | Переписать (Этап 5) |
| `proxy-turn-vk-android/` | **Соседний репозиторий — Android-версия (qWDTT)**, источник кода для переноса | Не трогать, только читать |

### Ключевые файлы backend

- `backend/app.go` — объект `App`, биндинги Wails (`Connect`, `Disconnect`, `GetProfile`...),
  `Version = "1.6.0"`. Импорт `wails/v2/pkg/runtime` → заменить на колбэк.
- `backend/bridge.go` — `Bridge` мост к ядру. `Connect(params)` → `core.New(cfg).Start()` →
  `forwardEvents()` читает `chan core.Event` и шлёт в `onEvent(name, args...)`.
  Событие `"wg_config"` вызывает `wg.Apply(ev.Data, ev.TurnIPs, wgLogf)`.
- `backend/store.go` — `~/.config/pwdtt/config.json` (AppSettings) и `servers/<name>.json`
  (ProfileData). Атомарная запись через `atomicWrite`.
- `backend/wg.go` — управление интерфейсом `pwdtt-wg`, **три отдельные реализации**:
  - `applyLinux(conf, turnIPs, logf)` — `ip`/`wg` через `sudo -n`
  - `applyWindows(conf, turnIPs, logf)` — wintun + userspace WireGuard + `netsh`/`route`
  - `applyDarwin(conf, turnIPs, logf)` — osascript-привилегированный helper, принимает
    флаги `-exclude` и `-tunnel`
  - `vkExcludeCIDRs` — hardcoded список IP VK/DNS для обхода туннеля (нужен только в full-режиме)
  - `parseWGConfig(conf) → (addr, mtu, allowedIPs, wgConf)`
- `backend/types.go` — `ConnectParams`, `ProfileData`, `AppSettings` (сюда добавлять новые поля)
- `backend/report.go`, `update.go`, `autostart.go`, `cmd_windows.go` (`hideWindow`)

### Ключевые файлы core

- `core/main.go` — `Core`, `Config`, `Start()`, канал `events chan Event`, типы событий
  (`EventState/EventLog/EventStats/EventError/EventEvent`), имена событий:
  `"wg_config"`, `"captcha_required"`, `"ready"`
- `core/session.go` — `RunSession(...)`: DTLS-сессия, relay, слот воркера `WorkerSlot{ID, SendCh}`
- `core/worker_group.go` — `WorkerGroup(...)`: каскадный старт групп, ретраи, бан-лист
- `core/dispatcher.go` — раздача пакетов воркерам (chunk=12, `SendCh` только)
- `core/blacklist.go` — `GlobalBlacklist` (бан TURN-адреса на 5 мин) — **наш козырь, сохранить**
- `core/obfs.go` — RTP-обфускация
- `core/dns.go` — резолвер (захардкоженный Яндекс DNS 77.88.8.8/8.8.8.1)
- `core/creds_vkcalls.go`, `captcha_v2.go`, `captcha_v2_slider.go`, `vk_auth.go` — авторизация/капча
- `core/main.go` `patchMTU` — вставляет `MTU = 1280`, если нет

---

## 2. Решения, согласованные с пользователем (НЕ ПЕРЕОБСУЖДАТЬ)

1. **GUI → консоль.** Полностью удалить frontend и Wails. Формат: подкоманды + режим по
   умолчанию (без аргументов — интерактивный промпт).
2. **Тюнелирование — переключатель** `full` (всё через VPN, как сейчас) | `split`
   (через VPN только IP из списка, остальное напрямую).
3. **Список IP — глобальный** в `config.json`, общий для всех серверов.
4. **Поддержка доменов в списке** — `pwdtt ips add vk.com` → резолв в IP с кэшем
   (референс реализации: `proxy-turn-vk-android/app/src/main/java/com/wdtt/client/settings/BypassRoutes.kt`).
5. **Перенос из Android-ядра — ВСЁ:**
   - Группа 1 (скорость): диспетчер с PrioCh/адаптивными chunk'ами/dwell, распределение по relay'ям, быстрый старт воркеров + allocate-gate
   - Группа 2 (надёжность): дедлайн записи 3 с, keepalive через PrioCh
   - CLI-фичи: check-hashes, ping-only, PAUSE/RESUME
   - DoH + replay-window в obfs
   - RAW-режим и NoDTLS — **сервер пользователя уже обновлён до последней версии** (поддерживает `-listen-raw` и `-listen-direct`)
6. **SOCKS5 — НЕ переносим.** Также не переносим: их строковую обработку ошибок (у нас
   типизированные `SessionError`), RTP extension header, battery/doze логику.

---

## 3. Статус работ (обновлять по мере выполнения)

- [x] **Этап 1.** Отвязка от Wails/GUI — **ГОТОВО (02.10.2026):** frontend/wails.json/Wails-манифесты
  удалены; `main.go` + `wintun_windows.go` созданы; `backend/app.go` переписан (`Init/Close`,
  колбэк `onEvent`, добавлены `Pause/Resume` в Bridge); `backend/admin_windows.go` — автоэлевация
  UAC вместо манифеста (принято решение: runtime-элевация, go-winres НЕ нужен); `go mod tidy` —
  Wails удалён; `go build` для windows/linux/darwin + `go test ./...` зелёные.
- [ ] **Этап 2.** Консольный интерфейс (CLI)
- [ ] **Этап 3.** Split tunneling (белый список + резолв доменов)
- [ ] **Этап 4.** Обновление ядра из qWDTT (4.1 скорость, 4.2 надёжность, 4.3 DoH+replay, 4.4 RAW/NoDTLS, 4.5 CLI-фичи)
- [ ] **Этап 5.** Тесты, CI, README

**После каждого этапа обязательно:** `go build ./...` и `go test ./...` — должны быть зелёными.

---

## 4. Этапы

### Этап 1 — Отвязка от Wails/GUI

**Цель:** собирать консольное приложение обычным `go build`, без Node.js, webkit и Wails.

**Задачи:**

1. Удалить: `frontend/` (весь), `wails.json`, `build/windows/wails.exe.manifest`,
   `build/windows/info.json`, `build/darwin/Info.plist`, `build/appicon.png`.
   **Оставить** `build/windows/icon.ico` (пригодится для манифеста), `assets/wintun.dll`,
   `assets/icons/*` (иконка приложения), `assets/install-sudoers.sh`.
2. Удалить `main_linux.go`, `main_windows.go`, `main_darwin.go`. Создать:
   - `main.go` — единая точка входа (без build-тегов), вызывает CLI из Этапа 2
     (на Этапе 1 можно временно `fmt.Println("TODO")`, чтобы сборка проходила);
   - `wintun_windows.go` (`//go:build windows`) — перенести сюда `//go:embed assets/wintun.dll`
     + `backend.InitWintun(dll)` (взять из старого `main_windows.go`).
   - Эмбед иконки (`appIcon`) больше не нужен — не переносить.
3. `backend/app.go`: убрать импорт `wails/v2/pkg/runtime`.
   - `onBridgeEvent` заменить на поле `a.onEvent func(name string, args ...any)`,
     которое задаётся снаружи (консоль передаст свою функцию печати).
   - `Startup(ctx)` / `Shutdown(ctx)` → `Init(onEvent)` / `Close()` без context.Wails-биндинги
     (`Connect`, `GetProfile`...) пока оставить как методы — они пригодятся CLI.
   - Обратить внимание: `Startup` делает `a.bridge = NewBridge(ctx, a.store, a.onBridgeEvent)` и
     `CleanupStaleExcludeRoutes` — перенести в `Init`.
4. `go.mod`: убрать `github.com/wailsapp/wails/v2`, затем `go mod tidy` (уберёт `go-webview2`,
   `go-toast`, `debounce`, `leaanthony/*` и прочий транзитивный мусор).
5. **Windows-права (РЕШЕНИЕ ПРИНЯТО: runtime-элевация, go-winres НЕ используется).**
   Манифест `requireAdministrator` раньше подставлял Wails. Теперь: `backend/admin_windows.go`
   — `IsElevated()` (x/sys/windows Token.IsElevated) и `ElevateAndExit()` (ShellExecuteW "runas"
   + exit). CLI (Этап 2) вызывает элевацию при `connect`/интерактивном режиме, если прав нет.
   На Linux/macOS — заглушки в `backend/admin_notwindows.go` (права берутся per-command:
   sudo/osascript).

**Приёмка:**
- `go build ./...` проходит (linux/windows/darwin через `GOOS=`)
- `go test ./...` — все 78 старых тестов зелёные
- В `go.mod` нет `wailsapp`
- `grep -r wails .` — пусто (кроме README-ссылок, их поправить в Этапе 5)

**Риски:** элевация на Windows — самое чувствительное место; обязательно проверить сборку
`GOOS=windows GOARCH=amd64` (в окружении пользователя Go может быть 32-битный — `go version`
показывает `windows/386`, поэтому для релиза всегда указывать `GOARCH=amd64` явно).

---

### Этап 2 — Консольный интерфейс

**Цель:** все функции GUI доступны из консоли.

**Структура:** новый пакет `cli/` (парсинг на stdlib `flag`, без новых зависимостей в go.mod).
`main.go` вызывает `cli.Run(os.Args[1:])`.

**Команды:**

```
pwdtt                                  → интерактивный режим (промпт "pwdtt> ")
pwdtt connect <имя|wdtt://ссылка> [--workers N] [--obfs audio|video] [--raw] [--notls]
pwdtt disconnect                       → (реализовать через управление фоном, см. ниже)
pwdtt servers list|add <wdtt://...>|show <имя>|rm <имя>|set-hashes <имя> h1,h2,...
pwdtt ips list|add <ip|cidr|домен>...|rm <ip|cidr|домен>...
pwdtt route full|split
pwdtt settings get|set <key> <value>   → keys: autostart, obfs, obfs-accepted, turn-tcp, workers, fingerprint, doh
pwdtt ping [<имя>]                     → замер RTT (Этап 4.5)
pwdtt servers check <имя>              → проверка хешей (Этап 4.5)
pwdtt pause|resume                     → пауза туннеля (Этап 4.5)
pwdtt update | report | version | help
```

**Как собирать `Connect` (повторяет GUI, см. `frontend/src/pages/Connect.tsx`):**
- `workers = max(9, len(hashes)*9)` (или из профиля/настройки)
- `captchaMode: "auto"`, `obfsMode` из настроек, `turnTcp` из настроек
- Хеши: если пустые — ошибка «Добавьте хеши в профиль»
- Профили хранятся через `Store` (`backend/store.go`), wdtt://-ссылки парсятся как в GUI
  (формат: `wdtt://<IP>:<DTLS_PORT>:<WG_PORT>:<PROXY_PORT>:<PASSWORD>[:<HASH1>,...][#название]`,
  также принимается `qwdtt://` — логику взять из `frontend/src/lib/utils/wdttLink.ts`)

**Вывод событий:** подписаться на колбэк `Bridge.onEvent` (Этап 1):
- `state_changed` — строка статуса цветом
- `log` (level, msg) — цветные уровни INFO/WARN/ERROR, SKIP не печатать
- `stats` — раз в 5 с обновлять строку: активные воркеры, ↓/↑ МБ
- `error` — красным
- `captcha_required` — вывести инструкцию (решается автоматически в режиме auto)

**Ctrl+C:** graceful shutdown — `bridge.Disconnect()` (уже делает `wg.Teardown()` + `core.Stop()`), выход 0.

**Интерактивный режим:** горутина читает stdin, команды `connect/disconnect/servers/ips/route/
stats/logs/pause/resume/help/quit`; логи продолжают печитаться поверх.

**Про `disconnect`:** либо `connect` живёт в своём процессе (тогда Ctrl+C = disconnect, а
отдельная команда не нужна), либо демон. **Принято:** процесс-модель — `connect` блокирует
терминал, Ctrl+C отключает. `pwdtt disconnect` не нужен; в интерактивном режиме команда есть.

**Приёмка:**
- `pwdtt servers add "wdtt://..."` создаёт профиль в `~/.config/pwdtt/servers/`
- `pwdtt connect <имя>` поднимает туннель (на Windows — со встроенным манифестом),
  логи видны, Ctrl+C снимает интерфейс (`route print` / `ip route` — маршрутных остатков нет)
- Интерактивный режим работает параллельно с печатью логов

---

### Этап 3 — Split tunneling

**Цель:** режим `split` — через VPN идут только адреса из глобального списка,
остальной трафик напрямую. Режим `full` — текущее поведение без изменений.

**3.1 Данные** (`backend/types.go`):
```go
type AppSettings struct {
    // ...существующие поля...
    RouteMode   string   `json:"routeMode"`   // "full" (default) | "split"
    SplitRoutes []string `json:"splitRoutes"` // IP/CIDR/домены, нормализованы
}
```
В `Store.LoadSettings()` — дефолт `"full"`.

**3.2 Планировщик маршрутов** — чистая функция (тестируется без root), например в
`backend/routeplan.go`:
```go
type RoutePolicy struct {
    Mode       string   // "full" | "split"
    SplitCIDRs []string // уже нормализованные CIDR (в split — резолвнутые домены)
}
type RoutePlan struct {
    TunnelRoutes   []string // через wg-интерфейс
    ExcludeRoutes  []string // мимо туннеля (только в full)
    HijackDefault  bool     // менять ли метрику дефолтного маршрута (только full)
}
func BuildRoutePlan(allowedIPs []string, turnIPs []string, p RoutePolicy) RoutePlan
```
- **full:** как сейчас (все `allowedIPs` в туннель, `turnIPs`+`vkExcludeCIDRs`+DNS в исключения,
  `HijackDefault=true`)
- **split:** `TunnelRoutes = SplitCIDRs`, `ExcludeRoutes` пусто, `HijackDefault=false`

**3.3 Прокидывание:** `wg.Apply(conf string, turnIPs []string, policy RoutePolicy, logf)` —
лишний параметр; `Bridge` в обработчике `"wg_config"` читает `b.store.LoadSettings()` и строит
policy (резолв доменов — see 3.5, выполняется заранее при `connect`).

**3.4 Изменения в трёх apply-функциях (`backend/wg.go`):**
- **Windows (`applyWindows`):**
  - если `!HijackDefault` — пропустить `netsh ... metric=1` для WG-интерфейса и
    `route change 0.0.0.0 ... metric 9999` (и не сохранять `physGW` для restore);
  - если `SplitRoutes` — не добавлять exclude-маршруты (`turnIPs`, `vkExcludeCIDRs`);
  - маршруты `netsh interface ip add route <cidr> pwdtt-wg` — только из `TunnelRoutes`
    (в split это только пользовательский список, **не** `allowedIPs` вида `0.0.0.0/0`);
  - teardown и так удаляет только записанное в `activeExcludeRoutes`/`activeRoutes` — логику
    не ломать.
- **Linux (`applyLinux`):** аналогично — в split не добавлять `allowedIPs` и excludes,
  добавить `ip route add <cidr> dev pwdtt-wg` только для списка (через `runCmdLinux` = sudo).
- **Darwin (`applyDarwin`):** helper получает `-tunnel` и `-exclude` через запятую —
  в split передать `-tunnel <список>` и `-exclude ""` (пусто), НЕ разворачивать `0.0.0.0/0`
  в `0.0.0.0/1,128.0.0.0/1`.

**ВАЖНО:** сам uapi-конфиг WireGuard (`AllowedIPs = 0.0.0.0/0` от сервера) НЕ трогаем —
это cryptokey routing, на OS-маршруты не влияет, зато принимает весь возвратный трафик.

**3.5 Резолв доменов с кэшем** (новый файл, напр. `backend/splitresolve.go`):
- `ips add vk.com` → резолвить в `/32` (A-записи), хранить в профиле списка **домен**,
  резолвить при `connect` (с кэшем в `~/.config/pwdtt/dnscache.json`, TTL ~10 мин,
  при неудаче — брать stale из кэша)
- Референс логики: `proxy-turn-vk-android/.../BypassRoutes.kt` (строки 289–330: резолв,
  stale-кэш, отсев пустых результатов)
- Валидация: `net.ParseIP` / `net.ParseCIDR`, bare IP → `/32`; мусор → предупреждение, не ошибка

**Приёмка:**
- `route full` — поведение байт-в-байт как раньше (`route print` до/после подключения идентичен)
- `route split` + `ips add 1.2.3.4` — при подключении появляется только маршрут `1.2.3.4/32`
  через `pwdtt-wg`, дефолтный маршрут НЕ тронут, `ping 8.8.8.8` идёт напрямую
- Юнит-тесты `BuildRoutePlan` (оба режима) + нормализация списка

---

### Этап 4 — Обновление ядра из qWDTT (перенос кода)

**Источник:** `proxy-turn-vk-android/go_client/` (module `wg-turn-client`, тот же что и `core/`).
Лицензии совпадают (GPLv3), код изначально общий — перенос легален.
**Стиль:** код qWDTT комментированный и объясняющий — переносить комментарии вместе с кодом.

#### 4.1 Скорость (перенос целиком из диспетчера)

**а) Диспетчер** — заменить `core/dispatcher.go` на логику из
`proxy-turn-vk-android/go_client/dispatcher.go` (406 строк против наших 174):
- `PrioCh chan []byte` в `WorkerSlot` (`prioBuf = 32`)
- `prioThreshold = 128` — пакеты ≤128 байт (TCP ACK) через `PrioCh`, с фолбэком на
  любой свободный `PrioCh`, и только потом обычная очередь
- `chunkSizeFor(pktSize)`: >1100 → 64, ≥701 → 24, ≥301 → 8, ≥101 → 3, иначе 1
  (вместо нашего `chunkSize = 12`)
- `maxDwellMS = 15` — не держать chunk на тормозящем relay дольше 15 мс
- сброс chunk при паузе >10 мс между пакетами (`lastPktTime`)
- **Не переносить:** их `tunFile`/`AttachTUN`-диагностику пока можно отложить до 4.4,
  но лучше переносить диспетчер сразу с поддержкой `tunFile` (нужен для RAW в 4.4) —
  тогда сразу пишем `d.tunFile != nil` ветки.

**б) Session (writer/reader):** `core/session.go`:
- создание слота: `PrioCh: make(chan []byte, prioBuf)` (строка ~421)
- writer-горутина (строка ~454): читает `PrioCh` **первым** (nested select как в
  `go_client/session.go` строки 608–620), осушает обе очереди при выходе (defer drain)
- **дедлайн записи 3 с вместо `sessionReadTimeout` (30 мин)** — см. 4.2
- keepalive — см. 4.2

**в) Распределение по relay'ям:** `core/worker_group.go`, строка ~274:
`turnAddr := available[0]` → брать по кругу `available[sessionID % len(available)]`
(важно: из **незабаненных**, `GlobalBlacklist` сохранить!).
В `RunSession` адрес приходит параметром — распределение делаем в группе.

**г) Быстрый старт + allocate-gate:** `core/worker_group.go`:
- сейчас: `firstBatchSize=5`, `workerStartDelay=200ms`, `workerBatchDelay=5s` (вторая волна
  через 6 секунд!) — заменить на линейную задержку `i * 75ms` (как
  `go_client/group.go` строка 138: `workerDelay := time.Duration(i) * 75 * time.Millisecond`)
- каскад групп: `signalCreds` спит 2 с → заменить на `500 + rand(250)` мс
  (`go_client/group.go` строки 115–122) — **сигнал следующей группе НЕ должен зависеть от
  GetCreds** (наш текущий код это уже умеет через отдельную горутину — сохранить это свойство!)
- **allocate-gate:** `allocateGateInterval = 100 * time.Millisecond`, тикер на группу,
  передаётся в `RunSession` и там ждёт тик перед аллокацией TURN (`go_client/session.go`
  строки 246–248) — это компенсирует потерю 5-секундной паузы (защита от квоты 486).
  Сигнатура `RunSession` расширится параметром `<-chan time.Time`.

#### 4.2 Надёжность

- **Дедлайн записи:** `core/session.go` writer: `SetWriteDeadline(now + 3*time.Second)`
  вместо `sessionReadTimeout` (комментарий из `go_client/session.go` строки 624–629 объясняет:
  зависшая запись держала воркера полчаса, пока очередь копилась и дропалась).
- **Keepalive через PrioCh:** вместо прямой записи в `dtlsConn` из отдельной горутины —
  класть пакет в `slot.PrioCh` (единый writer, порядок гарантирован). Размер
  `keepaliveMinSize=25 + rand(20)`, внутри `deviceID` (первые 16 байт после байта-маркера),
  интервал 10 с (у нас 15 с — взять их 10 с). Реализация:
  `go_client/session.go` строки 549–581.

#### 4.3 DoH + replay-window

- **DoH:** перенести `proxy-turn-vk-android/go_client/doh.go` (308 строк) в `core/`
  (переименовать вызовы, у них `package main`). Заменить `core/dns.go`
  `setupGlobalResolver()` → их `setupGlobalResolver(arg)` c выбором:
  `yandex|cloudflare|google|doh-yandex|doh-cloudflare|doh-google|custom:IP|doh:URL`.
  Новая настройка `AppSettings.DoH string` (default `"yandex"` — сохраняет текущее поведение)
  + CLI `pwdtt settings set doh doh-cloudflare`. Зависимость `golang.org/x/net/http2` — уже
  есть в go.mod (проверить).
- **Replay-window:** в `core/obfs.go` перенести `replayWindow` из
  `go_client/obfs.go` строки 34–93 (`replayWindowSpan`, `replayWindowMaxEntries = 8192`,
  метод `accept(wire)`), вызывать в `obfsUnwrapPacket`. Нужно состояние на сессию —
  см. как у них инстанс создаётся на приём. У нас `obfsUnwrapPacket` — свободная функция,
  state придётся повесить на сессию (параметр или объект `ObfsState`).

#### 4.4 RAW и NoDTLS (сервер поддерживает!)

- **NoDTLS (`--notls`):** `TurnParams.NoDTLS bool` — пропустить DTLS-хендшейк, писать
  RTP-obfs AEAD напрямую поверх TURN-relay. Референс: `go_client/session.go` (ветки `tp.NoDTLS`)
  и `TurnParams` в `go_client/group.go` строки 313–316. Прокинуть из CLI-флага в `core.Config`.
  Требует сервер с `-listen-direct` (у пользователя уже включено).
- **RAW (`--raw`):** режим `-mode rawtun` — без WireGuard-устройства, сырые IP-пакеты:
  - сервер шлёт конфиг строкой `RAWCONF:<ip>|<dns>|<mtu>` вместо WG-конфига
    (обработка: `go_client/main.go` строки 389–430)
  - диспетчер читает/пишет напрямую в TUN-fd (`tunFile`, `AttachTUN` — уже перенесены в 4.1)
  - на ПК: создавать wintun-интерфейс **без** WG-устройства (`device.NewDevice` не вызывать),
    назначить IP/маршруты как в `applyWindows`, но читать пакеты из TUN-файла в диспетчер
  - реализация: новый файл `backend/wg_raw.go` + ветка в `wg.Apply` по policy/флагу
  - интерфейс конфига: `ConnectParams.Raw bool` / `NoDTLS bool`
- Оба режима — под флагами CLI (`pwdtt connect --raw`, `--notls`), по умолчанию выключены.

#### 4.5 CLI-фичи (зависят от Этапа 2)

- `pwdtt servers check <имя>` → `runHashChecks` из `go_client/main.go` строки 59–107
  (статусы ok/captcha/dead/limited/network; у нас это печать в stdout, не формат `HASH_CHECK|`)
- `pwdtt ping [имя]` → `RunPing` из `go_client/session.go` (функцию перенести в core,
  флаг `-ping-only` логика: `go_client/main.go` строки 285–305)
- `pwdtt pause` / `pwdtt resume` → у `core.Core` уже есть `Pause()/Resume()`, но `Bridge`
  их не отдаёт — добавить методы `Bridge.Pause()/Resume()` и вызовы из CLI
  (пауза воркеров через `pauseFlag`)

**Приёмка Этапа 4:**
- `go test ./...` зелёный; **прогнать бенчмарки до и после 4.1**: `go test -bench=. ./tests/`
  (файл `tests/benchmarks_test.go`) — сравнить, деградации нет
- Новые тесты: `chunkSizeFor`, replay-window (повтор отбрасывается), DoH-парсер
- Ручная проверка на Windows: `pwdtt connect` → `route print`, скорость/отзывчивость
  сравнить с предыдущей версией (скачивание большого файла, `speedtest`)

---

### Этап 5 — Тесты, CI, README

1. **Тесты:** новые по Этапам 3–4 (BuildRoutePlan, SplitRoutes-нормализация, резолв-кэш,
   chunkSizeFor, replay, DoH). Старые 78 — зелёные. Фронтенд-тесты (`frontend/src/__tests__`)
   удалены вместе с frontend.
2. **CI `build.yml`:** убрать шаги Node/Wails/webkit →
   ```yaml
   - run: go test ./...
   - run: go build -o pwdtt-linux-amd64        # linux job
   - run: GOOS=windows GOARCH=amd64 go build -o pwdtt-windows-amd64.exe
   # macOS: go build для amd64+arm64 и lipo -create → universal
   ```
   Для Windows — шаг `go-winres make` (манифест админа, см. Этап 1).
   `release.yml` — имена артефактов сохранить (грузит из build.yml).
3. **README.md:** разделы запуска/сборки (`go build` вместо `wails build`), описание всех
   команд CLI, раздел «Маршрутизация: весь / только список» (`route`, `ips`), флаги
   `--raw/--notls`, настройка DoH. Убрать упоминания Wails/Node из зависимостей.
4. Финальный чек-лист: `go vet ./...`, `go build` для 3 ОС, `go test ./...`.

---

## 5. Быстрый справочник по коду Android (для переноса)

| Что | Файл qWDTT | Строки |
|---|---|---|
| Диспетчер (PrioCh, chunk, dwell) | `go_client/dispatcher.go` | весь файл |
| Writer с PrioCh + drain + deadline 3с | `go_client/session.go` | 583–645 |
| Keepalive через PrioCh | `go_client/session.go` | 549–581 |
| allocate-gate в RunSession | `go_client/session.go` | 170, 246–248 |
| Старт воркеров 75мс + каскад 500мс | `go_client/group.go` | 114–149 |
| Распределение sessionID % n | `go_client/session.go` | 181 |
| DoH | `go_client/doh.go` | весь файл |
| Replay-window | `go_client/obfs.go` | 34–93 |
| RAWCONF + AttachTUN | `go_client/main.go` | 389–430 |
| Check-hashes | `go_client/main.go` | 59–107 |
| Ping-only | `go_client/main.go` | 285–305 |
| TurnParams (NoDTLS/RawMode) | `go_client/group.go` | 306–325 |
| Bypass-резолв с кэшем (референс) | `app/.../settings/BypassRoutes.kt` | 289–330 |

## 6. Команды

```bash
go build ./...                       # проверка компиляции
go test ./...                        # все тесты (пакет tests/ = backend_test)
go test -bench=. -benchtime=1s ./tests/   # бенчмарки (до/после этапа 4.1)
GOOS=windows GOARCH=amd64 go build -o pwdtt.exe   # кросс-сборка (Go в окружении 32-битный!)
GOOS=linux  GOARCH=amd64 go build -o pwdtt-linux
```
