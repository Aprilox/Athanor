#!/bin/bash -p
# Runs a local MCP server (stdio) in a cage (bubblewrap), the way Claude's own commands run in the
# system sandbox. Inside: the system read-only, the project folder writable, the tool caches (tools/)
# writable, the network. Hidden: the user's home (except the toolchains: nvm, uv, cargo…), the Windows
# drives (/mnt), Athanor's folder and its secrets, the WSL bridge to Windows. The Claude token never
# reaches the server.
#
# Usage: mcp-jail.sh NAME TOOLS [--rw DIR]... [--ro DIR]... [--env NAME VALUE]... -- COMMAND [ARGS...]
# The server's variables come as --env (set inside the cage only): this script runs outside the cage, so
# its own environment is never taken from a server's configuration (-p: no BASH_ENV, no imported functions).
# (written into the MCP configuration by athanor extensions; never called by hand)
set -u
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin   # bwrap is found here, never elsewhere
unset LD_PRELOAD LD_LIBRARY_PATH LD_AUDIT GCONV_PATH
# athanor mcp-cage builds the cage
_k="$(dirname "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)")"
[ -x "$_k/athanor" ] || { echo "mcp-jail: Athanor's executable is missing from the tool folder: Setup & health → Rebuild Athanor." >&2; exit 127; }
exec "$_k/athanor" mcp-cage "$@"
