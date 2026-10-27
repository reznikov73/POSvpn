@echo off
setlocal
cd /d "%~dp0"

if not exist "out\posvpn-client.exe" (
    echo [POSvpn] Client is not built: out\posvpn-client.exe
    echo Run build.ps1 first.
    pause
    exit /b 1
)

rem Without arguments start the window and release the console.
rem Права администратора больше не нужны: если служба установлена, она владеет
rem адаптером; если нет — exe сам запросит UAC.
if "%~1"=="" (
    start "" "out\posvpn-client.exe"
    exit /b
)

out\posvpn-client.exe %*
echo.
pause
