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
# (written into the MCP configuration by system/extensions.py; never called by hand)
set -u
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin   # bwrap is found here, never elsewhere
unset LD_PRELOAD LD_LIBRARY_PATH LD_AUDIT GCONV_PATH
# Athanor's executable builds the cage when it runs here (athanor mcp-cage); ATHANOR_OUTILS=python keeps this script.
_k="$(dirname "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)")"
if [ "${ATHANOR_OUTILS:-}" != python ] && [ -f "$_k/athanor" ] && [ -x "$_k/athanor" ] \
   && timeout 15 "$_k/athanor" aide < /dev/null 2> /dev/null | grep -q '^  mcp-cage '; then
  exec "$_k/athanor" mcp-cage "$@"
fi
nom="${1:-}"; outils="${2:-}"; shift 2 2>/dev/null || { echo "mcp-jail: usage: NAME TOOLS [--rw DIR]... -- COMMAND" >&2; exit 2; }
ecriture=(); lecture=(); variables=()
while [ $# -gt 0 ] && [ "$1" != "--" ]; do
  case "$1" in
    --rw) ecriture+=("$2"); shift 2 ;;
    --ro) lecture+=("$2"); shift 2 ;;
    --env) [[ "${2:-}" =~ ^[A-Za-z_][A-Za-z0-9_]{0,127}$ ]] || { echo "mcp-jail: invalid variable" >&2; exit 2; }
           variables+=(--setenv "$2" "${3-}"); shift 3 ;;
    *) echo "mcp-jail: unknown option $1" >&2; exit 2 ;;
  esac
done
[ "${1:-}" = "--" ] && shift
[ $# -gt 0 ] || { echo "mcp-jail: no command" >&2; exit 2; }
[[ "$nom" =~ ^[a-z0-9][a-z0-9._-]{0,63}$ ]] || { echo "mcp-jail: invalid name" >&2; exit 2; }
[ -d "$outils" ] || { echo "mcp-jail: tool folder not found" >&2; exit 2; }
command -v bwrap > /dev/null || { echo "mcp-jail: bubblewrap is missing (run install)" >&2; exit 1; }

maison="$outils/mcp/$nom"
mkdir -p "$maison" "$outils/cache/npm" "$outils/cache/uv" || exit 1

a=(--ro-bind / / --dev /dev --proc /proc --tmpfs /tmp --tmpfs /mnt)
[ -d /run/WSL ] && a+=(--tmpfs /run/WSL)               # no launching Windows programs through WSL interop
# Name resolution: WSL links /etc/resolv.conf (and sometimes hosts) into /mnt/wsl, hidden above; the
# files themselves come back, read-only, so the server can still reach the network.
for f in /etc/resolv.conf /etc/hosts /etc/nsswitch.conf; do
  r=$(readlink -f "$f" 2>/dev/null) || continue
  case "$r" in /mnt/*) [ -f "$r" ] && a+=(--ro-bind "$r" "$r") ;; esac
done
[ -n "${HOME:-}" ] && [ -d "$HOME" ] && [ "$HOME" != / ] && {
  a+=(--tmpfs "$HOME")
  # the toolchains installed in the home stay usable (read-only): npx, uvx, node from nvm…
  for d in .nvm .volta .bun .deno .cargo .npm-global .local/bin .local/share/uv .local/share/pnpm .local/pipx .pyenv; do
    [ -d "$HOME/$d" ] && a+=(--ro-bind "$HOME/$d" "$HOME/$d")
  done
}
for d in "${lecture[@]}"; do [ -d "$d" ] && a+=(--ro-bind "$d" "$d"); done
for d in "${ecriture[@]}"; do [ -d "$d" ] && a+=(--bind "$d" "$d"); done
a+=(--bind "$outils" "$outils")
# Claude's screen (system/ecran.sh): a server's windows (Godot, a browser) open there, never on the user's screen.
[ -n "${ATHANOR_X11_DIR:-}" ] && [ -d "$ATHANOR_X11_DIR" ] && a+=(--bind "$ATHANOR_X11_DIR" /tmp/.X11-unix)
a+=("${variables[@]}")   # the server's own variables, before the cage's (HOME, caches…) which win
a+=(--setenv HOME "$maison" --setenv XDG_CACHE_HOME "$outils/cache" --setenv npm_config_cache "$outils/cache/npm"
    --setenv UV_CACHE_DIR "$outils/cache/uv" --setenv PLAYWRIGHT_BROWSERS_PATH "$outils/cache/ms-playwright"
    --unsetenv CLAUDE_CODE_OAUTH_TOKEN --unsetenv ANTHROPIC_API_KEY --unsetenv WSL_INTEROP
    --die-with-parent --chdir "${ecriture[0]:-$maison}")
exec bwrap "${a[@]}" -- "$@"
