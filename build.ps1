# Сборка обоих бинарников POSvpn.
# Запуск: .\build.ps1   или из VS Code: задача «build: all».

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

New-Item -ItemType Directory -Force -Path out | Out-Null

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
