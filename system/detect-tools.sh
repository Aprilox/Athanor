#!/usr/bin/env bash
# Detects the PC tools (Unreal, Blender, Android SDK, Java, winget, Docker) and writes
# system/pc_tools.env (for the gateways) and PROJECT/.athanor/PC_TOOLS.md (for Claude).
SYSTEME="$(cd "$(dirname "$0")" && pwd)"; KIT="$(dirname "$SYSTEME")"
UE_DIR="${UE_DIR:-}"; BLENDER_EXE="${BLENDER_EXE:-}"; ANDROID_SDK_DIR="${ANDROID_SDK_DIR:-}"; ANDROID_JDK_DIR="${ANDROID_JDK_DIR:-}"
WINDOWS=0; command -v cmd.exe >/dev/null 2>&1 && WINDOWS=1
# native Windows mode (Git Bash): the drives are /c, /d…, the profile is in the environment
NATIF=0; case "$(uname -s 2>/dev/null)" in MINGW*|MSYS*|CYGWIN*) NATIF=1; WINDOWS=1 ;; esac
PROFIL=""
if [ "$NATIF" = 1 ]; then
  [ -n "${USERPROFILE:-}" ] && PROFIL=$(cygpath -u "$USERPROFILE" 2>/dev/null)
elif [ "$WINDOWS" = 1 ]; then
  p=$(cd /mnt/c 2>/dev/null && cmd.exe /c "echo %USERPROFILE%" 2>/dev/null | tr -d '\r')
  [ -n "$p" ] && PROFIL=$(wslpath -u "$p" 2>/dev/null)
fi
# Windows drives (WSL: /mnt/c…; Git Bash: /c…): software can be installed on any of them.
LECTEURS=()
if [ "$NATIF" = 1 ]; then
  for m in /[a-z]; do
    { [ -d "$m/Program Files" ] || [ -d "$m/Epic Games" ] || [ -d "$m/Windows" ]; } && LECTEURS+=("$m")
  done
elif [ "$WINDOWS" = 1 ]; then
  for m in /mnt/[a-z]; do
    { [ -d "$m/Program Files" ] || [ -d "$m/Epic Games" ] || [ -d "$m/Windows" ]; } && LECTEURS+=("$m")
  done
fi
# Highest version among existing candidates (version sort: UE_5.10 comes after UE_5.8).
plus_recent() { local x; for x in "$@"; do [ -e "$x" ] && printf '%s\n' "$x"; done | sort -V | tail -n 1; }
if [ -z "$UE_DIR" ]; then
  c=(); for m in "${LECTEURS[@]}"; do for d in "$m/Program Files/Epic Games"/UE_* "$m/Epic Games"/UE_*; do
    [ -f "$d/Engine/Binaries/Win64/UnrealEditor.exe" ] && c+=("$d"); done; done
  UE_DIR=$(plus_recent "${c[@]}")
fi
if [ -z "$BLENDER_EXE" ]; then
  c=(); for m in "${LECTEURS[@]}"; do for f in "$m/Program Files/Blender Foundation"/Blender*/blender.exe; do [ -f "$f" ] && c+=("$f"); done; done
  BLENDER_EXE=$(plus_recent "${c[@]}")
fi
if [ -z "$ANDROID_SDK_DIR" ]; then
  for d in "$KIT/tools/windows/android-sdk" "${PROFIL:-/nulle-part}/AppData/Local/Android/Sdk"; do
    [ -d "$d/platform-tools" ] || [ -d "$d/cmdline-tools" ] && { ANDROID_SDK_DIR="$d"; break; }
  done
fi
if [ -z "$ANDROID_JDK_DIR" ]; then
  c=(); for m in "${LECTEURS[@]}"; do c+=("$m/Program Files/Android/Android Studio/jbr"); done
  for d in "${c[@]}" "$KIT"/tools/windows/jdk*; do
    [ -f "$d/bin/java.exe" ] && { ANDROID_JDK_DIR="$d"; break; }
  done
fi
ROBLOX_STUDIO=""; [ -n "$PROFIL" ] && ROBLOX_STUDIO=$(ls -t "$PROFIL"/AppData/Local/Roblox/Versions/*/RobloxStudioBeta.exe 2>/dev/null | head -n 1)
WINGET=0
if [ "$NATIF" = 1 ]; then command -v winget.exe >/dev/null 2>&1 && WINGET=1
elif [ "$WINDOWS" = 1 ]; then (cd /mnt/c && timeout 30 powershell.exe -NoProfile -Command "Get-Command winget" >/dev/null 2>&1) && WINGET=1; fi
{ echo "UE_DIR='${UE_DIR}'"; echo "BLENDER_EXE='${BLENDER_EXE}'"; echo "ANDROID_SDK_DIR='${ANDROID_SDK_DIR}'"; echo "ANDROID_JDK_DIR='${ANDROID_JDK_DIR}'"; echo "WIN_PROFIL='${PROFIL}'"; } > "$SYSTEME/pc_tools.env"
PROJECT="${PROJECT:-$KIT/projet}"; mkdir -p "$PROJECT/.athanor" "$KIT/tools/windows"
{
  echo "# PC tools available (written by the pilot before each cycle, do not edit)"
  echo
  if [ -n "$UE_DIR" ]; then
    echo "- **Unreal Engine**: $(basename "$UE_DIR")"
    echo "  - \`unreal-editor [--wait-mcp] Game.uproject\`: opens the editor (window) without blocking"
    echo "  - \`unreal-cmd Game.uproject -ExecCmds=\"...\" -unattended -nullrhi\`: commandlets and tests without a window"
    echo "  - \`unreal-uat BuildCookRun -project=Game.uproject -platform=Win64 -clientconfig=Shipping -build -cook -stage -pak -archive -archivedirectory=deliverable\`: Windows game (\`-platform=Android -cookflavor=ASTC\` for Android)"
    [ "${UNREAL_MCP:-0}" = 1 ] && echo "  - **Unreal MCP active**: \`mcp__unreal__*\` tools when the editor is open (the pilot opens it before each cycle if a .uproject exists)"
  else
    echo "- Unreal Engine: not detected"
  fi
  if [ -n "$ROBLOX_STUDIO" ]; then
    echo "- **Roblox Studio**: \`roblox-studio [--wait] PLACE.rbxl\` opens it on a place of the project, without blocking (nothing if a Studio is already open)"
    [ "${ROBLOX_MCP:-0}" = 1 ] && echo "  - **Roblox Studio MCP active**: \`mcp__roblox-studio__*\` tools on the open Studio (the pilot opens it before each cycle; Rojo project: \`.athanor/studio/SYNC.luau\` updates its scripts live, \`roblox-studio --rebuild\` rebuilds the place; a capture that times out: \`roblox-studio --screen\` says whether the Windows session is locked)"
  fi
  if [ -n "$BLENDER_EXE" ]; then echo "- **Blender**: \`blender -b -P script.py\` (headless), \`blender --open FILE.blend\` (its window, for the Blender MCP)"; else echo "- Blender: not detected"; fi
  if [ -n "$ANDROID_SDK_DIR" ]; then echo "- **Android SDK**: \`$ANDROID_SDK_DIR\`; \`adb devices\`, \`adb install -r game.apk\` (phone plugged in over USB)"; else echo "- Android SDK: missing (install it as a portable copy in \`\$TOOLS/windows/android-sdk\`, see PC_TOOLS_GUIDE.md)"; fi
  if [ -n "$ANDROID_JDK_DIR" ]; then echo "- **Java for Android**: \`$ANDROID_JDK_DIR\`"; else echo "- Java (JDK) for Android: missing"; fi
  if [ "$WINGET" = 1 ]; then echo "- **winget**: \`install-windows ID\` installs a Windows program (\`install-windows --search NAME\` to find the ID)"; else echo "- winget: unavailable"; fi
  if [ "$NATIF" = 1 ]; then echo "- **Native Windows**: you run on Windows itself (Git Bash): Windows programs run directly from your shell (\`program.exe ...\`, \`cmd //c ...\`, \`powershell -Command ...\`); portable ones go in \`\$TOOLS/windows\`"
  elif [ "$WINDOWS" = 1 ]; then echo "- **Portable Windows programs**: install them in \`\$TOOLS/windows\`, run them with \`pc-exec PATH.exe ...\`; Windows variables with \`windows-var NAME PATH\`"; fi
  if [ -f "$KIT/logs/bureau-isole" ] && [ "$NATIF" = 1 ]; then
    echo "- **Claude's desktop**: the Windows programs you open never show on the user's screen (a Windows desktop of their own) and never take the focus. Check what they show with screenshots (the program's own, or a PowerShell capture of its window)."
  elif [ -f "$KIT/logs/bureau-isole" ]; then
    echo "- **Claude's desktop**: the programs you open never show on the user's screen (Windows programs through the gateways: a Windows desktop of their own; Linux programs: a private X screen, \`DISPLAY\` is set). Check what they show with screenshots: the program's own, or \`import -window root shot.png\` for the Linux screen."
  fi
  if command -v docker >/dev/null 2>&1 && timeout 20 docker info >/dev/null 2>&1; then echo "- **Docker**: \`tool IMAGE COMMAND...\` (disposable container, project mounted on /work)"; else echo "- Docker: not available (Docker Desktop stopped$([ "$NATIF" = 1 ] || echo ", or WSL integration not enabled"))"; fi
  # What is already installed, checked here once: the agents never spend turns probing it again.
  echo
  echo "## Toolchain (checked by the pilot when it started)"
  echo
  echo "Installed on the system and usable from your sandbox (\`\$TOOLS/bin\` comes last in your PATH). Do not check these again: install what is missing into \`\$TOOLS\` within a milestone."
  echo
  version() {  # $1 = name shown, then the command; first line of its output, or "not installed"
    local nom="$1" v; shift
    command -v "$1" >/dev/null 2>&1 || { echo "- $nom: not installed"; return; }
    v=$(timeout 8 "$@" 2>&1 </dev/null) || { echo "- $nom: not installed"; return; }
    v=$(printf '%s\n' "$v" | tr -d '\r' | sed 's/\x1b\[[0-9;]*[A-Za-z]//g' | grep -v '^Picked up' | grep -m1 -E '[0-9]+\.[0-9]+' | cut -c1-90)
    echo "- $nom: ${v:-installed}"
  }
  # never run from tools/: what is there was written from the sandbox, and this check runs outside it
  version Node node --version
  version npm npm --version
  version Python python3 --version
  version pip python3 -m pip --version
  version PHP php --version
  version Composer composer --version
  version Git git --version
  version Java java -version
  version Go go version
  version Rust rustc --version
  version .NET dotnet --version
  version "PostgreSQL client" psql --version
  version SQLite sqlite3 --version
  # (Windows has a convert.exe of its own, which converts disks: ImageMagick is "magick" there)
  if [ "$NATIF" = 1 ]; then version ImageMagick magick -version; else version ImageMagick convert -version; fi
  version ffmpeg ffmpeg -version
  local_outils=$(find "$KIT/tools" -mindepth 1 -maxdepth 1 -type d ! -name cache ! -name bin ! -name mcp ! -name windows ! -name plugin-data -printf '%f\n' 2>/dev/null | sort | head -n 30 | tr '\n' ' ')
  [ -n "$local_outils" ] && echo "- Portable installs already in \`\$TOOLS\`: $local_outils"
  local_bin=$(find "$KIT/tools/bin" -mindepth 1 -maxdepth 1 \( -type f -o -type l \) -printf '%f\n' 2>/dev/null | sort | head -n 40 | tr '\n' ' ')
  [ -n "$local_bin" ] && echo "- Commands already in \`\$TOOLS/bin\` (not checked here): $local_bin"
} > "$PROJECT/.athanor/PC_TOOLS.md"
