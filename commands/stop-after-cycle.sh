#!/usr/bin/env bash
# Stops a project's pilot cleanly at the end of the current cycle: stop-after-cycle.sh [PROJECT_FOLDER]
cd "$(dirname "$0")/.." || exit 1
KIT=$(pwd)
. system/safety.sh   # native Windows mode: the shims (wslpath) in the PATH, as for start.sh and stop.sh
PROJECT="${1:-$KIT/projet}"
# A Windows path (D:\Dev\x) is accepted and converted.
case "$PROJECT" in [A-Za-z]:*) PROJECT=$(wslpath -u "$PROJECT") ;; esac
[ -n "$PROJECT" ] && [ -d "$PROJECT" ] || { echo "Project folder not found: ${1:-$PROJECT}. Nothing was changed." >&2; exit 1; }
[ -L "$PROJECT/.athanor" ] && rm -f "$PROJECT/.athanor"
mkdir -p "$PROJECT/.athanor" && rm -f "$PROJECT/.athanor/STOP" && : > "$PROJECT/.athanor/STOP"
# a pilot of the previous version still works in .autopilote/ (previous name): it reads its STOP there
[ -d "$PROJECT/.autopilote" ] && [ ! -L "$PROJECT/.autopilote" ] && : > "$PROJECT/.autopilote/STOP"
echo "OK: the pilot will stop at the end of the current cycle. To resume later: start."
