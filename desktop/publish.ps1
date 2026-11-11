# Выпуск ПК-версии в корень репозитория (как android/publish_apk.ps1 для APK):
#   VPN_routing-setup.exe          — установщик для Windows (меню «Пуск», «Параметры → Приложения»)
#   VPN_routing-windows.zip        — vpnrouting.exe + wintun.dll (без установки; по нему же самообновление)
#   VPN_routing-linux-amd64.tar.gz — vpnrouting для x86-64
#   desktop-version.json           — по нему приложение узнаёт о новой версии и проверяет скачанный файл
#
# Использование:
#   powershell -ExecutionPolicy Bypass -File desktop\publish.ps1 -Notes "Что нового"
#
# Перед выпуском увеличьте VersionCode (и VersionName) в desktop/buildinfo/buildinfo.go:
# приложение предлагает обновление, только если VersionCode больше установленного.

param([string]$Notes = '', [switch]$SkipTests)

$ErrorActionPreference = 'Stop'
if (Test-Path 'F:\Android\go\bin\go.exe') { $env:Path = "F:\Android\go\bin;$env:Path" }
Set-Location $PSScriptRoot

& (Join-Path $PSScriptRoot 'build.ps1') -SkipTests:$SkipTests
if ($LASTEXITCODE -and $LASTEXITCODE -ne 0) { throw 'Сборка не удалась' }

go run ./tools/pack -dist dist -out .. -notes $Notes
if ($LASTEXITCODE -ne 0) { throw 'Упаковка не удалась' }
'Дальше: git add VPN_routing-setup.exe VPN_routing-*.zip VPN_routing-*.tar.gz desktop-version.json, commit, push'
