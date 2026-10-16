# shellcheck shell=bash
# Safety functions shared by the pilot and the commands (sourced with ".", never executed).
#
# The pilot runs OUTSIDE the sandbox; Claude, inside the sandbox, can write anywhere in the project,
# including the .athanor/ workshop and .git/. Therefore the pilot:
#   - keeps its runtime state in a private folder of the tool (logs/pilotes/<id>), out of reach;
#   - never trusts a project file: numeric values are validated, symbolic links are removed
#     before writing, writes go through a temporary file then a rename;
#   - calls git with its automatic commands disabled (hooks, filters, fsmonitor, external diff),
#     which Claude could otherwise configure in .git/config to run outside the sandbox.

# The pilot's private folder for a project: $1 = absolute path of the project.
# Previous name of the tool (Claude Autopilote): the workshop was PROJECT/.autopilote. It is renamed
# to .athanor (never while an old pilot is alive in it); a near-empty .athanor (only a STOP file,
# created by a stop request before the migration) is folded into it. Usage: migrer_atelier PROJECT
migrer_atelier() {
  local p="$1" a="$1/.autopilote" n="$1/.athanor" f
  [ -d "$a" ] && [ ! -L "$a" ] || return 0
  [ -n "$(find "$a/pilot.pid" -mmin -1.5 2>/dev/null)" ] && return 0
  if [ -e "$n" ] || [ -L "$n" ]; then
    [ -d "$n" ] && [ ! -L "$n" ] && [ ! -e "$n/REQUEST.md" ] && [ ! -e "$n/PLAN.md" ] || return 0
    for f in "$n"/* "$n"/.[!.]*; do [ -e "$f" ] && [ ! -L "$f" ] && mv -n "$f" "$a/" 2>/dev/null; done
    rm -rf "$n"
  fi
  mv "$a" "$n" || return 0
  if [ -f "$p/.git/info/exclude" ] && [ ! -L "$p/.git/info/exclude" ]; then
    sed -i 's#^\.autopilote/$#.athanor/#' "$p/.git/info/exclude"
  fi
  if [ -d "$n/work" ]; then  # parallel workers' worktrees moved with the workshop
    g -C "$p" worktree repair "$n"/work/* >/dev/null 2>&1; g -C "$p" worktree prune >/dev/null 2>&1
  fi
  return 0
}

prive_de() {
  local kit="${KIT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
  printf '%s/logs/pilotes/%s' "$kit" "$(printf '%s' "$1" | sha1sum | cut -c1-16)"
}

# Positive integer read from a file or a variable, else the default value. $1 = text, $2 = default.
entier() { [[ "$1" =~ ^[0-9]{1,12}$ ]] && printf '%s' "$1" || printf '%s' "$2"; }

# Session or process id that is safe to signal (never 0 or 1, which would target everyone).
pid_sur() { [[ "$1" =~ ^[0-9]{2,9}$ ]] && printf '%s' "$1"; }

# Writes standard input to $1 (a workshop file) without ever following a link planted in its place.
publier() {
  local dest="$1" t
  [ -L "$(dirname "$dest")" ] && return 1
  t=$(mktemp "$(dirname "$dest")/.pub.XXXXXX" 2>/dev/null) || { cat > /dev/null; return 1; }
  cat > "$t" && mv -f "$t" "$dest" 2>/dev/null || rm -f "$t"
}

# Removes the workshop's symbolic links (except the workers' worktrees) before the pilot writes
# there: a link to one of the user's files would make it write outside the project.
assainir() {
  local etat="$1"
  [ -L "$etat" ] && rm -f "$etat"
  mkdir -p "$etat"
  find "$etat" -path "$etat/work" -prune -o -type l -print0 2>/dev/null | xargs -0 -r rm -f
  local d; for d in "$(dirname "$etat")/.git" "$(dirname "$etat")/.git/info"; do [ -L "$d" ] && rm -f "$d"; done
  [ -L "$(dirname "$etat")/.git/info/exclude" ] && rm -f "$(dirname "$etat")/.git/info/exclude"
  return 0
}

# git with nothing executable coming from the repository. Usage: g -C FOLDER command...
g() {
  local dossier="." f=() n
  [ "$1" = "-C" ] && { dossier="$2"; }
  while IFS= read -r n; do
    [ -n "$n" ] && f+=(-c "filter.$n.clean=cat" -c "filter.$n.smudge=cat" -c "filter.$n.process=" -c "filter.$n.required=false")
  done < <(git -C "$dossier" config --name-only --get-regexp '^filter\.' 2>/dev/null | sed -E 's/^filter\.(.*)\.[^.]+$/\1/' | sort -u)
  GIT_TERMINAL_PROMPT=0 git -c core.fsmonitor=false -c core.hooksPath=/dev/null -c diff.external= -c core.pager=cat \
    -c core.editor=true -c sequence.editor=true -c commit.gpgsign=false -c tag.gpgsign=false -c core.sshCommand=false \
    -c protocol.allow=never -c credential.helper= "${f[@]}" "$@"
}
