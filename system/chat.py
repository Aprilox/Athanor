#!/usr/bin/env python3
"""Claude Code settings of the application's chat (app/api_chat.go).

  chat.py settings PROJECT OUT    writes OUT: Claude's file tools stay inside PROJECT (guard.py as a
                                  PreToolUse hook, as for the pilot); no command, no web

The chat runs short sessions in the project's folder with read-only tools (and Edit/Write while the
pilot is stopped): no Bash, so no sandbox is needed for it.
"""
import json
import os
import shlex
import sys
import tempfile

NATIF = os.name == "nt"


def plugins_utilisateur():
    """The plugins switched on in the user's own Claude Code stay off in the chat (see settings.py)."""
    config = os.environ.get("CLAUDE_CONFIG_DIR") or os.path.join(os.path.expanduser("~"), ".claude")
    try:
        with open(os.path.join(config, "settings.json"), encoding="utf-8-sig") as f:
            p = json.load(f).get("enabledPlugins")
        return {k: False for k, v in p.items() if v is True} if isinstance(p, dict) else {}
    except (OSError, ValueError, AttributeError):
        return {}


def main():
    if len(sys.argv) != 4 or sys.argv[1] != "settings":
        sys.exit("usage: chat.py settings PROJECT OUT")
    projet, sortie = sys.argv[2], sys.argv[3]
    systeme = os.path.dirname(os.path.abspath(__file__))
    python = sys.executable or "/usr/bin/python3"
    garde = [python, os.path.join(systeme, "guard.py"), projet]
    commande = " ".join(shlex.quote(x) for x in garde)
    if NATIF:  # forward slashes for Git Bash, and the guard reads paths in UTF-8 whatever the code page
        garde = [x.replace("\\", "/") for x in garde]
        commande = " ".join([shlex.quote(garde[0]), "-X", "utf8"] + [shlex.quote(x) for x in garde[1:]])
    reglages = {
        "permissions": {"deny": ["Bash", "WebFetch", "WebSearch", "NotebookEdit"]},
        "hooks": {"PreToolUse": [
            {"matcher": "Read|Edit|Write|MultiEdit|NotebookEdit|Glob|Grep",
             "hooks": [{"type": "command", "timeout": 10, "command": commande}]},
        ]},
        "includeCoAuthoredBy": False,
        "enabledPlugins": plugins_utilisateur(),
    }
    d = os.path.dirname(sortie)
    if d:
        os.makedirs(d, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=d or ".", prefix=".chat-")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(reglages, f, ensure_ascii=False, indent=1)
    os.replace(tmp, sortie)


if __name__ == "__main__":
    main()
