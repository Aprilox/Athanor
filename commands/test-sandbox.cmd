@echo off
rem Runs test-sandbox.sh in WSL.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0..\system\windows\wsl-run.ps1" test-sandbox.sh
pause
