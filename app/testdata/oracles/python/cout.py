#!/usr/bin/env python3
"""Cost of a cycle whose log has no "result" line (the pilot closed it after its milestone, or the
watchdog stopped it): Claude Code only writes the cost in that last line.

  cout.py apprendre TARIFS LOG   learns the price of each model from the "result" line of LOG
                                 (its modelUsage: tokens and cost per model)
  cout.py estimer TARIFS LOG [CONFIG]
                                 prints, as JSON, the lead's turns of LOG (each message counted once)
                                 and their cost at the learned prices, plus the cost of the sub-agents
                                 of that session, read from Claude Code's own transcripts in CONFIG
                                 (~/.claude: projects/*/<session>/subagents/*.jsonl); total_cost_usd is
                                 absent when no price is known yet for a model used

Prices are never written here: they change, and depend on the plan. They are learned from this PC's
own results, at least 3 of them per model, in TARIFS (a JSON file of the kit's logs, outside the sandbox).
"""
import glob
import json
import os
import re
import sys
import tempfile
import time

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

MAX_ECHANTILLONS = 40
MAX_FICHIER = 1 << 20


def lignes(chemin):
    try:
        with open(chemin, encoding="utf-8", errors="replace") as f:
            for l in f:
                l = l.strip()
                if not l.startswith("{"):
                    continue
                try:
                    o = json.loads(l)
                except ValueError:
                    continue
                if isinstance(o, dict):
                    yield o
    except OSError:
        return


def entier(v):
    return v if isinstance(v, int) and v >= 0 else 0


def lire_tarifs(chemin):
    try:
        fd = os.open(pas_un_lien(chemin), os.O_RDONLY | NOFOLLOW | NONBLOCK | BINAIRE)
    except OSError:
        return {}
    try:
        import stat
        st = os.fstat(fd)
        if not stat.S_ISREG(st.st_mode) or st.st_size > MAX_FICHIER:
            return {}
        with os.fdopen(fd, encoding="utf-8") as f:
            fd = None
            d = json.load(f)
        return d if isinstance(d, dict) else {}
    except (OSError, ValueError):
        return {}
    finally:
        if fd is not None:
            os.close(fd)


def ecrire_tarifs(chemin, d):
    dossier = os.path.dirname(chemin) or "."
    fd, tmp = tempfile.mkstemp(dir=dossier, prefix=".tarifs-")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(d, f, indent=1)
        os.replace(tmp, chemin)
    except OSError:
        try:
            os.unlink(tmp)
        except OSError:
            pass


def variables(i, cr, cw, o):
    # three prices: cache writes (the few uncached input tokens are priced alike), cache reads, output
    return [i + cw, cr, o]


def ajuster(echantillons):
    """Least squares over [i, cr, cw, o, cost] samples; None when the prices cannot be told apart."""
    if len(echantillons) < 3:
        return None
    X = [variables(*e[:4]) for e in echantillons]
    y = [e[4] for e in echantillons]
    # scaled to millions of tokens, normal equations, Gaussian elimination
    X = [[v / 1e6 for v in r] for r in X]
    A = [[sum(r[a] * r[b] for r in X) for b in range(3)] for a in range(3)]
    B = [sum(r[a] * c for r, c in zip(X, y)) for a in range(3)]
    try:
        for c in range(3):
            p = max(range(c, 3), key=lambda k: abs(A[k][c]))
            if abs(A[p][c]) < 1e-9:
                raise ZeroDivisionError
            A[c], A[p], B[c], B[p] = A[p], A[c], B[p], B[c]
            for k in range(3):
                if k != c:
                    m = A[k][c] / A[c][c]
                    A[k] = [a - m * b for a, b in zip(A[k], A[c])]
                    B[k] -= m * B[c]
        coef = [B[c] / A[c][c] for c in range(3)]
    except ZeroDivisionError:
        coef = None
    if coef is None or min(coef) < 0:
        # not enough variety in the samples: one price, with the usual ratios between the three
        rapports = [1.0, 0.05, 2.0]
        total = sum(sum(r[a] * rapports[a] for a in range(3)) for r in X)
        if total <= 0:
            return None
        k = sum(y) / total
        coef = [k * r for r in rapports]
    return [round(c, 6) for c in coef]  # dollars per million tokens


def apprendre(tarifs, journal):
    res = None
    for o in lignes(journal):
        if o.get("type") == "result":
            res = o
    mu = res.get("modelUsage") if isinstance(res, dict) else None
    if not isinstance(mu, dict) or not mu:
        return
    verrou = open(tarifs + ".lock", "a")
    try:
        verrouiller(verrou.fileno())
        d = lire_tarifs(tarifs)
        modeles = d.get("modeles") if isinstance(d.get("modeles"), dict) else {}
        for nom, u in list(mu.items())[:10]:
            if not isinstance(nom, str) or not isinstance(u, dict) or len(nom) > 80:
                continue
            c = u.get("costUSD")
            if not isinstance(c, (int, float)) or c <= 0.01:
                continue
            e = [entier(u.get("inputTokens")), entier(u.get("cacheReadInputTokens")),
                 entier(u.get("cacheCreationInputTokens")), entier(u.get("outputTokens")), float(c)]
            m = modeles.get(nom) if isinstance(modeles.get(nom), dict) else {}
            ech = [x for x in m.get("echantillons", []) if isinstance(x, list) and len(x) == 5]
            ech = (ech + [e])[-MAX_ECHANTILLONS:]
            modeles[nom] = {"echantillons": ech, "prix_par_million": ajuster(ech)}
        ecrire_tarifs(tarifs, {"modeles": dict(list(modeles.items())[-30:])})
    finally:
        verrou.close()


def messages_de(chemin, chef_seul):
    """{message id: (model, usage)} of a log, each message once (one line per content block, and the
    streamed partial lines: the last one has the final usage), in order."""
    messages, ordre = {}, []
    for o in lignes(chemin):
        if o.get("type") != "assistant" or (chef_seul and o.get("parent_tool_use_id") is not None):
            continue
        m = o.get("message") if isinstance(o.get("message"), dict) else {}
        u = m.get("usage") if isinstance(m.get("usage"), dict) else {}
        modele = m.get("model") if isinstance(m.get("model"), str) else ""
        if modele.startswith("<"):  # "<synthetic>": written by Claude Code, not a model turn
            continue
        cle = m.get("id") if isinstance(m.get("id"), str) else "n%d" % len(ordre)
        if cle not in messages:
            ordre.append(cle)
        messages[cle] = (modele, u)
    return messages, ordre


def sessions(journal):
    vus = []
    for o in lignes(journal):
        sid = o.get("session_id")
        if isinstance(sid, str) and re.fullmatch(r"[0-9a-fA-F-]{8,64}", sid) and sid not in vus:
            vus.append(sid)
    return vus[:5]


def transcripts_sous_agents(journal, config):
    """Claude Code keeps each sub-agent's turns in projects/<folder>/<session>/subagents/agent-*.jsonl."""
    out = []
    if not config:
        return out
    for sid in sessions(journal):
        for f in sorted(glob.glob(os.path.join(glob.escape(config), "projects", "*", sid, "subagents", "agent-*.jsonl")))[:200]:
            try:
                if os.path.isfile(f) and not os.path.islink(f) and os.path.getsize(f) < 200 * MAX_FICHIER:
                    out.append(f)
            except OSError:
                pass
    return out


def cout_de(messages, ordre, prix, tot=None):
    cout, connu = 0.0, True
    for cle in ordre:
        modele, u = messages[cle]
        i, cr, cw, o = (entier(u.get(k)) for k in CLES)
        if tot is not None:
            for k, v in zip(CLES, (i, cr, cw, o)):
                tot[k] += v
        p = prix.get(modele)
        if isinstance(p, list) and len(p) == 3:
            cout += sum(a * b for a, b in zip(variables(i, cr, cw, o), p)) / 1e6
        elif i + cr + cw + o:
            connu = False
    return cout, connu


CLES = ("input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "output_tokens")


def estimer(tarifs, journal, config=None):
    messages, ordre = messages_de(journal, True)
    prix = {k: v.get("prix_par_million") for k, v in (lire_tarifs(tarifs).get("modeles") or {}).items()
            if isinstance(v, dict)}
    tot = {k: 0 for k in CLES}   # the lead's tokens, as in a "result" line
    cout, connu = cout_de(messages, ordre, prix, tot)
    fichiers = transcripts_sous_agents(journal, config)
    for f in fichiers:
        c, k = cout_de(*messages_de(f, False), prix)
        cout += c
        connu = connu and k
    out = {"num_turns": len(ordre), "usage": tot, "sous_agents": len(fichiers)}
    if connu:
        out["total_cost_usd"] = round(cout, 4)
    print(json.dumps(out))


if __name__ == "__main__":
    if len(sys.argv) not in (4, 5) or sys.argv[1] not in ("apprendre", "estimer"):
        sys.exit("usage: cout.py apprendre TARIFS LOG | estimer TARIFS LOG [CONFIG]")
    if sys.argv[1] == "apprendre":
        apprendre(sys.argv[2], sys.argv[3])
    else:
        estimer(sys.argv[2], sys.argv[3], sys.argv[4] if len(sys.argv) == 5 else None)
