@echo off
rem Shows live what Claude is doing (close the window to quit, the pilot goes on).
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0..\system\windows\wsl-run.ps1" follow.sh "%~1"
