#!/bin/bash
# Сборка программы с окном (Fyne, нужен компилятор C) — запускается в WSL из build.ps1:
#   bash desktop/build-wsl.sh [папка]        → [папка]/linux-amd64/vpnrouting и [папка]/windows/vpnrouting.exe
#   bash desktop/build-wsl.sh --vet          → только проверка кода (go vet) для Linux и Windows
# Нужны (один раз): sudo apt install build-essential pkg-config libgl1-mesa-dev xorg-dev libxkbcommon-dev
#   libwayland-dev gcc-mingw-w64-x86-64; Go 1.27+ (~/.local/go или в PATH).
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/.local/go/bin:$PATH"
export GOFLAGS=-buildvcs=false
WIN=(env CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc)

if [ "${1:-}" = "--vet" ]; then
    CGO_ENABLED=1 go vet ./...
    "${WIN[@]}" go vet ./...
    echo "vet ok"
    exit 0
fi

OUT="${1:-dist}"
mkdir -p "$OUT/linux-amd64" "$OUT/windows"
# Закрытый ключ зашифрованных ссылок vpnrouting://crypt1/ (core/linkcrypt.go): ../link-key.b64 (в .gitignore)
# или переменная VPNROUTING_LINK_KEY. Без ключа программа собирается, но такие ссылки не принимает.
LINK_KEY="${VPNROUTING_LINK_KEY:-}"
[ -z "$LINK_KEY" ] && [ -f ../link-key.b64 ] && LINK_KEY="$(cat ../link-key.b64)"
LINK_KEY="$(printf '%s' "$LINK_KEY" | tr -d ' \r\n\t')"
case "$LINK_KEY" in *[!A-Za-z0-9+/=]*) echo "link-key.b64: ожидается base64 одной строкой" >&2; exit 1 ;; esac
[ -z "$LINK_KEY" ] && echo "ВНИМАНИЕ: нет link-key.b64 — сборка без поддержки зашифрованных ссылок" >&2
KEYFLAG="-X vpnrouting/desktop/core.linkPrivateKey=$LINK_KEY"
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -buildid= $KEYFLAG" -o "$OUT/linux-amd64/vpnrouting" .
echo "  $OUT/linux-amd64/vpnrouting"
"${WIN[@]}" go build -trimpath -ldflags "-s -w -buildid= -H windowsgui $KEYFLAG" -o "$OUT/windows/vpnrouting.exe" .
echo "  $OUT/windows/vpnrouting.exe"
