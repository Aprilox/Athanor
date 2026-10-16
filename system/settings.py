#!/usr/bin/env python3
"""Writes the Claude Code settings for one cycle: system sandbox,
permissions, file guard, RTK and Unreal MCP. Called by the pilot at every start.

Usage: settings.py KIT OUTPUT.json   (PROJECT in the environment)
"""
import json, os, shlex, shutil, sys

kit = os.path.realpath(sys.argv[1]); sortie = sys.argv[2]
tools, system = (os.path.join(kit, d) for d in ("tools", "system"))
projet = os.path.realpath(os.environ.get("PROJECT") or os.path.join(kit, "projet"))
# The pilot works in the logical path (/mnt/c/...); on some PCs it resolves to another one (Docker Desktop
# bind mounts). The sandbox gets both forms, so that reads work whichever one a tool uses.
projets = sorted({projet, os.path.abspath(os.environ.get("PROJECT") or os.path.join(kit, "projet"))})
unreal_mcp = os.environ.get("UNREAL_MCP", "0") == "1"
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
passerelles = ["tool", "unreal-editor", "unreal-cmd", "unreal-uat", "blender",
               "install-windows", "pc-exec", "windows-var", "adb"]

reglages = {
    "language": "english",
    "permissions": {
        # No-questions mode: nobody is there to answer. The limits come from the system
        # sandbox (files, no escape hatch) and from the file tools guard (hook).
        "defaultMode": "bypassPermissions",
        "allow": ["WebFetch", "WebFetch(domain:*)", "WebSearch"]
                 + [f"Bash({p} *)" for p in passerelles]
                 + (["mcp__unreal-mcp"] if unreal_mcp else []),
        "deny": ["Bash(git push --force *)", "Bash(git push -f *)"],
        "additionalDirectories": [tools],
    },
    "env": {
        "TOOLS": tools,
        "PROJECT": projet,
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
            "allowRead": projets + [tools, system] + logiciels,   # system: read-only (RTK, pilot tools)
        },
        "network": {"allowedDomains": ["*"]},
        "credentials": {"envVars": [{"name": "CLAUDE_CODE_OAUTH_TOKEN", "mode": "deny"},
                                     {"name": "ANTHROPIC_API_KEY", "mode": "deny"}]},
    },
    "enableAllProjectMcpServers": unreal_mcp,
    "hooks": {"PreToolUse": [
        {"matcher": "Read|Edit|Write|MultiEdit|NotebookEdit|Glob|Grep",
         "hooks": [{"type": "command", "timeout": 10,
                    "command": " ".join(shlex.quote(x) for x in [python, os.path.join(system, "guard.py"), projet, tools, "/tmp", f"--lecture={system}"] + [f"--lecture={d}" for d in logiciels])}]},
    ]},
}
if rtk:
    reglages["hooks"]["PreToolUse"].append(
        {"matcher": "Bash", "hooks": [{"type": "command", "timeout": 10, "command": "exec " + shlex.quote(rtk_bin) + " hook claude"}]})

os.makedirs(os.path.dirname(sortie), exist_ok=True)
with open(sortie, "w", encoding="utf-8") as f:
    json.dump(reglages, f, ensure_ascii=False, indent=2)
print(sortie)
