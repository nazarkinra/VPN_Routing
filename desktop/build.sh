#!/bin/sh
# Сборка ПК-версии на Linux (без WSL): то же, что build-wsl.sh — dist/linux-amd64/vpnrouting
# и dist/windows/vpnrouting.exe. Нужны компилятор C, библиотеки X11/OpenGL и mingw-w64 (см. build-wsl.sh).
set -e
cd "$(dirname "$0")"
[ "$1" = "--skip-tests" ] || go test ./app ./api ./bmstuvpn ./core ./netenv
exec bash ./build-wsl.sh dist
