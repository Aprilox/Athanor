#!/usr/bin/env bash
# GitHub for a project: Athanor pushes the project's commits to ONE GitHub repository.
#
# What Claude can reach is kept to that repository only:
#   - signing in to GitHub (gh auth login, in a window the user sees) happens only while a project is
#     being linked, in a temporary folder deleted right after: no GitHub sign-in is ever kept;
#   - while signed in, Athanor creates the repository (or checks the user administers it) and gives it
#     its own key, a "deploy key" with write access: GitHub accepts that key for that repository and
#     nothing else, so even a leaked key can never touch another repository of the account;
#   - the key, the repository name and the settings live in the user's Linux home
#     (~/.local/share/athanor/github), which Claude's sandbox cannot read or write;
#   - pushes are done by the pilot, outside the sandbox, from a private git folder: no setting of the
#     project's .git (which Claude can write) is ever used, GitHub's identity is checked against the keys
#     it published, the push is never forced, and the commits are scanned for secrets first.
#
#   github.sh etat PROJECT                         JSON for the application
#   github.sh lier PROJECT creer NAME [AUTO] [BRANCH]       (interactive: signs in to GitHub once)
#   github.sh lier PROJECT existant OWNER/REPO [AUTO] [BRANCH]
#   github.sh pousser PROJECT [--auto] [--malgre]  push now (--auto: only when automatic push is on;
#                                                  --malgre: the user pushes despite a secret warning)
#   github.sh auto PROJECT 0|1                     automatic push after each milestone on/off
#   github.sh branche PROJECT NAME                 GitHub branch the project goes to
#   github.sh delier PROJECT                       forgets the link and deletes the key
#   github.sh suivre OLD NEW                       the project's folder changed: the link follows it
#   github.sh identite PROJECT                     JSON: author of the commits, and what to propose
#   github.sh regler-identite PROJECT NAME EMAIL 0|1   author of the commits, Claude co-author (0) or not (1)
#   github.sh identite-pilote PROJECT              "name<TAB>email<TAB>0|1" for the pilot
#   github.sh crochet PROJECT 0|1                  the project's commit-msg hook that removes Claude's co-author lines
#
# Exit codes of pousser: 0 pushed or up to date, 3 nothing to do (not linked, automatic push off,
# no commit yet), 4 blocked (secret found), 5 refused by GitHub (branch ahead, key removed...), 1 error.
set -u
SYSTEME="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KIT="${KIT:-$(dirname "$SYSTEME")}"
. "$SYSTEME/safety.sh"

DONNEES="${ATHANOR_GITHUB_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/athanor/github}"
# host:port of GitHub's SSH (the tests point it to a local server); port 443 is the fallback when 22 is blocked
HOTE_SSH="${ATHANOR_GITHUB_SSH:-github.com:22}"
SECOURS_SSH="${ATHANOR_GITHUB_SSH_SECOURS-ssh.github.com:443}"

chemin_projet() {  # the logical path the pilot uses (cd + pwd), as given when the folder is gone
  local p="${1%/}"
  [ -n "$p" ] || return 1
  (cd "$p" 2>/dev/null && pwd) || printf '%s' "$p"
}
dossier_de() { printf '%s/%s' "$DONNEES" "$(printf '%s' "$1" | sha1sum | cut -c1-16)"; }

depot_valide()   { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$ ]] && [[ "$1" != */. && "$1" != */.. ]]; }
nom_valide()     { [[ "$1" =~ ^[A-Za-z0-9._-]{1,100}$ ]] && [ "$1" != . ] && [ "$1" != .. ]; }
branche_valide() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$ ]] && [[ "$1" != *..* && "$1" != *//* && "$1" != */ && "$1" != *.lock && "$1" != *.lock/* ]] && git check-ref-format --branch "$1" >/dev/null 2>&1; }

# Settings: KEY=value lines, written only by this script (never by Claude), validated when read anyway.
lire_reglages() {
  DEPOT=""; BRANCHE=""; AUTO=1; COMPTE=""; LIE_LE=""; CLE_ID=""
  local f="$1/reglages" k v
  [ -f "$f" ] || return 1
  while IFS='=' read -r k v; do
    case "$k" in
      depot)   depot_valide "$v" && DEPOT="$v" ;;
      branche) branche_valide "$v" && BRANCHE="$v" ;;
      auto)    [ "$v" = 0 ] && AUTO=0 ;;
      compte)  [[ "$v" =~ ^[A-Za-z0-9-]{1,39}$ ]] && COMPTE="$v" ;;
      lie_le)  [[ "$v" =~ ^[0-9T:+-]{10,30}$ ]] && LIE_LE="$v" ;;
      cle_id)  [[ "$v" =~ ^[0-9]{1,15}$ ]] && CLE_ID="$v" ;;
    esac
  done < "$f"
  [ -n "$DEPOT" ] && [ -f "$1/cle" ] && [ -s "$1/known_hosts" ]
}
ecrire_reglages() {
  local d="$1" t
  t=$(mktemp "$d/.reglages.XXXXXX") || return 1
  printf 'depot=%s\nbranche=%s\nauto=%s\ncompte=%s\nlie_le=%s\ncle_id=%s\n' "$DEPOT" "$BRANCHE" "$AUTO" "$COMPTE" "$LIE_LE" "$CLE_ID" > "$t" && mv -f "$t" "$d/reglages"
}

# Result of the last push, for the application: {quand, resultat, modele, args, message, commit, fichiers}.
# modele is an English sentence with {0}, {1}... (translated by the application), args its values,
# message the sentence filled in (for the logs). NOTE_ARGS and NOTE_FICHIERS ("file<TAB>kind") are set by the caller.
NOTE_ARGS=(); NOTE_FICHIERS=(); NOTE_SHA=""
remplir() {  # $1 template, then values
  local m="$1" i=0; shift
  for a in "$@"; do m=${m//\{$i\}/$a}; i=$((i + 1)); done
  printf '%s' "$m"
}
noter() {  # $1 dir, $2 resultat (ok|bloque|rejete|erreur|attente), $3 template, $4 commit
  local d="$1" t f
  NOTE_MESSAGE=$(remplir "$3" "${NOTE_ARGS[@]}")
  t=$(mktemp "$d/.dernier.XXXXXX") || return 0
  f=$(for x in "${NOTE_FICHIERS[@]}"; do printf '%s\n' "$x"; done | jq -R -s -c 'split("\n") | map(select(length > 0) | split("\t") | {f: .[0], genre: (.[1] // "")})')
  jq -n --arg q "$(date '+%Y-%m-%dT%H:%M:%S')" --arg r "$2" --arg mo "$3" --arg m "$NOTE_MESSAGE" --arg c "$4" --arg s "$NOTE_SHA" --argjson f "$f" \
     '{quand:$q, resultat:$r, modele:$mo, args:$ARGS.positional, message:$m, commit:$c, sha:$s, fichiers:$f}' --args "${NOTE_ARGS[@]}" > "$t" && mv -f "$t" "$d/dernier.json"
  NOTE_ARGS=(); NOTE_FICHIERS=(); NOTE_SHA=""
}

# git on the private folder: no system or user configuration, nothing from the project's .git; the
# project's objects are read in place through the folder's own objects/info/alternates.
gm() {
  env -u GIT_SSH_COMMAND -u GIT_SSH -u GIT_SSH_VARIANT -u GIT_PROXY_COMMAND -u GIT_CONFIG_PARAMETERS -u GIT_CONFIG_COUNT \
      -u GIT_ALTERNATE_OBJECT_DIRECTORIES -u GIT_OBJECT_DIRECTORY -u GIT_COMMON_DIR -u GIT_WORK_TREE -u GIT_NAMESPACE \
    GIT_TERMINAL_PROMPT=0 GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_ASKPASS=/bin/false SSH_ASKPASS=/bin/false SSH_ASKPASS_REQUIRE=never \
    GIT_DIR="$MIROIR" \
    git -c core.hooksPath=/dev/null -c core.fsmonitor=false -c credential.helper= -c core.askPass= -c core.quotePath=false \
        -c protocol.allow=never -c push.recurseSubmodules=no -c push.gpgSign=false "$@"
}
commande_ssh() {  # $1 dir, $2 port
  printf "ssh -F /dev/null -p %s -i '%s' -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile='%s' -o GlobalKnownHostsFile=/dev/null -o ConnectTimeout=20 -o ServerAliveInterval=15 -o ServerAliveCountMax=4 -o LogLevel=ERROR" \
    "$2" "$1/cle" "$1/known_hosts"
}

# The project's own repository, and nothing else. Claude can write the project's .git: a gitfile, a
# commondir file, symbolic links or alternates would make the pilot read, then push, another repository
# of the computer. Sets SHA (HEAD) and OBJETS; RAISON = absent (no repository or no commit yet) | detourne.
lire_projet() {
  local gd="$1/.git"; SHA=""; OBJETS=""; RAISON=absent
  [ -e "$gd" ] || [ -L "$gd" ] || return 1
  RAISON=detourne
  [ -d "$gd" ] && [ ! -L "$gd" ] && [ ! -e "$gd/commondir" ] && [ ! -L "$gd/commondir" ] || return 1
  [ -d "$gd/objects" ] && [ ! -L "$gd/objects" ] || return 1
  [ -e "$gd/objects/info/alternates" ] || [ -L "$gd/objects/info/alternates" ] && return 1
  [ -n "$(find "$gd/HEAD" "$gd/packed-refs" "$gd/refs" "$gd/objects" -type l -print -quit 2>/dev/null)" ] && return 1
  [[ "$gd" == *$'\n'* ]] && return 1
  SHA=$(GIT_DIR="$gd" g rev-parse --verify -q 'HEAD^{commit}' 2>/dev/null)
  [[ "$SHA" =~ ^[0-9a-f]{40}([0-9a-f]{24})?$ ]] || { SHA=""; RAISON=absent; return 1; }
  OBJETS="$gd/objects"
}

# Secret scan of the commits about to be pushed (their changes, binary files included, and their
# messages). Prints "place<TAB>kind" lines, never a value; fails when the scan could not run to the end.
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
analyser() {  # $1 commit; what was already pushed (refs/athanor/pousse/*) is not scanned again
  local plage=("$1" --not --glob='refs/athanor/pousse/*') m d
  m=$(set -o pipefail; gm log --format='%x01%H%n%B' "${plage[@]}" 2>/dev/null | python3 -I -c "$SCANNER" messages) || return 1
  d=$(set -o pipefail; gm log -p --text --no-ext-diff --no-textconv --no-color --no-renames --diff-merges=first-parent \
        --format='%x01%H' -U0 "${plage[@]}" 2>/dev/null | python3 -I -c "$SCANNER" diff) || return 1
  [ "${m##*$'\n'}" = "#FIN#" ] && [ "${d##*$'\n'}" = "#FIN#" ] || return 1
  printf '%s\n%s\n' "$d" "$m" | grep -v -e '^#FIN#$' -e '^$'
  return 0
}

# Writes (stdin) or deletes a file of the project's workshop without following a link Claude could
# have put in place of .athanor or of the file. Usage: atelier PROJECT NAME ecrire|effacer
atelier() {
  python3 -I -c '
import os, sys
p, nom, action = sys.argv[1:4]
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
' "$1" "$2" "$3"
}

# The pilot found a secret before pushing: Claude removes it from the unpushed commits on its next cycle.
consigne_secrets() {  # $1 project, $2 base or empty, then "place<TAB>kind" lines on stdin
  local p="$1" base="$2" racine
  racine=$(GIT_DIR="$p/.git" g rev-list --max-parents=0 HEAD 2>/dev/null | tail -n 1)
  {
    echo "# Secrets found before the GitHub push"
    echo
    echo "The pilot did not push to GitHub: the commits not pushed yet contain what looks like a secret. A secret pushed once stays readable in the history, even after the file is deleted."
    echo
    echo "Found (the value itself is not repeated here):"
    while IFS=$'\t' read -r f genre; do [ -n "$f" ] && echo "- \`$f\` — $genre"; done
    echo
    echo "Fix it before any other work (these commits were never pushed: rewriting them is safe):"
    echo "1. Take each secret out of the code: read it from an environment variable or from an ignored file (\`.env\`, listed in \`.gitignore\`), with a placeholder in \`.env.example\`. A file that must not be published at all (\`.env\`, a private key, the \`.athanor\` workshop) leaves git: \`git rm -r --cached PATH\` and add it to \`.gitignore\`. A secret in a commit message goes away with the rewrite below."
    if [ -n "$base" ]; then
      echo "2. Rewrite the unpushed commits into one clean commit: \`git reset --soft $base\`, then \`git commit -m \"...\"\` (GitHub already has $base and what comes before it)."
    else
      echo "2. Nothing was pushed yet: rewrite the whole history into one clean commit: \`git reset --soft ${racine:-<first commit>}\`, then \`git commit --amend -m \"...\"\`."
    fi
    echo "3. Check that \`git log -p ${base:+$base..}HEAD\` no longer shows any secret, note it in \`.athanor/LOG.md\`, then delete this file. The pilot pushes at the end of the cycle."
    echo
    echo "If it is not a real secret (a test value, a documented example), leave the code as it is, say so in \`.athanor/BLOCKERS.md\` and delete this file: the user can push anyway from the application."
  } | atelier "$p" SECRETS_FOUND.md ecrire
}

pousser() {
  local projet="" auto=0 malgre="" a
  for a in "$@"; do
    case "$a" in --auto) auto=1 ;; --malgre=*) malgre="${a#--malgre=}" ;; *) projet="$a" ;; esac
  done
  projet=$(chemin_projet "$projet") || { echo "Usage: github.sh pousser PROJECT"; return 1; }
  local d; d=$(dossier_de "$projet")
  [ -f "$d/reglages" ] || { echo "Not linked to GitHub."; return 3; }
  exec 8> "$d/verrou" || { echo "Not linked to GitHub."; return 3; }
  flock -w 300 8 || { echo "Another push is under way."; return 1; }
  # read under the lock: a link or an unlink made meanwhile is seen
  lire_reglages "$d" || { echo "Not linked to GitHub."; return 3; }
  [ "$auto" = 1 ] && [ "$AUTO" != 1 ] && { echo "Automatic push is off."; return 3; }

  if ! lire_projet "$projet"; then
    if [ "$RAISON" = absent ]; then
      noter "$d" attente "No commit yet: the first push happens after the first milestone." ""
      echo "No commit to push yet."; return 3
    fi
    noter "$d" erreur "The project's .git folder points elsewhere (a link, or another repository's objects): nothing was pushed. Check that folder, or ask Claude to repair the project's git repository." ""
    echo "GitHub: not pushed, the project's .git folder points elsewhere."; return 1
  fi
  local sha="$SHA" branche="${BRANCHE:-main}"
  if [ -n "$malgre" ]; then
    # "push anyway" pushes exactly the commit the user reviewed, never a newer one
    [[ "$malgre" =~ ^[0-9a-f]{40}([0-9a-f]{24})?$ ]] && [ "$malgre" = "$(cat "$d/bloque" 2>/dev/null)" ] \
      || { echo "This push is no longer the one that was blocked: refresh."; return 1; }
    sha="$malgre"
  fi
  MIROIR="$d/miroir.git"
  [ -d "$MIROIR" ] || { git init -q --bare "$MIROIR" >/dev/null 2>&1 && GIT_DIR="$MIROIR" git config gc.auto 0; } || { echo "Cannot prepare the push folder."; return 1; }
  mkdir -p "$MIROIR/objects/info" && printf '%s\n' "$OBJETS" > "$MIROIR/objects/info/alternates" || { echo "Cannot prepare the push folder."; return 1; }
  # pushed commits that no longer exist (history rewritten) are forgotten, never an error
  local r; while IFS= read -r r; do
    gm cat-file -e "$(gm rev-parse -q --verify "$r" 2>/dev/null)^{commit}" 2>/dev/null || gm update-ref -d "$r" 2>/dev/null
  done < <(gm for-each-ref --format='%(refname)' refs/athanor/pousse/ 2>/dev/null)
  local resume; resume=$(gm log -1 --format='%h %s' "$sha" 2>/dev/null | cut -c1-120)
  [ -n "$resume" ] || { noter "$d" erreur "The project's commits cannot be read." ""; echo "The project's commits cannot be read."; return 1; }

  # what GitHub already has on this branch (last successful push)
  local base; base=$(gm rev-parse --verify -q "refs/athanor/pousse/$branche" 2>/dev/null)
  if [ "$base" = "$sha" ]; then
    noter "$d" ok "Already up to date on GitHub." "$resume"
    atelier "$projet" SECRETS_FOUND.md effacer 2>/dev/null
    echo "GitHub: already up to date ($DEPOT, $branche)."; return 0
  fi

  if [ -z "$malgre" ]; then
    local trouves
    if ! trouves=$(analyser "$sha"); then
      noter "$d" erreur "The secret check could not run to the end: nothing was pushed." "$resume"
      echo "GitHub: not pushed, the secret check failed."; return 1
    fi
    if [ -n "$trouves" ]; then
      while IFS= read -r a; do NOTE_FICHIERS+=("$a"); done <<< "$trouves"
      NOTE_SHA="$sha"
      local secrets; secrets=$(printf '%s\n' "$trouves" | grep -v $'\tGitHub Actions workflow$')
      if [ -n "$secrets" ]; then
        noter "$d" bloque "Not pushed: possible secrets in the commits (see the list). Claude removes them on its next cycle; if they are not real secrets, push anyway." "$resume"
        # Claude is asked once per set of findings (a false positive it reported is not asked again)
        local empreinte; empreinte=$(printf '%s' "$secrets" | sha1sum | cut -c1-16)
        if [ "$auto" = 1 ] && [ "$(cat "$d/signale" 2>/dev/null)" != "$empreinte" ]; then
          consigne_secrets "$projet" "$base" <<< "$secrets" && printf '%s\n' "$empreinte" > "$d/signale"
        fi
      else
        # GitHub runs workflows with the repository's secrets: a change there waits for the user
        noter "$d" bloque "Not pushed: these commits change GitHub Actions workflows (code GitHub runs on each push, with the repository's secrets). Check them, then push anyway." "$resume"
      fi
      printf '%s\n' "$sha" > "$d/bloque"
      echo "GitHub: not pushed, to check: $(printf '%s' "$trouves" | cut -f1 | tr '\n' ' ')"; return 4
    fi
  fi

  local hote port sortie="" code=1 url essai
  for essai in "$HOTE_SSH" "$SECOURS_SSH"; do
    [ -n "$essai" ] || continue
    hote=${essai%:*}; port=${essai##*:}
    url="ssh://git@$hote:$port/$DEPOT.git"
    sortie=$(gm -c protocol.ssh.allow=always -c core.sshCommand="$(commande_ssh "$d" "$port")" \
               push --porcelain --no-verify "$url" "$sha:refs/heads/$branche" 2>&1)
    code=$?
    printf '%s\n' "$sortie" | tail -c 20000 > "$d/derniere-sortie.log"   # private, for troubleshooting
    printf '%s' "$sortie" | grep -Eqi 'connection timed out|connection refused|network is unreachable|could not resolve|no route to host|connection reset|operation timed out' || break
  done
  local modele resultat=erreur ret=1
  if [ "$code" = 0 ]; then
    gm update-ref "refs/athanor/pousse/$branche" "$sha" 2>/dev/null
    atelier "$projet" SECRETS_FOUND.md effacer 2>/dev/null; rm -f "$d/signale" "$d/bloque"
    if printf '%s' "$sortie" | grep -q $'^=\t'; then modele="Already up to date on GitHub."; else modele="Pushed to {0} ({1})."; NOTE_ARGS=("$DEPOT" "$branche"); fi
    noter "$d" ok "$modele" "$resume"
    echo "GitHub: $NOTE_MESSAGE ($resume)"; return 0
  fi
  if printf '%s' "$sortie" | grep -Eqi 'GH006|protected branch'; then
    modele="The branch {0} is protected on GitHub: choose another branch, or allow the push in the repository's settings."; NOTE_ARGS=("$branche"); resultat=rejete; ret=5
  elif printf '%s' "$sortie" | grep -Eqi 'GH001|large files|file size limit'; then
    modele="A file is larger than GitHub's limit (100 MB): keep it out of git (.gitignore) or use Git LFS."; resultat=rejete; ret=5
  elif printf '%s' "$sortie" | grep -Eqi 'non-fast-forward|fetch first|\[rejected\]|stale info'; then
    modele="GitHub's branch {0} has commits the project does not have. Athanor never overwrites them: choose another branch, or bring those commits into the project."; NOTE_ARGS=("$branche"); resultat=rejete; ret=5
  elif printf '%s' "$sortie" | grep -Eqi 'host key verification failed|remote host identification has changed'; then
    modele="GitHub's identity could not be verified: push refused. Link the project again to get GitHub's current keys."
  elif printf '%s' "$sortie" | grep -Eqi 'permission denied|repository not found|read only|not authorized|access denied|denied to deploy key'; then
    modele="GitHub refused Athanor's key for {0}: the key was removed from the repository, or the repository was deleted or renamed. Link the project again."; NOTE_ARGS=("$DEPOT"); resultat=rejete; ret=5
  elif printf '%s' "$sortie" | grep -Eqi 'connection timed out|connection refused|network is unreachable|could not resolve|no route to host|connection reset|operation timed out'; then
    modele="GitHub cannot be reached (network). The next milestone tries again."
  else
    modele="Push failed: {0}"; NOTE_ARGS=("$(printf '%s' "$sortie" | grep -v '^\s*$' | tail -n 3 | tr '\n' ' ' | cut -c1-300)")
  fi
  noter "$d" "$resultat" "$modele" "$resume"
  echo "GitHub: $NOTE_MESSAGE"; return $ret
}

# ----------------------------------------------------------------------------- linking (interactive)
lier() {
  local projet mode cible auto="${4:-1}" branche="${5:-}"
  projet=$(chemin_projet "${1:-}") || { echo "Usage: github.sh lier PROJECT creer NAME | existant OWNER/REPO"; return 2; }
  mode="${2:-}"; cible="${3:-}"
  case "$mode" in
    creer) nom_valide "$cible" || { echo "Invalid repository name (letters, digits, . - _): $cible"; return 2; } ;;
    existant) depot_valide "$cible" || { echo "Invalid repository (expected OWNER/NAME): $cible"; return 2; } ;;
    *) echo "Usage: github.sh lier PROJECT creer NAME | existant OWNER/REPO"; return 2 ;;
  esac
  [ "$auto" = 0 ] || auto=1
  [ -z "$branche" ] || branche_valide "$branche" || { echo "Invalid branch name: $branche"; return 2; }
  command -v gh >/dev/null 2>&1 || { echo "GitHub CLI (gh) is missing: run Repair in the application's Options (Linux packages)."; return 1; }
  command -v ssh-keygen >/dev/null 2>&1 && command -v ssh >/dev/null 2>&1 || { echo "OpenSSH is missing: run Repair in the application's Options (Linux packages)."; return 1; }
  local aide; aide=$(gh auth login --help 2>&1)
  printf '%s' "$aide" | grep -q -- '--skip-ssh-key' || { echo "GitHub CLI is too old: run Repair in the application's Options to update it."; return 1; }

  local d; d=$(dossier_de "$projet")
  mkdir -p "$DONNEES" && chmod 700 "$DONNEES" "$(dirname "$DONNEES")" 2>/dev/null
  mkdir -p "$d" && chmod 700 "$d" || { echo "Cannot create $d"; return 1; }
  exec 7> "$d/liaison"; flock -n 7 || { echo "Another GitHub window is already open for this project: finish it first."; return 1; }
  CONNEXION=$(mktemp -d "$DONNEES/.connexion.XXXXXX") || return 1
  local tmp="$CONNEXION"
  chmod 700 "$tmp"
  # nothing of this sign-in survives: signed out and folder deleted whatever happens
  trap 'GH_CONFIG_DIR="$CONNEXION" gh auth logout --hostname github.com >/dev/null 2>&1 <<< "y"; rm -rf "$CONNEXION"' EXIT
  trap 'exit 130' INT TERM HUP
  export GH_CONFIG_DIR="$tmp" XDG_STATE_HOME="$tmp/etat" XDG_CACHE_HOME="$tmp/cache" GH_NO_UPDATE_NOTIFIER=1
  unset GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN GITHUB_ENTERPRISE_TOKEN GH_HOST
  {
    echo '#!/bin/sh'
    if command -v explorer.exe >/dev/null 2>&1; then echo 'exec explorer.exe "$1"'
    elif command -v wslview >/dev/null 2>&1; then echo 'exec wslview "$1"'
    else echo 'exec xdg-open "$1" >/dev/null 2>&1'; fi
  } > "$tmp/navigateur"; chmod 700 "$tmp/navigateur"
  export BROWSER="$tmp/navigateur"

  echo "=== Link this project to GitHub ==="
  echo
  echo "1. Below, GitHub shows a one-time code. Press Enter: your browser opens on GitHub."
  echo "2. Paste the code there and authorize."
  echo
  echo "Athanor keeps nothing of this sign-in: it only gives the repository its own key (valid for"
  echo "that repository alone), then signs out. Claude never sees the sign-in nor the key."
  echo
  local opts=(--hostname github.com --git-protocol ssh --skip-ssh-key --web --scopes user:email)
  printf '%s' "$aide" | grep -q -- '--insecure-storage' && opts+=(--insecure-storage)   # never the system keyring
  gh auth login "${opts[@]}" || { echo; echo "Sign-in cancelled or failed: nothing was changed."; return 1; }

  local compte cid cnom; IFS=$'\t' read -r compte cid cnom < <(gh api user --jq '[.login, (.id|tostring), (.name // "")] | @tsv' 2>/dev/null)
  [[ "$compte" =~ ^[A-Za-z0-9-]{1,39}$ ]] || { echo "Cannot read your GitHub account."; return 1; }
  # the account, to propose the commit author (name, verified emails, the private noreply address)
  {
    printf 'login=%s\n' "$compte"
    [[ "$cid" =~ ^[0-9]{1,15}$ ]] && printf 'id=%s\n' "$cid"
    nom_valide_auteur "$cnom" && printf 'nom=%s\n' "$cnom"
    gh api user/emails --jq '.[] | select(.verified) | .email' 2>/dev/null | while IFS= read -r e; do email_valide "$e" && printf 'email=%s\n' "$e"; done
  } > "$d/compte.neuf" && mv -f "$d/compte.neuf" "$d/compte"
  echo; echo "Signed in as $compte."

  local depot
  if [ "$mode" = creer ]; then
    depot=$(gh api -X POST user/repos -f name="$cible" -F private=true -F auto_init=false \
              -f description="Made with Athanor" --jq .full_name 2>"$tmp/erreur")
    if ! depot_valide "$depot"; then
      if grep -qi 'already exists' "$tmp/erreur"; then
        echo "You already have a repository named $cible: choose \"Use an existing repository\" ($compte/$cible), or another name."
      else
        echo "GitHub refused to create the repository: $(head -c 300 "$tmp/erreur")"
      fi
      return 1
    fi
    echo "Private repository created: $depot"
  else
    local info; info=$(gh api "repos/$cible" --jq '[.full_name, (.permissions.admin|tostring), (.archived|tostring), (.default_branch // "")] | @tsv' 2>"$tmp/erreur")
    local admin archive defaut; IFS=$'\t' read -r depot admin archive defaut <<< "$info"
    depot_valide "$depot" || { echo "Repository $cible not found, or not visible from your account."; return 1; }
    [ "$admin" = true ] || { echo "You are not an administrator of $depot: GitHub only lets an administrator add a key to it."; return 1; }
    [ "$archive" = true ] && { echo "$depot is archived on GitHub (read-only): unarchive it first."; return 1; }
    echo "Repository: $depot"
    [ -z "$branche" ] && branche_valide "$defaut" && branche="$defaut"
    # GitHub Actions run the pushed code with the repository's secrets: with workflows and secrets, the
    # code Claude writes would run there (a secret could reach other repositories). The user decides.
    local nwf nsec norg
    nwf=$(gh api "repos/$depot/actions/workflows" --jq .total_count 2>/dev/null)
    nsec=$(gh api "repos/$depot/actions/secrets" --jq .total_count 2>/dev/null)
    norg=$(gh api "repos/$depot/actions/organization-secrets" --jq .total_count 2>/dev/null)
    if ! [[ "$nwf" =~ ^[0-9]+$ ]] || { [ "$nwf" -gt 0 ] && { ! [[ "$nsec" =~ ^[0-9]+$ ]] || [ "$nsec" -gt 0 ] || { [[ "$norg" =~ ^[0-9]+$ ]] && [ "$norg" -gt 0 ]; }; }; }; then
      echo
      echo "WARNING: $depot runs GitHub Actions workflows (${nwf:-?}) and has Actions secrets (${nsec:-?} of its own, ${norg:-?} from its organization)."
      echo "Every push runs those workflows on the code Claude writes, with those secrets: a secret able to"
      echo "change other repositories could be used by that code. Prefer a new private repository, or remove"
      echo "such secrets first. Athanor never pushes a change to the workflows themselves without you."
      local rep; read -r -p "Type yes to link anyway: " rep || rep=""
      [ "$rep" = yes ] || { echo "Not linked."; return 1; }
    fi
  fi
  [ -n "$branche" ] || branche=main   # fixed now: never the branch the project's HEAD (Claude's) points to

  # the repository's own key; the previous key of this project (if any) is removed from GitHub
  local ancien_depot="" ancien_id=""
  if lire_reglages "$d"; then ancien_depot="$DEPOT"; ancien_id="$CLE_ID"; fi
  rm -f "$d/cle.neuve" "$d/cle.neuve.pub"
  ssh-keygen -q -t ed25519 -N '' -C "athanor-$(printf '%s' "${compte}-$(basename "$projet")" | tr -c 'A-Za-z0-9._-' '-' | cut -c1-60)" -f "$d/cle.neuve" </dev/null \
    || { echo "Cannot create the key."; return 1; }
  local id; id=$(gh api -X POST "repos/$depot/keys" -f title="Athanor ($(basename "$projet" | cut -c1-60))" \
                  -f key="$(cat "$d/cle.neuve.pub")" -F read_only=false --jq .id 2>"$tmp/erreur")
  [[ "$id" =~ ^[0-9]{1,15}$ ]] || { rm -f "$d/cle.neuve" "$d/cle.neuve.pub"; echo "GitHub refused the key: $(head -c 300 "$tmp/erreur")"; return 1; }
  if [ -n "$ancien_id" ] && depot_valide "$ancien_depot"; then
    gh api -X DELETE "repos/$ancien_depot/keys/$ancien_id" >/dev/null 2>&1 && echo "Previous key removed from $ancien_depot."
  fi
  # GitHub's SSH identity, as GitHub publishes it (the push refuses any other server)
  local cles; cles=$(gh api meta --jq '.ssh_keys[]' 2>/dev/null | grep -E '^(ssh-ed25519|ecdsa-sha2-nistp256|ssh-rsa) [A-Za-z0-9+/=]+$')
  [ -n "$cles" ] || { echo "Cannot read GitHub's SSH keys."; return 1; }
  local h noms=()
  for h in "$HOTE_SSH" "$SECOURS_SSH"; do
    [ -n "$h" ] || continue
    if [ "${h##*:}" = 22 ]; then noms+=("${h%:*}"); else noms+=("[${h%:*}]:${h##*:}"); fi
  done
  { while IFS= read -r c; do for h in "${noms[@]}"; do printf '%s %s\n' "$h" "$c"; done; done <<< "$cles"; } > "$d/known_hosts.neuf"

  exec 8> "$d/verrou"; flock -w 300 8
  mv -f "$d/cle.neuve" "$d/cle" && mv -f "$d/cle.neuve.pub" "$d/cle.pub" && mv -f "$d/known_hosts.neuf" "$d/known_hosts"
  chmod 600 "$d/cle"
  # another repository: what was pushed before means nothing for this one
  [ "$ancien_depot" != "$depot" ] && rm -rf "$d/miroir.git" "$d/dernier.json"
  DEPOT="$depot"; BRANCHE="$branche"; AUTO="$auto"; COMPTE="$compte"; LIE_LE="$(date '+%Y-%m-%dT%H:%M:%S')"; CLE_ID="$id"
  ecrire_reglages "$d" || { echo "Cannot save the settings."; return 1; }
  printf '%s\n' "$projet" > "$d/projet"
  flock -u 8
  echo; echo "Linked: $depot. The sign-in is now closed and deleted from this PC (GitHub still lists \"GitHub CLI\""
  echo "under Settings > Applications > Authorized OAuth Apps: you can revoke it there, the push key does not need it)."
  echo
  echo "First push..."
  sleep 2
  pousser "$projet"
  return 0
}

etat() {
  local projet d gh=false ssh=false
  projet=$(chemin_projet "${1:-}") || { echo '{}'; return 0; }
  d=$(dossier_de "$projet")
  command -v gh >/dev/null 2>&1 && gh=true
  command -v ssh >/dev/null 2>&1 && command -v ssh-keygen >/dev/null 2>&1 && ssh=true
  local dernier=null; [ -f "$d/dernier.json" ] && dernier=$(jq -c . "$d/dernier.json" 2>/dev/null || echo null)
  if lire_reglages "$d"; then
    local b="${BRANCHE:-main}"
    jq -n --argjson gh "$gh" --argjson ssh "$ssh" --arg depot "$DEPOT" --arg branche "$b" --arg choisie "$BRANCHE" \
          --argjson auto "$([ "$AUTO" = 1 ] && echo true || echo false)" --arg compte "$COMPTE" --arg lie "$LIE_LE" \
          --argjson dernier "$dernier" \
      --argjson identite "$(identite_json "$projet")" \
      '{gh:$gh, ssh:$ssh, lie:true, depot:$depot, url:("https://github.com/" + $depot), branche:$branche, branche_choisie:$choisie, auto:$auto, compte:$compte, lie_le:$lie, dernier:$dernier, identite:$identite}'
  else
    jq -n --argjson gh "$gh" --argjson ssh "$ssh" --argjson identite "$(identite_json "$projet")" '{gh:$gh, ssh:$ssh, lie:false, identite:$identite}'
  fi
}

reglage() {  # $1 key (auto|branche), $2 project, $3 value
  local projet d; projet=$(chemin_projet "${2:-}") || return 2
  d=$(dossier_de "$projet")
  [ -f "$d/reglages" ] || { echo "Not linked to GitHub."; return 3; }
  exec 8> "$d/verrou" && flock -w 60 8
  lire_reglages "$d" || { echo "Not linked to GitHub."; return 3; }
  case "$1" in
    auto) [ "${3:-}" = 0 ] && AUTO=0 || AUTO=1 ;;
    branche) BRANCHE="${3:-main}"; branche_valide "$BRANCHE" || { echo "Invalid branch name: $3"; return 2; }
             rm -f "$d/dernier.json" ;;
  esac
  ecrire_reglages "$d" && echo "Saved."
}

delier() {
  local projet d; projet=$(chemin_projet "${1:-}") || return 2
  d=$(dossier_de "$projet")
  [ -d "$d" ] || { echo "Not linked to GitHub."; return 0; }
  lire_reglages "$d"
  exec 8> "$d/verrou"; flock -w 300 8
  find "$d" -mindepth 1 -maxdepth 1 ! -name identite ! -name compte -exec rm -rf {} +   # the commit author stays
  echo "Unlinked${DEPOT:+ from $DEPOT}: the key is deleted from this PC (GitHub keeps an unusable entry under the repository's Settings > Deploy keys, which you can remove)."
}

suivre() {  # the project's folder changed: its GitHub link follows
  local ancien nouveau a n
  ancien=$(chemin_projet "${1:-}") && nouveau=$(chemin_projet "${2:-}") || return 2
  a=$(dossier_de "$ancien"); n=$(dossier_de "$nouveau")
  [ "$a" = "$n" ] || [ ! -d "$a" ] && return 0
  [ -e "$n" ] && { echo "The new folder already has a GitHub link: kept."; return 0; }
  mv "$a" "$n" && printf '%s\n' "$nouveau" > "$n/projet"
}

# ----------------------------------------------------------------------------- author of the commits
# The name and email of the project's commits (instead of "Athanor"), and whether Claude appears as
# co-author. Kept in DONNEES/<id>/identite (an unlink keeps it); the last one saved is also the default
# of the projects that have none.
nom_valide_auteur() { [ "${#1}" -le 100 ] && [[ "$1" =~ [^[:space:]] ]] && ! [[ "$1" =~ [\<\>[:cntrl:]] ]]; }
email_valide() { [ "${#1}" -le 254 ] && [[ "$1" =~ ^[^[:space:]\<\>@\"]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$ ]]; }

lire_identite() {  # $1 dir -> AUTEUR_NOM AUTEUR_EMAIL SANS_COAUTEUR IDENTITE_DEFINIE (0|1)
  AUTEUR_NOM=""; AUTEUR_EMAIL=""; SANS_COAUTEUR=0; IDENTITE_DEFINIE=0
  local f k v
  for f in "$DONNEES/identite-defaut" "$1/identite"; do
    [ -f "$f" ] || continue
    AUTEUR_NOM=""; AUTEUR_EMAIL=""; SANS_COAUTEUR=0   # the project's own choice replaces the default
    [ "$f" = "$1/identite" ] && IDENTITE_DEFINIE=1
    while IFS='=' read -r k v; do
      case "$k" in
        nom) nom_valide_auteur "$v" && AUTEUR_NOM="$v" ;;
        email) email_valide "$v" && AUTEUR_EMAIL="$v" ;;
        sans_coauteur) [ "$v" = 1 ] && SANS_COAUTEUR=1 ;;
      esac
    done < "$f"
  done
}

identite_json() {  # $1 project: the author and what to propose (GitHub account, noreply address, git settings)
  local d login="" id="" cnom="" emails=() k v
  d=$(dossier_de "$1")
  lire_identite "$d"
  if [ -f "$d/compte" ]; then
    while IFS='=' read -r k v; do
      case "$k" in login) login="$v" ;; id) id="$v" ;; nom) cnom="$v" ;; email) emails+=("$v") ;; esac
    done < "$d/compte"
  fi
  [ -n "$login" ] || { lire_reglages "$d" >/dev/null 2>&1; login="${COMPTE:-}"; }
  if [[ "$login" =~ ^[A-Za-z0-9-]{1,39}$ ]] && [ -z "$id" ] && command -v curl >/dev/null 2>&1; then
    # linked before 2.8.0: the account number (public) gives the private noreply address
    id=$(curl -fsS --max-time 5 "https://api.github.com/users/$login" 2>/dev/null | jq -r '.id // empty' 2>/dev/null)
    if [[ "$id" =~ ^[0-9]{1,15}$ ]] && [ -d "$d" ]; then
      { printf 'login=%s\nid=%s\n' "$login" "$id"; [ -f "$d/compte" ] && grep -v '^\(login\|id\)=' "$d/compte"; } > "$d/compte.neuf" && mv -f "$d/compte.neuf" "$d/compte"
    else
      id=""
    fi
  fi
  local gnom gemail; gnom=$(git config --global user.name 2>/dev/null); gemail=$(git config --global user.email 2>/dev/null)
  local noreply=""; [[ "$id" =~ ^[0-9]+$ ]] && [ -n "$login" ] && noreply="$id+$login@users.noreply.github.com"
  jq -n --arg nom "$AUTEUR_NOM" --arg email "$AUTEUR_EMAIL" --argjson sans "$([ "$SANS_COAUTEUR" = 1 ] && echo true || echo false)" \
        --argjson defini "$([ "$IDENTITE_DEFINIE" = 1 ] && echo true || echo false)" \
        --arg login "$login" --arg cnom "$cnom" --arg noreply "$noreply" --arg gnom "$gnom" --arg gemail "$gemail" \
        '{nom:$nom, email:$email, sans_coauteur:$sans, defini:$defini,
          noms: ([$cnom, $login, $gnom] | map(select(. != "")) | unique),
          emails: ([ (if $noreply != "" then {email:$noreply, genre:"noreply"} else empty end) ]
                   + ($ARGS.positional | map({email:., genre:"github"}))
                   + (if $gemail != "" then [{email:$gemail, genre:"git"}] else [] end)
                   | unique_by(.email))}' --args "${emails[@]}"
}

regler_identite() {  # $1 project, $2 name, $3 email, $4 1 = without Claude as co-author
  local projet d; projet=$(chemin_projet "${1:-}") || return 2
  nom_valide_auteur "${2:-}" || { echo "Invalid name."; return 2; }
  email_valide "${3:-}" || { echo "Invalid email."; return 2; }
  d=$(dossier_de "$projet")
  mkdir -p "$d" && chmod 700 "$d" "$DONNEES" 2>/dev/null
  local s=0; [ "${4:-0}" = 1 ] && s=1
  printf 'nom=%s\nemail=%s\nsans_coauteur=%s\n' "$2" "$3" "$s" > "$d/identite.neuf" && mv -f "$d/identite.neuf" "$d/identite" || return 1
  cp -f "$d/identite" "$DONNEES/identite-defaut" 2>/dev/null
  echo "Saved."
}

identite_pilote() {  # $1 project: "name<TAB>email<TAB>0|1"; the user's git settings when nothing was chosen
  local projet d; projet=$(chemin_projet "${1:-}") || return 2
  d=$(dossier_de "$projet")
  lire_identite "$d"
  if [ -z "$AUTEUR_NOM" ] || [ -z "$AUTEUR_EMAIL" ]; then
    local gn ge; gn=$(git config --global user.name 2>/dev/null); ge=$(git config --global user.email 2>/dev/null)
    nom_valide_auteur "$gn" && email_valide "$ge" && { AUTEUR_NOM="$gn"; AUTEUR_EMAIL="$ge"; }
  fi
  printf '%s\t%s\t%s\n' "$AUTEUR_NOM" "$AUTEUR_EMAIL" "$SANS_COAUTEUR"
}

# The project's commit-msg hook removing the co-author lines Claude Code adds. It runs only when Claude
# commits, inside its sandbox (the pilot runs git with hooks disabled). A hook of the user's (without
# Athanor's mark) is never replaced. Written without following a link Claude could have put in its place.
crochet() {
  local projet; projet=$(chemin_projet "${1:-}") || return 2
  python3 -I -c '
import os, sys
p, actif = sys.argv[1], sys.argv[2] == "1"
MARQUE = b"# athanor: commits without Claude as co-author"
texte = b"#!/bin/sh\n" + MARQUE + b" (option of the GitHub block, Manage page)\n" \
    b"sed -i -e \"/^[Cc]o-[Aa]uthored-[Bb]y:.*[Cc]laude/d\" -e \"/Generated with \\[Claude Code\\]/d\" -e \"/^.\\{0,4\\}Generated with Claude/d\" \"$1\"\n"
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
' "$projet" "${2:-0}"
}

case "${1:-}" in
  etat)    etat "${2:-}" ;;
  identite) identite_json "$(chemin_projet "${2:-}")" ;;
  regler-identite) regler_identite "${2:-}" "${3:-}" "${4:-}" "${5:-0}" ;;
  identite-pilote) identite_pilote "${2:-}" ;;
  crochet) crochet "${2:-}" "${3:-0}" ;;
  lier)    shift; lier "$@" ;;
  pousser) shift; pousser "$@" ;;
  auto)    reglage auto "${2:-}" "${3:-}" ;;
  branche) reglage branche "${2:-}" "${3:-}" ;;
  delier)  delier "${2:-}" ;;
  suivre)  suivre "${2:-}" "${3:-}" ;;
  *) sed -n '2,/^set -u/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit 2 ;;
esac
