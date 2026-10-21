#!/usr/bin/env python3
"""Guard for Claude's file tools (Read, Edit, Write, Glob, Grep...).

The system sandbox (Claude Code's sandbox) protects Bash commands.
Claude's built-in file tools do not go through it: this hook restricts
them to the allowed folders passed as arguments.
Exit code 2 = the action is refused and Claude receives the explanation.

Usage (PreToolUse hook): guard.py ALLOWED_FOLDER [ALLOWED_FOLDER...]
"""
import json, os, re, sys

NATIF = os.name == "nt"   # native Windows: C:\x, C:/x and /c/x (Git Bash) are the same path, any case

ECRITURE = {"Edit", "Write", "MultiEdit", "NotebookEdit"}


def windows(chemin):
    chemin = str(chemin)
    if NATIF:
        m = re.match(r"^/([a-zA-Z])(/|$)", chemin)
        if m:
            chemin = m.group(1) + ":/" + chemin[3:]
    return chemin


def propre(chemin):
    chemin = os.path.realpath(chemin)
    return chemin.replace("\\", "/").lower() if NATIF else chemin


def normaliser(chemin, base):
    chemin = os.path.expanduser(windows(chemin))
    if not os.path.isabs(chemin):
        chemin = os.path.join(windows(base), chemin)
    return propre(chemin)


def dedans(chemin, racines):
    return any(chemin == r or chemin.startswith(r.rstrip("/") + "/") for r in racines)


def main():
    racines = [propre(windows(r)) for r in sys.argv[1:] if not r.startswith("--lecture=")]
    lecture = [propre(windows(r[len("--lecture="):])) for r in sys.argv[1:] if r.startswith("--lecture=")]
    try:
        entree = json.load(sys.stdin)
    except Exception:
        return 0                                   # unreadable input: do not block Claude
    outil = entree.get("tool_name", "")
    param = entree.get("tool_input") or {}
    base = entree.get("cwd") or os.getcwd()
    candidats = []
    for cle in ("file_path", "notebook_path", "path"):
        if param.get(cle):
            candidats.append(param[cle])
    motif = param.get("pattern")
    if outil == "Glob" and isinstance(motif, str) and (motif.startswith(("/", "~")) or ".." in motif or re.match(r"^[A-Za-z]:[\\/]", motif)):
        candidats.append(motif.split("*")[0] or "/")
    if not candidats and outil in ("Glob", "Grep"):
        candidats.append(base)
    for c in candidats:
        chemin = normaliser(c, base)
        if outil not in ECRITURE and dedans(chemin, lecture):
            continue                                  # software folders: read-only
        if not dedans(chemin, racines):
            print(f"Refused by the guard: '{c}' is outside the allowed folders "
                  f"({', '.join(racines)}). Work only inside the project or in $TOOLS.", file=sys.stderr)
            return 2
        if outil in ECRITURE and "/.git/" in chemin + "/":
            print("Refused by the guard: do not modify the inside of .git; use the git commands.", file=sys.stderr)
            return 2
        if outil in ECRITURE and ("/.claude/" in chemin and os.path.basename(chemin).startswith("settings")
                                  or "/.claude/hooks/" in chemin or os.path.basename(chemin) == ".mcp.json"):
            print("Refused by the guard: Claude Code settings and .mcp.json are managed by the pilot.", file=sys.stderr)
            return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
