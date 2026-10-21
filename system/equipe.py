#!/usr/bin/env python3
"""The team of sub-agents: Athanor's own (template/.claude/agents), the ones the user creates in the app
(page Team) and the ones the project lead proposes (.athanor/agents/<name>.md, imported by the pilot).

  extensions/agents.json   {"agents": [{nom, description, consignes, modele, lecture_seule, projet,
                                         origine, cree}], "refuses": {project folder: [names]}}
     projet: "" = every project, else the project's folder as the app knows it (C:\\... or /home/...)
     modele: "" (as the other sub-agents) | "eco" (CHEAP_MODEL) | "fort" (opus)
     origine: "utilisateur" | "chef"

  equipe.py importer KIT PROJECT     imports the lead's proposals (validated), prints one line each
  equipe.py json KIT PROJECT AGENTS_DIR [--ouvrier]
                                      the --agents JSON (Athanor's agents + the project's own); with
                                      --ouvrier: executor + the project's own only (parallel workers)
  equipe.py fiche KIT PROJECT AGENTS_DIR   writes PROJECT/.athanor/TEAM.md (the team, refused proposals)

The proposals are data written by Claude: names, sizes and fields are validated, links are never
followed, and a proposed sub-agent gets nothing more than the others (same sandbox, same tools).
"""
import datetime, json, os, re, shutil, subprocess, sys

NOM_SUR = re.compile(r"^[a-z][a-z0-9-]{1,30}$")
RESERVES = {"architect", "creator", "developer", "tester", "critic", "executor", "scout", "general-purpose",
            "explore", "plan", "statusline-setup", "output-style-setup", "claude-code-guide", "fork"}
MAX_DESCRIPTION, MAX_CONSIGNES, MAX_PAR_PROJET, MAX_DU_CHEF = 400, 6000, 12, 5
MODELES = {"", "eco", "fort"}
PIED = ("\n\nNobody will answer your questions: when a detail is missing, choose the simplest option that fits "
        "`.athanor/REQUEST.md` and say so in your report. The files the user attached to the request are in "
        "`.athanor/attachments/` (listed in its \"Attached files\" section): read those that concern your task. "
        "The skills and MCP tools of the project are available to you as well (the Skill tool, `mcp__…` tools): "
        "use them whenever they fit the task. End with a short report: what you did, what you checked, what is left.")


def normaliser(p):
    p = (p or "").replace("\\", "/").rstrip("/").lower()
    return "//wsl.localhost/" + p[len("//wsl$/"):] if p.startswith("//wsl$/") else p


def formes(projet):
    """Every form under which the app may have written the project's folder."""
    noms = {normaliser(projet), normaliser(os.path.realpath(projet))}
    if shutil.which("wslpath"):
        try:
            w = subprocess.run(["wslpath", "-w", projet], capture_output=True, text=True, timeout=10).stdout.strip()
            if w:
                noms.add(normaliser(w))
        except (OSError, subprocess.SubprocessError):
            pass
    return noms


def cle_projet(projet):
    """The folder as the app writes it: the Windows form under WSL, else the path itself."""
    if shutil.which("wslpath"):
        try:
            w = subprocess.run(["wslpath", "-w", projet], capture_output=True, text=True, timeout=10).stdout.strip()
            if w:
                return w
        except (OSError, subprocess.SubprocessError):
            pass
    return projet


def fichier(kit):
    return os.path.join(kit, "extensions", "agents.json")


def lire(kit):
    try:
        with open(fichier(kit), encoding="utf-8-sig") as f:
            d = json.load(f)
    except (OSError, ValueError):
        d = {}
    if not isinstance(d, dict):
        d = {}
    d["agents"] = [a for a in d.get("agents", []) if isinstance(a, dict) and valide(a) is None]
    d["refuses"] = {k: [n for n in v if isinstance(n, str)] for k, v in (d.get("refuses") or {}).items() if isinstance(v, list)}
    return d


def ecrire(kit, d):
    f = fichier(kit)
    os.makedirs(os.path.dirname(f), exist_ok=True)
    t = f + ".%d.tmp" % os.getpid()
    with open(t, "w", encoding="utf-8") as o:
        json.dump(d, o, ensure_ascii=False, indent=2)
    os.replace(t, f)


def valide(a):
    """None when the definition is acceptable, else the reason (English, for the log)."""
    nom = a.get("nom")
    if not isinstance(nom, str) or not NOM_SUR.match(nom):
        return "invalid name (lowercase letters, digits and dashes, 2 to 31 characters)"
    if nom in RESERVES:
        return "name taken by a built-in sub-agent"
    for k, n in (("description", MAX_DESCRIPTION), ("consignes", MAX_CONSIGNES)):
        v = a.get(k)
        if not isinstance(v, str) or not v.strip() or len(v) > n:
            return "%s missing or longer than %d characters" % (k, n)
    if a.get("modele", "") not in MODELES:
        return "unknown model"
    if not isinstance(a.get("projet", ""), str):
        return "invalid project"
    return None


def du_projet(d, projet):
    noms = formes(projet)
    return [a for a in d["agents"] if not a.get("projet") or normaliser(a["projet"]) in noms][:MAX_PAR_PROJET]


def refuses_du_projet(d, projet):
    noms = formes(projet)
    r = set()
    for k, v in d["refuses"].items():
        if normaliser(k) in noms:
            r.update(v)
    return r


def lire_sans_lien(chemin, taille_max):
    fd = os.open(chemin, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as f:
        b = f.read(taille_max + 1)
    if len(b) > taille_max:
        raise ValueError("file too large")
    return b.decode("utf-8", "replace")


def lire_proposition(texte):
    m = re.match(r"^\ufeff?---\r?\n(.*?)\r?\n---\r?\n(.*)$", texte, re.S)
    if not m:
        raise ValueError("no front matter (--- name / description / model / read_only ---)")
    meta = {}
    for l in m.group(1).splitlines():
        if ":" in l:
            k, v = l.split(":", 1)
            meta[k.strip().lower()] = v.strip().strip('"').strip("'")
    premier = lambda k: (meta.get(k, "").split() or [""])[0].lower()   # "default   (or cheap...)" -> "default"
    modele = {"": "", "default": "", "inherit": "", "eco": "eco", "cheap": "eco", "strong": "fort", "fort": "fort"}.get(premier("model"))
    if modele is None:
        raise ValueError("unknown model (default, cheap or strong)")
    return {"nom": meta.get("name", "").lower(), "description": meta.get("description", ""), "consignes": m.group(2).strip(),
            "modele": modele, "lecture_seule": premier("read_only") in ("yes", "true", "1", "oui")}


def importer(kit, projet):
    dossier = os.path.join(projet, ".athanor", "agents")
    if os.path.islink(dossier) or not os.path.isdir(dossier):
        return
    if os.path.exists(fichier(kit)):
        try:  # an unreadable file is never overwritten (the user's sub-agents are in it)
            with open(fichier(kit), encoding="utf-8-sig") as f:
                if not isinstance(json.load(f), dict):
                    return
        except (OSError, ValueError):
            return
    d = lire(kit)
    refuses = refuses_du_projet(d, projet)
    existants = {a["nom"] for a in du_projet(d, projet)}
    du_chef = [a for a in du_projet(d, projet) if a.get("origine") == "chef"]
    cle = cle_projet(projet)
    change = False
    for nom in sorted(os.listdir(dossier)):
        chemin = os.path.join(dossier, nom)
        if not nom.endswith(".md") or nom.startswith("."):
            continue
        try:
            a = lire_proposition(lire_sans_lien(chemin, 40000))
            raison = valide(a)
            if raison is None and a["nom"] in existants:
                raison = "a sub-agent already has this name"
            if raison is None and a["nom"] in refuses:
                raison = "the user removed this sub-agent: do not propose it again"
            if raison is None and len(du_chef) >= MAX_DU_CHEF:
                raison = "already %d sub-agents proposed by the lead in this project" % MAX_DU_CHEF
            if raison is None and len(existants) >= MAX_PAR_PROJET:
                raison = "the team is full (%d sub-agents of the project)" % MAX_PAR_PROJET
        except OSError:
            a, raison = {"nom": nom[:-3]}, "unreadable, or a link in place of the file"
        except ValueError as e:
            a, raison = {"nom": nom[:-3]}, str(e)
        if raison is None:
            a.update(projet=cle, origine="chef", cree=datetime.date.today().isoformat())
            d["agents"].append(a)
            existants.add(a["nom"])
            du_chef.append(a)
            change = True
            print("Sub-agent proposed by the lead added to the team: %s (%s)" % (a["nom"], a["description"][:120]))
        else:
            print("Sub-agent proposal refused (%s): %s" % (nom, raison))
            journal_refus(projet, a.get("nom") or nom, raison)
        try:
            os.unlink(chemin)
        except OSError:
            pass
    if change:
        ecrire(kit, d)


def journal_refus(projet, nom, raison):
    # kept in the workshop for the lead (TEAM.md lists them); data only, never read back by the pilot
    f = os.path.join(projet, ".athanor", "agents", ".refused.txt")
    try:
        fd = os.open(f, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o644)
        with os.fdopen(fd, "a", encoding="utf-8") as o:
            o.write("%s: %s\n" % (re.sub(r"[^a-z0-9.-]", "", nom.lower())[:40], raison))
    except OSError:
        pass


def definition(a):
    x = {"description": a["description"].strip(), "prompt": a["consignes"].strip() + PIED}
    if a.get("lecture_seule"):
        x["disallowedTools"] = ["Write", "Edit", "NotebookEdit"]
    # "eco": CHEAP_MODEL as for executor (absent = haiku, empty = like the other sub-agents)
    simple = os.environ.get("CHEAP_MODEL", "haiku")
    if a.get("modele") == "eco" and simple:
        x["model"] = simple
    elif a.get("modele") == "fort":
        x["model"] = "opus"
    elif os.environ.get("SUBAGENT_MODEL"):
        x["model"] = os.environ["SUBAGENT_MODEL"]
    return x


def agents_athanor(dossier):
    """Athanor's sub-agents (template/.claude/agents copies), with their model."""
    sous, simple = os.environ.get("SUBAGENT_MODEL", ""), os.environ.get("CHEAP_MODEL", "")
    out = {}
    for f in sorted(os.listdir(dossier)) if os.path.isdir(dossier) else []:
        if not f.endswith(".md"):
            continue
        t = open(os.path.join(dossier, f), encoding="utf-8").read()
        m = re.match(r"^---\n(.*?)\n---\n(.*)$", t, re.S)
        if not m:
            continue
        meta = dict((l.split(":", 1)[0].strip(), l.split(":", 1)[1].strip()) for l in m.group(1).splitlines() if ":" in l)
        a = {"description": meta.get("description", ""), "prompt": m.group(2).strip()}
        # tools: an allowlist (MCP tools and skills are lost unless named); disallowedTools: the rest is inherited
        if meta.get("tools"):
            a["tools"] = [x.strip() for x in meta["tools"].split(",")]
        if meta.get("disallowedTools"):
            a["disallowedTools"] = [x.strip() for x in meta["disallowedTools"].split(",")]
        nom = meta.get("name", f[:-3])
        modele = meta.get("model") or (simple if nom == "executor" else "") or sous or ("sonnet" if nom == "scout" else "")
        if modele:
            a["model"] = modele
            if not meta.get("model"):  # .md copy (when --agents does not exist): same model
                open(os.path.join(dossier, f), "w", encoding="utf-8").write("---\n" + m.group(1) + "\nmodel: " + modele + "\n---\n" + m.group(2))
        out[nom] = a
    return out


def json_agents(kit, projet, dossier, ouvrier=False):
    out = agents_athanor(dossier)
    if ouvrier:
        out = {k: v for k, v in out.items() if k == "executor"}
    for a in du_projet(lire(kit), projet):
        out[a["nom"]] = definition(a)
    return out


def fiche(kit, projet, dossier):
    etat = os.path.join(projet, ".athanor")
    if os.path.islink(etat) or not os.path.isdir(etat):
        return
    d = lire(kit)
    lignes = ["# Your team (written by the pilot before each cycle, do not edit)", "",
              "Call them with the Task tool by their name. Propose a new one in `.athanor/agents/` (see your instructions).", ""]
    for nom, a in agents_athanor_lecture(dossier).items():
        lignes.append("- `%s` (Athanor): %s" % (nom, a))
    for a in du_projet(d, projet):
        qui = "created by the user" if a.get("origine") != "chef" else "proposed by you, kept by the user"
        lignes.append("- `%s` (%s%s): %s" % (a["nom"], qui, ", read only" if a.get("lecture_seule") else "", a["description"].strip().replace("\n", " ")))
    refus = sorted(refuses_du_projet(d, projet))
    if refus:
        lignes += ["", "Removed by the user, never propose them again: " + ", ".join("`%s`" % n for n in refus)]
    r = os.path.join(etat, "agents", ".refused.txt")
    if os.path.isfile(r) and not os.path.islink(r):
        try:
            derniers = lire_sans_lien(r, 20000).strip().splitlines()[-5:]
            if derniers:
                lignes += ["", "Your last refused proposals:"] + ["- " + l[:200] for l in derniers]
        except (OSError, ValueError):
            pass
    t = os.path.join(etat, ".TEAM.md.%d" % os.getpid())
    fd = os.open(t, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
    with os.fdopen(fd, "w", encoding="utf-8") as o:
        o.write("\n".join(lignes) + "\n")
    os.replace(t, os.path.join(etat, "TEAM.md"))


def agents_athanor_lecture(dossier):
    out = {}
    for f in sorted(os.listdir(dossier)) if os.path.isdir(dossier) else []:
        if f.endswith(".md"):
            t = open(os.path.join(dossier, f), encoding="utf-8").read()
            m = re.search(r"^description:\s*(.*)$", t, re.M)
            out[f[:-3]] = (m.group(1).strip() if m else "")[:300]
    return out


if __name__ == "__main__":
    a = sys.argv[1:]
    if len(a) >= 3 and a[0] == "importer":
        importer(a[1], os.path.abspath(a[2]))
    elif len(a) >= 4 and a[0] == "json":
        print(json.dumps(json_agents(a[1], os.path.abspath(a[2]), a[3], "--ouvrier" in a), ensure_ascii=False))
    elif len(a) >= 4 and a[0] == "fiche":
        fiche(a[1], os.path.abspath(a[2]), a[3])
    else:
        print(__doc__)
        sys.exit(2)
