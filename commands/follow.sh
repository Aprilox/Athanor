#!/usr/bin/env bash
# Shows live what Claude is doing on a project: follow.sh [PROJECT_FOLDER] (Ctrl+C to quit; the pilot goes on).
cd "$(dirname "$0")/.." || exit 1
KIT=$(pwd)
. system/safety.sh
PROJECT="${1:-$KIT/projet}"
# A Windows path (D:\Dev\x) is accepted and converted.
case "$PROJECT" in [A-Za-z]:*) PROJECT=$(wslpath -u "$PROJECT") ;; esac
PROJECT=$(cd "$PROJECT" 2>/dev/null && pwd -P) || { echo "Project folder not found."; exit 1; }
P="$(prive_de "$PROJECT")"
mkdir -p "$P"; touch "$P/output.log"
cat "$PROJECT/.athanor/STATUS.txt" 2>/dev/null; echo "----"
exec tail -n 40 -F "$P/output.log"
