# shellcheck shell=bash
# Roblox Studio with a Rojo project (sourced by the pilot and by the roblox-studio gateway).
# The project's files are the place: Studio works on .athanor/studio/NAME.rbxl, built from them by rojo,
# and stays open from one cycle to the next; the scripts reach it live (athanor roblox-sync, SYNC.luau).

# The Rojo project file: default.project.json, else the first *.project.json (2 levels at most).
roblox_projet_rojo() {
  [ -f "$PROJECT/default.project.json" ] && { printf '%s\n' "$PROJECT/default.project.json"; return 0; }
  find "$PROJECT" -maxdepth 2 \( -name '.*' -o -name node_modules -o -name build -o -name Packages \) -prune \
    -o -name '*.project.json' -type f -print -quit 2>/dev/null
}

# The rojo program: in the PATH, the kit's tools, then rokit's.
roblox_rojo() {
  local c
  for c in "$(command -v rojo 2>/dev/null)" "${TOOLS:-/nulle-part}/bin/rojo.exe" "${TOOLS:-/nulle-part}/bin/rojo" \
           "${WIN_PROFIL:-/nulle-part}/.rokit/bin/rojo.exe" "$HOME/.rokit/bin/rojo"; do
    [ -n "$c" ] && [ -f "$c" ] && { printf '%s\n' "$c"; return 0; }
  done
  return 1
}

# The place Studio works on for a Rojo project.
roblox_place_studio() {
  local nom
  nom=$(jq -r '.name // empty' "$1" 2>/dev/null | tr -cd 'A-Za-z0-9_-' | cut -c1-60)
  printf '%s/.athanor/studio/%s.rbxl\n' "$PROJECT" "${nom:-place}"
}

# roblox_construire PROJECT_FILE PLACE: rojo build (its errors on stderr).
roblox_construire() {
  local rojo f="$1" place="$2" journal
  rojo=$(roblox_rojo) || { echo "rojo is not installed (the project's tools: rokit install)." >&2; return 127; }
  mkdir -p "$(dirname "$place")"
  journal="$(dirname "$place")/build.log"
  case "$rojo" in
    *.exe) "$rojo" build "$(wslpath -w "$f")" -o "$(wslpath -w "$place")" > "$journal" 2>&1 ;;
    *) "$rojo" build "$f" -o "$place" > "$journal" 2>&1 ;;
  esac || { tail -n 15 "$journal" >&2; return 1; }
}

roblox_studio_ouvert() {
  (cd /mnt/c 2>/dev/null; MSYS2_ARG_CONV_EXCL='*' tasklist.exe /FI "IMAGENAME eq RobloxStudioBeta.exe" /NH 2>/dev/null) | grep -qi 'RobloxStudioBeta'
}

# The Windows session is locked (its lock screen, LogonUI.exe, is up): Windows programs draw nothing then,
# so Studio's captures time out and its play tests show nothing, until the user unlocks the session.
windows_verrouille() {
  (cd /mnt/c 2>/dev/null; MSYS2_ARG_CONV_EXCL='*' tasklist.exe /FI "IMAGENAME eq LogonUI.exe" /NH 2>/dev/null) | grep -qi 'LogonUI'
}

# roblox_fermer_projet: closes the Studios opened on a place of this project (never another one, the
# user's included), and prints their number.
roblox_fermer_projet() {
  local w
  w=$(wslpath -w "$PROJECT"); w=${w//\'/\'\'}; w="${w%\\}\\"
  (cd /mnt/c 2>/dev/null; powershell.exe -NoProfile -NonInteractive -Command "\$d = '$w'.ToLower(); \$n = 0; Get-CimInstance Win32_Process -Filter \"Name='RobloxStudioBeta.exe'\" | Where-Object { \$_.CommandLine -and \$_.CommandLine.Replace('/', '\\').ToLower().Contains(\$d) } | ForEach-Object { Stop-Process -Id \$_.ProcessId -Force -ErrorAction SilentlyContinue; \$n++ }; \$n" 2>/dev/null) | tr -dc '0-9'
}
