#!/usr/bin/env bash
# Builds the application for Windows (from Linux, macOS or WSL) and for the current machine:
# ../Athanor.exe and ../athanor, in the tool folder. Needs Go 1.24 or later.
# The Windows icon and version info (winres/athanor.rc) are compiled into rsrc_windows_amd64.syso
# when a resource compiler is present (llvm-windres or x86_64-w64-mingw32-windres); the committed .syso is used otherwise.
cd "$(dirname "$0")" || exit 1
if ! command -v go >/dev/null 2>&1; then
  echo "Go (the compiler of Athanor) is not installed: https://go.dev/dl/" >&2
  echo "or with your package manager: sudo apt install golang-go | sudo dnf install golang | sudo pacman -S go | brew install go" >&2
  exit 1
fi
for rc in llvm-windres llvm-windres-18 llvm-windres-17 x86_64-w64-mingw32-windres; do
  if command -v "$rc" >/dev/null 2>&1; then
    case "$rc" in llvm*) "$rc" --target=pe-x86-64 -i winres/athanor.rc -o rsrc_windows_amd64.syso ;; *) "$rc" -F pe-x86-64 -i winres/athanor.rc -o rsrc_windows_amd64.syso ;; esac && echo "resources compiled with $rc"
    break
  fi
done
export CGO_ENABLED=0
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o ../Athanor.exe . && echo "../Athanor.exe ready"
go build -trimpath -ldflags "-s -w" -o ../athanor . && echo "../athanor (this machine) ready"
