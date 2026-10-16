@echo off
rem Starts the pilot in a minimised window: as long as it is open, Claude works.
start "Athanor (close = stop)" /min powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0..\system\windows\wsl-run.ps1" start.sh "%~1"
echo Pilot started in a minimised window "Athanor". To follow: follow.cmd   To stop: stop.cmd
timeout /t 5 >nul
