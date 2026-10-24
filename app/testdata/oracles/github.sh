# TEST ORACLE ONLY: the Python that system/github.sh carried before Athanor ran without Python (2.24.29).
# The Go tests compare the sub-commands that replaced it with it; nothing runs this file.

# ----
SCANNER='
import codecs, re, sys
regles = [
  ("private key", re.compile(r"-----BEGIN (?:RSA |DSA |EC |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY")),
  ("AWS key", re.compile(r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b")),
  ("GitHub token", re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,})")),
  ("Anthropic key", re.compile(r"\bsk-ant-[A-Za-z0-9_-]{20,}")),
  ("OpenAI key", re.compile(r"\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{20,}T3BlbkFJ[A-Za-z0-9_-]{20,}|\bsk-[A-Za-z0-9]{48}\b")),
  ("Slack token", re.compile(r"\bxox[abposr]-[A-Za-z0-9-]{10,}")),
  ("Google API key", re.compile(r"\bAIza[0-9A-Za-z_-]{35}\b")),
  ("GitLab token", re.compile(r"\bglpat-[A-Za-z0-9_-]{20,}")),
  ("Stripe key", re.compile(r"\b(?:sk|rk)_live_[0-9A-Za-z]{20,}")),
  ("npm token", re.compile(r"\bnpm_[A-Za-z0-9]{36}\b")),
  ("Hugging Face token", re.compile(r"\bhf_[A-Za-z0-9]{34,}\b")),
  ("Discord token", re.compile(r"\b[MN][A-Za-z0-9_-]{23,25}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27,}\b")),
]
noms = [
  ("environment file", re.compile(r"(?:^|/)\.env(?:\.(?!example$|sample$|template$|dist$|defaults?$)[^/]+)?$")),
  ("private key file", re.compile(r"(?:^|/)(?:id_rsa|id_dsa|id_ecdsa|id_ed25519)$|\.(?:pem|p12|pfx|keystore|jks)$")),
  ("Athanor workshop", re.compile(r"(?:^|/)\.athanor(?:/|$)|^\.claude(?:/|$)")),
  ("GitHub Actions workflow", re.compile(r"^\.github/(?:workflows|actions)/")),
]
vus = set()
def ajouter(cle):
    if cle not in vus:
        vus.add(cle); print(cle)
def secret(texte):
    for genre, motif in regles:
        if motif.search(texte):
            return genre
def chemin(brut):
    if len(brut) > 1 and brut[0] == "\"" and brut[-1] == "\"":
        brut = codecs.escape_decode(brut[1:-1].encode("utf-8"))[0].decode("utf-8", "replace")
    return brut[2:] if brut.startswith("b/") else brut
mode = sys.argv[1]
commit, fichier, ligne, reste_a, reste_b = "", None, 0, 0, 0
for brut in sys.stdin.buffer:
    l = brut.decode("utf-8", "replace").rstrip("\n")
    if mode == "messages":
        if l.startswith("\x01"):
            commit = l[1:13]; continue
        g = secret(l)
        if g: ajouter("commit %s (message)\t%s" % (commit, g))
        continue
    if reste_a > 0 or reste_b > 0:   # inside a hunk: its header gave the exact number of lines
        c = l[:1]
        if c == "+":
            reste_b -= 1
            g = secret(l)
            if g and fichier: ajouter("%s:%d\t%s" % (fichier, ligne, g))
            ligne += 1; continue
        if c == "-": reste_a -= 1; continue
        if c == " ": reste_a -= 1; reste_b -= 1; ligne += 1; continue
        if c == "\\": continue
        sys.exit(3)                    # not a diff line where one is expected: never trust the rest
    if l.startswith("\x01") or l.startswith("diff --git ") or l.startswith("diff --cc "):
        fichier = None; continue
    if l.startswith("+++ "):
        fichier = None if l == "+++ /dev/null" else chemin(l[4:])
        if fichier:
            for genre, motif in noms:
                if motif.search(fichier): ajouter("%s\t%s" % (fichier, genre))
        continue
    m = re.match(r"@@ -\d+(?:,(\d+))? \+(\d+)(?:,(\d+))? @@", l)
    if m:
        reste_a = int(m.group(1)) if m.group(1) is not None else 1
        reste_b = int(m.group(3)) if m.group(3) is not None else 1
        ligne = int(m.group(2)); continue
if reste_a > 0 or reste_b > 0: sys.exit(3)
print("#FIN#")
'


# ----

atelier() {
  if github_go; then "$GITHUB_GO" github atelier "$1" "$2" "$3"; return; fi
  python3 -I -c '
import os, sys
p, nom, action = sys.argv[1:4]
if os.name == "nt":   # native Windows: no dir_fd; links checked by path
    d = os.path.join(p, ".athanor")
    if os.path.islink(d) or not os.path.isdir(d):
        sys.exit(1)
    cible = os.path.join(d, nom)
    if os.path.islink(cible):
        os.unlink(cible)
    if action == "effacer":
        try: os.unlink(cible)
        except FileNotFoundError: pass
    else:
        tmp = os.path.join(d, ".%s.%d" % (nom, os.getpid()))
        with open(tmp, "wb") as o: o.write(sys.stdin.buffer.read())
        os.replace(tmp, cible)
    sys.exit(0)
try:
    fd = os.open(os.path.join(p, ".athanor"), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
except OSError:
    sys.exit(1)
try:
    if action == "effacer":
        try: os.unlink(nom, dir_fd=fd)
        except FileNotFoundError: pass
    else:
        tmp = ".%s.%d" % (nom, os.getpid())
        f = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644, dir_fd=fd)
        with os.fdopen(f, "wb") as o: o.write(sys.stdin.buffer.read())
        os.replace(tmp, nom, src_dir_fd=fd, dst_dir_fd=fd)
finally:
    os.close(fd)
' 

# ----

crochet() {
  local projet; projet=$(chemin_projet "${1:-}") || return 2
  if github_go; then "$GITHUB_GO" github crochet "$projet" "${2:-0}"; return; fi
  python3 -I -c '
import os, sys
p, actif = sys.argv[1], sys.argv[2] == "1"
MARQUE = b"# athanor: commits without Claude as co-author"
texte = b"#!/bin/sh\n" + MARQUE + b" (option of the GitHub block, Manage page)\n" \
    b"sed -i -e \"/^[Cc]o-[Aa]uthored-[Bb]y:.*[Cc]laude/d\" -e \"/Generated with \\[Claude Code\\]/d\" -e \"/^.\\{0,4\\}Generated with Claude/d\" \"$1\"\n"
if os.name == "nt":   # native Windows: no dir_fd; links checked by path
    h = os.path.join(p, ".git", "hooks")
    if os.path.islink(os.path.join(p, ".git")) or os.path.islink(h):
        sys.exit(0)
    f = os.path.join(h, "commit-msg")
    actuel = None
    if os.path.islink(f):
        sys.exit(0)
    if os.path.isfile(f):
        actuel = open(f, "rb").read(4096)
    if actuel is not None and MARQUE not in actuel:
        sys.exit(0)
    if actif:
        os.makedirs(h, exist_ok=True)
        tmp = f + ".%d" % os.getpid()
        with open(tmp, "wb") as o: o.write(texte)
        os.replace(tmp, f)
    elif actuel is not None:
        os.unlink(f)
    sys.exit(0)
try:
    g = os.open(os.path.join(p, ".git"), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
except OSError:
    sys.exit(0)
try:
    try:
        h = os.open("hooks", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=g)
    except FileNotFoundError:
        if not actif: sys.exit(0)
        os.mkdir("hooks", 0o755, dir_fd=g)
        h = os.open("hooks", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=g)
    except OSError:
        sys.exit(0)                          # a link in its place: left alone
    try:
        actuel = None
        try:
            f = os.open("commit-msg", os.O_RDONLY | os.O_NOFOLLOW, dir_fd=h)
            with os.fdopen(f, "rb") as o: actuel = o.read(4096)
        except FileNotFoundError:
            pass
        except OSError:
            sys.exit(0)                      # a link in its place: left alone
        if actuel is not None and MARQUE not in actuel:
            sys.exit(0)                      # a hook of the user, not ours
        if actif:
            tmp = ".commit-msg.%d" % os.getpid()
            f = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o755, dir_fd=h)
            with os.fdopen(f, "wb") as o: o.write(texte)
            os.replace(tmp, "commit-msg", src_dir_fd=h, dst_dir_fd=h)
        elif actuel is not None:
            os.unlink("commit-msg", dir_fd=h)
    finally:
        os.close(h)
finally:
    os.close(g)
' 
