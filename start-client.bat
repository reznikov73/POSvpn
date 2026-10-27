@echo off
setlocal
cd /d "%~dp0"

if not exist "out\posvpn-client.exe" (
    echo [POSvpn] Client is not built: out\posvpn-client.exe
    echo Run build.ps1 first.
    pause
    exit /b 1
)

rem The virtual adapter needs administrator rights. Re-launch this file elevated.
fltmc >nul 2>&1
if errorlevel 1 (
    echo Requesting administrator rights for the virtual adapter...
    if "%~1"=="" (
        powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    ) else (
        powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -ArgumentList '%*' -Verb RunAs"
    )
    exit /b
)

out\posvpn-client.exe %*
echo.
pause
