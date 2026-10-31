@echo off
rem Double-click to install veyra. Windows blocks .ps1 files by default, so this runs the installer with that restriction lifted for this one run only.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install.ps1"
echo.
pause
