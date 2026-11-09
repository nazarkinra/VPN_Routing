# Сборка app/libs/libvpn.aar: ядро Xray (AndroidLibXrayLite v26.10.31) + клиент OpenVPN МГТУ (bmstuvpn).
# Оба пакета собираются одной командой: две отдельные gomobile-библиотеки в одном приложении конфликтуют
# (одинаковые libgojni.so и классы go.Seq).
#
# Нужны: Go 1.27+, gomobile/gobind (go install golang.org/x/mobile/cmd/gomobile@<версия из go.mod>),
# Android SDK, NDK и JDK. Пути ниже — значения по умолчанию на этой машине.

$ErrorActionPreference = 'Stop'
if (-not $env:ANDROID_HOME) { $env:ANDROID_HOME = 'F:\Android\SDK' }
if (-not $env:ANDROID_NDK_HOME) { $env:ANDROID_NDK_HOME = 'F:\Android\SDK\ndk\30.0.16248370' }
if (-not $env:JAVA_HOME) { $env:JAVA_HOME = 'C:\Program Files\Android\Android Studio\jre' }
$env:Path = "F:\Android\go\bin;$env:USERPROFILE\go\bin;$env:JAVA_HOME\bin;$env:Path"

Set-Location $PSScriptRoot
go test ./bmstuvpn
if ($LASTEXITCODE -ne 0) { throw 'go test failed' }

# -checklinkname=0: github.com/wlynxg/anet (зависимость Xray) обращается к внутренностям пакета net
$out = Join-Path $PSScriptRoot '..\app\libs\libvpn.aar'
gomobile bind -v `
    -target 'android/arm64,android/arm,android/amd64,android/386' `
    -androidapi 24 `
    -trimpath `
    -ldflags '-s -w -buildid= -checklinkname=0' `
    -o $out `
    github.com/2dust/AndroidLibXrayLite ./bmstuvpn
if ($LASTEXITCODE -ne 0) { throw 'gomobile bind failed' }

# Исходники Java-обёрток gomobile в проекте не нужны
Remove-Item (Join-Path $PSScriptRoot '..\app\libs\libvpn-sources.jar') -ErrorAction SilentlyContinue
Get-Item $out | Select-Object Name, Length
