#!/usr/bin/env python3
"""Writes the Claude Code settings for one cycle: system sandbox,
permissions, file guard, RTK and Unreal MCP. Called by the pilot at every start.

Usage: settings.py KIT OUTPUT.json   (PROJECT in the environment)
"""
import json, os, shlex, shutil, sys, tempfile

# Native Windows mode (Git Bash, no WSL): Claude Code has no sandbox there (its docs: "On native Windows,
# Claude Code runs commands unsandboxed"; with failIfUnavailable it would not even start). The user chose
# that mode in the application, which says so; the file guard and the deny rules stay.
NATIF = os.name == "nt"


def chemin(p):
    """Native Windows: a Git Bash path the environment passed unconverted (/c/x) as Windows knows it (C:/x)."""
    if NATIF and p and len(p) >= 2 and p[0] == "/" and p[1].isalpha() and (len(p) == 2 or p[2] == "/"):
        return p[1] + ":/" + p[3:]
    return p


for v in ("PROJECT", "ATHANOR_PROJET_PRINCIPAL", "ATHANOR_PLUGIN", "ATHANOR_PLUGINS"):
    if os.environ.get(v):
        os.environ[v] = chemin(os.environ[v])

kit = os.path.realpath(sys.argv[1]); sortie = sys.argv[2]
tools, system = (os.path.join(kit, d) for d in ("tools", "system"))
projet = os.path.realpath(os.environ.get("PROJECT") or os.path.join(kit, "projet"))
# The pilot works in the logical path (/mnt/c/...); on some PCs it resolves to another one (Docker Desktop
# bind mounts). The sandbox gets both forms, so that reads work whichever one a tool uses.
projets = sorted({projet, os.path.abspath(os.environ.get("PROJECT") or os.path.join(kit, "projet"))})
# Unreal MCP: the "unreal" server of the library switched on for this project (Skills & MCP page)
sys.dont_write_bytecode = True
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
try:
    from extensions import choix_du_projet
    unreal_mcp = "unreal" in choix_du_projet(kit, os.environ.get("ATHANOR_PROJET_PRINCIPAL") or os.environ.get("PROJECT") or projet)["mcp"]
except Exception:
    unreal_mcp = False
# The skills chosen in the app (Skills & MCP page), copied by system/extensions.py: readable, never writable.
plugin = os.environ.get("ATHANOR_PLUGIN") or ""
plugins_tiers = os.environ.get("ATHANOR_PLUGINS") or ""   # the plugins switched on (Plugins tab)
lectures = [system] + ([plugin] if plugin else []) + ([plugins_tiers] if plugins_tiers else [])
# Claude Code's own folder for this project (CONFIG/projects/<project path, non-alphanumerics as "-">):
# a long command output is saved there and Claude reads it back. Read-only, and only this project's.
import re
config_claude = os.environ.get("CLAUDE_CONFIG_DIR") or os.path.join(os.path.expanduser("~"), ".claude")
for p in sorted({os.environ.get("PROJECT") or "", projet}):
    if p:
        lectures.append(os.path.join(config_claude, "projects", re.sub(r"[^A-Za-z0-9]", "-", p)))
# The plugins the user switched on in their own Claude Code (~/.claude/settings.json) stay off in Athanor's
# sessions: their hooks would run outside the sandbox and their servers would open what nobody asked
# for here; Athanor's plugins come from its Plugins tab (--plugin-dir).
def plugins_utilisateur():
    try:
        with open(os.path.join(config_claude, "settings.json"), encoding="utf-8-sig") as f:
            p = json.load(f).get("enabledPlugins")
        return {k: False for k, v in p.items() if v is True} if isinstance(p, dict) else {}
    except (OSError, ValueError, AttributeError):
        return {}
# Hooks run OUTSIDE the sandbox: never a program taken from tools/ (modifiable by Claude).
rtk_bin = os.path.join(system, "bin", "rtk")
rtk = os.environ.get("RTK", "1") != "0" and os.access(rtk_bin, os.X_OK)
python = sys.executable or "/usr/bin/python3"

# Folders of the detected PC software: read-only (project templates, plugins, SDKs...).
logiciels = []
try:
    for ligne in open(os.path.join(system, "pc_tools.env"), encoding="utf-8"):
        cle, _, val = ligne.strip().partition("=")
        val = val.strip("'")
        if cle in ("UE_DIR", "ANDROID_SDK_DIR", "ANDROID_JDK_DIR") and val:
            logiciels.append(val)
        if cle == "BLENDER_EXE" and val:
            logiciels.append(os.path.dirname(val))
except OSError:
    pass

# Gateways to the PC tools and Docker: they leave the sandbox, so they live
# in system/ (not modifiable by Claude) and must be allowed one by one.
passerelles = ["tool", "unreal-editor", "unreal-cmd", "unreal-uat", "blender", "roblox-studio",
               "install-windows", "pc-exec", "windows-var", "adb"]

reglages = {
    "language": "english",
    "permissions": {
        # No-questions mode: nobody is there to answer. The limits come from the system
        # sandbox (files, no escape hatch) and from the file tools guard (hook).
        "defaultMode": "bypassPermissions",
        "allow": ["WebFetch", "WebFetch(domain:*)", "WebSearch"]
                 + [f"Bash({p} *)" for p in passerelles]
                 + (["mcp__unreal"] if unreal_mcp else []),
        "deny": ["Bash(git push --force *)", "Bash(git push -f *)"],
        "additionalDirectories": [tools],
    },
    "env": {
        "TOOLS": tools,
        "PROJECT": projet,
        # the bridge of the main project (a parallel worker works in a worktree, the bridge stays with the lead)
        "ATHANOR_PONT": os.path.join(os.environ.get("ATHANOR_PROJET_PRINCIPAL") or os.environ.get("PROJECT") or os.path.join(kit, "projet"), ".athanor", "pont"),
        "XDG_CACHE_HOME": os.path.join(tools, "cache"),
        "PIP_CACHE_DIR": os.path.join(tools, "cache", "pip"),
        "npm_config_cache": os.path.join(tools, "cache", "npm"),
        "PIP_DISABLE_PIP_VERSION_CHECK": "1",
        "BASH_DEFAULT_TIMEOUT_MS": "600000",
        "BASH_MAX_TIMEOUT_MS": "900000",
    },
    "sandbox": {
        "enabled": True,
        "failIfUnavailable": True,          # no sandbox = Claude does not start
        "autoAllowBashIfSandboxed": True,   # commands inside the sandbox: no questions
        "allowUnsandboxedCommands": False,  # no "rerun outside the sandbox" escape hatch
        "excludedCommands": [f"{p} *" for p in passerelles],
        "filesystem": {
            "allowWrite": [tools],
            "denyRead": ["~/", "/mnt"],
            "allowRead": projets + [tools] + lectures + logiciels,   # system, skills: read-only
        },
        "network": {"allowedDomains": ["*"]},
        # tokens Claude must never inherit from the user's environment (GitHub: Athanor pushes with a
        # key valid for the project's repository alone, see github.sh)
        "credentials": {"envVars": [{"name": v, "mode": "deny"} for v in (
            "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY",
            "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN")]},
    },
    "enableAllProjectMcpServers": unreal_mcp,
    "enabledPlugins": plugins_utilisateur(),
    "hooks": {"PreToolUse": [
        {"matcher": "Read|Edit|Write|MultiEdit|NotebookEdit|Glob|Grep",
         "hooks": [{"type": "command", "timeout": 10,
                    "command": " ".join(shlex.quote(x) for x in [python, os.path.join(system, "guard.py"), projet, tools, tempfile.gettempdir() if NATIF else "/tmp"] + [f"--lecture={d}" for d in lectures] + [f"--lecture={d}" for d in logiciels])}]},
        # no long waits, no closing the user's Studio or Unreal by program name (system/bash_guard.py)
        {"matcher": "Bash",
         "hooks": [{"type": "command", "timeout": 10,
                    "command": " ".join(shlex.quote(x) for x in [python, os.path.join(system, "bash_guard.py")])}]},
    ]},
}
if NATIF:
    # no sandbox: no sandbox settings; the gateways run directly (no bridge); Windows paths written with
    # "/" so that the Git Bash of Claude's commands reads them as they are
    del reglages["sandbox"]
    reglages["env"]["PONT_DIRECT"] = "1"
    # the pilot tells Git Bash not to rewrite Claude's own arguments; Claude's commands get the usual rewriting back
    reglages["env"]["MSYS2_ARG_CONV_EXCL"] = ""
    # the guard reads paths in UTF-8 whatever the Windows code page
    for crochet in reglages["hooks"]["PreToolUse"][:2]:
        crochet["hooks"][0]["command"] = crochet["hooks"][0]["command"].replace(
            shlex.quote(python), shlex.quote(python.replace("\\", "/")) + " -X utf8", 1)
    reglages["hooks"]["PreToolUse"][1]["hooks"][0]["command"] = reglages["hooks"]["PreToolUse"][1]["hooks"][0]["command"].replace(
        shlex.quote(os.path.join(system, "bash_guard.py")), shlex.quote(os.path.join(system, "bash_guard.py").replace("\\", "/")), 1)
    reglages["env"] = {k: (v.replace("\\", "/") if isinstance(v, str) else v) for k, v in reglages["env"].items()}
    reglages["permissions"]["additionalDirectories"] = [d.replace("\\", "/") for d in reglages["permissions"]["additionalDirectories"]]
    rtk = False   # RTK here is a Linux program
if rtk:
    reglages["hooks"]["PreToolUse"].append(
        {"matcher": "Bash", "hooks": [{"type": "command", "timeout": 10, "command": "exec " + shlex.quote(rtk_bin) + " hook claude"}]})

# Commits without Claude as co-author (GitHub block of the Manage page, read by the pilot through github.sh)
if os.environ.get("ATHANOR_SANS_COAUTEUR") == "1":
    reglages["attribution"] = {"commit": "", "pr": ""}
    reglages["includeCoAuthoredBy"] = False   # older Claude Code versions

os.makedirs(os.path.dirname(sortie), exist_ok=True)
with open(sortie, "w", encoding="utf-8") as f:
    json.dump(reglages, f, ensure_ascii=False, indent=2)
print(sortie)
