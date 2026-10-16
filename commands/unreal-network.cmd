@echo off
rem Makes Unreal's MCP reachable from WSL (mirrored, otherwise local relay).
chcp 65001 >nul
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0..\system\windows\unreal-network.ps1"
pause
