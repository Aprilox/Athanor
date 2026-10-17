#!/usr/bin/env bash
# Detects the PC tools (Unreal, Blender, Android SDK, Java, winget, Docker) and writes
# system/pc_tools.env (for the gateways) and PROJECT/.athanor/PC_TOOLS.md (for Claude).
SYSTEME="$(cd "$(dirname "$0")" && pwd)"; KIT="$(dirname "$SYSTEME")"
UE_DIR="${UE_DIR:-}"; BLENDER_EXE="${BLENDER_EXE:-}"; ANDROID_SDK_DIR="${ANDROID_SDK_DIR:-}"; ANDROID_JDK_DIR="${ANDROID_JDK_DIR:-}"
WINDOWS=0; command -v cmd.exe >/dev/null 2>&1 && WINDOWS=1
PROFIL=""
if [ "$WINDOWS" = 1 ]; then
  p=$(cd /mnt/c 2>/dev/null && cmd.exe /c "echo %USERPROFILE%" 2>/dev/null | tr -d '\r')
  [ -n "$p" ] && PROFIL=$(wslpath -u "$p" 2>/dev/null)
fi
# Windows drives mounted in WSL (C:, D:, ...): software can be installed on any of them.
LECTEURS=()
if [ "$WINDOWS" = 1 ]; then
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
WINGET=0; [ "$WINDOWS" = 1 ] && (cd /mnt/c && timeout 30 powershell.exe -NoProfile -Command "Get-Command winget" >/dev/null 2>&1) && WINGET=1
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
  if [ -n "$BLENDER_EXE" ]; then echo "- **Blender**: \`blender -b -P script.py\`"; else echo "- Blender: not detected"; fi
  if [ -n "$ANDROID_SDK_DIR" ]; then echo "- **Android SDK**: \`$ANDROID_SDK_DIR\`; \`adb devices\`, \`adb install -r game.apk\` (phone plugged in over USB)"; else echo "- Android SDK: missing (install it as a portable copy in \`\$TOOLS/windows/android-sdk\`, see PC_TOOLS_GUIDE.md)"; fi
  if [ -n "$ANDROID_JDK_DIR" ]; then echo "- **Java for Android**: \`$ANDROID_JDK_DIR\`"; else echo "- Java (JDK) for Android: missing"; fi
  if [ "$WINGET" = 1 ]; then echo "- **winget**: \`install-windows ID\` installs a Windows program (\`install-windows --search NAME\` to find the ID)"; else echo "- winget: unavailable"; fi
  if [ "$WINDOWS" = 1 ]; then echo "- **Portable Windows programs**: install them in \`\$TOOLS/windows\`, run them with \`pc-exec PATH.exe ...\`; Windows variables with \`windows-var NAME PATH\`"; fi
  if [ -f "$KIT/logs/bureau-isole" ]; then
    echo "- **Claude's desktop**: the programs you open never show on the user's screen (Windows programs through the gateways: a Windows desktop of their own; Linux programs: a private X screen, \`DISPLAY\` is set). Check what they show with screenshots: the program's own, or \`import -window root shot.png\` for the Linux screen."
  fi
  if command -v docker >/dev/null 2>&1 && timeout 20 docker info >/dev/null 2>&1; then echo "- **Docker**: \`tool IMAGE COMMAND...\` (disposable container, project mounted on /work)"; else echo "- Docker: not available (Docker Desktop stopped, or WSL integration not enabled)"; fi
} > "$PROJECT/.athanor/PC_TOOLS.md"
