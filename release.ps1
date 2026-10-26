# Полный цикл после каждого изменения: сборка exe → установщик → коммит → пуш.
#
#   .\release.ps1 "сообщение коммита"
#
# Без сообщения коммит называется «Сборка <версия>».

param(
    [string]$Message = ""
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

$version = (Get-Content 'installer\version.txt' -Raw).Trim()
if (-not $Message) { $Message = "Сборка $version" }

Write-Host "== 1/4. Сборка exe ==" -ForegroundColor Cyan
& (Join-Path $root 'build.ps1')
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "== 2/4. Установщик ==" -ForegroundColor Cyan
$iscc = 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe'
if (-not (Test-Path $iscc)) {
    $found = Get-Command ISCC -ErrorAction SilentlyContinue
    if (-not $found) { Write-Host "ISCC.exe не найден — установите Inno Setup 6" -ForegroundColor Red; exit 1 }
    $iscc = $found.Source
}
& $iscc 'installer\posvpn-setup.iss' /Qp
if ($LASTEXITCODE -ne 0) { Write-Host "Установщик не собрался" -ForegroundColor Red; exit 1 }
$setup = Get-ChildItem 'installer\out\POSvpn-Setup-*.exe' -ErrorAction SilentlyContinue |
    Sort-Object LastWriteTime -Descending | Select-Object -First 1
if (-not $setup) { Write-Host "Установщик не найден в installer\out" -ForegroundColor Red; exit 1 }
Write-Host ("   {0} ({1} МБ)" -f $setup.Name, [math]::Round($setup.Length / 1MB, 1)) -ForegroundColor Green

Write-Host "== 3/4. Коммит ==" -ForegroundColor Cyan
git add -A
$pending = git status --porcelain
if (-not $pending) {
    Write-Host "   изменений нет — коммит не нужен"
} else {
    git commit -m $Message
    if ($LASTEXITCODE -ne 0) { Write-Host "Коммит не удался" -ForegroundColor Red; exit 1 }
}

Write-Host "== 4/4. Пуш ==" -ForegroundColor Cyan
git push origin main
if ($LASTEXITCODE -ne 0) { Write-Host "Пуш не удался" -ForegroundColor Red; exit 1 }

Write-Host ("Готово: {0} ({1})" -f $Message, $version) -ForegroundColor Green
