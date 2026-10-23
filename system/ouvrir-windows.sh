#!/usr/bin/env bash
# ouvrir-windows.sh PROGRAM ARGUMENTS...: the Windows program an MCP server drives ("ouvrir" of the catalog
# or of the library: athanor mcp-ouvrir, before each cycle), opened without blocking, on Claude's desktop
# when that option is on. PROGRAM: C:\...\name.exe; the project's files among the arguments are converted.
. "$(dirname "$0")/bin/_common.sh"
prog=${1:-}; shift || true
case "$prog" in [A-Za-z]:\\*|[A-Za-z]:/*) ;; *) echo "Refused: a Windows program (C:\\...\\name.exe)." >&2; exit 2 ;; esac
[ -f "$(wslpath -u "$prog" 2>/dev/null)" ] || { echo "Not found: $prog" >&2; exit 127; }
convertir_args "$@"
if [ -x "$KIT/Athanor.exe" ]; then
  lancer_windows --detache "$prog" "${ARGS[@]}"
else
  lancer_windows --detache "$(wslpath -u "$prog")" "${ARGS[@]}"
fi
echo "started"
