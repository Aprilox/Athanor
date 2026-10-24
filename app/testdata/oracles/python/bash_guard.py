#!/usr/bin/env python3
"""PreToolUse hook of Claude's Bash tool (the pilot's settings, system/settings.py).

Refuses, with the reason Claude reads:
  - a long wait: one sleep (sleep, Start-Sleep, timeout /t) of more than LIMITE seconds. Nobody watches a
    cycle: a wait for a screen, a program or a reset is a night lost; the pilot handles the quota and
    restarts, and a failing tool is noted and worked around (CLAUDE.md, "Never freeze");
  - closing Roblox Studio or Unreal by program name, which would close the user's own windows too: the
    gateways close only the project's (roblox-studio --rebuild).
Any unreadable input lets the command through (the hook never blocks Claude by accident).
"""
import json
import re
import sys

LIMITE = 120

DUREE = {"": 1, "s": 1, "m": 60, "h": 3600, "d": 86400}
ATTENTES = [
    (re.compile(r"(?<![\w.-])sleep\s+((?:\d+(?:\.\d+)?[smhd]?\s*)+)"), "sleep"),
    (re.compile(r"Start-Sleep\s+(?:-s(?:econds)?\s+)?(\d+(?:\.\d+)?)(?!\s*-m)", re.I), "s"),
    (re.compile(r"Start-Sleep\s+-m(?:illiseconds)?\s+(\d+)", re.I), "ms"),
    (re.compile(r"(?<![\w.-])timeout(?:\.exe)?\s+/+t\s+(\d+)", re.I), "s"),
]
PROGRAMMES = re.compile(r"(taskkill|stop-process|pkill|killall|kill)\b[^\n;|&]*\b(robloxstudio|unrealeditor|ue4editor)", re.I)


def secondes(commande):
    plus = 0.0
    for motif, genre in ATTENTES:
        for m in motif.finditer(commande):
            if genre == "sleep":
                total = 0.0
                for n, u in re.findall(r"(\d+(?:\.\d+)?)([smhd]?)", m.group(1)):
                    total += float(n) * DUREE[u]
                plus = max(plus, total)
            elif genre == "ms":
                plus = max(plus, float(m.group(1)) / 1000)
            else:
                plus = max(plus, float(m.group(1)))
    return plus


def main():
    try:
        entree = json.load(sys.stdin)
        commande = (entree.get("tool_input") or {}).get("command") or ""
    except Exception:
        return 0
    if not isinstance(commande, str) or not commande:
        return 0
    s = secondes(commande)
    if s > LIMITE:
        print(f"Refused by the pilot: a wait of {int(s)} s (more than {LIMITE} s). Never wait for a screen, a "
              "program or a reset: if a tool keeps failing (a Studio capture that times out, a program that does "
              "not answer), try one quick fix, then note it in .athanor/BUGS.md and go on with what does not need "
              "it. To wait for something that will come (a server starting), poll it with a short limit: "
              "timeout 120 bash -c 'until CONDITION; do sleep 5; done'.", file=sys.stderr)
        return 2
    if PROGRAMMES.search(commande):
        print("Refused by the pilot: closing Roblox Studio or Unreal by program name would also close the user's "
              "own windows. Roblox: roblox-studio --rebuild closes and reopens only this project's Studio; "
              "roblox-studio --screen says whether Studio can draw right now.", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
