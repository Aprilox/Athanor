@echo off
rem Allows (or updates) Claude's installations and program runs with administrator rights, plus the
rem Windows firewall rules. Windows asks for your consent only once.
rem   allow-admin.cmd          allow / refresh
rem   allow-admin.cmd remove   remove the task, the firewall rules and C:\ProgramData\Athanor
if /i "%~1"=="remove" (
  powershell -NoProfile -ExecutionPolicy Bypass -Command "Start-Process powershell.exe -Verb RunAs -Wait -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-NoExit','-File','\"%~dp0..\system\windows\allow-admin.ps1\"','-Retirer')"
) else (
  powershell -NoProfile -ExecutionPolicy Bypass -Command "Start-Process powershell.exe -Verb RunAs -Wait -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-NoExit','-File','\"%~dp0..\system\windows\allow-admin.ps1\"','-Kit','\"%~dp0..\"','-Utilisateur','\"%USERDOMAIN%\%USERNAME%\"')"
)
