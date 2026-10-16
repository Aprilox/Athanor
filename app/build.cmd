@echo off
rem Builds Athanor.exe from the Go sources (Go 1.24 or later: winget install GoLang.Go).
cd /d "%~dp0"
where go >nul 2>nul || ( echo Go is not installed: winget install GoLang.Go & pause & exit /b 1 )
go build -trimpath -ldflags "-s -w -H windowsgui" -o ..\Athanor.exe . && echo Done: ..\Athanor.exe
pause
