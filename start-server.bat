@echo off
setlocal
cd /d "%~dp0"

if not exist "out\posvpn-server.exe" (
    echo [POSvpn] Server is not built: out\posvpn-server.exe
    echo Run build.ps1 first.
    pause
    exit /b 1
)

echo Starting POSvpn server...
echo   control: http://0.0.0.0:8787
echo   hub:     quic://0.0.0.0:8788
echo   state:   %CD%\server.json
echo.
echo Keep this window open. Ctrl+C stops the server.
echo.

out\posvpn-server.exe -listen 0.0.0.0:8787 -hub 0.0.0.0:8788 -store server.json %*

echo.
echo Server stopped.
pause
