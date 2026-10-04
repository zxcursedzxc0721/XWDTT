<p align="center">
  <img src="assets/icons/icon.png" width="96" />
</p>

<h1 align="center">PWDTT</h1>

<p align="center">
  Консольный VPN-клиент, который туннелирует трафик через TURN-серверы VK,<br>
  маскируя соединение под зашифрованный медиатрафик звонка.<br>
  <sub>Форк <a href="https://github.com/amurcanov/proxy-turn-vk-android">proxy-turn-vk-android</a> — версия для ПК</sub>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Linux-amd64-FCC624?style=for-the-badge&logo=linux&logoColor=black" alt="Linux">
  <img src="https://img.shields.io/badge/Windows-amd64-0078D4?style=for-the-badge&logo=windows&logoColor=white" alt="Windows">
  <img src="https://img.shields.io/badge/macOS-Universal-000000?style=for-the-badge&logo=apple&logoColor=white" alt="macOS">
</p>

---

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
sudo bash <(curl -s https://raw.githubusercontent.com/luminescq/PWDTT/main/assets/install-sudoers.sh)
```

Или вручную через `visudo` — добавьте в `/etc/sudoers`:
```
your_user ALL=(ALL) NOPASSWD: /usr/bin/ip, /usr/bin/wg
```

Скрипт автоматически определит текущего пользователя и создаст файл `/etc/sudoers.d/pwdtt`.

**2. Запустите:**

```bash
chmod +x pwdtt-linux-amd64
./pwdtt-linux-amd64
```

### Windows

Скачайте `pwdtt-windows-amd64.exe` из [Releases](https://github.com/luminescq/PWDTT/releases) и запустите. Драйвер WireGuard (wintun) встроен. Права администратора запрашиваются автоматически (UAC) при подключении.

### macOS

Скачайте `PWDTT-macos.zip` из [Releases](https://github.com/luminescq/PWDTT/releases), распакуйте и запустите бинарник. При первом запуске macOS запросит разрешение на создание сетевого интерфейса — введите пароль администратора.

> Universal бинарник: работает на Intel и Apple Silicon (M1/M2/M3/M4).

---

## Справка по CLI

Запуск без аргументов открывает **интерактивный режим**:

```bash
./pwdtt
```

### Команды

| Команда | Описание |
|---|---|
| `pwdtt` | интерактивный промпт (все те же команды + `stats`, `logs`, `quit`) |
| `pwdtt connect [флаги] <имя\|wdtt://ссылка>` | подключить туннель (Ctrl+C — отключить) |
| `pwdtt ping [имя]` | замер RTT до TURN-сервера, без поднятия туннеля |
| `pwdtt pause` / `pwdtt resume` | пауза / продолжить (в интерактиве; из отдельного процесса недоступны — туннель живёт в своём процессе) |
| `pwdtt route [full\|split]` | режим маршрутизации (см. ниже) |
| `pwdtt ips list \| add ...\| rm ...` | список адресов split-режима |
| `pwdtt servers list\|add\|show\|rm\|set-hashes\|check\|edit` | управление серверами |
| `pwdtt settings get\|set <ключ> <значение>` | настройки |
| `pwdtt update` / `pwdtt report` / `pwdtt version` | обновление / отчёт / версия |

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
pwdtt connect "Мой сервер"
pwdtt connect "wdtt://1.2.3.4:8443:51820:1080:пароль#Мой сервер" -workers 27
pwdtt connect "Мой сервер" -raw -notls
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
pwdtt route          # показать текущий режим
pwdtt route full     # через VPN — весь трафик (по умолчанию)
pwdtt route split    # через VPN — только адреса из списка
```

В режиме `split` трафик идёт через туннель **только** к адресам из списка, остальное — напрямую. Список общий для всех серверов:

```bash
pwdtt ips list
pwdtt ips add 1.2.3.4          # IP
pwdtt ips add 10.0.0.0/24      # CIDR
pwdtt ips add vk.com           # домен — резолвится с кэшем (10 мин)
pwdtt ips rm vk.com
```

Ограничения: 100 записей, 400 маршрутов. Домены резолвятся при подключении и кэшируются в `dnscache.json`; недоступный домен при наличии старого кэша использует его.

TURN/VK/DNS-адреса в режиме `full` идут мимо туннеля автоматически — петля исключена.

---

## Настройки

```bash
pwdtt settings get
pwdtt settings set autostart on|off     # автозапуск
pwdtt settings set obfs audio|video     # режим обфускации
pwdtt settings set turn-tcp on|off      # TCP-транспорт TURN
pwdtt settings set workers N            # воркеров (0 = авто)
pwdtt settings set fingerprint ИМЯ      # chrome|firefox|safari|android|ios
pwdtt settings set doh РЕЖИМ            # DNS-over-HTTPS (см. ниже)
```

### DNS over HTTPS

Обычный DNS-резолв утекает провайдеру — кто вы посещаете. DoH шлёт DNS-запросы по HTTPS уже внутри туннеля:

```bash
pwdtt settings set doh yandex      # по умолчанию
pwdtt settings set doh cloudflare
pwdtt settings set doh google
pwdtt settings set doh doh:URL      # свой DoH-провайдер
pwdtt settings set doh custom:IP    # свой обычный DNS-сервер
```

Доступны также режимы `doh-yandex` / `doh-cloudflare` / `doh-google` (то же самое, явная запись).

---

## Проверка сервера и хешей

```bash
pwdtt servers check <имя>   # проверить VK-хеши: ok/captcha/dead/limited/network
pwdtt ping [имя]            # RTT до TURN-сервера (аллокация + хендшейк)
```

---

## Сборка из исходников

**Зависимости:** только Go 1.26+. Node.js, Wails и webkit больше не нужны — интерфейс полностью консольный.

```bash
git clone https://github.com/luminescq/PWDTT
cd PWDTT

go build ./...                # проверка компиляции
go test ./...                 # тесты (core: cd core && go test ./...)

go build -o pwdtt             # нативная сборка
```

Кросс-сборка:

```bash
GOOS=windows GOARCH=amd64 go build -o pwdtt-windows-amd64.exe
GOOS=linux   GOARCH=amd64 go build -o pwdtt-linux-amd64
GOOS=darwin  GOARCH=arm64 go build -o pwdtt-macos-arm64
```

> `assets/wintun.dll` встраивается в Windows-бинарник автоматически (`//go:embed`).

CI (`.github/workflows/build.yml`) собирает Linux/Windows/macOS артефакты обычным `go build`; `release.yml` публикует их в Releases по тегам `v*`.

---

## Отчёт об ошибках

Соберите отчёт командой:

```bash
pwdtt report
```

Вывод содержит версию ОС, версию приложения, состояние настроек и логи текущей сессии — скопируйте его в [Issue](https://github.com/luminescq/PWDTT/issues/new).

---

> [!IMPORTANT]
> Приложение является техническим инструментом для защищённого туннелирования собственного трафика через ваш сервер. Автор не призывает использовать PWDTT для противоправных целей или нарушения правил сторонних сервисов.

---

## Лицензия

Этот проект распространяется под лицензией GNU General Public License v3.0.
