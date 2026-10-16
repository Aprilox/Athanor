#!/usr/bin/env bash
# =============================================================================
#  Athanor (WSL2 / Linux): installation, to run once.
#  On Windows, run install.cmd instead (it prepares WSL2 then calls this script).
#  Installs: Claude Code, the system sandbox (bubblewrap, socat, seccomp filter),
#  RTK and a few basic tools. Asks for your Linux password (sudo).
# =============================================================================
set -u
cd "$(dirname "$0")/.." || exit 1
KIT=$(pwd); SYSTEME="$KIT/system"
mkdir -p "$KIT/logs"; exec > >(tee -a "$KIT/logs/install-wsl.log") 2>&1
echo "===== $(date '+%Y-%m-%d %H:%M:%S') install.sh ($(uname -r))"
VERT=$'\033[32m'; JAUNE=$'\033[33m'; GRAS=$'\033[1m'; FIN=$'\033[0m'
etape() { printf '\n%s== %s%s\n' "$GRAS" "$1" "$FIN"; }
ok()    { printf '%s✓ %s%s\n' "$VERT" "$1" "$FIN"; }
alerte(){ printf '%s! %s%s\n' "$JAUNE" "$1" "$FIN"; }

WSL=0; grep -qi microsoft /proc/version 2>/dev/null && WSL=1
etape "System"
if [ "$WSL" = 1 ]; then
  [ -e /proc/sys/fs/binfmt_misc/WSLInterop ] || [ -n "${WSL_INTEROP:-}" ] || alerte "Windows interoperability disabled: Unreal and Blender will not be launchable."
  uname -r | grep -qi wsl2 || grep -qi 'WSL2' /proc/version || alerte "Check that this distribution runs on WSL2 (PowerShell: wsl -l -v). The sandbox does not work on WSL1."
  ok "WSL detected"
  case "$KIT" in /mnt/*) ok "Kit on the Windows drive: Unreal and Blender will be able to open the project" ;;
                 *) alerte "The kit is not on a Windows drive (/mnt/c...): Unreal and Blender will have to go through \\\\wsl\$." ;; esac
else
  ok "Native Linux"
fi

etape "Packages (sudo)"
if command -v apt-get >/dev/null 2>&1; then
  # A previously interrupted installation blocks apt: finish it first.
  sudo dpkg --configure -a >/dev/null 2>&1 || true
  sudo apt-get -f install -y -qq >/dev/null 2>&1 || true
  sudo apt-get update -qq && sudo apt-get install -y -qq git curl ca-certificates jq ripgrep procps \
    python3 python3-venv python3-pip python3-pil nodejs npm imagemagick ffmpeg zip unzip bubblewrap socat \
    && ok "Packages installed" || alerte "Package installation incomplete: read the messages above."
elif command -v dnf >/dev/null 2>&1; then
  sudo dnf install -y git curl jq ripgrep procps-ng python3 python3-pillow nodejs npm ImageMagick ffmpeg zip unzip bubblewrap socat \
    && ok "Packages installed" || alerte "Package installation incomplete."
else
  alerte "Unknown package manager: install by hand git curl jq python3 python3-pil nodejs npm imagemagick bubblewrap socat."
fi

etape "System sandbox"
if [ "$(sysctl -n kernel.apparmor_restrict_unprivileged_userns 2>/dev/null)" = 1 ]; then
  sudo tee /etc/apparmor.d/bwrap > /dev/null <<'PROFIL'
abi <abi/4.0>,
include <tunables/global>

profile bwrap /usr/bin/bwrap flags=(unconfined) {
  userns,
  include if exists <local/bwrap>
}
PROFIL
  sudo systemctl reload apparmor 2>/dev/null || sudo apparmor_parser -r /etc/apparmor.d/bwrap
  ok "AppArmor profile for bubblewrap added"
fi
if bwrap --ro-bind / / --dev /dev --proc /proc true 2>/dev/null; then ok "bubblewrap works"; else alerte "bubblewrap does not start: the sandbox will be unavailable and Claude will refuse to start."; fi
# seccomp filter: blocks Unix sockets, hence launching cmd.exe or powershell.exe from inside the sandbox.
if sudo npm install -g --silent @anthropic-ai/sandbox-runtime >/dev/null 2>&1; then ok "seccomp filter installed"; else alerte "seccomp filter not installed (npm): run this script again later."; fi

etape "Claude Code"
export PATH="$HOME/.local/bin:$PATH"
if command -v claude >/dev/null 2>&1; then
  ok "Claude Code already installed ($(claude --version 2>/dev/null | head -n1))"
else
  curl -fsSL https://claude.ai/install.sh | bash && ok "Claude Code installed" || alerte "Claude Code installation failed."
fi
grep -q '.local/bin' "$HOME/.bashrc" 2>/dev/null || echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.bashrc"

etape "RTK (command output compression)"
RTK_VERSION=v0.50.0
case "$(uname -m)" in x86_64) cible=x86_64-unknown-linux-musl ;; aarch64) cible=aarch64-unknown-linux-gnu ;; *) cible="" ;; esac
if [ -x "$SYSTEME/bin/rtk" ]; then ok "RTK already present"
elif [ -n "$cible" ] && curl -fsSL "https://github.com/rtk-ai/rtk/releases/download/$RTK_VERSION/rtk-$cible.tar.gz" | tar -xz -C "$SYSTEME/bin" rtk; then ok "RTK $RTK_VERSION installed"
else alerte "RTK not installed: Claude will work without it."; fi

etape "Docker (optional, for \"tool IMAGE COMMAND\")"
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then ok "Docker reachable"
else alerte "Docker unreachable. Windows: Docker Desktop > Settings > Resources > WSL integration > tick this distribution."; fi

etape "PC tools"
chmod +x "$KIT"/commands/*.sh "$SYSTEME"/*.sh "$SYSTEME"/*.py "$SYSTEME"/bin/* 2>/dev/null
PROJECT="$KIT/logs/sandbox-test" "$SYSTEME/detect-tools.sh" && sed -n '3,20p' "$KIT/logs/sandbox-test/.athanor/PC_TOOLS.md"

etape "Done"
echo "Next step: configure (token, model...). Windows: commands\\configure.cmd · Linux: commands/configure.sh"
echo "Then, to test the sandbox: commands/test-sandbox.sh (Windows: commands\\test-sandbox.cmd)"
