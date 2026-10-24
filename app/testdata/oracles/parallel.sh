# TEST ORACLE ONLY: the Python that system/parallel.sh carried before Athanor ran without Python (2.24.29).
# The Go tests compare the sub-commands that replaced it with it; nothing runs this file.

# ----
parallele_plan() {
  if [ -n "${ATHANOR_GO:-}" ]; then "$ATHANOR_GO" plan lister "$ETAT/PLAN.md"; return; fi   # athanor plan (Go), else:
  python3 - "$ETAT/PLAN.md" <<'PY'
import re, sys
lignes = open(sys.argv[1], encoding="utf-8").read().splitlines()
jalons = []; vague = 0; vus = set()
for l in lignes:
    if re.match(r"^##\s+wave", l, re.I):
        vague += 1; continue
    m = re.match(r"^- \[([ xX])\] (.*)$", l.rstrip())   # top-level checkboxes only
    if not m: continue
    t = m.group(2).strip().replace("\t", " ")
    mi = re.match(r"^([A-Za-z]+\d+(?:[.\-]?[A-Za-z0-9]+)?)", t)
    ident = re.sub(r"[^A-Za-z0-9_]", "_", mi.group(1))[:20] if mi else ""
    if not ident or ident in vus:
        ident = "J%d" % (len(jalons) + 1)
        while ident in vus: ident += "x"
    vus.add(ident)
    md = re.search(r"Depends on:\s*([^.;]*)", t, re.I)
    deps = [re.sub(r"[^A-Za-z0-9_]", "_", d.strip())[:20] for d in re.split(r"[,\s]+", md.group(1)) if d.strip()] if md else None
    if deps is not None:
        deps = [d for d in deps if d.lower() not in ("none", "nothing", "-")]
    jalons.append([ident, m.group(1) != " ", deps, vague, t])
for k, (ident, fait, deps, vague, t) in enumerate(jalons):
    if deps is None:   # no explicit dependencies: everything in the earlier waves
        deps = [x[0] for x in jalons if x[3] < vague] if vague else [x[0] for x in jalons[:k]]
    print("\t".join([ident, "1" if fait else "0", ",".join(deps), t]))
PY


# ----
parallele_chevauchement() {
  if [ -n "${ATHANOR_GO:-}" ]; then "$ATHANOR_GO" plan chevauchement "$ETAT/PLAN.md" "$@"; return; fi
  python3 -I - "$ETAT/PLAN.md" "$@" <<'PY'
import re, sys
from collections import Counter
def fichiers(l):
    m = re.search(r"Files?:\s*(.*?)(?:\.\s+(?:Depends on|Acceptance|Read)\b|$)", l, re.I)
    if not m:
        return set()
    out = set()
    for f in re.split(r"[,;]", m.group(1)):
        f = re.sub(r"\s*\(.*$", "", f).strip().strip("`'\" ").lstrip("./").rstrip("/.")
        f = f.split("*")[0].rstrip("/")   # a glob: its folder
        if f and " " not in f and len(f) < 200:
            out.add(f)
    return out
def touche(a, b):
    return a == b or a.startswith(b + "/") or b.startswith(a + "/")
plan, ligne, autres = sys.argv[1], sys.argv[2], sys.argv[3:]
try:
    lignes = [l for l in open(plan, encoding="utf-8").read().splitlines() if re.match(r"^- \[[ xX]\] ", l)]
except OSError:
    lignes = []
compte = Counter(f for l in lignes for f in fichiers(l))
communs = {f for f, n in compte.items() if n >= 3}
communs |= {"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "composer.lock", "poetry.lock", "Cargo.lock", "go.sum"}
miens = {f for f in fichiers(ligne) if f not in communs and f.rsplit("/", 1)[-1] not in communs}
for a in autres:
    ident, _, reste = a.partition("\t")
    l, _, res = reste.partition("\t")
    siens = fichiers(l) | {f.strip().lstrip("./").rstrip("/") for f in res.split(",") if f.strip()}
    if any(touche(x, y) for x in miens for y in siens if y not in communs):
        print(ident)
        break
PY


# ----
parallele_documentation_seule() {
  if [ -n "${ATHANOR_GO:-}" ]; then "$ATHANOR_GO" plan documentation; return; fi
  python3 -I -c '
import re, sys
def fichiers(l):
    m = re.search(r"Files?:\s*(.*?)(?:\.\s+(?:Depends on|Acceptance|Read)\b|$)", l, re.I)
    return [re.sub(r"\s*\(.*$", "", f).strip().strip("`\x27\" ").lstrip("./") for f in re.split(r"[,;]", m.group(1))] if m else []
def doc(f):
    return bool(re.search(r"\.(md|markdown|txt|rst|adoc)$", f, re.I)) or f.lower().startswith("docs/")
lignes = [l for l in sys.stdin.read().splitlines() if l.strip()]
sys.exit(0 if lignes and all(fichiers(l) and all(doc(f) for f in fichiers(l) if f) for l in lignes) else 1)
'
}

# ----
issue=fusionne
    if [ -n "${ATHANOR_GO:-}" ]; then "$ATHANOR_GO" plan cocher "$ETAT/PLAN.md" "$ident"
    else python3 - "$ETAT/PLAN.md" "$ident" <<'PY'
import re, sys
p, ident = sys.argv[1], sys.argv[2]
s = open(p, encoding="utf-8").read().splitlines(True); out = []
for l in s:
    m = re.match(r"^- \[ \] (([A-Za-z]+\d+(?:[.\-]?[A-Za-z0-9]+)?))", l)
    if m and re.sub(r"[^A-Za-z0-9_]", "_", m.group(1))[:20] == ident:
        l = "- [x] " + l[6:]
    out.append(l)
open(p, "w", encoding="utf-8").write("".join(out))
PY


# ----
"${OUTIL_REGLAGES[@]}" "$KIT" "$PRIVE/reglages-$ident.json" > /dev/null || { log "Worker $ident: cannot build the settings."; return 1; }
  if [ -n "${ATHANOR_GO:-}" ]; then "$ATHANOR_GO" reglages-ouvrier "$PRIVE/reglages-$ident.json" "$PROJECT/.git" || return 1
  else python3 - "$PRIVE/reglages-$ident.json" "$PROJECT/.git" <<'PY' || return 1
import json, sys
f, git = sys.argv[1], sys.argv[2]
r = json.load(open(f, encoding="utf-8"))
if "sandbox" in r:   # native Windows: no sandbox, nothing to open
    fs = r["sandbox"]["filesystem"]
    fs["allowWrite"] = fs.get("allowWrite", []) + [git]
    fs["allowRead"] = fs.get("allowRead", []) + [git]
json.dump(r, open(f, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
PY

