#!/usr/bin/env python3
"""Bridge between the sandbox and the PC tools.

Inside the sandbox, commands cannot start Windows programs or reach Docker.
The gateways (unreal-cmd, blender, tool...) therefore drop a request in PROJECT/.athanor/pont/;
this service, started by the pilot OUTSIDE the sandbox, runs it and streams the output back live.
Only the gateways in the list are accepted, and only from the project or tools/.

Usage (by the pilot): bridge.py KIT [PROJECT]
"""
import json, os, signal, subprocess, sys, threading, time

KIT = os.path.realpath(sys.argv[1])
SYSTEME, TOOLS = (os.path.join(KIT, d) for d in ("system", "tools"))
# kept as given (logical): dedans() resolves both sides at each request, so a mount path that changes
# while the pilot runs (Docker Desktop bind mounts) does not lock the gateways out
PROJECT = os.path.abspath(sys.argv[2]) if len(sys.argv) > 2 else os.path.join(KIT, "projet")
DOSSIER = os.path.join(PROJECT, ".athanor", "pont")
AUTORISES = {"tool", "unreal-editor", "unreal-cmd", "unreal-uat", "blender",
             "install-windows", "pc-exec", "windows-var", "adb"}
en_cours = {}
NOFOLLOW = getattr(os, "O_NOFOLLOW", 0)


# The bridge folder lives in the project, so Claude can modify it: the bridge (outside the sandbox) never
# writes there through a symbolic link, and recreates every file it writes itself.
def ouvrir_ecriture(chemin, ajout=False):
    try:
        if os.path.islink(chemin) or (not ajout and os.path.exists(chemin)):
            os.remove(chemin)
    except OSError:
        pass
    drapeaux = os.O_WRONLY | os.O_CREAT | NOFOLLOW | (os.O_APPEND if ajout else os.O_EXCL)
    return os.fdopen(os.open(chemin, drapeaux, 0o644), "ab" if ajout else "w", **({"buffering": 0} if ajout else {}))


def dossier_sur():
    atelier = os.path.join(PROJECT, ".athanor")
    for d in (atelier, DOSSIER):
        if os.path.islink(d):
            os.remove(d)
    os.makedirs(DOSSIER, exist_ok=True)


def dedans(chemin, racine):
    chemin, racine = os.path.realpath(chemin), os.path.realpath(racine)
    return chemin == racine or chemin.startswith(racine + "/")


def executer(ident, demande):
    sortie = os.path.join(DOSSIER, f"sortie-{ident}.log")
    code_f = os.path.join(DOSSIER, f"code-{ident}")
    code = 126
    with ouvrir_ecriture(sortie, ajout=True) as journal:
        try:
            outil = demande.get("outil", "")
            args = [str(a) for a in demande.get("args", [])]
            cwd = demande.get("cwd") or PROJECT
            if outil not in AUTORISES:
                journal.write(f"Bridge: '{outil}' is not an allowed gateway.\n".encode()); code = 2
            elif not (dedans(cwd, PROJECT) or dedans(cwd, TOOLS)):
                journal.write(f"Bridge: folder refused ({cwd}).\n".encode()); code = 2
            else:
                env = dict(os.environ, PONT_DIRECT="1", PROJECT=PROJECT)
                for cle, val in (demande.get("env") or {}).items():
                    if cle in ("UNREAL_MCP_URL", "TZ", "LANG"):
                        env[cle] = str(val)
                p = subprocess.Popen([os.path.join(SYSTEME, "bin", outil)] + args, cwd=cwd, env=env,
                                     stdin=subprocess.DEVNULL, stdout=journal, stderr=subprocess.STDOUT,
                                     start_new_session=True)
                en_cours[ident] = p
                code = p.wait()
        except Exception as e:
            journal.write(f"Bridge: error {e}\n".encode()); code = 125
        finally:
            en_cours.pop(ident, None)
    with ouvrir_ecriture(code_f + ".tmp") as f:
        f.write(str(code))
    os.replace(code_f + ".tmp", code_f)


def boucle():
    dossier_sur()
    for vieux in os.listdir(DOSSIER):          # requests from a previous session
        try: os.remove(os.path.join(DOSSIER, vieux))
        except OSError: pass
    battement = os.path.join(DOSSIER, "vivant")
    dernier = 0
    parent = os.getppid()
    while True:
        if os.getppid() != parent:             # the pilot is gone: no orphan bridge
            try: os.remove(battement)
            except OSError: pass
            return
        if time.time() - dernier > 2:
            try:
                dossier_sur()
                with ouvrir_ecriture(battement) as f: f.write(str(os.getpid()))
            except OSError:
                pass
            dernier = time.time()
            for nom in os.listdir(DOSSIER):     # leftovers of cancelled commands
                chemin = os.path.join(DOSSIER, nom)
                try:
                    if nom.startswith(("code-", "sortie-")) and time.time() - os.path.getmtime(chemin) > 3600:
                        os.remove(chemin)
                except OSError:
                    pass
        for nom in sorted(os.listdir(DOSSIER)):
            chemin = os.path.join(DOSSIER, nom)
            if nom.startswith("demande-") and nom.endswith(".json"):
                ident = nom[len("demande-"):-len(".json")]
                if not ident.replace("-", "").replace("_", "").isalnum() or os.path.islink(chemin):
                    try: os.remove(chemin)
                    except OSError: pass
                    continue
                try:
                    with open(chemin, encoding="utf-8") as f: demande = json.load(f)
                except (OSError, ValueError):
                    continue
                os.remove(chemin)
                threading.Thread(target=executer, args=(ident, demande), daemon=True).start()
            elif nom.startswith("annuler-"):
                ident = nom[len("annuler-"):]
                os.remove(chemin)
                p = en_cours.get(ident)
                if p:
                    try: os.killpg(p.pid, signal.SIGTERM)
                    except OSError: pass
        time.sleep(0.2)


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    boucle()
