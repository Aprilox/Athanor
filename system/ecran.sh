#!/usr/bin/env bash
# Claude's screen on the Linux side. The Linux programs Claude starts with a window (a game under test,
# Godot, a browser with its window…) would otherwise open on the user's screen (on Windows: through
# WSLg) and take the focus. With the "Claude's desktop" option (logs/bureau-isole, written by the
# application), each pilot runs a private X server (Xvfb, see pilot.sh) and starts Claude in a mount
# namespace where /tmp/.X11-unix holds only that server's socket: every window opens there, and the
# user's display (X11 or Wayland) is out of reach.
#
#   ecran.sh lancer PROGRAM ARGS...  runs PROGRAM on Claude's screen (as is when there is none)
#   ecran.sh capture PROJECT          PNG of that project's Claude screen on stdout (for the application)
set -u
SYSTEME="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KIT="${KIT:-$(dirname "$SYSTEME")}"

case "${1:-}" in
  lancer)
    shift
    if [ -n "${ATHANOR_X11_DIR:-}" ] && [ -d "$ATHANOR_X11_DIR" ] && command -v bwrap > /dev/null; then
      a=(--dev-bind / / --bind "$ATHANOR_X11_DIR" /tmp/.X11-unix)
      # the user's session is out of reach too: its Wayland sockets, and its D-Bus bus (through which
      # xdg-open, gio and portals would open windows on the user's screen)
      if [ -n "${XDG_RUNTIME_DIR:-}" ]; then
        for s in "$XDG_RUNTIME_DIR"/wayland-* "$XDG_RUNTIME_DIR/bus"; do [ -S "$s" ] && a+=(--ro-bind /dev/null "$s"); done
      fi
      exec bwrap "${a[@]}" -- "$@"
    fi
    exec "$@"
    ;;
  capture)
    . "$SYSTEME/safety.sh"
    p=$(prive_de "$(cd "${2:-/nulle-part}" 2> /dev/null && pwd)")   # same logical path as the pilot
    read -r n dir < "$p/ecran" 2> /dev/null || { echo "Claude's screen is not running for this project." >&2; exit 1; }
    [[ "$n" =~ ^[0-9]{1,4}$ ]] && [ -S "$dir/X$n" ] || { echo "Claude's screen is not running for this project." >&2; exit 1; }
    command -v import > /dev/null || { echo "ImageMagick is missing (run install)." >&2; exit 1; }
    exec bwrap --dev-bind / / --bind "$dir" /tmp/.X11-unix -- import -display ":$n" -window root png:- 2> /dev/null
    ;;
  *)
    echo "Usage: ecran.sh lancer PROGRAM ARGS... | ecran.sh capture PROJECT" >&2
    exit 2
    ;;
esac
