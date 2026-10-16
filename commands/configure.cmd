@echo off
rem Runs configure.sh in WSL.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0..\system\windows\wsl-run.ps1" configure.sh
pause
