#!/bin/bash -p
# Starts a local MCP server that is a Windows program (Roblox Studio's, a .exe, a .bat…) for Claude
# Code, through WSL interop: the MCP messages go through its standard input and output, as with Claude
# Desktop. It runs on Windows, with the user's rights and outside any cage (a Windows program cannot be
# caged from WSL): only the servers the user added in the app (Skills & MCP page, "Windows program") get
# here. Claude can neither write their configuration nor start a Windows program itself: interop is
# hidden from its sandbox and from the caged servers (mcp-jail.sh).
#
# Usage: windows-mcp.sh [--env NAME VALUE]... -- PROGRAM [ARGS...]
#   PROGRAM: a name Windows finds (cmd.exe, npx.cmd, python.exe), a Windows path (C:\…\server.exe) or a
#   WSL path (/mnt/c/…). A .bat or .cmd runs through cmd.exe, a .ps1 through PowerShell. %NAME% in the
#   program and its arguments are Windows variables (%LOCALAPPDATA%…).
# The server's variables reach the Windows program only (WSLENV), nothing else of the environment.
# When Claude's session ends (or the pilot stops), the program and everything it started are stopped,
# even if this script is killed outright (a watchdog outside its process group).
# (written into the MCP configuration by athanor extensions; never called by hand)
set -u
# athanor mcp-windows does it, with the PATH as inherited (it reads the Windows folders from it)
_wm_k="$(dirname "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)")"
[ -x "$_wm_k/athanor" ] || { echo "windows-mcp: Athanor's executable is missing from the tool folder: Setup & health → Rebuild Athanor." >&2; exit 127; }
KIT="$_wm_k" exec "$_wm_k/athanor" mcp-windows "$@"
