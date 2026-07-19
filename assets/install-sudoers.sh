#!/bin/bash
# Установка sudoers-правила для pwdtt
# Запускать от рута: sudo bash install-sudoers.sh

if [ "$(id -u)" -ne 0 ]; then
    echo "Запускайте от root: sudo bash $0"
    exit 1
fi

USER=$(logname 2>/dev/null || whoami)

cat > /etc/sudoers.d/pwdtt << EOF
${USER} ALL=(ALL) NOPASSWD: /usr/bin/ip, /usr/bin/wg
EOF

chmod 440 /etc/sudoers.d/pwdtt
echo "Правило установлено для пользователя: ${USER}"
echo "Проверка: sudo -n ip link show lo"
