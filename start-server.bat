@echo off
setlocal
cd /d "%~dp0"

if not exist "out\posvpn-server.exe" (
    echo [POSvpn] Server is not built: out\posvpn-server.exe
    echo Run build.ps1 first.
    pause
    exit /b 1
)

rem Server window: status, networks, nodes, hub counters.
rem The server keeps running while the window is open; closing it stops the server.
start "" "out\posvpn-server.exe" -gui -listen 0.0.0.0:8787 -hub 0.0.0.0:8788 -store server.json %*
exit /b
