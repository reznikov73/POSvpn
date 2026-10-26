# Загружает подписанную wintun.dll официальной сборки Wintun.
# Кладёт файл в third_party\wintun\amd64\wintun.dll; build.ps1 копирует его в out\.
#
#   .\tools\fetch-wintun.ps1                  # скачать с wintun.net
#   .\tools\fetch-wintun.ps1 -Force           # скачать заново
#   .\tools\fetch-wintun.ps1 -From C:\path\wintun.dll   # взять готовый файл
#
# Если сеть медленная, скачайте wintun-0.14.1.zip вручную, распакуйте и
# укажите путь к bin\amd64\wintun.dll через -From.

param(
    [string]$Version = '0.14.1',
    [string]$From = '',
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$destDir = Join-Path $root 'third_party\wintun\amd64'
$dest = Join-Path $destDir 'wintun.dll'

if ((Test-Path $dest) -and -not $Force -and -not $From) {
    Write-Host "wintun.dll уже на месте: $dest" -ForegroundColor Green
    exit 0
}

$url = "https://www.wintun.net/builds/wintun-$Version.zip"
$zip = Join-Path $env:TEMP "wintun-$Version.zip"
$unpack = Join-Path $env:TEMP "wintun-$Version"

if ($From) {
    if (-not (Test-Path $From)) { throw "файл не найден: $From" }
    $dll = (Resolve-Path $From).Path
} else {
    Write-Host "Скачиваем $url …"
    $downloaded = $false
    if (Get-Command curl.exe -ErrorAction SilentlyContinue) {
        & curl.exe -L --max-time 120 -sS -o $zip $url
        $downloaded = ($LASTEXITCODE -eq 0)
    } else {
        Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing -TimeoutSec 120
        $downloaded = $true
    }
    if (-not $downloaded -or -not (Test-Path $zip)) {
        throw "не удалось скачать $url. Скачайте архив вручную и укажите -From <путь к wintun.dll>"
    }

    Remove-Item $unpack -Recurse -Force -ErrorAction SilentlyContinue
    Expand-Archive -Path $zip -DestinationPath $unpack -Force
    $dll = Join-Path $unpack 'bin\amd64\wintun.dll'
    if (-not (Test-Path $dll)) { throw "в архиве нет bin\amd64\wintun.dll" }
}

# Драйвер грузится в ядро, поэтому проверяем подпись, а не только контрольную сумму.
$signature = Get-AuthenticodeSignature $dll
if ($signature.Status -ne 'Valid') {
    throw "подпись wintun.dll недействительна: $($signature.Status)"
}
if (-not $signature.SignerCertificate -or $signature.SignerCertificate.Subject -notmatch 'WireGuard') {
    throw "неожиданный подписант wintun.dll: $($signature.SignerCertificate.Subject)"
}

New-Item -ItemType Directory -Force -Path $destDir | Out-Null
Copy-Item $dll $dest -Force
if (-not $From) { Remove-Item $zip -Force -ErrorAction SilentlyContinue }

Write-Host "wintun.dll → $dest" -ForegroundColor Green
Write-Host "подписант: $($signature.SignerCertificate.Subject)"
