#!/usr/bin/env bash
# Stops a project's pilot right away: stop.sh [PROJECT_FOLDER] (the current cycle is interrupted).
cd "$(dirname "$0")/.." || exit 1
KIT=$(pwd)
. system/safety.sh
PROJECT="${1:-$KIT/projet}"
# A Windows path (D:\Dev\x) is accepted and converted.
case "$PROJECT" in [A-Za-z]:*) PROJECT=$(wslpath -u "$PROJECT") ;; esac
PROJECT=$(cd "$PROJECT" 2>/dev/null && pwd) || { echo "Project folder not found."; exit 1; }
# The reference pid is in the pilot's private folder (the one in the workshop is only for display).
if P=$(pilote_vivant "$PROJECT"); then : > "$(prive_de "$PROJECT")/arreter" 2>/dev/null; kill -TERM "$P" 2>/dev/null; for _ in 1 2 3 4 5 6 7 8 9 10; do sleep 1; kill -0 "$P" 2>/dev/null || break; done; echo "Pilot stopped."
else echo "The pilot is not running on this project."; fi
