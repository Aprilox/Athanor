#!/usr/bin/env python3
"""What a fresh session needs to take over a milestone when the previous one's context was full.

  relais.py contexte LOG          prints the size of the lead's context at its latest turn (tokens)
  relais.py sur LOG               exit 0 when the session is between two steps (its last event is a tool
                                  result or a text), 1 while a tool is still running
  relais.py resume LOG HANDOFF    prints, in Markdown, the relay notes: the session's own HANDOFF file
                                  (where it said it was), then its last actions and last messages, read
                                  from LOG

The log is Claude Code's stream-json output. Everything read from it is data written by the model: it is
quoted, never run.
"""
import json
import os
import sys

MAX_LECTURE = 8 << 20   # only the end of a long log matters


def lignes(chemin):
    try:
        with open(chemin, "rb") as f:
            f.seek(0, os.SEEK_END)
            taille = f.tell()
            f.seek(max(0, taille - MAX_LECTURE))
            brut = f.read().decode("utf-8", "replace")
    except OSError:
        return []
    out = []
    for l in brut.splitlines():
        l = l.strip()
        if not l.startswith("{"):
            continue
        try:
            o = json.loads(l)
        except ValueError:
            continue
        if isinstance(o, dict) and o.get("type") not in ("rate_limit_event", "tool_progress", "stream_event"):
            out.append(o)
    return out


def du_chef(o):
    return o.get("parent_tool_use_id") is None


def contenu(o):
    m = o.get("message") if isinstance(o.get("message"), dict) else {}
    c = m.get("content")
    return c if isinstance(c, list) else []


def contexte(journal):
    n = 0
    for o in lignes(journal):
        if o.get("type") == "assistant" and du_chef(o):
            u = (o.get("message") or {}).get("usage") or {}
            if isinstance(u, dict):
                t = sum(v for v in (u.get("input_tokens"), u.get("cache_read_input_tokens"), u.get("cache_creation_input_tokens")) if isinstance(v, int))
                if t:
                    n = t
    print(n)


def sur(journal):
    """Between two steps: no tool call of the lead waits for its result."""
    en_cours = set()
    for o in lignes(journal):
        if not du_chef(o):
            continue
        for b in contenu(o):
            if not isinstance(b, dict):
                continue
            if b.get("type") == "tool_use" and isinstance(b.get("id"), str):
                en_cours.add(b["id"])
            elif b.get("type") == "tool_result":
                en_cours.discard(b.get("tool_use_id"))
    return 0 if not en_cours else 1


def court(t, n):
    t = " ".join(str(t).split())
    return t if len(t) <= n else t[: n - 1] + "…"


def resume(journal, handoff):
    actions, textes = [], []
    for o in lignes(journal):
        if o.get("type") != "assistant" or not du_chef(o):
            continue
        for b in contenu(o):
            if not isinstance(b, dict):
                continue
            if b.get("type") == "tool_use":
                i = b.get("input") if isinstance(b.get("input"), dict) else {}
                chemin = i.get("file_path") if isinstance(i.get("file_path"), str) else ""
                chemin = chemin.replace("\\", "/")
                if chemin.count("/") > 3:
                    chemin = "…/" + "/".join(chemin.split("/")[-3:])
                quoi = i.get("description") or chemin or i.get("command") or i.get("pattern") or i.get("prompt") or ""
                actions.append("%s: %s" % (court(b.get("name", "?"), 40), court(quoi, 180)))
            elif b.get("type") == "text" and str(b.get("text", "")).strip():
                textes.append(court(b["text"], 500))
    print("## Relay: you take over a milestone in progress\n")
    print("The previous session's context was full, so the pilot stopped it between two steps and started you with a fresh one. Its work is in the folder: committed (`git log`) or not yet (`git status`, `git diff`). Carry on from where it was; do not redo what it did, and check before trusting what it says.\n")
    note = ""
    try:
        if handoff and os.path.isfile(handoff) and not os.path.islink(handoff) and os.path.getsize(handoff) < 64 << 10:
            note = open(handoff, encoding="utf-8", errors="replace").read().strip()
    except OSError:
        pass
    if note:
        print("### Where it said it was (`.athanor/HANDOFF.md`)\n")
        print("\n".join("> " + l for l in note.splitlines()[:60]))
        print()
    if actions:
        print("### Its last actions\n")
        for a in actions[-20:]:
            print("- " + a.replace("\n", " "))
        print()
    if textes:
        print("### Its last messages\n")
        for t in textes[-3:]:
            print("> " + t.replace("\n", " "))
        print()


if __name__ == "__main__":
    a = sys.argv[1:]
    if len(a) >= 2 and a[0] == "contexte":
        contexte(a[1])
    elif len(a) >= 2 and a[0] == "sur":
        sys.exit(sur(a[1]))
    elif len(a) >= 2 and a[0] == "resume":
        resume(a[1], a[2] if len(a) > 2 else "")
    else:
        sys.exit("usage: relais.py contexte LOG | sur LOG | resume LOG [HANDOFF]")
