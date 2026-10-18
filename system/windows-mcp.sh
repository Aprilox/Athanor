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
# (written into the MCP configuration by system/extensions.py; never called by hand)
set -u
# System32 (cmd.exe, taskkill.exe): the Windows entry of the inherited PATH (Windows is not always on C:)
_wm_sys32=/mnt/c/Windows/System32
IFS=: read -ra _wm_chemins <<< "${PATH:-}"
_wm_windows_path=()   # the Windows folders WSL appends to PATH: where a bare name is looked for
for _wm_d in "${_wm_chemins[@]}"; do
  case "$_wm_d" in
    /mnt/[a-z]/*) _wm_windows_path+=("$_wm_d")
                  [[ "${_wm_d,,}" == /mnt/?/windows/system32 ]] && [ -f "$_wm_d/cmd.exe" ] && _wm_sys32=$_wm_d ;;
  esac
done
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
unset LD_PRELOAD LD_LIBRARY_PATH LD_AUDIT GCONV_PATH CLAUDE_CODE_OAUTH_TOKEN ANTHROPIC_API_KEY
_wm_kit="$(dirname "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)")"
echouer() { echo "windows-mcp: $*" >&2; exit 1; }
# a Windows command of this script's own: never reading the MCP input (interop would hand it over)
win() { WSLENV= "$@" </dev/null 2>/dev/null; }

if [ ! -e /proc/sys/fs/binfmt_misc/WSLInterop ] && [ ! -e /proc/sys/fs/binfmt_misc/WSLInterop-late ] && [ -z "${WSL_INTEROP:-}" ]; then
  echouer "Windows programs cannot be started from here (not on Windows, or WSL interoperability is off)."
fi

# The server's variables: given to the Windows program alone (env, at launch), never set in this script.
_wm_env=(); _wm_noms=()
while [ $# -gt 0 ] && [ "$1" != "--" ]; do
  case "$1" in
    --env)
      [ $# -ge 3 ] || echouer "--env NAME VALUE"
      [[ "$2" =~ ^[A-Za-z_][A-Za-z0-9_]{0,127}$ ]] || echouer "invalid variable"
      # names that would change how WSL interop itself starts the program
      case "${2^^}" in
        PATH|HOME|SHELL|ENV|BASH_ENV|IFS|WSLENV|LD_*|BASH_*|WSL_*|GCONV_PATH|LOCPATH|LC_*|LANG|LANGUAGE|POSIXLY_CORRECT|TMPDIR|_WM_*)
          echouer "variable $2 cannot be set for a Windows program" ;;
      esac
      _wm_env+=("$2=$3"); _wm_noms+=("$2"); shift 3 ;;
    *) echouer "unknown option $1" ;;
  esac
done
[ "${1:-}" = "--" ] && shift
[ $# -gt 0 ] || echouer "no program"
_wm_wslenv=$(IFS=:; printf '%s' "${_wm_noms[*]}")

# A Windows program started from a Linux folder would complain about the UNC path (\\wsl.localhost\…).
cd "$_wm_sys32/../.." 2>/dev/null || cd /mnt/c 2>/dev/null || cd /

# %NAME% -> its Windows value, read raw from "set NAME" (no cmd parsing of the value); unknown: as written.
declare -A _wm_valeurs=()
developper() {
  local s="$1" out="" v val
  while [[ "$s" =~ ^([^%]*)%([A-Za-z_][A-Za-z0-9_\(\)]{0,63})%(.*)$ ]]; do
    out+="${BASH_REMATCH[1]}"; v="${BASH_REMATCH[2]}"; s="${BASH_REMATCH[3]}"
    if [ -z "${_wm_valeurs[$v]+x}" ]; then
      # "set NAME" lists every variable starting with NAME: the line of NAME itself (case does not matter)
      val=$(win "$_wm_sys32/cmd.exe" /d /c "set $v" | tr -d '\r' \
            | awk -v n="$v" 'BEGIN { n = toupper(n) } { i = index($0, "="); if (i > 1 && toupper(substr($0, 1, i - 1)) == n) { print substr($0, i + 1); exit } }')
      [ -n "$val" ] || val="%$v%"
      _wm_valeurs[$v]=$val
    fi
    out+="${_wm_valeurs[$v]}"
  done
  printf '%s' "$out$s"
}

prog=$(developper "$1"); shift
args=(); for a in "$@"; do args+=("$(developper "$a")"); done
[[ "$prog" =~ ^[A-Za-z]:/ ]] && prog=${prog//\//\\}   # C:/Program Files/… as Claude Desktop configs write it
# a script runs through its interpreter (the name is then looked for by Windows itself)
case "${prog,,}" in
  *.bat|*.cmd) args=(/c "$prog" "${args[@]}"); prog=cmd.exe ;;
  *.ps1) args=(-NoProfile -ExecutionPolicy Bypass -File "$prog" "${args[@]}"); prog=powershell.exe ;;
esac
# The program as WSL sees it (prog_l, always a Windows file: never a Linux program, which would run uncaged)
# and as Windows starts it (prog_w: that same file whenever it is known, so what was checked is what runs).
case "$prog" in
  /mnt/[a-z]/*) [ -f "$prog" ] || echouer "program not found: $prog"
                prog_l=$prog; prog_w=$(wslpath -w "$prog") ;;
  /*) echouer "not a Windows program: $prog" ;;
  [A-Za-z]:\\*|\\\\*)
      prog_w=$prog; prog_l=$(wslpath -u "$prog" 2>/dev/null) && [ -f "$prog_l" ] || echouer "program not found: $prog" ;;
  */*|*\\*) echouer "give the program's full path (C:\\…) or a name Windows finds (cmd.exe): $prog" ;;
  *)  prog_l=""
      for d in "$_wm_sys32" "$_wm_sys32/WindowsPowerShell/v1.0" "${_wm_windows_path[@]}"; do
        for c in "$prog" "$prog.exe"; do [ -z "$prog_l" ] && [ -f "$d/$c" ] && prog_l="$d/$c"; done
      done
      # known: Windows runs that very file; unknown to WSL (a PATH WSL does not append): Windows looks for it
      if [ -n "$prog_l" ]; then prog_w=$(wslpath -w "$prog_l"); else prog_w=$prog; fi ;;
esac

# Through Athanor.exe: the program runs on Claude's desktop when that option is on (never on the user's
# screen), and its whole process tree can be stopped (WSL only stops the Linux side of interop).
_wm_logs="$_wm_kit/logs"; mkdir -p "$_wm_logs" 2>/dev/null
# pid files of launchers that died with the computer: removed once their Windows process is gone
for f in "$_wm_logs"/.mcp-*; do
  [ -f "$f" ] && [ -n "$(find "$f" -mmin +1440 2>/dev/null)" ] || continue
  w=$(cat "$f" 2>/dev/null)
  [[ "$w" =~ ^[0-9]+$ ]] && win "$_wm_sys32/tasklist.exe" /FI "PID eq $w" /NH | grep -q " $w " || rm -f "$f"
done
if [ -x "$_wm_kit/Athanor.exe" ] && _wm_pid=$(mktemp -p "$_wm_logs" .mcp-XXXXXX 2>/dev/null); then
  ici=(--ici); [ -f "$_wm_logs/bureau-isole" ] && ici=()
  # stops the Windows tree (taskkill /T), from the PID Athanor.exe writes before starting the program
  arret='f=$1; s=$2; p=$3; w=""; for _ in 1 2 3 4 5 6 7 8 9 10; do w=$(cat "$f" 2>/dev/null); [ -n "$w" ] && break; sleep 0.2; done
         case "$w" in ""|*[!0-9]*) ;; *) WSLENV= "$s/taskkill.exe" /T /F /PID "$w" </dev/null >/dev/null 2>&1 ;; esac
         kill "$p" 2>/dev/null; rm -f "$f"'
  env "${_wm_env[@]}" WSLENV="$_wm_wslenv" "$_wm_kit/Athanor.exe" --isole "${ici[@]}" --pid "$(wslpath -w "$_wm_pid")" -- "$prog_w" "${args[@]}" <&0 &
  p=$!
  # watchdog, in a session of its own (a KILL of this script or of its process group never reaches it):
  # if this script disappears while the program still runs, the program is stopped
  setsid -f bash -c 'while kill -0 "$4" 2>/dev/null; do kill -0 "$5" 2>/dev/null || exit 0; sleep 2; done
                     kill -0 "$3" 2>/dev/null && bash -c "$6" _ "$1" "$2" "$3"' _ "$_wm_pid" "$_wm_sys32" "$p" "$$" "$p" "$arret" \
    </dev/null >/dev/null 2>&1
  trap 'bash -c "$arret" _ "$_wm_pid" "$_wm_sys32" "$p"; exit 143' TERM INT HUP
  wait "$p"; code=$?
  trap - TERM INT HUP
  rm -f "$_wm_pid"
  exit "$code"
fi
[ -n "$prog_l" ] || echouer "program not found: $prog"
exec env "${_wm_env[@]}" WSLENV="$_wm_wslenv" "$prog_l" "${args[@]}"
