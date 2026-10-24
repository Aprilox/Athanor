#!/usr/bin/env bash
# =============================================================================
#  Athanor (WSL2 / Linux / macOS): installation, to run once.
#  On Windows, run install.cmd instead (it prepares WSL2 then calls this script).
#  Installs: Claude Code, the system sandbox (bubblewrap, socat, seccomp filter; Seatbelt on macOS),
#  RTK and a few basic tools. Asks for your password (sudo; Homebrew's installer on a Mac).
#  The work is done by Athanor's executable: athanor installer.
# =============================================================================
cd "$(dirname "$0")/.." || exit 1
[ -x ./athanor ] || { echo "Athanor's executable (athanor) is missing from the tool folder: download the kit again."; exit 1; }
exec ./athanor installer "$PWD"
