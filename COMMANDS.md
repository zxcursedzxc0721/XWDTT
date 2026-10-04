# PWDTT — справочник по командам

Все команды CLI, их описание, флаги, коды выхода и типовые сценарии.

---

## Сборка

```powershell
cd F:\etc\PWDTT
$env:GOOS="windows"; $env:GOARCH="amd64"; go build -o "$env:TEMP\pw64.exe" .
```

> ⚠️ **Только `GOARCH=amd64`!** Локальный `go build` в 32-битном окружении даёт
> бинарник `windows/386`, который **несовместим с `assets/wintun.dll`** (amd64):
> при подключении упадёт с
> `create TUN: Error loading wintun.dll: %1 is not a valid Win32 application`.
> Релизы в CI собираются amd64 — пользователям это не грозит.

В дальнейшем примеры используют `$env:TEMP\pw64.exe` — подставь свой путь к бинарнику.

---

## Общие правила

| Правило | Пояснение |
|---|---|
| Права администратора | `connect` и интерактив запрашивают UAC при старте (после валидации аргументов) — `route`, `netsh`, wintun требуют прав. `ping`/`servers`/`settings`/`route`/`ips` прав не требуют |
| Процесс-модель | Туннель живёт **в своём процессе** (`connect` или интерактив). `pause`/`resume`/`disconnect` из другого процесса не работают |
| Ctrl+C | В окне `connect`: 1-й — аккуратное отключение (exit 0), 2-й — принудительно (exit 130) |
| Конфиги | `~/.config/pwdtt/` — `config.json`, `servers/*.json`, `dnscache.json`, `logs/` |

**Коды выхода:**

| Код | Значение |
|---|---|
| `0` | успех (для `connect` — туннель запущен; для `--help` — справка показана) |
| `1` | ошибка (нет профиля, сбой подключения, `pause`/`disconnect` из чужого процесса) |
| `2` | неверный аргумент / неизвестная подкоманда (печатается usage) |
| `130` | принудительный выход по двойному Ctrl+C |

---

## Справочные команды

| Команда | Описание |
|---|---|
| `pwdtt` | интерактивный режим (промпт `pwdtt>`) |
| `pwdtt help` | эта справка |
| `pwdtt version` | версия (`-v`, `--version`) |
| `pwdtt report` | отчёт для Issue: ОС, версия, настройки, логи сессии (печатает в stdout) |
| `pwdtt update` | проверить наличие обновления (печатает ссылку на скачивание) |

---

## `connect` — подключение туннеля

```text
pwdtt connect [флаги] <имя|wdtt://ссылка>
```

Поднимает TUN-интерфейс, применяет маршруты (по режиму `route`), запускает
воркеры. Блокирует терминал до Ctrl+C.

### Флаги

| Флаг | Описание |
|---|---|
| `-workers N` | число воркеров. Без флага: профиль → настройки → `max(9, хеши*9)` |
| `-obfs audio\|video` | режим обфускации (video требует `obfs-accepted on`) |
| `-fingerprint ИМЯ` | `chrome` / `firefox` / `safari` / `android` / `ios` |
| `-turn-tcp` | TCP-транспорт TURN вместо UDP |
| `-notls` | без DTLS: RTP-obfs напрямую поверх TURN (сервер с `-listen-direct`) |
| `-raw` | raw-IP без WireGuard: адресация `/32`, сервер с `-listen-raw` |

Флаги можно писать в любом порядке, до или после имени: `connect zxc --workers 18`.
`-h`/`--help` — справка, exit 0.

### Примеры

```powershell
pw64.exe connect "Мой сервер"
pw64.exe connect "wdtt://1.2.3.4:8443:51820:1080:пароль#Мой сервер" -workers 27
pw64.exe connect zxc -raw -notls
```

---

## `ping` — замер RTT (туннель НЕ поднимает)

```text
pwdtt ping [флаги] [имя]
```

Замеряет время отклика до TURN-сервера: аллокация + DTLS-хендшейк (или прямой
запрос в raw/`-notls` режиме). Полезно проверить сервер **до** подключения.

| Флаг | Описание |
|---|---|
| `-obfs audio\|video` | режим обфускации |
| `-turn-tcp` | TCP-транспорт TURN |
| `-notls` | прямой режим без DTLS |
| `-raw` | raw-режим (GETCONF_RAW) |

```powershell
pw64.exe ping zxc          # RTT: 318 мс (хеш 1 из 4)
pw64.exe ping -raw         # без профиля, прямой запрос
```

---

## `servers` — управление профилями серверов

```text
pwdtt servers list                                  # таблица: имя, адрес, хеши, TCP, воркеры
pwdtt servers add "wdtt://IP:ДТЛС:WG:ПРОКСИ:ПАРОЛЬ[:ХЕШИ][#название]"
pwdtt servers show <имя>                            # все поля профиля
pwdtt servers rm <имя>                              # удалить
pwdtt servers set-hashes <имя> h1,h2,...            # заменить хеши VK
pwdtt servers check <имя>                           # проверить хеши: ok/captcha/dead/limited/network
pwdtt servers edit <имя> [флаги]                    # правка полей
```

**`add` — upsert:** если профиль с тем же адресом и паролем уже есть, он
**обновляется** (хеши/порт/воркеры), дубль не создаётся. Без `#название`
имя генерируется: `Сервер 1`, `Сервер 2`, …

**Флаги `edit`:**

| Флаг | Поле |
|---|---|
| `-peer A` | адрес пира (DTLS) |
| `-password P` | пароль |
| `-name N` | **переименовать** профиль |
| `-listen L` | listen-адрес |
| `-turn H` / `-port P` | TURN-хост / порт |
| `-turn-tcp` | TCP-транспорт (булев) |
| `-workers N` | воркеры |

```powershell
pw64.exe servers add "wdtt://1.2.3.4:56000:56001:9000:пароль:хеш1,хеш2#Мой"
pw64.exe servers set-hashes zxc h1,h2,h3,h4
pw64.exe servers edit zxc -workers 27 -turn-tcp
pw64.exe servers check zxc
```

---

## `settings` — настройки (сохраняются в `config.json`)

```text
pwdtt settings get
pwdtt settings set <ключ> <значение>
```

| Ключ | Значения | Описание |
|---|---|---|
| `autostart` | `on\|off` | автозапуск при входе в систему |
| `doh` | `yandex` (по умолчанию) / `cloudflare` / `google` / `doh-yandex` / `doh-cloudflare` / `doh-google` / `doh:URL` / `custom:IP[,IP]` | DNS-over-HTTPS; пустое = `yandex` |
| `fingerprint` | `chrome\|firefox\|safari\|android\|ios` | TLS-отпечаток по умолчанию |
| `obfs` | `audio\|video` | режим обфускации |
| `obfs-accepted` | `on\|off` | принять предупреждение (нужно для video) |
| `turn-tcp` | `on\|off` | TCP-транспорт TURN |
| `workers` | целое `>= 0` | воркеры по умолчанию (`0` = авто) |

```powershell
pw64.exe settings get
pw64.exe settings set doh cloudflare
pw64.exe settings set workers 27
```

---

## `route` — режим маршрутизации

```text
pwdtt route           # показать текущий режим
pwdtt route full      # весь трафик через VPN (по умолчанию)
pwdtt route split     # через VPN только адреса из списка (ips)
```

Режим хранится в настройках и применяется при **следующем** подключении.
В `full` адреса TURN/VK/DNS идут мимо туннеля автоматически (петля исключена).
В `split` с пустым списком через VPN не пойдёт ничего — печатается предупреждение.

---

## `ips` — список адресов split-режима

```text
pwdtt ips list
pwdtt ips add <ip|cidr|домен>...      # можно несколько разом
pwdtt ips rm  <ip|cidr|домен>...
```

- Домены резолвятся **при подключении**, кэш в `dnscache.json` (10 минут);
  при сбое резолва используется старый кэш.
- Ограничения: **100 записей**, до **400 маршрутов**.
- Список глобальный (общий для всех серверов), хранится в `config.json`.

```powershell
pw64.exe ips add 1.2.3.4 10.0.0.0/24 vk.com
pw64.exe ips list
pw64.exe ips rm vk.com
```

---

## Управление живым туннелем

Из **отдельного процесса** не работают (exit 1): `pause`, `resume`, `disconnect`.
Работают только в интерактивном режиме, либо `Ctrl+C` в окне `connect`.

---

## Интерактивный режим (`pwdtt` без аргументов)

| Команда | Описание |
|---|---|
| `connect <имя\|ссылка> [флаги]` | подключить (промпт возвращается сразу) |
| `disconnect` | аккуратное отключение |
| `pause` / `resume` | остановить/возобновить воркеры (интерфейс поднят) |
| `servers ...` / `settings ...` / `route ...` / `ips ...` / `ping` | те же подкоманды, что и в CLI |
| `stats` | статистика сессии (пакеты/байты) |
| `logs` | последний лог-файл |
| `update` / `report` / `version` | обновление / отчёт / версия |
| `help` | справка |
| `quit` (`exit`) | выход (туннель отключится сам) |

`Ctrl+C`: 1-й — отключение и выход 0; 2-й — принудительно.

---

## Типовые сценарии

### Split-проверка

```powershell
pw64.exe ping zxc
pw64.exe route split
pw64.exe ips add 1.1.1.1
route print -4                                  # состояние ДО
Start-Process -FilePath "$env:TEMP\pw64.exe" -ArgumentList "connect","zxc"

# пока окно открыто:
Find-NetRoute -RemoteIPAddress 1.1.1.1 | Select InterfaceAlias   # → pwdtt-wg
Find-NetRoute -RemoteIPAddress 8.8.8.8 | Select InterfaceAlias   # → Ethernet
(Invoke-WebRequest https://api.ipify.org -UseBasicParsing).Content  # IP не изменился

# отключение: Ctrl+C в окне процесса
```

### Full-проверка

```powershell
pw64.exe route full
Start-Process -FilePath "$env:TEMP\pw64.exe" -ArgumentList "connect","zxc"

route print -4                                  # default теперь через pwdtt-wg
(Invoke-WebRequest https://api.ipify.org -UseBasicParsing).Content  # IP сервера
Test-Connection 8.8.8.8 -Count 3                # пинг через туннель

# отключение: Ctrl+C; если туннель мешает — второй Ctrl+C
```

### Аварийный откат

```powershell
Get-Process pw64 -ErrorAction SilentlyContinue | Stop-Process -Force
# wintun-адаптер удаляется вместе с процессом (проверено), мусора не остаётся:
Get-NetAdapter -IncludeHidden | Where-Object {$_.Name -like '*pwdtt*'}   # пусто = чисто
Get-NetRoute -DestinationPrefix '0.0.0.0/0'                              # default на Ethernet
```

### `--help` у подкоманд

```text
pwdtt connect --help    pwdtt ping --help    pwdtt servers --help
pwdtt settings --help   pwdtt route --help    pwdtt ips --help
```
Все возвращают exit **0**; неизвестная подкоманда — exit **2**.
