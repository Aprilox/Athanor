# shellcheck shell=bash
# Shared functions of the gateways (sourced, not executed).
SYSTEME="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIT="$(dirname "$SYSTEME")"
PROJECT="${PROJECT:-$KIT/projet}"
[ -f "$SYSTEME/pc_tools.env" ] && . "$SYSTEME/pc_tools.env"

# Converts Linux paths to Windows paths for a .exe program (also "-opt=/path").
convertir_args() {
  ARGS=()
  local a cle val
  for a in "$@"; do
    if [[ "$a" == /* ]] && [ -e "$a" -o -e "$(dirname "$a")" ]; then
      ARGS+=("$(wslpath -w "$a")")
    elif [[ "$a" == -*=/* ]]; then
      cle=${a%%=*}; val=${a#*=}
      if [ -e "$val" ] || [ -e "$(dirname "$val")" ]; then ARGS+=("$cle=$(wslpath -w "$val")"); else ARGS+=("$a"); fi
    else
      ARGS+=("$a")
    fi
  done
}

absent() { echo "$1 is not installed on this PC (or not detected). See .athanor/PC_TOOLS.md." >&2; exit 127; }

# Programs with windows (Unreal, Blender, a game under test) open on Claude's own Windows desktop when the
# option is on in the app (it writes logs/bureau-isole): never on the user's screen, never taking the focus,
# and what they start stays there too. Usage: lancer_windows [--detache] PROGRAM ARGUMENTS...
# Returns the program's exit code (--detache: returns at once, the program keeps running).
lancer_windows() {
  local detache=() prog pid_f p code
  [ "${1:-}" = "--detache" ] && { detache=(--detache); shift; }
  if [ -f "$KIT/logs/bureau-isole" ] && [ -x "$KIT/Athanor.exe" ]; then
    prog=$1
    [[ "$prog" == */* ]] || prog=$(command -v "$prog" 2>/dev/null || echo "$prog")
    [[ "$prog" == /* ]] && prog=$(wslpath -w "$prog")
    if [ ${#detache[@]} -gt 0 ]; then
      "$KIT/Athanor.exe" --isole --detache -- "$prog" "${@:2}"
      return
    fi
    # A cancelled command (the bridge stops this process group) only stops the Linux side of WSL
    # interop: the Windows process tree is stopped explicitly, from the PID Athanor.exe writes.
    pid_f=$(mktemp -p "$KIT/logs" .isole-XXXXXX)
    "$KIT/Athanor.exe" --isole --pid "$(wslpath -w "$pid_f")" -- "$prog" "${@:2}" &
    p=$!
    trap 'w=$(cat "$pid_f" 2>/dev/null); [[ "$w" =~ ^[0-9]+$ ]] && (cd /mnt/c 2>/dev/null; taskkill.exe /T /F /PID "$w" >/dev/null 2>&1); kill "$p" 2>/dev/null; rm -f "$pid_f"; exit 143' TERM INT HUP
    wait "$p"; code=$?
    trap - TERM INT HUP
    rm -f "$pid_f"
    return "$code"
  fi
  if [ ${#detache[@]} -gt 0 ]; then
    nohup "$@" >/dev/null 2>&1 &
    disown
    return 0
  fi
  "$@"
}

# Bridge: inside the sandbox, a gateway cannot start a Windows program or reach Docker.
# It therefore hands the command to the bridge (system/bridge.py, outside the sandbox) and shows its output live.
# Called at the top of every gateway: passer_par_le_pont "$@"
passer_par_le_pont() {
  [ "${PONT_DIRECT:-}" = 1 ] && return 0          # already outside the sandbox: run directly
  local dossier="${ATHANOR_PONT:-$PROJECT/.athanor/pont}" outil id demande sortie code_f vivant i taille=0 nouveau
  outil=$(basename "$0")
  vivant="$dossier/vivant"
  if [ ! -f "$vivant" ] || [ $(( $(date +%s) - $(stat -c %Y "$vivant" 2>/dev/null || echo 0) )) -gt 10 ]; then
    echo "$outil: the pilot's bridge is not responding (pilot stopped?). Restart the pilot." >&2; exit 127
  fi
  id="$(date +%s%N)-$$"; demande="$dossier/demande-$id.json"; sortie="$dossier/sortie-$id.log"; code_f="$dossier/code-$id"
  python3 - "$demande.tmp" "$outil" "$(pwd)" "${UNREAL_MCP_URL:-}" "$@" <<'PY'
import json, sys
f, outil, cwd, mcp, *args = sys.argv[1:]
json.dump({"outil": outil, "cwd": cwd, "args": args, "env": {"UNREAL_MCP_URL": mcp} if mcp else {}}, open(f, "w"))
PY
  mv "$demande.tmp" "$demande"
  trap 'touch "$dossier/annuler-$id"; exit 130' INT TERM
  while :; do
    if [ -f "$sortie" ]; then
      nouveau=$(stat -c %s "$sortie" 2>/dev/null || echo 0)
      if [ "$nouveau" -gt "$taille" ]; then tail -c +"$((taille + 1))" "$sortie" | head -c "$((nouveau - taille))"; taille=$nouveau; fi
    fi
    if [ -f "$code_f" ]; then
      nouveau=$(stat -c %s "$sortie" 2>/dev/null || echo 0)
      [ "$nouveau" -gt "$taille" ] && tail -c +"$((taille + 1))" "$sortie"
      i=$(cat "$code_f"); rm -f "$code_f" "$sortie"; exit "${i:-1}"
    fi
    sleep 0.3
  done
}

# Runs "$@" with administrator rights through the scheduled task "Athanor - installations" (allowed once
# by the user, in the app: Setup & health -> Administrator rights). No UAC prompt. Prints the output, returns the
# exit code; 127 if the task does not exist, 124 if no answer after 20 minutes.
# Usage: executer_admin "C:\path\program.exe" "arg1 arg2"   or   executer_admin WINGET_ID
executer_admin() {
  local tache="Athanor - installations" dossier="$KIT/tools/windows/admin-requests" n code
  (cd /mnt/c 2>/dev/null && schtasks.exe /query /tn "$tache" >/dev/null 2>&1) || return 127
  mkdir -p "$dossier"; n="$(date +%s)-$$"
  printf '%s' "$1${2:+ $2}" > "$dossier/demande-$n.txt"
  (cd /mnt/c && schtasks.exe /run /tn "$tache" >/dev/null 2>&1)
  for _ in $(seq 1 240); do [ -f "$dossier/resultat-$n.txt" ] && break; sleep 5; done
  if [ -f "$dossier/resultat-$n.txt" ]; then
    tr -d '\r' < "$dossier/resultat-$n.txt" | grep -v '^code='
    code=$(sed -n 's/^code=//p' "$dossier/resultat-$n.txt" | tr -d '\r' | head -n1); rm -f "$dossier/resultat-$n.txt"
    return "${code:-1}"
  fi
  rm -f "$dossier/demande-$n.txt"; echo "No answer from the administrator task after 20 min." >&2; return 124
}
