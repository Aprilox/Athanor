#!/usr/bin/env bash
# =============================================================================
#  Athanor (WSL2 / Linux / macOS): configuration assistant
#  Asks a few questions and writes .env (settings shared by all projects). The app
#  drives it. Run it again whenever you want: your current answers are
#  offered as defaults (Enter key).
#  Windows: double-click configure.cmd · Linux and macOS: ./configure.sh
#  The work is done by Athanor's executable: athanor configurer.
# =============================================================================
cd "$(dirname "$0")/.." || exit 1
[ -x ./athanor ] || { echo "Athanor's executable (athanor) is missing from the tool folder: download the kit again."; exit 1; }
exec ./athanor configurer "$PWD"
