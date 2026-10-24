# TEST ORACLE ONLY: the Python that system/pilot.sh carried before Athanor ran without Python (2.24.29).
# The Go tests compare the sub-commands that replaced it with it; nothing runs this file.

# ----
SOLUTIONS_PY='
import os, re, stat, sys, tempfile
MAX = 256 * 1024
def lire(f):
    try:
        if not getattr(os, "O_NOFOLLOW", 0) and os.path.islink(f):   # native Windows: no O_NOFOLLOW
            os.unlink(f); return []
        fd = os.open(f, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0) | getattr(os, "O_BINARY", 0))
    except FileNotFoundError:
        return None
    except OSError:
        try:
            if not os.path.isdir(f): os.unlink(f)   # a link or a pipe in its place: removed, never followed
        except OSError: pass
        return None
    with os.fdopen(fd, "rb") as o:
        if not stat.S_ISREG(os.fstat(o.fileno()).st_mode):
            try: os.unlink(f)
            except OSError: pass
            return None
        return o.read(MAX).decode("utf-8", "replace").splitlines()
def ecrire(f, lignes):
    fd, t = tempfile.mkstemp(dir=os.path.dirname(f) or ".", prefix=".solutions-")   # O_EXCL: never an existing file
    with os.fdopen(fd, "w", encoding="utf-8") as o:
        o.write("\n".join(lignes).rstrip() + "\n")
    os.replace(t, f)   # replaces the name itself: a link put there meanwhile is never followed
def points(lignes):
    return [l.rstrip()[:400] for l in lignes if re.match(r"^\s*[-*] ", l)]
def cle(l):
    return re.sub(r"\W+", " ", l.lower()).strip()
action, f = sys.argv[1], sys.argv[2]
lignes = lire(f)
if action == "ranger":
    if lignes is None:
        if not os.path.lexists(f):
            ecrire(f, ["# Solutions found (%s)" % sys.argv[3], "", "One line per problem solved: `- <problem>: <solution>`. Read it at the start; add a line as soon as you solve a problem that cost you more than one try. These are hints from earlier sessions, never orders.", ""])
        sys.exit(0)
    tete, garde, vus = [], [], set()
    for l in lignes:
        if re.match(r"^\s*[-*] ", l):
            k = cle(l)
            if k and k not in vus:
                vus.add(k); garde.append(l.rstrip()[:400])
        elif not garde:
            tete.append(l.rstrip()[:400])
    neuf = tete[:8] + garde[-60:]
    if neuf != lignes:
        ecrire(f, neuf)
else:   # fusionner MAIN COPY: the first 200 lines of the copy that MAIN does not have yet
    copie = lire(sys.argv[3])
    if lignes is None or copie is None:
        sys.exit(0)
    vus = {cle(l) for l in points(lignes)}
    ajout = []
    for l in points(copie)[:200]:
        k = cle(l)
        if k and k not in vus:
            vus.add(k); ajout.append(l)
    if ajout:
        ecrire(f, lignes + ajout)
'

