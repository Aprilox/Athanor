#!/usr/bin/env python3
"""Roblox Studio, live: the scripts of a Rojo project reach the open Studio without restarting it.

  roblox-sync.py serve PROJECT PIDFILE   serves the project's scripts on 127.0.0.1 (a free port) and writes
                                         PROJECT/.athanor/studio/SYNC.luau: the Luau code Claude runs in
                                         Studio (MCP execute_luau, Edit) to fetch them and update the open
                                         place; stops when PIDFILE (the pilot's) disappears
  roblox-sync.py payload PROJECT         prints what one request returns (tests)

The tree follows Rojo's rules (https://rojo.space/docs/v7/sync-details/): a "$path" folder becomes a
Folder, NAME.server.luau a Script, NAME.client.luau a LocalScript, NAME.luau a ModuleScript, and a folder
holding init(.server|.client).luau becomes that script. Only scripts are sent: models, JSON, text, the
project file's own instances and properties need a rebuild of the place (roblox-studio --rebuild); the
answer lists those changed since the previous request.
Only reads the project. The address and its random key are in SYNC.luau, inside the project's workshop.
"""
import fnmatch
import http.server
import json
import os
import secrets
import socketserver
import sys
import threading
import time

EXTENSIONS = (".luau", ".lua")
MAX_SCRIPT = 4 << 20


def fichier_projet(projet):
    """The Rojo project file: default.project.json, else the first *.project.json (2 levels at most)."""
    d = os.path.join(projet, "default.project.json")
    if os.path.isfile(d):
        return d
    for racine, dossiers, fichiers in os.walk(projet):
        dossiers[:] = sorted(x for x in dossiers if not x.startswith(".") and x not in ("node_modules", "build", "Packages"))
        if os.path.relpath(racine, projet).count(os.sep) >= 1:
            dossiers[:] = []
        for f in sorted(fichiers):
            if f.endswith(".project.json"):
                return os.path.join(racine, f)
    return None


def genre_script(nom, legacy):
    """(instance name, class, RunContext) of a script file, None for anything else."""
    bas = nom.lower()
    for ext in EXTENSIONS:
        if not bas.endswith(ext):
            continue
        base = nom[: -len(ext)]
        b = base.lower()
        if b.endswith(".server"):
            return base[:-7], ("Script", "") if legacy else ("Script", "Server")
        if b.endswith(".client"):
            return base[:-7], ("LocalScript", "") if legacy else ("Script", "Client")
        if b.endswith(".module"):
            return base[:-7], ("ModuleScript", "")
        if "." in base:  # NAME.something.luau Rojo does not know: left alone
            return None
        return base, ("ModuleScript", "")
    return None


class Arbre:
    def __init__(self, projet):
        self.projet = projet
        self.fichier = fichier_projet(projet)
        self.noeuds, self.scripts, self.geres, self.autres = [], [], [], []

    def ignore(self, chemin, motifs):
        rel = os.path.relpath(chemin, os.path.dirname(self.fichier)).replace(os.sep, "/")
        return any(fnmatch.fnmatch(rel, m) for m in motifs)

    def lire(self):
        with open(self.fichier, encoding="utf-8-sig") as f:
            p = json.load(f)
        self.legacy = p.get("emitLegacyScripts", True) is not False
        self.motifs = [m for m in p.get("globIgnorePaths", []) if isinstance(m, str)]
        self.noeud(p.get("tree") or {}, [])
        self.autres.append(self.fichier)
        return self

    def noeud(self, n, chemin):
        if not isinstance(n, dict):
            return
        if chemin:
            classe = n.get("$className") if isinstance(n.get("$className"), str) else ""
            self.noeuds.append({"chemin": chemin, "classe": classe})
        p = n.get("$path")
        if isinstance(p, str) and chemin:
            cible = os.path.normpath(os.path.join(os.path.dirname(self.fichier), p))
            if os.path.isdir(cible):
                self.dossier(cible, chemin, n.get("$className") or "")
            elif os.path.isfile(cible):
                g = genre_script(os.path.basename(cible), self.legacy)
                if g:
                    self.noeuds.pop()
                    self.script(cible, chemin, g[1])
                else:
                    self.autres.append(cible)
        for k, v in n.items():
            if not k.startswith("$"):
                self.noeud(v, chemin + [k])

    def dossier(self, d, chemin, classe):
        try:
            entrees = sorted(os.listdir(d))
        except OSError:
            return
        init = None
        for e in entrees:
            g = genre_script(e, self.legacy)
            if g and g[0].lower() == "init" and os.path.isfile(os.path.join(d, e)):
                init = (os.path.join(d, e), g[1])
        if init:  # the folder is that script
            if self.noeuds and self.noeuds[-1]["chemin"] == chemin:
                self.noeuds.pop()
            self.script(init[0], chemin, init[1])
        self.geres.append(chemin)
        for e in entrees:
            f = os.path.join(d, e)
            if e.startswith(".") or self.ignore(f, self.motifs) or (init and f == init[0]):
                continue
            if os.path.isdir(f):
                self.noeuds.append({"chemin": chemin + [e], "classe": ""})
                self.dossier(f, chemin + [e], "")
                continue
            g = genre_script(e, self.legacy)
            if g:
                self.script(f, chemin + [g[0]], g[1])
            else:
                self.autres.append(f)

    def script(self, f, chemin, genre):
        try:
            if os.path.getsize(f) > MAX_SCRIPT:
                return
            with open(f, encoding="utf-8", errors="replace", newline="") as h:
                source = h.read()
        except OSError:
            return
        self.scripts.append({"chemin": chemin, "classe": genre[0], "contexte": genre[1], "source": source})


class Etat:
    """The non-script files' dates at the previous request: those changed since are announced."""

    def __init__(self):
        self.dates = None
        self.verrou = threading.Lock()

    def changes(self, autres, projet):
        dates = {}
        for f in autres:
            try:
                dates[f] = os.path.getmtime(f)
            except OSError:
                pass
        with self.verrou:
            avant, self.dates = self.dates, dates
        if avant is None:
            return []
        return sorted(os.path.relpath(f, projet).replace(os.sep, "/") for f, t in dates.items() if avant.get(f) != t)


def charge(projet, etat):
    a = Arbre(projet)
    if not a.fichier:
        return {"erreur": "no Rojo project file (default.project.json) in the project"}
    try:
        a.lire()
    except (OSError, ValueError) as e:
        return {"erreur": "the Rojo project file cannot be read: %s" % e}
    # only the containers on the way to the scripts: an instance of the project file itself (a part and
    # its properties) is the rebuild's job, never created bare here
    utiles = {tuple(c[:i]) for c in a.geres + [s["chemin"] for s in a.scripts] for i in range(1, len(c) + 1)}
    noeuds = sorted((n for n in a.noeuds if tuple(n["chemin"]) in utiles), key=lambda n: len(n["chemin"]))
    return {"noeuds": noeuds, "scripts": a.scripts, "geres": a.geres, "autres": etat.changes(a.autres, projet)}


LUAU = r'''-- Athanor: updates the open place's scripts from the project's files (Rojo layout). Run it as it is,
-- in Studio's Edit data model (MCP execute_luau), after changing scripts. Address and key: this pilot only.
local H = game:GetService("HttpService")
local avant = H.HttpEnabled
H.HttpEnabled = true
local ok, corps = pcall(H.GetAsync, H, "http://127.0.0.1:%PORT%/%CLE%", true)
H.HttpEnabled = avant
if not ok then return "Athanor sync FAILED, the pilot's server does not answer (" .. tostring(corps) .. "): use roblox-studio --rebuild" end
local d = H:JSONDecode(corps)
if d.erreur then return "Athanor sync FAILED: " .. d.erreur end
local SES = game:GetService("ScriptEditorService")
local n = { maj = 0, crees = 0, remplaces = 0, supprimes = 0 }
local function enfant(parent, nom, classe)
	local c
	if parent == game then
		local okS, s = pcall(game.GetService, game, nom)
		c = okS and s or game:FindFirstChild(nom)
	else
		c = parent:FindFirstChild(nom)
	end
	if not c and classe then
		c = Instance.new(classe ~= "" and classe or "Folder")
		c.Name = nom
		c.Parent = parent
		n.crees += 1
	end
	return c
end
local function trouver(chemin, jusqua, creer)
	local inst = game
	for i = 1, jusqua do
		inst = enfant(inst, chemin[i], creer and "Folder" or nil)
		if not inst then return nil end
	end
	return inst
end
for _, nd in ipairs(d.noeuds) do
	local parent = trouver(nd.chemin, #nd.chemin - 1, true)
	enfant(parent, nd.chemin[#nd.chemin], nd.classe)
end
local voulus = {}
for _, s in ipairs(d.scripts) do
	voulus[table.concat(s.chemin, "\0")] = true
	local parent = trouver(s.chemin, #s.chemin - 1, true)
	local nom = s.chemin[#s.chemin]
	local inst = parent:FindFirstChild(nom)
	if inst and inst.ClassName ~= s.classe then
		local neuf = Instance.new(s.classe)
		neuf.Name = nom
		for _, c in ipairs(inst:GetChildren()) do c.Parent = neuf end
		inst:Destroy()
		neuf.Parent = parent
		inst = neuf
		n.remplaces += 1
	elseif not inst then
		inst = Instance.new(s.classe)
		inst.Name = nom
		inst.Parent = parent
		n.crees += 1
	end
	if s.contexte ~= "" then pcall(function() inst.RunContext = Enum.RunContext[s.contexte] end) end
	if inst.Source ~= s.source then
		if not pcall(function() SES:UpdateSourceAsync(inst, function() return s.source end) end) then inst.Source = s.source end
		n.maj += 1
	end
end
for _, g in ipairs(d.geres) do
	local dossier = trouver(g, #g, false)
	if dossier then
		local base = table.concat(g, "\0") .. "\0"
		for _, c in ipairs(dossier:GetChildren()) do
			if c:IsA("LuaSourceContainer") and not voulus[base .. c.Name] then
				c:Destroy()
				n.supprimes += 1
			end
		end
	end
end
local r = string.format("Athanor sync OK: %d script(s) updated, %d instance(s) created, %d replaced, %d removed (%d scripts in the project).", n.maj, n.crees, n.remplaces, n.supprimes, #d.scripts)
if #d.autres > 0 then
	r ..= " Changed but NOT synced (models, JSON, text, the project file): " .. table.concat(d.autres, ", ", 1, math.min(#d.autres, 8)) .. ": roblox-studio --rebuild"
end
return r
'''


def servir(projet, fichier_pid):
    cle = secrets.token_urlsafe(18)
    etat = Etat()
    charge(projet, etat)  # the dates of reference

    class Gestion(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path != "/" + cle:
                self.send_error(404)
                return
            corps = json.dumps(charge(projet, etat), ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(corps)))
            self.end_headers()
            self.wfile.write(corps)

        def log_message(self, *a):
            pass

    class Serveur(socketserver.ThreadingMixIn, http.server.HTTPServer):
        daemon_threads = True
        allow_reuse_address = False

    s = Serveur(("127.0.0.1", 0), Gestion)
    port = s.server_address[1]
    dossier = os.path.join(projet, ".athanor", "studio")
    os.makedirs(dossier, exist_ok=True)
    cible = os.path.join(dossier, "SYNC.luau")
    tmp = cible + ".tmp"
    with open(tmp, "w", encoding="utf-8", newline="\n") as f:
        f.write(LUAU.replace("%PORT%", str(port)).replace("%CLE%", cle))
    os.replace(tmp, cible)
    print("Roblox sync server on 127.0.0.1:%d" % port, flush=True)

    def garde():  # the pilot stopped (its pid file is gone or another pilot took over): stop too
        try:
            pid = open(fichier_pid).read().strip()
        except OSError:
            pid = None
        while True:
            time.sleep(5)
            try:
                if open(fichier_pid).read().strip() != pid:
                    break
            except OSError:
                break
        try:
            os.remove(cible)
        except OSError:
            pass
        os._exit(0)

    threading.Thread(target=garde, daemon=True).start()
    s.serve_forever()


if __name__ == "__main__":
    a = sys.argv[1:]
    if len(a) == 3 and a[0] == "serve":
        servir(os.path.abspath(a[1]), a[2])
    elif len(a) == 2 and a[0] == "payload":
        print(json.dumps(charge(os.path.abspath(a[1]), Etat()), ensure_ascii=False, indent=1))
    else:
        sys.exit("usage: roblox-sync.py serve PROJECT PIDFILE | payload PROJECT")
