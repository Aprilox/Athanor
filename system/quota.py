#!/usr/bin/env python3
"""Automatic stops on the subscription's quota (Overview -> Subscription quota -> Auto stop).

  quota.py verifier RULES LOG...   prints "action<TAB>reset<TAB>message" for the first rule that the latest
                                   rate_limit_event of these logs (the newest log first) meets while its
                                   window is still open; nothing otherwise

RULES is the kit's logs/quota-arret.json, written by the application:
  {"five_hour": {"seuil": 80, "action": "apres"}, "seven_day": {...}, "modele": {...}}
"modele" stands for every model's own week (seven_day_fable, seven_day_opus...). Actions: "apres" (stop at
the end of the cycle), "maintenant" (stop the cycle now), "pause" (wait for the reset, then go on).
The logs are Claude Code's stream-json output: data, never run.
"""
import json
import os
import sys
import time

ACTIONS = ("apres", "maintenant", "pause")
NOMS = {"five_hour": "5-hour session", "seven_day": "week, all models"}


def regles(chemin):
    try:
        if os.path.getsize(chemin) > 64 << 10:
            return {}
        with open(chemin, encoding="utf-8") as f:
            d = json.load(f)
    except (OSError, ValueError):
        return {}
    out = {}
    for cle in ("five_hour", "seven_day", "modele"):
        r = d.get(cle) if isinstance(d, dict) else None
        if not isinstance(r, dict):
            continue
        seuil, action = r.get("seuil"), r.get("action")
        if isinstance(seuil, (int, float)) and 1 <= seuil <= 100 and action in ACTIONS:
            out[cle] = (float(seuil), action)
    return out


def dernier_evenement(journaux):
    for j in journaux:
        try:
            with open(j, "rb") as f:
                f.seek(0, os.SEEK_END)
                taille = f.tell()
                f.seek(max(0, taille - (2 << 20)))
                lignes = f.read().decode("utf-8", "replace").splitlines()
        except OSError:
            continue
        for l in reversed(lignes):
            if '"rate_limit_event"' not in l:
                continue
            try:
                o = json.loads(l)
            except ValueError:
                continue
            if isinstance(o, dict) and o.get("type") == "rate_limit_event":
                return o.get("rate_limit_info") or {}
    return None


def verifier(fichier_regles, journaux):
    r = regles(fichier_regles)
    if not r:
        return
    info = dernier_evenement(sorted(journaux, key=lambda j: os.path.getmtime(j) if os.path.exists(j) else 0, reverse=True))
    if not info:
        return
    fenetres = info.get("unifiedWindows") if isinstance(info.get("unifiedWindows"), dict) else {}
    maintenant = time.time()
    for nom, f in sorted(fenetres.items(), key=lambda x: (x[0] != "five_hour", x[0] != "seven_day", x[0])):
        if not isinstance(f, dict):
            continue
        regle = r.get(nom) or (r.get("modele") if nom.startswith("seven_day_") else None)
        u, reinit = f.get("utilization"), f.get("resetsAt")
        if not regle or not isinstance(u, (int, float)) or not isinstance(reinit, (int, float)) or reinit <= maintenant:
            continue
        seuil, action = regle
        if u * 100 >= seuil:
            libelle = NOMS.get(nom) or "week of " + nom[len("seven_day_"):].replace("_", " ").title()
            print("%s\t%d\t%s at %d%% (threshold %d%%)" % (action, int(reinit), libelle, int(u * 100), int(seuil)))
            return


if __name__ == "__main__":
    a = sys.argv[1:]
    if len(a) >= 2 and a[0] == "verifier":
        verifier(a[1], a[2:])
    else:
        sys.exit("usage: quota.py verifier RULES LOG...")
