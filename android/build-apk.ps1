# Обёртка для Windows: готовит окружение и запускает android/build-apk.sh в Git Bash.
#
# Что нужно заранее:
#   - Git for Windows (bash.exe);
#   - JDK 17+ в JAVA_HOME (или JDK);
#   - Android SDK в ANDROID_HOME (или SDK): platforms/android-33, build-tools-35;
#   - NDK r27 в ANDROID_NDK_HOME;
#   - gomobile в PATH (go install golang.org/x/mobile/cmd/gomobile@...);
#   - zip в PATH (choco install zip).
#
# Запуск:  .\android\build-apk.ps1 [-Version 0.9.8] [-Code 8]
# Результат: android\android\out\POSvpn-<версия>.apk

param(
    [string]$Version = '',
    [string]$Code = ''
)

$ErrorActionPreference = 'Stop'

$bash = @(
    'C:\Program Files\Git\bin\bash.exe',
    'C:\Program Files (x86)\Git\bin\bash.exe'
) | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $bash) {
    $bash = (Get-Command bash.exe -ErrorAction SilentlyContinue)
    if ($bash) { $bash = $bash.Source }
}
if (-not $bash) { throw 'bash.exe не найден: установите Git for Windows' }

# Переменные читаем из процесса, а если VS Code запущен давно и не видит свежих
# значений — из профиля пользователя и из системы.
function Get-Setting([string]$name) {
    foreach ($scope in 'Process', 'User', 'Machine') {
        $value = [Environment]::GetEnvironmentVariable($name, $scope)
        if ($value) { return $value }
    }
    return ''
}

$sdk = Get-Setting 'ANDROID_HOME'
if (-not $sdk) { $sdk = Get-Setting 'SDK' }
if (-not $sdk) { $sdk = Join-Path $env:LOCALAPPDATA 'Android\Sdk' }

$jdk = Get-Setting 'JAVA_HOME'
if (-not $jdk) { $jdk = Get-Setting 'JDK' }
if (-not $jdk) { $jdk = '' }
if (-not $jdk) {
    $jdk = @(
        (Get-ChildItem 'C:\Program Files\Eclipse Adoptium\jdk-*' -Directory -ErrorAction SilentlyContinue |
            Sort-Object Name -Descending | Select-Object -First 1).FullName,
        (Get-ChildItem 'C:\Program Files\Java\jdk-*' -Directory -ErrorAction SilentlyContinue |
            Sort-Object Name -Descending | Select-Object -First 1).FullName
    ) | Where-Object { $_ } | Select-Object -First 1
}

$ndk = Get-Setting 'ANDROID_NDK_HOME'
if (-not $ndk -and $sdk) {
    $ndk = (Get-ChildItem (Join-Path $sdk 'ndk') -Directory -ErrorAction SilentlyContinue |
        Sort-Object Name -Descending | Select-Object -First 1).FullName
}

if (-not $sdk -or -not (Test-Path (Join-Path $sdk 'platforms\android-33'))) {
    throw 'Android SDK не найден: задайте ANDROID_HOME и поставьте platforms;android-33'
}
if (-not $jdk) { throw 'JDK не найден: задайте JAVA_HOME (JDK 17 или новее)' }
if (-not $ndk) { throw 'NDK не найден: задайте ANDROID_NDK_HOME (NDK r27)' }

# Windows-путь в вид, понятный bash: E:\Work -> /e/Work
function ConvertTo-Msys([string]$path) {
    $p = $path.TrimEnd('\', '/') -replace '\\', '/'
    if ($p -match '^([A-Za-z]):/(.*)$') {
        return '/' + $Matches[1].ToLower() + '/' + $Matches[2]
    }
    return $p
}

$root = Split-Path -Parent $PSScriptRoot
$android = ConvertTo-Msys (Join-Path $root 'android')
$androidAndroid = ConvertTo-Msys (Join-Path $root 'android\android')

$lines = @(
    "export SDK='$(ConvertTo-Msys $sdk)'"
    "export JDK='$(ConvertTo-Msys $jdk)'"
    # NDK читает gomobile.exe, поэтому путь остаётся windows-ным.
    "export ANDROID_NDK_HOME='$ndk'"
    'export PATH="/c/ProgramData/chocolatey/bin:$PATH"'
)
# GOCACHE нужен gomobile: в чистом окружении задачи его может не быть.
$gocache = if ($env:GOCACHE) { $env:GOCACHE } else { Join-Path $env:TEMP 'posvpn-gocache' }
$lines += "export GOCACHE='$(ConvertTo-Msys $gocache)'"
if ($Version) { $lines += "export VERSION_NAME='$Version'" }
if ($Code) { $lines += "export VERSION_CODE='$Code'" }

# Каталог libs создаём до gomobile bind: сам build-apk.sh делает это позже.
$script = ($lines -join '; ') + "; mkdir -p '$androidAndroid/libs' && cd '$android' && ./build-apk.sh"

Write-Host "Сборка APK: SDK=$sdk NDK=$ndk" -ForegroundColor Cyan
& $bash -lc $script
exit $LASTEXITCODE
