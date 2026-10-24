#!/usr/bin/env bash
# Starts the pilot on a project: start.sh [--background | --keep] [PROJECT_FOLDER]
# Without a folder: KIT/projet. --background: in the background, returns immediately (Linux).
# --keep (Windows app): pilot detached like --background, then this command stays open as long as it
# runs, so that Windows keeps WSL alive; if it is killed, the pilot goes on. Pilot already running: it
# simply waits (the app relaunches it when it opens).
# --attach (Windows app, on opening): only keeps an already running pilot alive (WSL awake); never starts one.
cd "$(dirname "$0")/.." || exit 1
KIT=$(pwd); BACKGROUND=0
. system/safety.sh
KEEP=0; ATTACH=0
for a in "$@"; do case "$a" in --background) BACKGROUND=1 ;; --keep) BACKGROUND=1; KEEP=1 ;; --attach) KEEP=1; ATTACH=1 ;; *) PROJECT="$a" ;; esac; done
PROJECT="${PROJECT:-$KIT/projet}"
# A Windows path (D:\Dev\x) is accepted and converted.
case "$PROJECT" in [A-Za-z]:*) PROJECT=$(wslpath -u "$PROJECT") ;; esac
[ -d "$PROJECT" ] && migrer_atelier "$PROJECT"
mkdir -p "$PROJECT/.athanor" || { echo "Project folder not accessible: $PROJECT"; exit 1; }
PROJECT=$(cd "$PROJECT" && pwd)   # logical path: the resolved one can change (Docker Desktop bind mounts)
PRIVE="$(prive_de "$PROJECT")"; mkdir -p "$PRIVE"
# The reference pid and the pilot's output live in its private folder (out of Claude's reach).
PID="$PRIVE/pilot.pid"; SORTIE="$PRIVE/output.log"
garder() {  # waits for the pilot to end (keeps WSL alive)
  [ "$KEEP" = 1 ] || return 0
  # a single keeper per project: when the app reopens, the one left by the previous session is enough
  exec 8>"$PRIVE/keeper.lock"
  flock -n 8 || exit 0
  sleep 5
  while pilote_vivant "$PROJECT" >/dev/null; do sleep 30; done
}
if VIVANT=$(pilote_vivant "$PROJECT"); then echo "The pilot is already running on this project (PID $VIVANT)."; garder; exit 0; fi
[ "$ATTACH" = 1 ] && exit 0   # nothing to keep: --attach never starts a pilot
rm -f "$PID"   # stale file (pilot killed before it could clean up)
[ -f .env ] || { echo "No .env: set the token in the app (Settings)."; exit 1; }
[ -e "$PROJECT/.athanor/STOP" ] && rm -f "$PROJECT/.athanor/STOP" && echo "(previous end-of-cycle stop cancelled: the pilot restarts)"
# macOS: the pilot in Go directly (the bash of macOS, 3.2, is too old for pilot.sh), without setsid
PILOTE=(bash system/pilot.sh); [ "$(uname -s)" = Darwin ] && PILOTE=("$KIT/athanor" pilote)
SETSID=(setsid); command -v setsid >/dev/null 2>&1 || SETSID=()
if [ "$BACKGROUND" = 1 ]; then
  "${SETSID[@]}" nohup "${PILOTE[@]}" "$PROJECT" >> "$SORTIE" 2>&1 < /dev/null &
  echo "Pilot started in the background on $PROJECT"
  garder
else
  "${PILOTE[@]}" "$PROJECT" 2>&1 | tee -a "$SORTIE"
fi
