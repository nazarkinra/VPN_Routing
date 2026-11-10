# Собирает подписанный выпуск и кладёт его в корень репозитория:
#   VPN_routing.apk — для скачивания со страницы репозитория;
#   version.json    — по нему приложение узнаёт о новой версии и проверяет скачанный файл.
#
# Использование:
#   publish_apk.ps1 -Notes "Что нового в этой версии"
#
# Перед выпуском увеличьте versionCode (и versionName) в app/build.gradle:
# приложение предлагает обновление, только если versionCode больше установленного.
# Нужен android/keystore.properties (образец — keystore.properties.example).

param([string]$Notes = '')

$ErrorActionPreference = 'Stop'
if (-not $env:JAVA_HOME) { $env:JAVA_HOME = 'C:\Program Files\Android\Android Studio\jre' }
Set-Location $PSScriptRoot
$root = Split-Path $PSScriptRoot -Parent

if (-not (Test-Path 'keystore.properties')) {
    throw 'Нет keystore.properties: скопируйте keystore.properties.example и впишите пароли.'
}

.\gradlew.bat assembleRelease testDebugUnitTest --console=plain
if ($LASTEXITCODE -ne 0) { throw 'Сборка не удалась' }

$apk = Get-ChildItem 'app\build\outputs\apk\release\*.apk' | Select-Object -First 1
if ($apk.Name -match 'unsigned') { throw 'APK не подписан: проверьте keystore.properties' }

$meta = Get-Content 'app\build\outputs\apk\release\output-metadata.json' -Raw | ConvertFrom-Json
$code = [int]$meta.elements[0].versionCode
$name = [string]$meta.elements[0].versionName

$versionFile = Join-Path $root 'version.json'
if (Test-Path $versionFile) {
    $prev = Get-Content $versionFile -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($code -le [int]$prev.versionCode) {
        throw "versionCode $code не больше опубликованного ($($prev.versionCode)): увеличьте его в app/build.gradle"
    }
}

$target = Join-Path $root 'VPN_routing.apk'
Copy-Item $apk.FullName $target -Force
$item = Get-Item $target
$sha = (Get-FileHash $target -Algorithm SHA256).Hash.ToLower()

$json = [ordered]@{
    versionCode = $code
    versionName = $name
    apk         = 'VPN_routing.apk'
    size        = $item.Length
    sha256      = $sha
    notes       = $Notes
    date        = (Get-Date -Format 'yyyy-MM-dd')
} | ConvertTo-Json
[IO.File]::WriteAllText($versionFile, $json + "`n", (New-Object Text.UTF8Encoding($false)))

"Готово: версия $name (код $code), $([math]::Round($item.Length / 1MB, 1)) МБ"
'Дальше: git add VPN_routing.apk version.json, commit, push'
