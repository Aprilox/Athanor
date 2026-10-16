@echo off
rem Runs stop.sh in WSL.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0..\system\windows\wsl-run.ps1" stop.sh "%~1"
pause
