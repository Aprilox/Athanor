#!/usr/bin/env python3
"""Skills and MCP servers chosen in the app (Skills & MCP page), for one Claude session.

The library lives in the tool folder, out of Claude's reach (the app writes it, the pilot reads it):
  extensions/bibliotheque.json   {"skills": {id: {...}}, "mcp": {id: {"config": {...}, ...}}}
  extensions/skills/<id>/        the skill folders (SKILL.md and its files)
  extensions/projets.json        {project folder: {"skills": [ids], "mcp": [ids]}}
  extensions/secrets.json        {mcp id: {VARIABLE: value}}   (API keys, never in a log)

Usage:
  extensions.py preparer KIT PROJECT PRIVATE_DIR [--mcp FILE] [--dossier-mcp DIR] [--principal DIR]
      rebuilds PRIVATE_DIR/plugin (the project's skills, loaded with --plugin-dir) and the MCP file
      (default PRIVATE_DIR/mcp.json); prints one summary line.
  extensions.py mcp KIT PROJECT FILE [--dossier-mcp DIR] [--principal DIR]
      only the MCP file (a parallel worker: its servers see its worktree).
  extensions.py tester KIT PROJECT ID
      starts the server ID in its cage, lists its tools, prints a JSON result (the app's "Test" button).
Unreal MCP (UNREAL_MCP=1, UNREAL_MCP_URL) is part of the MCP file as before.
"""
import json, os, re, select, shutil, subprocess, sys, time

ID_SUR = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")


def lire_json(chemin, defaut):
    try:
        with open(chemin, encoding="utf-8-sig") as f:
            v = json.load(f)
        return v if isinstance(v, type(defaut)) else defaut
    except (OSError, ValueError):
        return defaut


def normaliser(p):
    return p.replace("\\", "/").rstrip("/").lower()


def choix_du_projet(kit, projet):
    """The project's entry in projets.json. The app writes the folder as it knows it (C:\\... on Windows,
    /home/... on Linux); the pilot knows the WSL form (/mnt/c/...): both are compared."""
    projets = lire_json(os.path.join(kit, "extensions", "projets.json"), {})
    noms = {normaliser(projet), normaliser(os.path.realpath(projet))}
    if shutil.which("wslpath"):
        try:
            w = subprocess.run(["wslpath", "-w", projet], capture_output=True, text=True, timeout=10).stdout.strip()
            if w:
                noms.add(normaliser(w))
        except (OSError, subprocess.SubprocessError):
            pass
    for cle, v in projets.items():
        if normaliser(cle) in noms and isinstance(v, dict):
            return {"skills": [x for x in v.get("skills", []) if isinstance(x, str) and ID_SUR.match(x)],
                    "mcp": [x for x in v.get("mcp", []) if isinstance(x, str) and ID_SUR.match(x)]}
    return {"skills": [], "mcp": []}


def construire_plugin(kit, choix, prive):
    """PRIVATE_DIR/plugin: a Claude Code plugin named "athanor" holding a copy of each chosen skill
    (copies, not links: Claude Code refuses links that lead outside a plugin)."""
    source = os.path.join(kit, "extensions", "skills")
    neuf, final = os.path.join(prive, "plugin.neuf"), os.path.join(prive, "plugin")
    shutil.rmtree(neuf, ignore_errors=True)
    copies = []
    for sid in choix["skills"]:
        d = os.path.join(source, sid)
        if os.path.isfile(os.path.join(d, "SKILL.md")) and not os.path.islink(d):
            shutil.copytree(d, os.path.join(neuf, "skills", sid), symlinks=False, ignore_dangling_symlinks=True,
                            ignore=shutil.ignore_patterns(".git", "__pycache__"))
            copies.append(sid)
    if copies:
        os.makedirs(os.path.join(neuf, ".claude-plugin"), exist_ok=True)
        with open(os.path.join(neuf, ".claude-plugin", "plugin.json"), "w", encoding="utf-8") as f:
            json.dump({"name": "athanor", "description": "Skills chosen in Athanor for this project"}, f)
    vieux = os.path.join(prive, "plugin.vieux")
    shutil.rmtree(vieux, ignore_errors=True)
    if os.path.isdir(final):
        os.rename(final, vieux)
    if copies:
        os.rename(neuf, final)
    shutil.rmtree(vieux, ignore_errors=True)
    shutil.rmtree(neuf, ignore_errors=True)
    return copies


def remplacer(valeur, secrets, dossier, manquants):
    """${VARIABLE} -> secret of the server, {PROJECT} -> the folder the server works on."""
    if not isinstance(valeur, str):
        return valeur
    def secret(m):
        if m.group(1) in secrets:
            return secrets[m.group(1)]
        manquants.add(m.group(1))
        return ""
    return re.sub(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}", secret, valeur.replace("{PROJECT}", dossier))


def serveur(kit, sid, definition, secrets, dossier, principal):
    """Claude Code configuration of one server, or (None, reason). Local servers (stdio) are started
    through system/mcp-jail.sh."""
    c = definition.get("config") if isinstance(definition, dict) else None
    if not isinstance(c, dict):
        return None, "no configuration"
    manquants = set()
    # an optional key (a header or variable that only improves limits) is left out when it is not set
    optionnels = set(definition.get("optionnels") or [])
    def facultatif(cles):
        """Values of a header or env table; an entry missing only optional keys is dropped."""
        garde = {}
        for k, v in (cles or {}).items():
            m = set()
            val = remplacer(str(v), secrets, dossier, m)
            if m and m <= optionnels:
                continue
            manquants.update(m)
            garde[str(k)] = val
        return garde
    t = c.get("type") or ("http" if c.get("url") else "stdio")
    if t in ("http", "sse"):
        url = remplacer(c.get("url", ""), secrets, dossier, manquants)
        if not re.match(r"^https?://", url):
            return None, "invalid URL"
        s = {"type": t, "url": url}
        h = facultatif(c.get("headers"))
        if h:
            s["headers"] = h
    elif t == "stdio":
        cmd = c.get("command")
        if not isinstance(cmd, str) or not cmd.strip():
            return None, "no command"
        args = [remplacer(str(a), secrets, dossier, manquants) for a in (c.get("args") or [])]
        env = facultatif(c.get("env"))
        cage = [sid, os.path.join(kit, "tools"), "--rw", dossier]
        if principal and os.path.realpath(principal) != os.path.realpath(dossier):
            # a worker: its worktree writable, the main project readable, its .git writable (git objects)
            cage = [sid, os.path.join(kit, "tools"), "--ro", principal, "--rw", dossier]
            if os.path.isdir(os.path.join(principal, ".git")):
                cage += ["--rw", os.path.join(principal, ".git")]
        s = {"type": "stdio", "command": os.path.join(kit, "system", "mcp-jail.sh"),
             "args": cage + ["--", remplacer(cmd, secrets, dossier, manquants)] + args}
        if env:
            s["env"] = env
    else:
        return None, "unknown type " + str(t)
    if manquants:
        return None, "missing key " + ", ".join(sorted(manquants))
    return s, ""


def construire_mcp(kit, choix, projet, fichier, dossier, principal):
    serveurs, notes = {}, []
    if os.environ.get("UNREAL_MCP", "0") == "1":
        serveurs["unreal-mcp"] = {"type": "http", "url": os.environ.get("UNREAL_MCP_URL") or "http://127.0.0.1:8000/mcp"}
    biblio = lire_json(os.path.join(kit, "extensions", "bibliotheque.json"), {}).get("mcp", {})
    tous_secrets = lire_json(os.path.join(kit, "extensions", "secrets.json"), {})
    for sid in choix["mcp"]:
        if sid not in biblio:
            continue
        s, raison = serveur(kit, sid, biblio[sid], tous_secrets.get(sid) or {}, dossier, principal)
        if s:
            serveurs[sid] = s
        else:
            notes.append(f"{sid} skipped ({raison})")
    if serveurs:
        neuf = fichier + ".neuf"
        fd = os.open(neuf, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump({"mcpServers": serveurs}, f, ensure_ascii=False, indent=2)
        os.replace(neuf, fichier)
    elif os.path.exists(fichier):
        os.remove(fichier)
    return [k for k in serveurs if k != "unreal-mcp"], notes


def options(args):
    v = {}
    while args:
        cle = args.pop(0)
        if cle.startswith("--") and args:
            v[cle[2:]] = args.pop(0)
    return v


def tester(kit, projet, sid, delai=150):
    biblio = lire_json(os.path.join(kit, "extensions", "bibliotheque.json"), {}).get("mcp", {})
    if sid not in biblio:
        return {"ok": False, "erreur": "unknown server"}
    s, raison = serveur(kit, sid, biblio[sid], (lire_json(os.path.join(kit, "extensions", "secrets.json"), {}).get(sid) or {}), projet, None)
    if not s:
        return {"ok": False, "erreur": raison}
    if s["type"] != "stdio":
        return {"ok": True, "outils": [], "note": "remote server: Claude Code connects to it at the next cycle"}
    env = dict(os.environ, **s.get("env", {}))
    try:
        p = subprocess.Popen([s["command"]] + s["args"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, cwd=projet if os.path.isdir(projet) else None)
    except OSError as e:
        return {"ok": False, "erreur": str(e)}
    def envoyer(m):
        p.stdin.write((json.dumps(m) + "\n").encode()); p.stdin.flush()
    def attendre(i, fin):
        tampon = b""
        while time.time() < fin:
            pret, _, _ = select.select([p.stdout], [], [], 1)
            if not pret:
                if p.poll() is not None:
                    return None
                continue
            bout = os.read(p.stdout.fileno(), 65536)
            if not bout:
                return None
            tampon += bout
            while b"\n" in tampon:
                ligne, tampon = tampon.split(b"\n", 1)
                try:
                    m = json.loads(ligne)
                except ValueError:
                    continue
                if m.get("id") == i:
                    return m
        return None
    fin = time.time() + delai
    try:
        envoyer({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "athanor", "version": "1"}}})
        r = attendre(1, fin)
        if not r or "result" not in r:
            raise RuntimeError("no answer to initialize")
        info = r["result"].get("serverInfo") or None
        envoyer({"jsonrpc": "2.0", "method": "notifications/initialized"})
        envoyer({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
        r = attendre(2, fin)
        outils = [t.get("name", "") for t in ((r or {}).get("result") or {}).get("tools", [])]
        return {"ok": True, "outils": outils, "serveur": info}
    except (RuntimeError, OSError, BrokenPipeError) as e:
        p.kill()
        erreur = (p.stderr.read() or b"").decode("utf-8", "replace").strip().splitlines()[-6:]
        return {"ok": False, "erreur": str(e), "details": erreur}
    finally:
        if p.poll() is None:
            p.kill()


def main():
    if len(sys.argv) < 4:
        print(__doc__, file=sys.stderr); return 2
    action, kit, projet = sys.argv[1], os.path.abspath(sys.argv[2]), sys.argv[3]
    if action == "tester":
        sid = sys.argv[4] if len(sys.argv) > 4 else ""
        print(json.dumps(tester(kit, projet, sid) if ID_SUR.match(sid) else {"ok": False, "erreur": "invalid id"}))
        return 0
    choix = choix_du_projet(kit, projet)
    if action == "preparer":
        prive = sys.argv[4]; o = options(sys.argv[5:])
        os.makedirs(prive, exist_ok=True)
        skills = construire_plugin(kit, choix, prive)
        mcp, notes = construire_mcp(kit, choix, projet, o.get("mcp") or os.path.join(prive, "mcp.json"), o.get("dossier-mcp") or projet, o.get("principal"))
        print("skills: " + (", ".join(skills) or "none") + " · MCP: " + (", ".join(mcp) or "none") + ("" if not notes else " · " + "; ".join(notes)))
        return 0
    if action == "mcp":
        fichier = sys.argv[4]; o = options(sys.argv[5:])
        construire_mcp(kit, choix, projet, fichier, o.get("dossier-mcp") or projet, o.get("principal"))
        return 0
    print(__doc__, file=sys.stderr); return 2


if __name__ == "__main__":
    sys.exit(main())
