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
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -buildid=" -o "$OUT/linux-amd64/vpnrouting" .
echo "  $OUT/linux-amd64/vpnrouting"
"${WIN[@]}" go build -trimpath -ldflags "-s -w -buildid= -H windowsgui" -o "$OUT/windows/vpnrouting.exe" .
echo "  $OUT/windows/vpnrouting.exe"
