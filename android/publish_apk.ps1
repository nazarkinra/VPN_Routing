# Собирает подписанный выпуск и кладёт его в корень репозитория как VPN_routing.apk,
# чтобы его можно было скачать прямо со страницы репозитория.
#
# Нужен android/keystore.properties (образец — keystore.properties.example).

$ErrorActionPreference = 'Stop'
if (-not $env:JAVA_HOME) { $env:JAVA_HOME = 'C:\Program Files\Android\Android Studio\jre' }
Set-Location $PSScriptRoot

if (-not (Test-Path 'keystore.properties')) {
    throw 'Нет keystore.properties: скопируйте keystore.properties.example и впишите пароли.'
}

.\gradlew.bat assembleRelease testDebugUnitTest --console=plain
if ($LASTEXITCODE -ne 0) { throw 'Сборка не удалась' }

$apk = Get-ChildItem 'app\build\outputs\apk\release\*.apk' | Select-Object -First 1
if ($apk.Name -match 'unsigned') { throw 'APK не подписан: проверьте keystore.properties' }

$target = Join-Path (Split-Path $PSScriptRoot -Parent) 'VPN_routing.apk'
Copy-Item $apk.FullName $target -Force
Get-Item $target | Select-Object Name, @{ n = 'MB'; e = { [math]::Round($_.Length / 1MB, 1) } }
