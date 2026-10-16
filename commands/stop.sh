#!/usr/bin/env bash
# Stops a project's pilot right away: stop.sh [PROJECT_FOLDER] (the current cycle is interrupted).
cd "$(dirname "$0")/.." || exit 1
KIT=$(pwd)
. system/safety.sh
PROJECT="${1:-$KIT/projet}"
# A Windows path (D:\Dev\x) is accepted and converted.
case "$PROJECT" in [A-Za-z]:*) PROJECT=$(wslpath -u "$PROJECT") ;; esac
PROJECT=$(cd "$PROJECT" 2>/dev/null && pwd -P) || { echo "Project folder not found."; exit 1; }
# The reference pid is in the pilot's private folder (the one in the workshop is only for display).
PID="$(prive_de "$PROJECT")/pilot.pid"
P=$(pid_sur "$(cat "$PID" 2>/dev/null)")
if [ -n "$P" ] && kill -0 "$P" 2>/dev/null; then kill -TERM "$P"; for i in 1 2 3 4 5 6 7 8 9 10; do sleep 1; [ -f "$PID" ] || break; done; echo "Pilot stopped."
else echo "The pilot is not running on this project."; fi
