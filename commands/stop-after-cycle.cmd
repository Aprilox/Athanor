@echo off
rem Runs stop-after-cycle.sh in WSL.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0..\system\windows\wsl-run.ps1" stop-after-cycle.sh "%~1"
pause
