#!/usr/bin/env python3
"""Tasks for the user: what Claude cannot do itself (an account to create, a payment, a code received by
SMS, a key to generate...). Claude writes them with `athanor todo`, the user answers them in the
application (page "For you"): done, or skip (find another way). Claude checks each answer on its next
cycle and says whether it works or what is still missing.

  .athanor/for-you/tasks.json     written by Claude (this script, never by hand)
  .athanor/for-you/replies.json   written by the application: the user's answers, round by round
  .athanor/secrets.env            written by the application: values the user gave (NAME=value)
  .athanor/WAITING_FOR_YOU        `athanor todo wait`: nothing else to do, the pilot pauses until an answer

States of a task (derived, the same in the application):
  pour_vous      waits for the user (new, or reopened by Claude with what is missing)
  a_verifier     the user says it is done: Claude checks it first
  a_contourner   the user skipped it: Claude finds another way without it
  close          verified, or resolved another way

  athanor todo add "TITLE" [--need "what I need back"] [--secret NAME]... [--milestone M3]... < details
  athanor todo edit ID [--need ...] [--secret NAME]... < details   rewrite the steps of an open task
  athanor todo list [--all]          the tasks and the user's answers
  athanor todo ok ID "what you checked"           it works: closed, the user is told
  athanor todo missing ID "what is still missing"  back to the user, with this message
  athanor todo resolved ID "what you did instead"   skipped by the user, or no longer needed: closed
  athanor todo wait                  every remaining milestone waits for the user: the pilot pauses
  taches.py etat PROJECT             (pilot) {"pour_vous": n, "a_traiter": n, "attente": 0|1}
"""
import datetime, json, os, re, stat, sys, time

# Native Windows (no O_NOFOLLOW, O_NONBLOCK nor fcntl there): links are checked by path before opening,
# and locks are msvcrt's. In WSL and on Linux, nothing changes.
NOFOLLOW, NONBLOCK, BINAIRE = (getattr(os, n, 0) for n in ("O_NOFOLLOW", "O_NONBLOCK", "O_BINARY"))
try:
    import fcntl

    def verrouiller(fd):
        fcntl.flock(fd, fcntl.LOCK_EX)
except ImportError:
    import msvcrt

    def verrouiller(fd):
        while True:
            try:
                msvcrt.locking(fd, msvcrt.LK_LOCK, 1)
                return
            except OSError:
                time.sleep(0.2)


def pas_un_lien(chemin):
    """Where O_NOFOLLOW does not exist, a link is refused before opening."""
    if not NOFOLLOW and os.path.islink(chemin):
        raise OSError("%s is a link" % chemin)
    return chemin

MAX_TITRE, MAX_DETAILS, MAX_ATTENDU, MAX_NOTE = 150, 8000, 1000, 2000
MAX_OUVERTES, MAX_TOTAL, MAX_FICHIER = 30, 200, 2 << 20
NOM_SECRET = re.compile(r"[A-Z][A-Z0-9_]{1,59}")
JALON = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,19}")
ID = re.compile(r"T[0-9]{1,5}")
LIGNE_SECRET = re.compile(r"([A-Z][A-Z0-9_]{1,59})='(?:[^']|'\\'')*'")
# names a value could not take: secrets.env is sourced with `set -a`, they would change how commands run
SECRETS_INTERDITS = {"PATH", "HOME", "SHELL", "ENV", "IFS", "PS1", "PS2", "PS3", "PS4", "PROMPT_COMMAND", "SHELLOPTS",
                     "CDPATH", "GLOBIGNORE", "TMPDIR", "PWD", "OLDPWD", "USER", "LOGNAME", "TERM", "LANG", "TOOLS", "PROJECT",
                     "SSH_AUTH_SOCK", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "EDITOR", "VISUAL", "PAGER"}
PREFIXES_INTERDITS = ("LD_", "DYLD_", "BASH", "GIT_", "NPM_CONFIG_", "PYTHON", "NODE_", "PERL", "RUBY", "LC_",
                      "ATHANOR", "CLAUDE", "ANTHROPIC", "XDG_", "SUDO")


def nom_secret_valide(n):
    return isinstance(n, str) and bool(NOM_SECRET.fullmatch(n)) and n not in SECRETS_INTERDITS and not n.startswith(PREFIXES_INTERDITS)


def maintenant():
    return datetime.datetime.now().strftime("%Y-%m-%dT%H:%M:%S")


class Erreur(Exception):
    pass


def dossier_sur(projet, *parties):
    """PROJECT/.athanor/... without ever going through a symbolic link (the workshop is Claude's)."""
    d = os.path.join(projet, ".athanor")
    if os.path.islink(d) or not os.path.isdir(d):
        raise Erreur("no workshop (.athanor/) in %s" % projet)
    for p in parties:
        d = os.path.join(d, p)
        if os.path.islink(d):
            raise Erreur("%s is a link: nothing written" % d)
        if not os.path.isdir(d):
            os.mkdir(d)
    return d


def lire_octets(chemin, taille_max):
    """A regular file only: never through a link, never blocking on a pipe Claude put in its place."""
    fd = os.open(pas_un_lien(chemin), os.O_RDONLY | NOFOLLOW | NONBLOCK | BINAIRE)
    with os.fdopen(fd, "rb") as f:
        if not stat.S_ISREG(os.fstat(f.fileno()).st_mode):
            raise OSError("not a regular file")
        b = f.read(taille_max + 1)
    if len(b) > taille_max:
        raise OSError("file too large")
    return b


def lire_json(chemin, defaut):
    for essai in range(3):
        try:
            d = json.loads(lire_octets(chemin, MAX_FICHIER).decode("utf-8-sig"))
            return d if isinstance(d, type(defaut)) else defaut
        except OSError:
            return defaut
        except ValueError:   # being rewritten: try again
            time.sleep(0.1)
    return defaut


def ecrire_json(chemin, d):
    t = "%s.%d.tmp" % (chemin, os.getpid())
    fd = os.open(pas_un_lien(t), os.O_WRONLY | os.O_CREAT | os.O_TRUNC | NOFOLLOW | NONBLOCK | BINAIRE, 0o644)
    with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as o:
        json.dump(d, o, ensure_ascii=False, indent=2)
    os.replace(t, chemin)


# The files are read the same way here and in the application (api_pourvous.go): a field of the wrong type
# takes its empty value, an entry that is not an object is skipped, the first task of an identifier wins.
def texte(v):
    return v if isinstance(v, str) else ""


def entier(v):
    return v if type(v) is int and v >= 0 else 0


def textes(v):
    return [x for x in v if isinstance(x, str)] if isinstance(v, list) else []


def normaliser(t):
    return {"id": t["id"], "titre": texte(t.get("titre")), "details": texte(t.get("details")), "attendu": texte(t.get("attendu")),
            "secrets": textes(t.get("secrets")), "jalons": textes(t.get("jalons")), "statut": texte(t.get("statut")),
            "tour": entier(t.get("tour")), "note": texte(t.get("note")), "cree": texte(t.get("cree")), "maj": texte(t.get("maj")),
            "historique": [{"tour": entier(j.get("tour")), "statut": texte(j.get("statut")), "note": texte(j.get("note")), "le": texte(j.get("le"))}
                           for j in (t.get("historique") if isinstance(t.get("historique"), list) else []) if isinstance(j, dict)]}


def lire_taches(projet):
    d = lire_json(os.path.join(projet, ".athanor", "for-you", "tasks.json"), {})
    taches, vus = [], set()
    for t in d.get("taches") if isinstance(d.get("taches"), list) else []:
        if isinstance(t, dict) and isinstance(t.get("id"), str) and ID.fullmatch(t["id"]) and t["id"] not in vus:
            vus.add(t["id"])
            taches.append(normaliser(t))
    return {"suivant": entier(d.get("suivant")) or len(taches) + 1, "taches": taches}


def lire_reponses(projet):
    d = lire_json(os.path.join(projet, ".athanor", "for-you", "replies.json"), {})
    return {k: [{"action": texte(r.get("action")), "texte": texte(r.get("texte")), "fichiers": textes(r.get("fichiers")),
                 "secrets": textes(r.get("secrets")), "le": texte(r.get("le"))} for r in v if isinstance(r, dict)]
            for k, v in d.items() if isinstance(v, list)}


def etat(t, reponses):
    n = len(reponses)
    tour = entier(t.get("tour"))
    if t.get("statut") in ("verifiee", "resolue") and tour >= n:
        return "close"
    if n > tour:
        return "a_contourner" if reponses[-1].get("action") == "passer" else "a_verifier"
    return "pour_vous"


def secrets_presents(projet):
    noms = set()
    try:
        for l in lire_octets(os.path.join(projet, ".athanor", "secrets.env"), MAX_FICHIER).decode("utf-8", "replace").split("\n"):
            m = LIGNE_SECRET.fullmatch(l)
            if m:
                noms.add(m.group(1))
    except OSError:
        pass
    return noms


def vus(projet):
    """The answers Claude has read, per task (written by `athanor todo list`): a verdict never closes an
    answer Claude has not seen, even one sent while it was judging."""
    d = lire_json(os.path.join(projet, ".athanor", "for-you", "seen.json"), {})
    return {k: entier(v) for k, v in d.items()}


def court(s, n):
    s = (s or "").strip()
    return s if len(s) <= n else s[:n - 1] + "…"


# ----------------------------------------------------------------------------- commands (Claude)
def verrou(projet):
    d = dossier_sur(projet, "for-you")
    fd = os.open(pas_un_lien(os.path.join(d, ".lock")), os.O_WRONLY | os.O_CREAT | NOFOLLOW | NONBLOCK, 0o644)
    verrouiller(fd)
    return d, fd


def lire_entree():
    if sys.stdin is None or sys.stdin.isatty():
        return ""
    return sys.stdin.read(MAX_DETAILS + 1)


def options(args, multiples=("--secret", "--milestone")):
    pos, opt = [], {"--need": None}
    for m in multiples:
        opt[m] = []
    i = 0
    while i < len(args):
        a = args[i]
        if a in opt and i + 1 < len(args):
            if a in multiples:
                opt[a] += [x.strip() for x in args[i + 1].split(",") if x.strip()]
            else:
                opt[a] = args[i + 1]
            i += 2
        else:
            pos.append(a)
            i += 1
    return pos, opt


def valider_champs(opt):
    for s in opt["--secret"]:
        if not nom_secret_valide(s):
            raise Erreur("secret name %r: capital letters, digits and _ (e.g. SHOPIFY_ADMIN_TOKEN), not a name the shell"
                         " or a tool already uses (PATH, LD_…, GIT_…)" % s)
    for j in opt["--milestone"]:
        if not JALON.fullmatch(j):
            raise Erreur("milestone %r: its identifier as in the plan (e.g. M3)" % j)
    if opt["--need"] is not None and len(opt["--need"]) > MAX_ATTENDU:
        raise Erreur("--need: %d characters at most" % MAX_ATTENDU)


def cmd_add(projet, args):
    pos, opt = options(args)
    titre = " ".join(pos).strip()
    if not titre or len(titre) > MAX_TITRE or "\n" in titre:
        raise Erreur("a title on one line, %d characters at most" % MAX_TITRE)
    valider_champs(opt)
    details = lire_entree()
    if len(details) > MAX_DETAILS:
        raise Erreur("details: %d characters at most" % MAX_DETAILS)
    d, fd = verrou(projet)
    try:
        f = lire_taches(projet)
        rep = lire_reponses(projet)
        for t in f["taches"]:
            if t.get("titre", "").strip().lower() == titre.lower() and etat(t, rep.get(t["id"], [])) != "close":
                print("%s already asks this (%s): use `athanor todo edit %s` to change it." % (t["id"], etat(t, rep.get(t["id"], [])), t["id"]))
                return 0
        ouvertes = [t for t in f["taches"] if etat(t, rep.get(t["id"], [])) != "close"]
        if len(ouvertes) >= MAX_OUVERTES:
            raise Erreur("already %d tasks open for the user: close or merge some first" % MAX_OUVERTES)
        n = max(f["suivant"], 1)
        pris = {t["id"] for t in f["taches"]} | set(rep)
        while "T%d" % n in pris:
            n += 1
        if n > 99999:
            raise Erreur("too many tasks in this project")
        t = {"id": "T%d" % n, "titre": titre, "details": details.strip(), "attendu": (opt["--need"] or "").strip(),
             "secrets": sorted(set(opt["--secret"])), "jalons": opt["--milestone"], "statut": "ouverte", "tour": 0,
             "note": "", "cree": maintenant(), "maj": maintenant()}
        f["taches"].append(t)
        f["suivant"] = n + 1
        # the oldest closed tasks go when the list is too long
        while len(f["taches"]) > MAX_TOTAL:
            closes = [x for x in f["taches"] if etat(x, rep.get(x["id"], [])) == "close"]
            if not closes:
                break
            f["taches"].remove(closes[0])
        ecrire_json(os.path.join(d, "tasks.json"), f)
    finally:
        os.close(fd)
    print("%s created: the user sees it in the application (page For you). Go on with what does not need it;"
          " if nothing else can be done, run `athanor todo wait`." % t["id"])
    return 0


def trouver(f, id_):
    for t in f["taches"]:
        if t["id"] == id_.upper():
            return t
    raise Erreur("no task %s (athanor todo list --all)" % id_)


def cmd_edit(projet, args):
    pos, opt = options(args)
    if len(pos) != 1:
        raise Erreur("usage: athanor todo edit ID [--need ...] [--secret NAME]... < new details")
    valider_champs(opt)
    details = lire_entree()
    if len(details) > MAX_DETAILS:
        raise Erreur("details: %d characters at most" % MAX_DETAILS)
    d, fd = verrou(projet)
    try:
        f = lire_taches(projet)
        t = trouver(f, pos[0])
        if details.strip():
            t["details"] = details.strip()
        if opt["--need"] is not None:
            t["attendu"] = opt["--need"].strip()
        if opt["--secret"]:
            t["secrets"] = sorted(set(t.get("secrets", [])) | set(opt["--secret"]))
        if opt["--milestone"]:
            t["jalons"] = opt["--milestone"]
        t["maj"] = maintenant()
        ecrire_json(os.path.join(d, "tasks.json"), f)
    finally:
        os.close(fd)
    print("%s updated." % t["id"])
    return 0


def cmd_juger(projet, args, statut):
    if len(args) < 2 or not " ".join(args[1:]).strip():
        raise Erreur("usage: athanor todo %s ID \"message for the user\"" % {"verifiee": "ok", "manque": "missing", "resolue": "resolved"}[statut])
    note = " ".join(args[1:]).strip()
    if len(note) > MAX_NOTE:
        raise Erreur("message: %d characters at most" % MAX_NOTE)
    d, fd = verrou(projet)
    try:
        f = lire_taches(projet)
        t = trouver(f, args[0])
        # the rounds Claude has read (athanor todo list): an answer sent since then stays to be handled
        tour = min(len(lire_reponses(projet).get(t["id"], [])), vus(projet).get(t["id"], 0))
        if statut == "verifiee" and not tour:
            statut = "resolue"   # nothing answered by the user: closed all the same
        t.update(statut=statut, tour=tour, note=note, maj=maintenant())
        hist = t["historique"]
        hist.append({"tour": tour, "statut": statut, "note": note, "le": maintenant()})
        del hist[:-20]
        ecrire_json(os.path.join(d, "tasks.json"), f)
    finally:
        os.close(fd)
    print({"verifiee": "%s closed as verified: the user is told.",
           "resolue": "%s closed: the user is told.",
           "manque": "%s back with the user, with your message."}[statut] % t["id"])
    return 0


def cmd_wait(projet, args):
    f, rep = lire_taches(projet), lire_reponses(projet)
    etats = [etat(t, rep.get(t["id"], [])) for t in f["taches"]]
    if any(e in ("a_verifier", "a_contourner") for e in etats):
        raise Erreur("the user answered tasks: handle them first (athanor todo list)")
    if "pour_vous" not in etats:
        raise Erreur("no task waits for the user: carry on with the plan")
    d = dossier_sur(projet)
    t = os.path.join(d, ".WAITING_FOR_YOU.%d" % os.getpid())
    with open(t, "w") as o:
        o.write(maintenant() + "\n")
    os.replace(t, os.path.join(d, "WAITING_FOR_YOU"))
    print("The pilot pauses after this cycle and resumes as soon as the user answers. End the cycle now"
          " (commit what is done, note it in .athanor/LOG.md).")
    return 0


ETIQUETTES = {"pour_vous": "waits for the user", "a_verifier": "THE USER SAYS IT IS DONE: check it now",
              "a_contourner": "THE USER SKIPPED IT: find another way without it", "close": "closed"}


def cmd_list(projet, args):
    f, rep = lire_taches(projet), lire_reponses(projet)
    try:   # what Claude has now read (see vus)
        d, fd = verrou(projet)
        try:
            ecrire_json(os.path.join(d, "seen.json"), {t["id"]: len(rep.get(t["id"], [])) for t in f["taches"]})
        finally:
            os.close(fd)
    except (OSError, Erreur):
        pass
    tout = "--all" in args
    presents = secrets_presents(projet)
    lignes = []
    for t in f["taches"]:
        r = rep.get(t["id"], [])
        e = etat(t, r)
        if e == "close" and not tout:
            continue
        lignes.append("## %s · %s [%s]" % (t["id"], t.get("titre", ""), ETIQUETTES[e]))
        if t.get("jalons"):
            lignes.append("Blocks: " + ", ".join(t["jalons"]))
        if t.get("details") and e != "close":
            lignes.append("Steps you gave: " + court(t["details"], 600).replace("\n", " / "))
        if t.get("attendu"):
            lignes.append("You asked for: " + t["attendu"])
        if t.get("secrets"):
            lignes.append("Secrets: " + ", ".join("%s (%s)" % (s, "given, in .athanor/secrets.env" if s in presents else "not given yet") for s in t["secrets"]))
        if t.get("note") and e != "a_verifier":
            lignes.append("Your last message: " + t["note"])
        if r:
            x = r[-1]
            lignes.append("User's answer (round %d, %s, %s): %s" % (len(r), "done" if x.get("action") != "passer" else "skip, find another way",
                                                                 x.get("le", ""), court(x.get("texte") or "(no text)", 4000)))
            if x.get("fichiers"):
                lignes.append("Files: " + ", ".join("`%s`" % p for p in x["fichiers"] if isinstance(p, str))[:3000])
            if x.get("secrets"):
                lignes.append("Secrets given: " + ", ".join(s for s in x["secrets"] if isinstance(s, str)) + " (in .athanor/secrets.env)")
        lignes.append("")
    if not lignes:
        print("No task for the user%s." % ("" if tout else " open"))
    else:
        print("\n".join(lignes).rstrip())
    return 0


def cmd_etat(projet):
    f, rep = lire_taches(projet), lire_reponses(projet)
    etats = [etat(t, rep.get(t["id"], [])) for t in f["taches"]]
    print(json.dumps({"pour_vous": etats.count("pour_vous"),
                      "a_traiter": etats.count("a_verifier") + etats.count("a_contourner"),
                      "attente": int(os.path.isfile(os.path.join(projet, ".athanor", "WAITING_FOR_YOU")))}))
    return 0


def main(argv, projet):
    if not argv:
        print(__doc__)
        return 2
    c, args = argv[0], argv[1:]
    try:
        if c == "add":
            return cmd_add(projet, args)
        if c == "edit":
            return cmd_edit(projet, args)
        if c == "list":
            try:
                return cmd_list(projet, args)
            except BrokenPipeError:
                return 0
        if c in ("ok", "missing", "resolved"):
            return cmd_juger(projet, args, {"ok": "verifiee", "missing": "manque", "resolved": "resolue"}[c])
        if c == "wait":
            return cmd_wait(projet, args)
    except Erreur as e:
        print("athanor todo: %s" % e, file=sys.stderr)
        return 1
    print(__doc__)
    return 2


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "etat":
        sys.exit(cmd_etat(os.path.abspath(sys.argv[2])))
    sys.exit(main(sys.argv[1:], os.environ.get("PROJECT") or os.getcwd()))
