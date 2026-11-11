# Сборка ПК-версии:
#   dist\VPN_routing-setup.exe — установщик для Windows (внутри vpnrouting.exe и wintun.dll)
#   dist\windows               — vpnrouting.exe + wintun.dll (переносная версия, архив для самообновления)
#   dist\linux-amd64           — vpnrouting
# Программа с окном (Fyne) собирается в WSL (build-wsl.sh): там нужны компилятор C и библиотеки X11/OpenGL,
# для Windows — кросс-компилятор mingw-w64. Установщик и упаковка — Go для Windows (F:\Android\go). Запуск:
#   powershell -ExecutionPolicy Bypass -File desktop\build.ps1
# Ключ -SkipTests пропускает тесты.

param([switch]$SkipTests)

$ErrorActionPreference = 'Stop'
if (Test-Path 'F:\Android\go\bin\go.exe') { $env:Path = "F:\Android\go\bin;$env:Path" }
Set-Location $PSScriptRoot

if (-not $SkipTests) {
    # Окно (gui) требует компилятор C — его код проверяет build-wsl.sh
    go test ./app ./api ./bmstuvpn ./core ./netenv ./install
    if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
}

$dist = Join-Path $PSScriptRoot 'dist'
New-Item -ItemType Directory -Force (Join-Path $dist 'windows') | Out-Null
$env:CGO_ENABLED = '0'

# Wintun — драйвер TUN для Windows от авторов WireGuard (https://www.wintun.net). Его DLL должна лежать
# рядом с vpnrouting.exe. Скачивается один раз, контрольная сумма проверяется.
$wintun = Join-Path $dist 'windows\wintun.dll'
if (-not (Test-Path $wintun)) {
    $version = '0.14.1'
    $sha256 = '07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51'
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("wintun-" + [Guid]::NewGuid())
    New-Item -ItemType Directory $tmp | Out-Null
    try {
        $zip = Join-Path $tmp 'wintun.zip'
        Write-Host "Загрузка Wintun $version…"
        Invoke-WebRequest "https://www.wintun.net/builds/wintun-$version.zip" -OutFile $zip -UseBasicParsing
        $hash = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
        if ($hash -ne $sha256) { throw "контрольная сумма wintun-$version.zip не совпала: $hash" }
        Expand-Archive $zip -DestinationPath $tmp
        Copy-Item (Join-Path $tmp 'wintun\bin\amd64\wintun.dll') $wintun
        Copy-Item (Join-Path $tmp 'wintun\LICENSE.txt') (Join-Path $dist 'windows\wintun-LICENSE.txt')
    } finally {
        Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Значок, версия и манифест exe (у установщика — запрос прав администратора)
Write-Host 'Ресурсы:'
go run ./tools/res -syso rsrc_windows_amd64.syso -desc 'VPN routing' -name vpnrouting.exe
if ($LASTEXITCODE -ne 0) { throw 'resources failed' }
go run ./tools/res -syso installer/rsrc_windows_amd64.syso -admin -desc 'Установка VPN routing' -name VPN_routing-setup.exe
if ($LASTEXITCODE -ne 0) { throw 'installer resources failed' }

function Build($goos, $goarch, $out, $ldExtra, $pkg = '.') {
    $env:GOOS = $goos; $env:GOARCH = $goarch
    go build -trimpath -ldflags "-s -w -buildid= $ldExtra" -o $out $pkg
    if ($LASTEXITCODE -ne 0) { throw "build $pkg $goos/$goarch failed" }
    Write-Host "  $out"
}

Write-Host 'Сборка (WSL):'
$wslDir = '/mnt/' + $PSScriptRoot.Substring(0, 1).ToLower() + ($PSScriptRoot.Substring(2) -replace '\\', '/')
wsl.exe -d Ubuntu-24.04 -- bash "$wslDir/build-wsl.sh" dist
if ($LASTEXITCODE -ne 0) { throw 'WSL build failed' }
# Установщик: программа и wintun.dll внутри него
$payload = Join-Path $PSScriptRoot 'installer\payload'
foreach ($f in 'vpnrouting.exe', 'wintun.dll', 'wintun-LICENSE.txt') {
    # Сжатыми: установщик вдвое меньше
    $in = [IO.File]::OpenRead((Join-Path $dist "windows\$f"))
    $out = [IO.File]::Create((Join-Path $payload "$f.gz"))
    $gz = New-Object IO.Compression.GZipStream($out, [IO.Compression.CompressionLevel]::Optimal)
    $in.CopyTo($gz); $gz.Dispose(); $out.Dispose(); $in.Dispose()
}
try {
    Build windows amd64 (Join-Path $dist 'VPN_routing-setup.exe') '-H windowsgui' './installer'
} finally {
    foreach ($f in 'vpnrouting.exe', 'wintun.dll', 'wintun-LICENSE.txt') {
        Remove-Item (Join-Path $payload "$f.gz") -ErrorAction SilentlyContinue
    }
}
Remove-Item Env:GOOS, Env:GOARCH

Get-ChildItem $dist -Recurse -File | Select-Object @{n = 'Файл'; e = { $_.FullName.Substring($dist.Length + 1) } }, Length
