#!/bin/sh
# Удаление VPN routing. Настройки (/var/lib/vpnrouting) остаются; с ключом --purge удаляются и они.
#   sudo ./uninstall.sh [--purge]
set -e
if [ "$(id -u)" -ne 0 ]; then
    exec sudo "$0" "$@"
fi
if [ -x /usr/local/bin/vpnrouting ]; then
    exec /usr/local/bin/vpnrouting uninstall "$@"
fi
cd "$(dirname "$0")"
exec ./vpnrouting uninstall "$@"
