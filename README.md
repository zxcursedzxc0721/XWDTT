<p align="center">
  <img src="assets/icons/icon.png" width="96" />
</p>

<h1 align="center">PWDTT</h1>

<p align="center">
  Десктопный VPN-клиент, который туннелирует трафик через TURN-серверы VK,<br>
  маскируя соединение под зашифрованный медиатрафик звонка.<br>
  <sub>Форк <a href="https://github.com/amurcanov/proxy-turn-vk-android">proxy-turn-vk-android</a> — версия для ПК</sub>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Wails-v2-red?style=for-the-badge&logo=wails&logoColor=white" alt="Wails">
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

**1. Установите зависимости:**

```bash
# Ubuntu/Debian
sudo apt install wireguard-tools libwebkit2gtk-4.1-dev

# Arch
sudo pacman -S wireguard-tools webkit2gtk-4.1
```

**2. Настройте права для WireGuard:**

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

**3. Запустите:**

```bash
chmod +x pwdtt-linux-amd64
./pwdtt-linux-amd64
```

### Windows

Скачайте `pwdtt-windows-amd64.exe` из [Releases](https://github.com/luminescq/PWDTT/releases) и запустите. Драйвер WireGuard (wintun) встроен.

### macOS

Скачайте `PWDTT-macos.zip` из [Releases](https://github.com/luminescq/PWDTT/releases), распакуйте и запустите `PWDTT.app`. При первом запуске macOS запросит разрешение на создание сетевого интерфейса — введите пароль администратора.

> Universal бинарник: работает на Intel и Apple Silicon (M1/M2/M3/M4).

---

## Быстрый старт

1. **Добавьте сервер** — кнопка `+` → вставьте `wdtt://`-ссылку или введите вручную
2. **VK-хеши** — Настройки → вставьте хеши из `vk.com/call/join/<hash>`
3. **Подключение** — кнопка питания

---

## Ссылки

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

Вставить ссылку можно через кнопку `+` или просто **Ctrl+V** в любом месте окна.

> Также принимаються ссылки qwdtt.

---

## Сборка из исходников

**Зависимости:** Go 1.26+, Node.js 22+, [Wails v2](https://wails.io)

```bash
# Linux
sudo apt install libayatana-appindicator3-dev pkg-config gcc libwebkit2gtk-4.1-dev
go install github.com/wailsapp/wails/v2/cmd/wails@latest

git clone https://github.com/luminescq/PWDTT
cd PWDTT
wails build -platform linux/amd64 -tags webkit2_41 -o pwdtt-linux-amd64
# → build/bin/pwdtt-linux-amd64
```

```bash
# Windows (кросс-компиляция с Linux)
wails build -platform windows/amd64
# → build/bin/pwdtt.exe
```

```bash
# macOS (только на macOS)
wails build -platform darwin/universal
# → build/bin/pwdtt-macos
```

---

## Отчёт об ошибках

Если приложение работает некорректно, вы можете собрать отчёт прямо из интерфейса:

1. Откройте **Настройки** (шестерёнка вверху)
2. Нажмите **Отчёт** — информация о системе и логах сессии скопируется в буфер обмена
3. Создайте [Issue](https://github.com/luminescq/PWDTT/issues/new) и вставьте отчёт

Отчёт содержит: версию ОС, версию Go, версию приложения, имя хоста и.filtered логи текущей сессии.

---

> [!IMPORTANT]
> Приложение является техническим инструментом для защищённого туннелирования собственного трафика через ваш сервер. Автор не призывает использовать PWDTT для противоправных целей или нарушения правил сторонних сервисов.

---

## Лицензия

Этот проект распространяется под лицензией GNU General Public License v3.0.
