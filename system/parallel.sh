# shellcheck shell=bash
# Parallel mode (loaded by pilot.sh; setting PARALLEL=1 in PROJECT/.athanor/.env).
#
# The plan is organised in waves ("## Wave N") of independent milestones. On each parallel cycle:
#   1. every unchecked milestone of the current wave is handed to a "worker": a separate Claude session,
#      in its own git worktree (.athanor/work/Mx, branch auto/Mx), all at the same time;
#   2. when all of them are done, the project lead merges the branches, runs the full check,
#      ticks what passed, notes the rest in BUGS.md and commits.
# Much faster; uses more quota (one session per worker). So the pilot starts with 2 workers and adds one
# each time a worker delivers, up to PARALLEL_MAX; once a worker meets the quota, no other one starts
# (they would all fail on it together). Workers run on WORKER_MODEL (pilot.sh), the lead on its own.

PARALLELE_MAX_DEFAUT=3
PARALLELE_DEPART=2

# Re-reads PARALLEL / PARALLEL_MAX from the project's .env on every cycle (editable from the app).
# Worktrees of a wave that did not finish (stop, crash): removed, the branches auto/* are kept (they hold
# the work and are recreated or merged by the next wave). Called at stop and at startup.
parallele_nettoyer_worktrees() {
  local t
  for t in "$ETAT"/work/*/; do
    [ -d "$t" ] || continue
    g -C "$PROJECT" worktree remove --force "${t%/}" >/dev/null 2>&1 || rm -rf "${t%/}"
  done
  g -C "$PROJECT" worktree prune >/dev/null 2>&1
  return 0
}

parallele_lire_reglage() {
  PARALLEL=0; PARALLEL_MAX=$PARALLELE_MAX_DEFAUT
  [ -f "$ETAT/.env" ] || return 0
  local ligne
  while IFS= read -r ligne || [ -n "$ligne" ]; do
    ligne=${ligne%$'\r'}
    case "$ligne" in
      PARALLEL=*|PARALLELE=*) PARALLEL="${ligne#*=}" ;;
      PARALLEL_MAX=*|PARALLELE_MAX=*) PARALLEL_MAX="${ligne#*=}" ;;
    esac
  done < "$ETAT/.env"
  # Claude can edit this file: only two exact values are accepted
  [ "$PARALLEL" = 1 ] || PARALLEL=0
  [[ "$PARALLEL_MAX" =~ ^([2-9]|10)$ ]] || PARALLEL_MAX=$PARALLELE_MAX_DEFAUT
  # Worktrees would wipe a workshop that git already tracks (and the merge would delete it):
  # parallel mode needs .athanor/ out of the index first.
  if [ "$PARALLEL" = 1 ] && [ -n "$(g -C "$PROJECT" ls-files -- .athanor 2>/dev/null | head -n 1)" ]; then
    PARALLEL=0
    [ -n "${PARALLELE_REFUS_SIGNALE:-}" ] || log "Parallel mode ignored: .athanor/ is tracked by git. Run 'git rm -r --cached .athanor' in the project, then commit, to enable it."
    PARALLELE_REFUS_SIGNALE=1
  fi
}

# Parallel mode: the queued requests do not wait for the current one to be delivered. They join the
# current request right away (appended to REQUEST.md, moved to done/); the change of REQUEST.md makes
# the lead fold their milestones into the plan as waves, so they are built alongside the rest.
parallele_absorber_file() {
  local f nom faite n=0
  for f in "$ETAT"/queue/*.md; do
    [ -f "$f" ] && [ ! -L "$f" ] || continue
    nom=$(basename "$f" .md)
    faite="$ETAT/done/$(date '+%Y%m%d-%H%M%S')-$nom.md"
    mkdir -p "$ETAT/done" && mv -f "$f" "$faite" || continue
    {
      printf '\n\n## Additional request (%s, %s)\n\n' "$(date '+%Y-%m-%d %H:%M')" "$nom"
      tr -d '\r' < "$faite"
      printf '\n'
    } >> "$ETAT/REQUEST.md"
    n=$((n + 1))
  done
  [ "$n" -gt 0 ] && log "Parallel mode: $n queued request(s) joined the current plan."
  return 0
}

parallele_plan_a_des_vagues() { grep -qiE '^## +Wave|Depends on:' "$ETAT/PLAN.md" 2>/dev/null; }

# The whole plan, one top-level milestone per line: "id<TAB>done(0/1)<TAB>deps(comma list)<TAB>full line".
# Dependencies: an explicit "Depends on: M1, M2" in the milestone line (preferred), else every milestone of
# the earlier waves (## Wave N headings). Identifiers: "M4", "M4b", "M4.1" -> M4, M4b, M4_1 (never "-",
# which separates the worker from the cycle in log file names); unique within the plan.
parallele_plan() {
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
}

# Milestones ready to start: unchecked, every dependency ticked, not already handled this cycle
# ($1 = space-separated identifiers to exclude). Output like parallele_plan.
parallele_prets() {
  local exclus=" $1 " ident fait deps ligne d ok liste
  declare -A faits=()
  while IFS=$'\t' read -r ident fait deps ligne; do [ "$fait" = 1 ] && faits[$ident]=1; done < <(parallele_plan)
  while IFS=$'\t' read -r ident fait deps ligne; do
    [ "$fait" = 0 ] || continue
    case "$exclus" in *" $ident "*) continue ;; esac
    ok=1; IFS=, read -ra liste <<< "$deps"
    for d in "${liste[@]}"; do [ -z "$d" ] || [ -n "${faits[$d]:-}" ] || { ok=0; break; }; done
    [ "$ok" = 1 ] && printf '%s\t%s\t%s\t%s\n' "$ident" "$fait" "$deps" "$ligne"
  done < <(parallele_plan)
  return 0
}

# Two workers must not change the same files at once (their merges would conflict, and the lead would
# merge them by hand). $1 = the line of a ready milestone, then "ident<TAB>line" for each running worker;
# prints the first running worker whose "Files:" share a file with it, nothing otherwise. Files listed by
# 3 milestones or more of the plan (routes, package.json, shared types…) are left out: waiting on them
# would make every milestone serial, and their small conflicts stay with the lead.
parallele_chevauchement() {
  python3 -I - "$ETAT/PLAN.md" "$@" <<'PY'
import re, sys
from collections import Counter
def fichiers(l):
    m = re.search(r"Files?:\s*(.*?)(?:\.\s+(?:Depends on|Acceptance|Read)\b|$)", l, re.I)
    if not m:
        return set()
    out = set()
    for f in re.split(r"[,;]", m.group(1)):
        f = re.sub(r"\s*\(.*$", "", f).strip().strip("`'\" ").lstrip("./").rstrip("/")
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
    ident, _, l = a.partition("\t")
    if any(touche(x, y) for x in miens for y in fichiers(l) if y not in communs):
        print(ident)
        break
PY
}

# Branches auto/<milestone> left with work not merged (a worker stopped by the quota or the watchdog), for
# milestones still open: one identifier per line. The branches of milestones already ticked are deleted:
# their work is in, or was redone.
parallele_branches_partielles() {
  local br id ident fait deps ligne
  declare -A etat=()
  while IFS=$'\t' read -r ident fait deps ligne; do etat[$ident]=$fait; done < <(parallele_plan 2>/dev/null)
  while read -r br; do
    [ -n "$br" ] || continue
    id=${br#auto/}
    case "${etat[$id]:-}" in
      1) g -C "$PROJECT" branch -D "$br" >/dev/null 2>&1 && log "Branch $br deleted: its milestone is done." >&2 ;;
      0) [ "$(entier "$(g -C "$PROJECT" rev-list --count HEAD.."$br" 2>/dev/null)" 0)" -gt 0 ] && printf '%s\n' "$id" ;;
    esac
  done < <(g -C "$PROJECT" for-each-ref --format='%(refname:short)' 'refs/heads/auto/*' 2>/dev/null | head -n 50)
  return 0
}

# Kept for the pilot: number of milestones that could start right now.
parallele_vague_courante() { parallele_prets "" | head -n "$PARALLEL_MAX"; }

parallele_consignes() {  # section appended to the lead's instructions when the mode is active
  cat <<'EOF'

## Parallel mode (active for this project)

Workers build milestones **at the same time**, each in its own git worktree and branch `auto/<milestone>`; the pilot merges each one as soon as it finishes and immediately starts the next milestone whose dependencies are met. Your job as the lead is therefore a **precise, fine-grained plan** with explicit dependencies: the better it is cut, the more work runs in parallel and the simpler the merges.

Plan rules (`.athanor/PLAN.md`):
- One milestone = one worker session = a small, self-contained piece of work (one page, one module, one feature with its tests), doable alone by someone who only knows the documents, the skeleton and the milestone text. Prefer many small milestones (10 to 25) to a few big ones.
- Every milestone line ends with `Files: <files it creates or modifies>. Depends on: <milestone ids, or none>. Acceptance: <verifiable criteria>. Read: <the document sections the worker needs>.` Example: `- [ ] M7 : Contact form with client-side validation — Files: contact.html, css/contact.css, js/contact.js, tests/e2e/test_contact.py. Depends on: M1. Acceptance: empty submit shows 5 errors; valid submit shows "Thank you"; check.sh quick contact passes. Read: DESIGN.md §Contact.` A worker reads only its line and what `Read` names: everything it needs is there.
- **M1 is always the foundation**: the runnable skeleton, `check.sh`, and the **contracts** everything else relies on (data structures, interfaces, conventions, shared stylesheet, header/footer, test helpers). Everything shared is built before it is parallelised; milestones that only depend on M1 can all run at once.
- Two milestones that modify the same file must depend on one another (one after the other). A shared file discovered later is changed minimally and flagged in the worker's report.
- Milestones that test, review or polish the work of others (cross-cutting tests, tester/critic passes, polish, documentation, delivery) depend on everything they cover; the final delivery milestone depends on all the others.
- `## Wave N` headings are still accepted (a milestone without `Depends on:` depends on every milestone of the earlier waves), but explicit dependencies are preferred: they let the pilot start work earlier.
- **Additional requests** (sections "Additional request" of REQUEST.md, flagged by `.athanor/REQUEST_CHANGED`): turn each into milestones with their dependencies and append them to the plan; keep a single final delivery milestone at the very end that depends on everything.
If the plan already exists without `Depends on:` nor waves, add the dependencies without changing the milestones' content (commit), and stop there for this cycle.
EOF
}

parallele_consignes_ouvrier() {  # $1 = identifier, $2 = milestone line, $3 = 1 if the executor sub-agent is available, $4 = 1 if resuming
  local aides="Do not launch any subagent."
  [ "$3" = 1 ] && aides="The only sub-agent you may call is \`executor\` (cheap model), for mechanical work that would flood your context: running the test suite or a long build and returning only the failures, searching the code, the same small edit on many of your files. Give it a closed brief: what, where (only your milestone's files, in this folder), how to verify. A short command, run it yourself."
  cat <<EOF

## You are a parallel worker

You carry out **only** this milestone, in this folder (git worktree of the project, branch \`auto/$1\`):

> $2

$([ "${4:-0}" = 1 ] && printf -- '- **This branch already holds partial work** from an earlier attempt that was stopped (quota, watchdog): read its commits (`git log --oneline -5`, `git show --stat HEAD`) and carry on from there, never start over.\n')
- Other workers are doing other milestones at the same time, each in their own worktree; the pilot merges your branch as soon as you finish. Touch only the files of your milestone; if you must modify a shared file (common stylesheet, header...), make the minimal change and flag it in your report.
- Do not modify \`.athanor/PLAN.md\` or \`.athanor/LOG.md\`: the project lead handles them after the merge.
- $aides
- Check with \`bash .athanor/check.sh quick <pattern>\` (or your milestone's tests if there is no script), then \`git add -A && git commit -m "$1: ..."\` on your branch. Commit even partial work, and say so.
- Something only the user can do (an account, a payment, a code, a key)? Do not ask anyone and do not create a task: do what you can without it and say exactly what is missing in your report; the lead hands it to the user.
- End with a report of at most 10 lines: done / not done / files touched / what the lead needs to know to merge.
EOF
}

parallele_consignes_base() {  # Claude's instructions, with the commit-author rule of this cycle (pilot.sh)
  if [ "${ATHANOR_SANS_COAUTEUR:-0}" = 1 ] && [ -f "$PRIVE/CLAUDE.auteur.md" ]; then echo "$PRIVE/CLAUDE.auteur.md"; else echo "$CONSIGNES"; fi
}

parallele_consignes_fusion() {  # $1 = auto-merged, $2 = to merge (conflicts), $3 = failed
  cat <<EOF

## Verification after a parallel cycle

The pilot has just run workers in parallel (reports in \`.athanor/WORKER_REPORTS.md\`):
- **Merged automatically** (no conflict, already in the current branch): ${1:-none}. Verify them, do not redo them.
- **To merge by you** (conflicts): ${2:-none}. For each, in plan order: \`git merge --no-ff auto/<milestone>\`, resolve the conflict keeping the intent of both sides, complete the merge, run \`bash .athanor/check.sh quick <pattern>\`.
- **Failed or empty** (no usable commit): ${3:-none}. Their branch may hold partial work; decide whether to keep it or redo the milestone later.
Then **one** full check \`bash .athanor/check.sh\` and fix whatever the merges broke (small fixes only; a real rework becomes a note in \`.athanor/BUGS.md\`). In \`.athanor/PLAN.md\`, keep ticked only the milestones that really pass; untick the others and note in \`.athanor/BUGS.md\` what is missing. One log entry, one commit.
EOF
}

# Launches a worker: $1 = identifier, $2 = milestone line, $3 = log file, $4 = model (empty = default).
parallele_lancer_ouvrier() {
  local ident="$1" ligne="$2" journal="$3" template="$4"
  local trav="$ETAT/work/$ident" reprise=0
  g -C "$PROJECT" worktree remove --force "$trav" >/dev/null 2>&1 || rm -rf "$trav"
  g -C "$PROJECT" worktree prune >/dev/null 2>&1
  # an earlier attempt left commits on its branch (stopped by the quota or the watchdog): kept, resumed
  if g -C "$PROJECT" rev-parse -q --verify "auto/$ident^{commit}" >/dev/null 2>&1 \
     && [ "$(entier "$(g -C "$PROJECT" rev-list --count HEAD.."auto/$ident" 2>/dev/null)" 0)" -gt 0 ] \
     && g -C "$PROJECT" worktree add -q "$trav" "auto/$ident" >/dev/null 2>&1; then
    reprise=1
    # on top of what was merged since, so that its own merge does not conflict; as it was if that fails
    if g -C "$trav" rebase -q "$(g -C "$PROJECT" rev-parse HEAD)" >/dev/null 2>&1; then
      log "Worker $ident: resumes the work already on auto/$ident, brought up to date."
    else
      g -C "$trav" rebase --abort >/dev/null 2>&1
      log "Worker $ident: resumes the work already on auto/$ident (not brought up to date: it conflicts, the merge will tell)."
    fi
  else
    g -C "$PROJECT" branch -D "auto/$ident" >/dev/null 2>&1 || true
    g -C "$PROJECT" worktree add -q -b "auto/$ident" "$trav" HEAD >/dev/null 2>&1 || { log "Worker $ident: cannot create the worktree."; return 1; }
  fi
  rm -rf "$trav/.athanor" && mkdir -p "$trav/.athanor/screenshots" "$trav/.athanor/.verif"
  for f in PLAN.md REQUEST.md check.sh PC_TOOLS.md PC_TOOLS_GUIDE.md MAP.md BUGS.md SOLUTIONS.md secrets.env; do
    [ -f "$ETAT/$f" ] && [ ! -L "$ETAT/$f" ] && cp -f "$ETAT/$f" "$trav/.athanor/$f"
  done
  # the working documents and the files attached to the requests: the worker's sandbox only sees its
  # worktree (copied, never linked: a worker's edit stays its own; links Claude planted are not followed)
  for f in docs attachments; do
    if [ -d "$ETAT/$f" ] && [ ! -L "$ETAT/$f" ]; then
      cp -R -P "$ETAT/$f" "$trav/.athanor/$f" 2>/dev/null
      find "$trav/.athanor/$f" -type l -delete 2>/dev/null
    fi
  done
  # The worker's team: only "executor" (cheap model), so that long outputs do not fill its context.
  local agents_ouvrier=""
  # plus the project's own sub-agents (created by the user, or proposed by the lead): system/equipe.py
  [ "$AGENTS_PAR_JSON" = 1 ] && agents_ouvrier=$(SUBAGENT_MODEL="$SUBAGENT_MODEL" CHEAP_MODEL="$CHEAP_MODEL" python3 "$SYSTEME/equipe.py" json "$KIT" "$PROJECT" "$PRIVE/agents" --ouvrier 2>/dev/null)
  [ "$agents_ouvrier" = "{}" ] && agents_ouvrier=""
  { cat "$(parallele_consignes_base)"; parallele_consignes_ouvrier "$ident" "$ligne" "$([ -n "$agents_ouvrier" ] && echo 1)" "$reprise"; } > "$PRIVE/CLAUDE.ouvrier-$ident.md"
  # The worker's sandbox: its worktree (PROJECT = the worktree, for the guard and the tools),
  # plus write access to the project's .git, where git stores the worktree's objects and branch.
  local principal="$PROJECT"
  ATHANOR_PROJET_PRINCIPAL="$principal" PROJECT="$trav" python3 "$SYSTEME/settings.py" "$KIT" "$PRIVE/reglages-$ident.json" > /dev/null || { log "Worker $ident: cannot build the settings."; return 1; }
  python3 - "$PRIVE/reglages-$ident.json" "$PROJECT/.git" <<'PY' || return 1
import json, sys
f, git = sys.argv[1], sys.argv[2]
r = json.load(open(f, encoding="utf-8"))
fs = r["sandbox"]["filesystem"]
fs["allowWrite"] = fs.get("allowWrite", []) + [git]
fs["allowRead"] = fs.get("allowRead", []) + [git]
json.dump(r, open(f, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
PY
  local but="/goal The milestone $ident of .athanor/PLAN.md is carried out according to your worker instructions, checked, and committed on branch auto/$ident (git log shows it), with your report as the final answer. Or stop after ${MAX_TURNS_PER_CYCLE} turns. ${PHRASE_LANGUE}"
  local a=(-p "$but" --permission-mode bypassPermissions --settings "$PRIVE/reglages-$ident.json" --permission-prompts none --output-format stream-json --verbose
           "${ARGS_SURS[@]}" --append-system-prompt "$(cat "$PRIVE/CLAUDE.ouvrier-$ident.md")")
  [ -n "$agents_ouvrier" ] && a+=(--agents "$agents_ouvrier")
  # its MCP servers work on its worktree; the skills are the project's
  python3 "$SYSTEME/extensions.py" mcp "$KIT" "$PROJECT" "$PRIVE/mcp-$ident.json" --dossier-mcp "$trav" --principal "$PROJECT" 2>> "$PRIVE/pilot.log"
  [ -f "$PRIVE/mcp-$ident.json" ] && a+=(--mcp-config "$PRIVE/mcp-$ident.json")
  [ -n "${PLUGIN_ATHANOR:-}" ] && a+=(--plugin-dir "$PLUGIN_ATHANOR")
  local dp; for dp in "${PLUGINS_DIRS[@]}"; do a+=(--plugin-dir "$dp"); done
  [ -n "$template" ] && a+=(--model "$template")
  [ -n "$FALLBACK_MODEL" ] && [ "$template" != "$FALLBACK_MODEL" ] && a+=(--fallback-model "$FALLBACK_MODEL")
  [ -n "$EFFORT" ] && a+=(--effort "$EFFORT")
  : > "$journal"
  setsid bash -c 'echo $$ > "$1"; f="$2"; d="$3"; b="$4"; PATH="$5"; export PROJECT="$d"; shift 5; cd "$d" && "$b" "$@" < /dev/null 2>&1; echo $? > "$f"' \
    _ "$PRIVE/ouvrier_$ident.sid" "$PRIVE/ouvrier_$ident.code" "$trav" "$BASH" "$CHEMIN_CLAUDE" "$SYSTEME/ecran.sh" lancer "$CLAUDE_BIN" "${a[@]}" > "$journal" 9>&- &
  echo $! > "$PRIVE/ouvrier_$ident.pid"
}

# Handles a worker that has just finished: leftover work committed on its branch, statistics, report,
# then an automatic merge into the current branch when it applies cleanly. Sets `issue` to
# fusionne | a_fusionner | echoue.
parallele_terminer_ouvrier() {
  local ident="$1" prefixe="$2" trav="$ETAT/work/$1" code sid commits
  wait "$(pid_sur "$(cat "$PRIVE/ouvrier_$ident.pid" 2>/dev/null)")" 2>/dev/null
  sid=$(pid_sur "$(cat "$PRIVE/ouvrier_$ident.sid" 2>/dev/null)"); [ -n "$sid" ] && pkill -KILL -s "$sid" 2>/dev/null
  code=$(entier "$(cat "$PRIVE/ouvrier_$ident.code" 2>/dev/null)" 1)
  if [ -d "$trav" ] && [ -n "$(g -C "$trav" status --porcelain 2>/dev/null)" ]; then
    g -C "$trav" add -A >/dev/null 2>&1 && g -C "$trav" commit -qm "$ident: uncommitted worker work (recorded by the pilot)" >/dev/null 2>&1 \
      && log "Worker $ident: uncommitted work recorded on auto/$ident."
  fi
  modele_cycle="${modele_ouvrier:-$modele_cycle}" enregistrer_stats "$prefixe-$ident.jsonl" "$(if journal_quota "$prefixe-$ident.jsonl"; then echo quota; elif [ "$code" = 0 ]; then echo ok; else echo erreur; fi)" "$cycle·$ident" "ref:auto/$ident"
  # what the worker learned about the tools or the project: kept for everyone
  fusionner_solutions "$ETAT/SOLUTIONS.md" "$trav/.athanor/SOLUTIONS.md"
  commits=$(g -C "$PROJECT" rev-list --count HEAD.."auto/$ident" 2>/dev/null || echo 0)
  {
    echo; echo "## $ident (branch auto/$ident, exit code $code, commits: $commits)"
    jq -rR 'fromjson? | select(type == "object" and .type == "result") | .result // empty' "$prefixe-$ident.jsonl" 2>/dev/null | tail -n 40
  } >> "$PRIVE/rapports_ouvriers.md"
  g -C "$PROJECT" worktree remove --force "$trav" >/dev/null 2>&1 || rm -rf "$trav"
  g -C "$PROJECT" worktree prune >/dev/null 2>&1
  issue=echoue
  [ "$code" = 0 ] && [ "$commits" -gt 0 ] || { log "Worker $ident finished without a usable result (exit $code, $commits commit(s))."; return 0; }
  # Automatic merge: clean = merged now (the lead verifies at the end of the cycle); conflict = for the lead.
  if g -C "$PROJECT" merge --no-ff --no-edit -q -m "Merge auto/$ident (worker $ident)" "auto/$ident" >/dev/null 2>&1; then
    issue=fusionne
    python3 - "$ETAT/PLAN.md" "$ident" <<'PY'
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
    printf -- '- %s — %s merged automatically by the pilot (branch auto/%s, %s commit(s)); verified by the lead at the end of the cycle.\n' "$(date '+%Y-%m-%d %H:%M')" "$ident" "$ident" "$commits" >> "$ETAT/LOG.md"
    g -C "$PROJECT" branch -d "auto/$ident" >/dev/null 2>&1
    log "Worker $ident merged automatically ($commits commit(s))."
  else
    g -C "$PROJECT" merge --abort >/dev/null 2>&1
    issue=a_fusionner
    log "Worker $ident finished: conflicts, the lead will merge auto/$ident."
  fi
}

# One full parallel cycle: a scheduler keeps up to PARALLEL_MAX workers busy, merges each one as soon as
# it finishes and starts the next milestones whose dependencies are met; then one verification session
# by the lead. Input: $1 = log prefix (without .jsonl), $2 = model. Output: fichier_log and its exit code.
parallele_cycle() {
  local prefixe="$1" template="$2" ident fait deps ligne debut maintenant derniere sid issue
  local -a courants=() fusionnes=() a_fusionner=() echoues=() traites=()
  local modele_ouvrier="${WORKER_MODEL:-$template}" quota_vu=0 attentes=""
  local limite=$(( PARALLEL_MAX < PARALLELE_DEPART ? PARALLEL_MAX : PARALLELE_DEPART ))
  declare -A lignes=()
  mkdir -p "$ETAT/work"
  # uncommitted changes in the main checkout (a lead session cut short) would make the merges fail:
  # they are committed first, the lead reviews them like the workers' branches
  if [ -n "$(g -C "$PROJECT" status --porcelain --untracked-files=no 2>/dev/null)" ]; then
    g -C "$PROJECT" add -u >/dev/null 2>&1 && g -C "$PROJECT" commit -q -m "Work in progress before the parallel cycle (lead)" >/dev/null 2>&1 && log "Uncommitted changes of the lead committed before the cycle."
  fi
  parallele_branches_partielles >/dev/null   # stale branches out before any worktree is made
  rm -f "$PRIVE"/ouvrier_*.sid "$PRIVE"/ouvrier_*.code "$PRIVE"/ouvrier_*.pid "$PRIVE"/mcp-*.json "$PRIVE/rapports_ouvriers.md"
  echo "# Worker reports ($(date '+%Y-%m-%d %H:%M'))" > "$PRIVE/rapports_ouvriers.md"
  debut=$(date +%s)
  lancer_prets() {  # fills the free slots with ready milestones (none once the quota was met)
    local n
    [ "$quota_vu" = 1 ] && return 0
    while [ "${#courants[@]}" -lt "$limite" ]; do
      n=${#courants[@]}
      while IFS=$'\t' read -r ident fait deps ligne; do
        [ "${#courants[@]}" -lt "$limite" ] || break
        case " ${traites[*]} " in *" $ident "*) continue ;; esac
        local occupe="" c; local -a encours=()
        for c in "${courants[@]}"; do encours+=("$c"$'\t'"${lignes[$c]}"); done
        [ "${#encours[@]}" -gt 0 ] && occupe=$(parallele_chevauchement "$ligne" "${encours[@]}")
        if [ -n "$occupe" ]; then   # it starts as soon as the other one is done
          case " $attentes " in *" $ident "*) ;; *) attentes+=" $ident"; log "Worker $ident waits for $occupe: they change the same files." ;; esac
          continue
        fi
        traites+=("$ident"); lignes[$ident]="$ligne"
        if parallele_lancer_ouvrier "$ident" "$ligne" "$prefixe-$ident.jsonl" "$modele_ouvrier"; then
          courants+=("$ident"); log "Worker $ident started: ${ligne%% — *}"
        else
          echoues+=("$ident")
        fi
      done < <(parallele_prets "${traites[*]}")
      [ "${#courants[@]}" -gt "$n" ] || break
    done
    ETAT_OUVRIERS="${courants[*]}"
    statut "Workers in parallel: ${courants[*]:-none}${fusionnes[*]:+ · merged: ${fusionnes[*]}}"
  }
  log "Cycle $cycle (parallel): $limite workers to start with, up to $PARALLEL_MAX${modele_ouvrier:+, with $modele_ouvrier}"
  lancer_prets
  [ "${#courants[@]}" -gt 0 ] || { log "No worker could start: back to the serial mode for this cycle."; return 3; }
  while [ "${#courants[@]}" -gt 0 ]; do
    sleep 10 9>&- & wait $! 2>/dev/null
    maintenant=$(date +%s)
    local -a restants=()
    for ident in "${courants[@]}"; do
      if kill -0 "$(pid_sur "$(cat "$PRIVE/ouvrier_$ident.pid" 2>/dev/null)")" 2>/dev/null; then
        derniere=$(entier "$(stat -c %Y "$prefixe-$ident.jsonl" 2>/dev/null)" "$debut")
        if [ $(( maintenant - derniere )) -ge $(( IDLE_MAX_MIN * SECONDS_PER_MINUTE )) ] || [ $(( maintenant - debut )) -ge $(( CYCLE_MAX_MIN * SECONDS_PER_MINUTE )) ]; then
          log "Worker $ident stopped by the watchdog."
          sid=$(pid_sur "$(cat "$PRIVE/ouvrier_$ident.sid" 2>/dev/null)")
          [ -n "$sid" ] && { pkill -TERM -s "$sid" 2>/dev/null; sleep 5; pkill -KILL -s "$sid" 2>/dev/null; }
        fi
        restants+=("$ident"); continue
      fi
      assainir "$ETAT"
      parallele_terminer_ouvrier "$ident" "$prefixe"
      case "$issue" in fusionne) fusionnes+=("$ident") ;; a_fusionner) a_fusionner+=("$ident") ;; *) echoues+=("$ident") ;; esac
      if journal_quota "$prefixe-$ident.jsonl"; then
        [ "$quota_vu" = 1 ] || log "Worker $ident met the quota: no other worker starts in this cycle."
        quota_vu=1
      elif [ "$issue" != echoue ] && [ "$limite" -lt "$PARALLEL_MAX" ]; then
        limite=$(( limite + 1 ))   # a delivery without quota trouble: one more worker at once
      fi
      publier "$ETAT/WORKER_REPORTS.md" < "$PRIVE/rapports_ouvriers.md"
    done
    courants=("${restants[@]}")
    # the cycle's time budget is spent: no new worker, the lead verifies what is there
    [ $(( maintenant - debut )) -lt $(( CYCLE_MAX_MIN * SECONDS_PER_MINUTE )) ] && lancer_prets || { ETAT_OUVRIERS="${courants[*]}"; statut "Workers in parallel: ${courants[*]:-none}"; }
  done
  ETAT_OUVRIERS=""
  # every worker met the quota and nothing came in: no verification session (it would meet it too); the
  # pilot reads the quota, and its reset time, from a worker's log, then waits as after a serial cycle
  if [ "$quota_vu" = 1 ] && [ "${#fusionnes[@]}" = 0 ] && [ "${#a_fusionner[@]}" = 0 ]; then
    log "Workers finished: all stopped by the quota (${echoues[*]}). No verification by the lead."
    for ident in "${echoues[@]}"; do
      journal_quota "$prefixe-$ident.jsonl" || continue
      jq -c -R 'fromjson? | select(type == "object" and (.type == "rate_limit_event" or .type == "result")) | if .type == "result" then {type, subtype, is_error, result} else . end' \
        "$prefixe-$ident.jsonl" > "$prefixe.jsonl" 2>/dev/null
      break
    done
    QUOTA_OUVRIERS=1
    fichier_log="$prefixe.jsonl"
    return 1
  fi
  # every worker merged cleanly: the verification only runs the checks and ticks the plan, execution work
  # for the workers' model; a conflict or a failure needs the lead's judgement, on its own model
  local verif="$template"
  if [ "${#a_fusionner[@]}" = 0 ] && [ "${#echoues[@]}" = 0 ] && [ -n "$modele_ouvrier" ] && [ "$modele_ouvrier" != "$template" ]; then
    verif="$modele_ouvrier"
    log "Workers finished: merged ${fusionnes[*]}, all cleanly. The verification runs with $verif."
  else
    log "Workers finished: merged ${fusionnes[*]:-none}; to merge ${a_fusionner[*]:-none}; failed ${echoues[*]:-none}. The lead verifies."
  fi
  MODELE_VERIF="$verif"
  statut "Merging milestones ${a_fusionner[*]} ${fusionnes[*]}"
  fichier_log="$prefixe.jsonl"
  local but="/goal The parallel cycle is verified according to your verification instructions: the branches ${a_fusionner[*]:-(none)} are merged, bash .athanor/check.sh has been run and its output is shown, .athanor/PLAN.md ticks exactly the milestones that pass, .athanor/LOG.md has a new entry, git log shows the commit. Or stop after ${MAX_TURNS_PER_CYCLE} turns. ${PHRASE_LANGUE}"
  args=(-p "$but" --permission-mode bypassPermissions --settings "$REGLAGES" --permission-prompts none --output-format stream-json --verbose
        "${ARGS_SURS[@]}" --append-system-prompt "$(cat "$(parallele_consignes_base)"; parallele_consignes_fusion "${fusionnes[*]}" "${a_fusionner[*]}" "${echoues[*]}")")
  [ "$AGENTS_PAR_JSON" = 1 ] && args+=(--agents "$AGENTS_JSON")
  [ -f "$PRIVE/mcp.json" ] && args+=(--mcp-config "$PRIVE/mcp.json")
  [ -n "${PLUGIN_ATHANOR:-}" ] && args+=(--plugin-dir "$PLUGIN_ATHANOR")
  local dp; for dp in "${PLUGINS_DIRS[@]}"; do args+=(--plugin-dir "$dp"); done
  [ -n "$verif" ] && args+=(--model "$verif")
  [ -n "$FALLBACK_MODEL" ] && [ "$verif" != "$FALLBACK_MODEL" ] && args+=(--fallback-model "$FALLBACK_MODEL")
  [ -n "$EFFORT" ] && args+=(--effort "$EFFORT")
  local clore="$CLOSE_AFTER_MILESTONE"; CLOSE_AFTER_MILESTONE=0   # the verification touches several milestones: do not close at the first one
  lancer_claude_surveille "$fichier_log"
  local code=$?
  CLOSE_AFTER_MILESTONE="$clore"
  # the workers' model ran out during the verification: the pilot treats it as the workers' quota
  [ "$verif" != "$template" ] && journal_quota "$fichier_log" && QUOTA_OUVRIERS=1
  # Merged branches: deleted; the others stay visible (git branch) until the next attempt.
  for ident in "${a_fusionner[@]}" "${echoues[@]}"; do
    g -C "$PROJECT" branch --merged HEAD 2>/dev/null | grep -qx "  auto/$ident" && g -C "$PROJECT" branch -d "auto/$ident" >/dev/null 2>&1
  done
  return "$code"
}
