#!/usr/bin/env bash
# Downloaded the source code instead of a release? This builds ./athanor (and Athanor.exe) here (see app/build.sh).
exec "$(cd "$(dirname "$0")" && pwd)/app/build.sh" "$@"
