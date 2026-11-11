#!/bin/sh
# Установка VPN routing: программа в /usr/local/bin, ярлык и значок в меню приложений (раздел «Сеть»),
# служба systemd vpnrouting.service (запускается сейчас и при каждой загрузке).
#   sudo ./install.sh
set -e
cd "$(dirname "$0")"
if [ "$(id -u)" -ne 0 ]; then
    exec sudo "$0" "$@"
fi
chmod +x ./vpnrouting
exec ./vpnrouting install "$@"
