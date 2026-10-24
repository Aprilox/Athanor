#!/usr/bin/env bash
# Claude's screen on the Linux side. The Linux programs Claude starts with a window (a game under test,
# Godot, a browser with its window…) would otherwise open on the user's screen (on Windows: through
# WSLg) and take the focus. With the "Claude's desktop" option (logs/bureau-isole, written by the
# application), each pilot runs a private X server (Xvfb, started by the pilot) and starts Claude in a mount
# namespace where /tmp/.X11-unix holds only that server's socket: every window opens there, and the
# user's display (X11 or Wayland) is out of reach.
#
#   ecran.sh lancer PROGRAM ARGS...  runs PROGRAM on Claude's screen (as is when there is none)
#   ecran.sh capture PROJECT          PNG of that project's Claude screen on stdout (for the application)
set -u
SYSTEME="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KIT="${KIT:-$(dirname "$SYSTEME")}"
# Linux (and WSL): athanor ecran does it all. Elsewhere there is no private screen: the program runs as is.
case "$(uname -s 2>/dev/null)" in
  Linux)
    [ -x "$KIT/athanor" ] || { echo "Athanor's executable is missing from the tool folder ($KIT/athanor): Setup & health → Rebuild Athanor." >&2; exit 127; }
    export KIT; exec "$KIT/athanor" ecran "$@" ;;
esac
case "${1:-}" in
  lancer) shift; exec "$@" ;;
  capture) echo "Claude's screen exists on Linux only." >&2; exit 1 ;;
  *) echo "Usage: ecran.sh lancer PROGRAM ARGS... | ecran.sh capture PROJECT" >&2; exit 2 ;;
esac
