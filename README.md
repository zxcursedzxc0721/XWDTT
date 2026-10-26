<p align="center">
  <img src="assets/icons/icon.png" width="96" />
</p>

<h1 align="center">XWDTT</h1>

<p align="center">
  Консольный VPN-клиент, который туннелирует трафик через TURN-серверы VK,<br>
  маскируя соединение под зашифрованный медиатрафик звонка.<br>
  <sub>Форк <a href="https://github.com/amurcanov/proxy-turn-vk-android">proxy-turn-vk-android</a> — версия для ПК</sub>
</p>

<p align="center">
  <a href="https://github.com/zxcursedzxc0721/XWDTT/actions/workflows/build.yml"><img src="https://img.shields.io/github/actions/workflow/status/zxcursedzxc0721/XWDTT/build.yml?branch=main&style=for-the-badge&label=build" alt="Build"></a>
  <a href="https://github.com/zxcursedzxc0721/XWDTT/releases/latest"><img src="https://img.shields.io/github/v/release/zxcursedzxc0721/XWDTT?style=for-the-badge&label=release" alt="Release"></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/github/license/zxcursedzxc0721/XWDTT?style=for-the-badge&label=license&color=blue" alt="License">
</p>

<p align="center">
  <a href="https://github.com/zxcursedzxc0721/XWDTT/releases/latest"><b>⬇ Скачать XWDTT</b></a>
</p>

---

## Возможности

- 🎭 **Маскировка под звонок** — трафик оборачивается в RTP с ChaCha20-Poly1305, для провайдера это выглядит как зашифрованный медиапоток VK
- 🚀 **Режимы маршрутизации** — `full` (весь трафик) или `split` (только выбранные IP/домены, до 65536 записей)
- 🔒 **WireGuard + raw-режим** — драйвер wintun встроен в бинарник; режим `-raw` работает без WireGuard
- 🛡 **Защита от утечек** — автообход петли TURN-адресов, перехват IPv6 в full-режиме, DNS-over-HTTPS
- 🌐 **Автопереключение** — при недоступности TURN транспорт пересоздаётся, сессии восстанавливаются
- 📋 **Импорт ссылок** — `wdtt://` и `qwdtt`-ссылки, интерактивный режим, автостарт
- 🖥 **Кроссплатформенность** — Windows / Linux / macOS (Intel и Apple Silicon)

## Как это работает

Приложение поднимает локальный WireGuard-интерфейс и передаёт его трафик через TURN/DTLS серверы VK, оборачивая пакеты в RTP с шифрованием ChaCha20-Poly1305. С точки зрения провайдера — это обычный зашифрованный VK-звонок.

```
Приложение → WireGuard → ChaCha20/RTP → VK TURN/DTLS → wdtt-server (VPS) → интернет
```

---

## Запуск

### Linux

**1. Права для WireGuard:**

Приложению нужны права root для управления сетевым интерфейсом. Установите sudoers-правило:

```bash
# Одной командой (скачает и запустит):
sudo bash <(curl -s https://raw.githubusercontent.com/zxcursedzxc0721/XWDTT/main/assets/install-sudoers.sh)
```

Или вручную через `visudo` — добавьте в `/etc/sudoers`:
```
your_user ALL=(ALL) NOPASSWD: /usr/bin/ip, /usr/bin/wg
```

Скрипт автоматически определит текущего пользователя и создаст файл `/etc/sudoers.d/xwdtt`.

**2. Запустите:**

```bash
chmod +x xwdtt-linux-amd64
./xwdtt-linux-amd64
```

### Windows

Скачайте `xwdtt-windows-amd64.exe` из [Releases](https://github.com/zxcursedzxc0721/XWDTT/releases) и запустите. Драйвер WireGuard (wintun) встроен. Права администратора запрашиваются автоматически (UAC) при подключении.

### macOS

Скачайте `XWDTT-macos.zip` из [Releases](https://github.com/zxcursedzxc0721/XWDTT/releases), распакуйте и запустите бинарник. При первом запуске macOS запросит разрешение на создание сетевого интерфейса — введите пароль администратора.

> Universal бинарник: работает на Intel и Apple Silicon (M1/M2/M3/M4).

---

## Справка по CLI

> Полный список команд с флагами, кодами выхода и сценариями проверок — в [COMMANDS.md](COMMANDS.md).

Запуск без аргументов открывает **интерактивный режим**:

```bash
./xwdtt
```

### Команды

| Команда | Описание |
|---|---|
| `xwdtt` | интерактивный промпт (все те же команды + `stats`, `logs`, `quit`) |
| `xwdtt connect [флаги] <имя\|wdtt://ссылка>` | подключить туннель (Ctrl+C — отключить) |
| `xwdtt ping [имя]` | замер RTT до TURN-сервера, без поднятия туннеля |
| `xwdtt pause` / `xwdtt resume` | пауза / продолжить (в интерактиве; из отдельного процесса недоступны — туннель живёт в своём процессе) |
| `xwdtt route [full\|split]` | режим маршрутизации (см. ниже) |
| `xwdtt ips list \| add ...\| rm ...` | список адресов split-режима |
| `xwdtt servers list\|add\|show\|rm\|set-hashes\|check\|edit` | управление серверами |
| `xwdtt settings get\|set <ключ> <значение>` | настройки |
| `xwdtt update` / `xwdtt report` / `xwdtt version` | обновление / отчёт / версия |

### Флаги `connect`

```
-workers N          число воркеров (по умолчанию: max(9, хеши*9))
-obfs audio|video   режим обфускации
-fingerprint ИМЯ    chrome|firefox|safari|android|ios
-turn-tcp           TCP-транспорт TURN
-notls              без DTLS: RTP-obfs напрямую поверх TURN
                    (сервер с -listen-direct)
-raw                raw-IP без WireGuard (сервер с -listen-raw)
```

Примеры:
```bash
xwdtt connect "Мой сервер"
xwdtt connect "wdtt://1.2.3.4:8443:51820:1080:пароль#Мой сервер" -workers 27
xwdtt connect "Мой сервер" -raw -notls
```

### Ссылки

```
wdtt://<IP>:<DTLS_PORT>:<WG_PORT>:<PROXY_PORT>:<PASSWORD>[:<HASH1>,<HASH2>,...][#название]
```

- Поля 1–5 обязательны
- Хеши — опциональны, через запятую, до 4 штук
- `#название` — опциональный псевдоним сервера

Пример:
```
wdtt://1.2.3.4:56000:56001:0:mypassword:AbCdEfGh,XyZ12345#Мой сервер
```

Ссылку можно передать аргументом команды `connect`. Также принимются ссылки qwdtt.

---

## Маршрутизация: весь трафик или только список

Режим выбирается командой `route` (хранится в настройках):

```bash
xwdtt route          # показать текущий режим
xwdtt route full     # через VPN — весь трафик (по умолчанию)
xwdtt route split    # через VPN — только адреса из списка
```

В режиме `split` трафик идёт через туннель **только** к адресам из списка, остальное — напрямую. Список общий для всех серверов:

```bash
xwdtt ips list
xwdtt ips add 1.2.3.4          # IP
xwdtt ips add 10.0.0.0/24      # CIDR
xwdtt ips add vk.com           # домен — резолвится с кэшем (10 мин)
xwdtt ips rm vk.com
```

Ограничение: до 65536 записей в списке. Домены резолвятся при подключении и кэшируются в `dnscache.json`; недоступный домен при наличии старого кэша использует его.

TURN/VK/DNS-адреса в режиме `full` идут мимо туннеля автоматически — петля исключена.

---

## Настройки

```bash
xwdtt settings get
xwdtt settings set autostart on|off     # автозапуск
xwdtt settings set obfs audio|video     # режим обфускации
xwdtt settings set turn-tcp on|off      # TCP-транспорт TURN
xwdtt settings set workers N            # воркеров (0 = авто)
xwdtt settings set fingerprint ИМЯ      # chrome|firefox|safari|android|ios
xwdtt settings set doh РЕЖИМ            # DNS-over-HTTPS (см. ниже)
```

### DNS over HTTPS

Обычный DNS-резолв утекает провайдеру — кто вы посещаете. DoH шлёт DNS-запросы по HTTPS уже внутри туннеля:

```bash
xwdtt settings set doh yandex      # по умолчанию
xwdtt settings set doh cloudflare
xwdtt settings set doh google
xwdtt settings set doh doh:URL      # свой DoH-провайдер
xwdtt settings set doh custom:IP    # свой обычный DNS-сервер
```

Доступны также режимы `doh-yandex` / `doh-cloudflare` / `doh-google` (то же самое, явная запись).

---

## Проверка сервера и хешей

```bash
xwdtt servers check <имя>   # проверить VK-хеши: ok/captcha/dead/limited/network
xwdtt ping [имя]            # RTT до TURN-сервера (аллокация + хендшейк)
```

---

## Сборка из исходников

**Зависимости:** только Go 1.26+. Node.js, Wails и webkit больше не нужны — интерфейс полностью консольный.

```bash
git clone https://github.com/zxcursedzxc0721/XWDTT
cd XWDTT

go build ./...                # проверка компиляции
go test ./...                 # тесты (core: cd core && go test ./...)

go build -o xwdtt             # нативная сборка
```

Кросс-сборка:

```bash
GOOS=windows GOARCH=amd64 go build -o xwdtt-windows-amd64.exe
GOOS=linux   GOARCH=amd64 go build -o xwdtt-linux-amd64
GOOS=darwin  GOARCH=arm64 go build -o xwdtt-macos-arm64
```

> `assets/wintun.dll` встраивается в Windows-бинарник автоматически (`//go:embed`).

CI (`.github/workflows/build.yml`) собирает Linux/Windows/macOS артефакты обычным `go build`; `release.yml` публикует их в [Releases](https://github.com/zxcursedzxc0721/XWDTT/releases) по тегам `v*` — каждый релиз содержит бинарники трёх платформ и файл `SHA256SUMS` для проверки целостности.

---

## Отчёт об ошибках

Соберите отчёт командой:

```bash
xwdtt report
```

Вывод содержит версию ОС, версию приложения, состояние настроек и логи текущей сессии — скопируйте его в [Issue](https://github.com/zxcursedzxc0721/XWDTT/issues/new).

---

> [!IMPORTANT]
> Приложение является техническим инструментом для защищённого туннелирования собственного трафика через ваш сервер. Автор не призывает использовать XWDTT для противоправных целей или нарушения правил сторонних сервисов.

---

## Лицензия

Этот проект распространяется под лицензией GNU General Public License v3.0.
