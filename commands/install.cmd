@echo off
rem Installation (once): WSL2, Claude Code, sandbox, tools.
chcp 65001 >nul
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install.ps1"
pause
