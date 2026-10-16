@echo off
rem Builds Athanor.exe (in the tool folder) from the Go sources. Needs Go 1.24 or later:
rem if it is missing, it is offered through winget.
setlocal
cd /d "%~dp0"
set "GO=go"
where go >nul 2>nul && goto build
if exist "%ProgramFiles%\Go\bin\go.exe" ( set "GO=%ProgramFiles%\Go\bin\go.exe" & goto build )
echo Go (the compiler of Athanor) is not installed.
where winget >nul 2>nul || ( echo Install it from https://go.dev/dl/ then run build.cmd again. & pause & exit /b 1 )
choice /m "Install Go now with winget"
if errorlevel 2 ( echo Cancelled: install Go from https://go.dev/dl/ then run build.cmd again. & pause & exit /b 1 )
winget install --id GoLang.Go -e --accept-source-agreements --accept-package-agreements
if not exist "%ProgramFiles%\Go\bin\go.exe" ( echo Go was not installed: install it from https://go.dev/dl/ then run build.cmd again. & pause & exit /b 1 )
set "GO=%ProgramFiles%\Go\bin\go.exe"
:build
echo Building Athanor.exe...
set "CGO_ENABLED=0"
"%GO%" build -trimpath -ldflags "-s -w -H windowsgui" -o ..\Athanor.exe . || ( echo Build failed. & pause & exit /b 1 )
echo Done: Athanor.exe is in the tool folder, next to commands\ and system\.
pause
