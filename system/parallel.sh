# shellcheck shell=bash
# Parallel mode (loaded by pilot.sh; setting PARALLEL=1 in PROJECT/.athanor/.env).
#
# The plan is organised in waves ("## Wave N") of independent milestones. On each parallel cycle:
#   1. every unchecked milestone of the current wave is handed to a "worker": a separate Claude session,
#      in its own git worktree (.athanor/work/Mx, branch auto/Mx), all at the same time;
#   2. when all of them are done, the project lead merges the branches, runs the full check,
#      ticks what passed, notes the rest in BUGS.md and commits.
# Much faster; uses more quota (one session per worker).

PARALLELE_MAX_DEFAUT=3

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

# Kept for the pilot: number of milestones that could start right now.
parallele_vague_courante() { parallele_prets "" | head -n "$PARALLEL_MAX"; }

parallele_consignes() {  # section appended to the lead's instructions when the mode is active
  cat <<'EOF'

## Parallel mode (active for this project)

Workers build milestones **at the same time**, each in its own git worktree and branch `auto/<milestone>`; the pilot merges each one as soon as it finishes and immediately starts the next milestone whose dependencies are met. Your job as the lead is therefore a **precise, fine-grained plan** with explicit dependencies: the better it is cut, the more work runs in parallel and the simpler the merges.

Plan rules (`.athanor/PLAN.md`):
- One milestone = one worker session = a small, self-contained piece of work (one page, one module, one feature with its tests), doable alone by someone who only knows the documents, the skeleton and the milestone text. Prefer many small milestones (10 to 25) to a few big ones.
- Every milestone line ends with `Files: <files it creates or modifies>. Depends on: <milestone ids, or none>. Acceptance: <verifiable criteria>.` Example: `- [ ] M7 : Contact form with client-side validation — Files: contact.html, css/contact.css, js/contact.js, tests/e2e/test_contact.py. Depends on: M1. Acceptance: empty submit shows 5 errors; valid submit shows "Thank you"; check.sh quick contact passes.`
- **M1 is always the foundation**: the runnable skeleton, `check.sh`, and the **contracts** everything else relies on (data structures, interfaces, conventions, shared stylesheet, header/footer, test helpers). Everything shared is built before it is parallelised; milestones that only depend on M1 can all run at once.
- Two milestones that modify the same file must depend on one another (one after the other). A shared file discovered later is changed minimally and flagged in the worker's report.
- Milestones that test, review or polish the work of others (cross-cutting tests, tester/critic passes, polish, documentation, delivery) depend on everything they cover; the final delivery milestone depends on all the others.
- `## Wave N` headings are still accepted (a milestone without `Depends on:` depends on every milestone of the earlier waves), but explicit dependencies are preferred: they let the pilot start work earlier.
- **Additional requests** (sections "Additional request" of REQUEST.md, flagged by `.athanor/REQUEST_CHANGED`): turn each into milestones with their dependencies and append them to the plan; keep a single final delivery milestone at the very end that depends on everything.
If the plan already exists without `Depends on:` nor waves, add the dependencies without changing the milestones' content (commit), and stop there for this cycle.
EOF
}

parallele_consignes_ouvrier() {  # $1 = identifier, $2 = milestone line, $3 = 1 if the executor sub-agent is available
  local aides="Do not launch any subagent."
  [ "$3" = 1 ] && aides="The only sub-agent you may call is \`executor\` (cheap model), for mechanical work that would flood your context: running the test suite or a long build and returning only the failures, searching the code, the same small edit on many of your files. Give it a closed brief: what, where (only your milestone's files, in this folder), how to verify. A short command, run it yourself."
  cat <<EOF

## You are a parallel worker

You carry out **only** this milestone, in this folder (git worktree of the project, branch \`auto/$1\`):

> $2

- Other workers are doing other milestones at the same time, each in their own worktree; the pilot merges your branch as soon as you finish. Touch only the files of your milestone; if you must modify a shared file (common stylesheet, header...), make the minimal change and flag it in your report.
- Do not modify \`.athanor/PLAN.md\` or \`.athanor/LOG.md\`: the project lead handles them after the merge.
- $aides
- Check with \`bash .athanor/check.sh quick <pattern>\` (or your milestone's tests if there is no script), then \`git add -A && git commit -m "$1: ..."\` on your branch. Commit even partial work, and say so.
- End with a report of at most 10 lines: done / not done / files touched / what the lead needs to know to merge.
EOF
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
  local trav="$ETAT/work/$ident"
  g -C "$PROJECT" worktree remove --force "$trav" >/dev/null 2>&1 || rm -rf "$trav"
  g -C "$PROJECT" worktree prune >/dev/null 2>&1
  g -C "$PROJECT" branch -D "auto/$ident" >/dev/null 2>&1 || true
  g -C "$PROJECT" worktree add -q -b "auto/$ident" "$trav" HEAD >/dev/null 2>&1 || { log "Worker $ident: cannot create the worktree."; return 1; }
  rm -rf "$trav/.athanor" && mkdir -p "$trav/.athanor/screenshots" "$trav/.athanor/.verif"
  for f in PLAN.md REQUEST.md check.sh PC_TOOLS.md PC_TOOLS_GUIDE.md MAP.md BUGS.md; do
    [ -f "$ETAT/$f" ] && [ ! -L "$ETAT/$f" ] && cp -f "$ETAT/$f" "$trav/.athanor/$f"
  done
  # The worker's team: only "executor" (cheap model), so that long outputs do not fill its context.
  local agents_ouvrier=""
  [ "$AGENTS_PAR_JSON" = 1 ] && agents_ouvrier=$(printf '%s' "$AGENTS_JSON" | python3 -c 'import json, sys
d = {k: v for k, v in json.load(sys.stdin).items() if k == "executor"}
print(json.dumps(d, ensure_ascii=False) if d else "")' 2>/dev/null)
  { cat "$CONSIGNES"; parallele_consignes_ouvrier "$ident" "$ligne" "$([ -n "$agents_ouvrier" ] && echo 1)"; } > "$PRIVE/CLAUDE.ouvrier-$ident.md"
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
  local but="/goal The milestone $ident of .athanor/PLAN.md is carried out according to your worker instructions, checked, and committed on branch auto/$ident (git log shows it), with your report as the final answer. Or stop after ${MAX_TURNS_PER_CYCLE} turns."
  local a=(-p "$but" --permission-mode bypassPermissions --settings "$PRIVE/reglages-$ident.json" --permission-prompts none --output-format stream-json --verbose
           "${ARGS_SURS[@]}" --append-system-prompt "$(cat "$PRIVE/CLAUDE.ouvrier-$ident.md")")
  [ -n "$agents_ouvrier" ] && a+=(--agents "$agents_ouvrier")
  [ -f "$PRIVE/mcp.json" ] && a+=(--mcp-config "$PRIVE/mcp.json")
  [ -n "$template" ] && a+=(--model "$template")
  [ -n "$FALLBACK_MODEL" ] && [ "$template" != "$FALLBACK_MODEL" ] && a+=(--fallback-model "$FALLBACK_MODEL")
  [ -n "$EFFORT" ] && a+=(--effort "$EFFORT")
  : > "$journal"
  setsid bash -c 'echo $$ > "$1"; f="$2"; d="$3"; b="$4"; PATH="$5"; export PROJECT="$d"; shift 5; cd "$d" && "$b" "$@" < /dev/null 2>&1; echo $? > "$f"' \
    _ "$PRIVE/ouvrier_$ident.sid" "$PRIVE/ouvrier_$ident.code" "$trav" "$CLAUDE_BIN" "$CHEMIN_CLAUDE" "${a[@]}" > "$journal" 9>&- &
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
  enregistrer_stats "$prefixe-$ident.jsonl" "$([ "$code" = 0 ] && echo ok || echo erreur)" "$cycle·$ident" "ref:auto/$ident"
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
  declare -A lignes=()
  mkdir -p "$ETAT/work"
  # uncommitted changes in the main checkout (a lead session cut short) would make the merges fail:
  # they are committed first, the lead reviews them like the workers' branches
  if [ -n "$(g -C "$PROJECT" status --porcelain --untracked-files=no 2>/dev/null)" ]; then
    g -C "$PROJECT" add -u >/dev/null 2>&1 && g -C "$PROJECT" commit -q -m "Work in progress before the parallel cycle (lead)" >/dev/null 2>&1 && log "Uncommitted changes of the lead committed before the cycle."
  fi
  rm -f "$PRIVE"/ouvrier_*.sid "$PRIVE"/ouvrier_*.code "$PRIVE"/ouvrier_*.pid "$PRIVE/rapports_ouvriers.md"
  echo "# Worker reports ($(date '+%Y-%m-%d %H:%M'))" > "$PRIVE/rapports_ouvriers.md"
  debut=$(date +%s)
  lancer_prets() {  # fills the free slots with ready milestones
    local n
    while [ "${#courants[@]}" -lt "$PARALLEL_MAX" ]; do
      n=${#courants[@]}
      while IFS=$'\t' read -r ident fait deps ligne; do
        [ "${#courants[@]}" -lt "$PARALLEL_MAX" ] || break
        case " ${traites[*]} " in *" $ident "*) continue ;; esac
        traites+=("$ident"); lignes[$ident]="$ligne"
        if parallele_lancer_ouvrier "$ident" "$ligne" "$prefixe-$ident.jsonl" "$template"; then
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
  log "Cycle $cycle (parallel): up to $PARALLEL_MAX workers${template:+ with $template}"
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
      publier "$ETAT/WORKER_REPORTS.md" < "$PRIVE/rapports_ouvriers.md"
    done
    courants=("${restants[@]}")
    # the cycle's time budget is spent: no new worker, the lead verifies what is there
    [ $(( maintenant - debut )) -lt $(( CYCLE_MAX_MIN * SECONDS_PER_MINUTE )) ] && lancer_prets || { ETAT_OUVRIERS="${courants[*]}"; statut "Workers in parallel: ${courants[*]:-none}"; }
  done
  ETAT_OUVRIERS=""
  log "Workers finished: merged ${fusionnes[*]:-none}; to merge ${a_fusionner[*]:-none}; failed ${echoues[*]:-none}. The lead verifies."
  statut "Merging milestones ${a_fusionner[*]} ${fusionnes[*]}"
  fichier_log="$prefixe.jsonl"
  local but="/goal The parallel cycle is verified according to your verification instructions: the branches ${a_fusionner[*]:-(none)} are merged, bash .athanor/check.sh has been run and its output is shown, .athanor/PLAN.md ticks exactly the milestones that pass, .athanor/LOG.md has a new entry, git log shows the commit. Or stop after ${MAX_TURNS_PER_CYCLE} turns."
  args=(-p "$but" --permission-mode bypassPermissions --settings "$REGLAGES" --permission-prompts none --output-format stream-json --verbose
        "${ARGS_SURS[@]}" --append-system-prompt "$(cat "$CONSIGNES"; parallele_consignes_fusion "${fusionnes[*]}" "${a_fusionner[*]}" "${echoues[*]}")")
  [ "$AGENTS_PAR_JSON" = 1 ] && args+=(--agents "$AGENTS_JSON")
  [ -f "$PRIVE/mcp.json" ] && args+=(--mcp-config "$PRIVE/mcp.json")
  [ -n "$template" ] && args+=(--model "$template")
  [ -n "$FALLBACK_MODEL" ] && [ "$template" != "$FALLBACK_MODEL" ] && args+=(--fallback-model "$FALLBACK_MODEL")
  [ -n "$EFFORT" ] && args+=(--effort "$EFFORT")
  local clore="$CLOSE_AFTER_MILESTONE"; CLOSE_AFTER_MILESTONE=0   # the verification touches several milestones: do not close at the first one
  lancer_claude_surveille "$fichier_log"
  local code=$?
  CLOSE_AFTER_MILESTONE="$clore"
  # Merged branches: deleted; the others stay visible (git branch) until the next attempt.
  for ident in "${a_fusionner[@]}" "${echoues[@]}"; do
    g -C "$PROJECT" branch --merged HEAD 2>/dev/null | grep -qx "  auto/$ident" && g -C "$PROJECT" branch -d "auto/$ident" >/dev/null 2>&1
  done
  return "$code"
}
