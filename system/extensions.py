#!/usr/bin/env python3
"""Skills and MCP servers chosen in the app (Skills & MCP page), for one Claude session.

The library lives in the tool folder, out of Claude's reach (the app writes it, the pilot reads it):
  extensions/bibliotheque.json   {"skills": {id: {...}}, "mcp": {id: {"config": {...}, ...}}}
  extensions/skills/<id>/        the skill folders (SKILL.md and its files)
  extensions/projets.json        {project folder: {"skills": [ids], "mcp": [ids], "plugins": [ids]}}
  extensions/secrets.json        {mcp id: {VARIABLE: value}}   (API keys, never in a log)
  extensions/plugins/<id>/       Claude Code plugins, taken apart by the app (api_plugins.go): their MCP
                                 servers are in the library (on with the plugin), their hooks removed

Usage:
  extensions.py preparer KIT PROJECT PRIVATE_DIR [--mcp FILE] [--dossier-mcp DIR] [--principal DIR]
      rebuilds PRIVATE_DIR/plugin (the project's skills, loaded with --plugin-dir), PRIVATE_DIR/plugins/
      (a copy of each plugin switched on, one --plugin-dir each) and the MCP file (default
      PRIVATE_DIR/mcp.json); prints one summary line.
  extensions.py mcp KIT PROJECT FILE [--dossier-mcp DIR] [--principal DIR]
      only the MCP file (a parallel worker: its servers see its worktree).
  extensions.py tester KIT PROJECT ID
      starts the server ID in its cage, lists its tools, prints a JSON result (the app's "Test" button).
  extensions.py connexion KIT ID FILE
      FILE: an MCP configuration with the remote server ID alone, exactly as the pilot gives it, for the
      one-time sign-in window of the app (Claude Code keeps the sign-in, valid for every project).
A server's settings (an address, a port, a path: "reglages" in the library, name -> default) are
${NAME} placeholders like the keys, with the default when the user set nothing. The "unreal" server of
the verified catalog goes through the Windows relay (system/unreal_relay.txt) when "mirrored"
networking is unavailable. A "windows" server (a Windows program: Roblox Studio's, a .exe, a .bat) is
started on Windows through WSL interop by system/windows-mcp.sh, outside the cage.
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
            return {k: [x for x in (v.get(k) or []) if isinstance(x, str) and ID_SUR.match(x)] for k in ("skills", "mcp", "plugins")}
    return {"skills": [], "mcp": [], "plugins": []}


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


def construire_plugins(kit, choix, prive):
    """PRIVATE_DIR/plugins/<id>: a copy of each plugin switched on for the project (the app already took
    out their hooks and MCP servers). Copies, not links, read-only for Claude (settings.py)."""
    source = os.path.join(kit, "extensions", "plugins")
    biblio = lire_json(os.path.join(kit, "extensions", "bibliotheque.json"), {}).get("plugins") or {}
    neuf, final, vieux = (os.path.join(prive, n) for n in ("plugins.neuf", "plugins", "plugins.vieux"))
    shutil.rmtree(neuf, ignore_errors=True)
    os.makedirs(neuf)
    copies = []
    for pid in choix.get("plugins", []):
        d = os.path.join(source, pid)
        if pid in biblio and os.path.isdir(d) and not os.path.islink(d):
            shutil.copytree(d, os.path.join(neuf, pid), symlinks=True,
                            ignore=shutil.ignore_patterns(".git", "__pycache__"))
            for r, ds, fs in os.walk(os.path.join(neuf, pid)):   # a link is never followed nor kept
                for n in ds + fs:
                    if os.path.islink(os.path.join(r, n)):
                        os.remove(os.path.join(r, n))
            for f in ("hooks/hooks.json", ".mcp.json", ".lsp.json", "settings.json"):   # never run outside the sandbox
                try:
                    os.remove(os.path.join(neuf, pid, f))
                except OSError:
                    pass
            for f in ("monitors", "workflows"):
                shutil.rmtree(os.path.join(neuf, pid, f), ignore_errors=True)
            copies.append(pid)
    shutil.rmtree(vieux, ignore_errors=True)
    if os.path.isdir(final):
        os.rename(final, vieux)
    os.rename(neuf, final)
    shutil.rmtree(vieux, ignore_errors=True)
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
    # settings: the default unless the user set a value
    reglages = {k: str(v) for k, v in (definition.get("reglages") or {}).items() if isinstance(k, str)} if isinstance(definition.get("reglages"), dict) else {}
    secrets = dict(reglages, **{k: v for k, v in (secrets or {}).items() if v != ""})
    if definition.get("catalogue") == "unreal":
        secrets["UNREAL_MCP_URL"] = adresse_unreal(kit, secrets.get("UNREAL_MCP_URL", ""))
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
    # a plugin's server: its own variables (the plugin's folder, read-only in the cage; a data folder of its own)
    pid = definition.get("plugin") if isinstance(definition.get("plugin"), str) and ID_SUR.match(definition.get("plugin")) else ""
    racine_plugin = os.path.join(kit, "extensions", "plugins", pid) if pid else ""
    if pid:
        donnees = os.path.join(kit, "tools", "plugin-data", pid)
        os.makedirs(donnees, exist_ok=True)
        secrets = dict(secrets, CLAUDE_PLUGIN_ROOT=racine_plugin, CLAUDE_PLUGIN_DATA=donnees, CLAUDE_PROJECT_DIR=dossier)
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
        if racine_plugin:
            cage += ["--ro", racine_plugin]
        # the server's variables go to the cage as arguments, set inside it (bwrap --setenv): never in the
        # environment of mcp-jail.sh itself, which runs outside the cage (BASH_ENV, LD_PRELOAD, PATH…)
        for k, v in sorted(env.items()):
            if not re.match(r"^[A-Za-z_][A-Za-z0-9_]{0,127}$", k) or "\0" in v:
                return None, "invalid variable " + k[:40]
            cage += ["--env", k, v]
        s = {"type": "stdio", "command": os.path.join(kit, "system", "mcp-jail.sh"),
             "args": cage + ["--", remplacer(cmd, secrets, dossier, manquants)] + args}
    elif t == "windows":
        # a Windows program: started on Windows (WSL interop), outside the cage; its variables reach it alone
        cmd = c.get("command")
        if not isinstance(cmd, str) or not cmd.strip():
            return None, "no command"
        if not interop_windows():
            return None, "Windows program: Windows is not reachable from here"
        dossier_w = chemin_windows(dossier)   # {PROJECT} as the Windows program knows it (C:\…)
        args = [remplacer(str(a), secrets, dossier_w, manquants) for a in (c.get("args") or [])]
        lancement = []
        for k, v in sorted(facultatif(c.get("env")).items()):
            if not re.match(r"^[A-Za-z_][A-Za-z0-9_]{0,127}$", k) or "\0" in v:
                return None, "invalid variable " + k[:40]
            lancement += ["--env", k, v]
        s = {"type": "stdio", "command": os.path.join(kit, "system", "windows-mcp.sh"),
             "args": lancement + ["--", remplacer(cmd, secrets, dossier_w, manquants)] + args}
    else:
        return None, "unknown type " + str(t)
    if manquants:
        return None, "missing key " + ", ".join(sorted(manquants))
    return s, ""


def interop_windows():
    """WSL can start Windows programs (interop on): the condition of a "windows" server."""
    return any(os.path.exists("/proc/sys/fs/binfmt_misc/" + n) for n in ("WSLInterop", "WSLInterop-late")) \
        or bool(os.environ.get("WSL_INTEROP"))


def chemin_windows(dossier):
    """A WSL folder as Windows names it (/mnt/c/x -> C:\\x); left as it is when wslpath cannot say."""
    try:
        w = subprocess.run(["wslpath", "-w", dossier], capture_output=True, text=True, timeout=10).stdout.strip()
        return w or dossier
    except (OSError, subprocess.SubprocessError):
        return dossier


def adresse_unreal(kit, url):
    """The Unreal MCP as seen from WSL: through the Windows relay (commands/unreal-network.cmd) when
    "mirrored" networking is unavailable; the relay's port is in system/unreal_relay.txt and the Windows
    host is WSL's default gateway (it changes at every WSL start)."""
    url = url or "http://127.0.0.1:8000/mcp"
    relais = os.path.join(kit, "system", "unreal_relay.txt")
    if not os.path.isfile(relais) or not re.match(r"^https?://(127\.0\.0\.1|localhost)(:\d+)?(/|$)", url):
        return url
    try:
        port = re.sub(r"\D", "", open(relais, encoding="utf-8").read())
        route = subprocess.run(["ip", "route", "show", "default"], capture_output=True, text=True, timeout=5).stdout.split()
        passerelle = route[route.index("via") + 1] if "via" in route else ""
    except (OSError, ValueError, subprocess.SubprocessError):
        return url
    if not port or not passerelle:
        return url
    return re.sub(r"^(https?://)[^/]+", r"\g<1>%s:%s" % (passerelle, port), url, count=1)


def construire_mcp(kit, choix, projet, fichier, dossier, principal):
    serveurs, notes = {}, []
    biblio = lire_json(os.path.join(kit, "extensions", "bibliotheque.json"), {}).get("mcp", {})
    tous_secrets = lire_json(os.path.join(kit, "extensions", "secrets.json"), {})
    # the servers switched on, and those of the plugins switched on (on and off with their plugin)
    ids = [x for x in choix["mcp"] if not (biblio.get(x) or {}).get("plugin")]
    ids += sorted(x for x, d in biblio.items() if isinstance(d, dict) and d.get("plugin") in choix.get("plugins", []) and ID_SUR.match(x))
    for sid in ids:
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
    return list(serveurs), notes


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
        arreter(p)
        erreur = lire_reste(p.stderr).decode("utf-8", "replace").strip().splitlines()[-6:]
        return {"ok": False, "erreur": str(e), "details": erreur}
    finally:
        arreter(p)


def lire_reste(flux, delai=3, taille_max=1 << 20):
    """What a stopped server left on a pipe, without waiting for ever: a Windows program can outlive its
    launcher and keep the pipe open."""
    morceaux, fin = [], time.time() + delai
    while time.time() < fin and sum(map(len, morceaux)) < taille_max:
        pret, _, _ = select.select([flux], [], [], max(0, fin - time.time()))
        if not pret:
            break
        bout = os.read(flux.fileno(), 65536)
        if not bout:
            break
        morceaux.append(bout)
    return b"".join(morceaux)


def arreter(p):
    """Stops a server the way Claude Code does: end of its input, then TERM (a Windows program's launcher
    stops the whole Windows process tree on TERM), KILL only if it is still there."""
    if p.poll() is not None:
        return
    try:
        p.stdin.close()
    except OSError:
        pass
    p.terminate()
    try:
        p.wait(5)
    except subprocess.TimeoutExpired:
        p.kill()
        p.wait()


def main():
    if len(sys.argv) < 4:
        print(__doc__, file=sys.stderr); return 2
    action, kit, projet = sys.argv[1], os.path.abspath(sys.argv[2]), sys.argv[3]
    if action == "tester":
        sid = sys.argv[4] if len(sys.argv) > 4 else ""
        print(json.dumps(tester(kit, projet, sid) if ID_SUR.match(sid) else {"ok": False, "erreur": "invalid id"}))
        return 0
    if action == "connexion":
        sid, fichier = (sys.argv[3], sys.argv[4]) if len(sys.argv) > 4 else ("", "")
        biblio = lire_json(os.path.join(kit, "extensions", "bibliotheque.json"), {}).get("mcp", {})
        if not ID_SUR.match(sid) or sid not in biblio:
            print("unknown server", file=sys.stderr); return 1
        s, raison = serveur(kit, sid, biblio[sid], (lire_json(os.path.join(kit, "extensions", "secrets.json"), {}).get(sid) or {}), os.getcwd(), None)
        if not s or s.get("type") not in ("http", "sse"):
            print(raison or "not a remote server", file=sys.stderr); return 1
        fd = os.open(fichier, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump({"mcpServers": {sid: s}}, f)
        return 0
    choix = choix_du_projet(kit, projet)
    if action == "preparer":
        prive = sys.argv[4]; o = options(sys.argv[5:])
        os.makedirs(prive, exist_ok=True)
        skills = construire_plugin(kit, choix, prive)
        plugins = construire_plugins(kit, choix, prive)
        mcp, notes = construire_mcp(kit, choix, projet, o.get("mcp") or os.path.join(prive, "mcp.json"), o.get("dossier-mcp") or projet, o.get("principal"))
        print("skills: " + (", ".join(skills) or "none") + " · plugins: " + (", ".join(plugins) or "none") + " · MCP: " + (", ".join(mcp) or "none") + ("" if not notes else " · " + "; ".join(notes)))
        return 0
    if action == "mcp":
        fichier = sys.argv[4]; o = options(sys.argv[5:])
        construire_mcp(kit, choix, projet, fichier, o.get("dossier-mcp") or projet, o.get("principal"))
        return 0
    print(__doc__, file=sys.stderr); return 2


if __name__ == "__main__":
    sys.exit(main())
