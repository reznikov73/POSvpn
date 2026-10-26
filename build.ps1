# Сборка обоих бинарников POSvpn.
# Запуск: .\build.ps1   или из VS Code: задача «build: all».

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

New-Item -ItemType Directory -Force -Path out | Out-Null

# Драйвер туннеля: wintun.dll должен лежать рядом с exe, из памяти DLL не
# грузится. Если файла нет, сборка продолжается, но туннель не запустится.
# Загрузка: .\tools\fetch-wintun.ps1  (или -From <путь к готовому wintun.dll>).
$wintun = Join-Path $root 'third_party\wintun\amd64\wintun.dll'
if (-not (Test-Path $wintun)) {
    Write-Host 'wintun.dll не найден: .\tools\fetch-wintun.ps1 (или -From <путь>)' -ForegroundColor Yellow
}

$targets = @(
    @{ Name = 'posvpn-server'; Path = './posvpn-server' },
    @{ Name = 'posvpn-client'; Path = './posvpn-client' }
)

$failed = @()
foreach ($t in $targets) {
    Write-Host "Сборка $($t.Name)… " -NoNewline
    $output = go build -o "out\$($t.Name).exe" $t.Path 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ОШИБКА" -ForegroundColor Red
        $output | ForEach-Object { Write-Host $_ }
        $failed += $t.Name
        continue
    }
    $size = [math]::Round((Get-Item "out\$($t.Name).exe").Length / 1MB, 1)
    Write-Host "готово ($size МБ)" -ForegroundColor Green
}

if ($failed.Count -gt 0) {
    Write-Host "Не собралось: $($failed -join ', ')" -ForegroundColor Red
    exit 1
}

Write-Host "Всё собрано: $root\out" -ForegroundColor Green

if (Test-Path $wintun) {
    Copy-Item $wintun 'out\wintun.dll' -Force
    Write-Host 'wintun.dll скопирован в out' -ForegroundColor Green
} else {
    Write-Host 'ВНИМАНИЕ: wintun.dll отсутствует — туннель не запустится' -ForegroundColor Yellow
}
