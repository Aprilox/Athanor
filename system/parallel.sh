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
  [[ "$PARALLEL_MAX" =~ ^[2-6]$ ]] || PARALLEL_MAX=$PARALLELE_MAX_DEFAUT
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

parallele_plan_a_des_vagues() { grep -qiE '^## +Wave' "$ETAT/PLAN.md" 2>/dev/null; }

# Unchecked milestones of the first unfinished wave (at most PARALLEL_MAX), one per line: "M4<TAB>full line".
parallele_vague_courante() {
  python3 - "$ETAT/PLAN.md" "$PARALLEL_MAX" <<'PY'
import re, sys
lignes = open(sys.argv[1], encoding="utf-8").read().splitlines()
maxi = int(sys.argv[2]); vagues = []; courante = None
for l in lignes:
    if re.match(r"^##\s+wave", l, re.I):
        courante = []; vagues.append(courante); continue
    m = re.match(r"^- \[([ xX])\] (.*)$", l.rstrip())   # top-level checkboxes only
    if m and courante is not None:
        courante.append((m.group(1) != " ", m.group(2).strip().replace("\t", " ")))
for v in vagues:
    restants = [t for fait, t in v if not fait]
    if not restants:
        continue
    vus = set()
    for i, t in enumerate(restants[:maxi], 1):
        # worker identifier: "M4", "M4b", "M4.1" -> M4, M4b, M4_1 (never "-": it separates the worker
        # from the cycle in the log file names); unique within the wave
        m = re.match(r"^([A-Za-z]+\d+(?:[.\-]?[A-Za-z0-9]+)?)", t)
        ident = re.sub(r"[^A-Za-z0-9_]", "_", m.group(1))[:20] if m else ""
        if not ident or ident in vus:
            ident = "J%d" % i
            while ident in vus: ident += "x"
        vus.add(ident)
        print(ident + "\t" + t)
    break
PY
}

parallele_consignes() {  # section appended to the lead's instructions when the mode is active
  cat <<'EOF'

## Parallel mode (active for this project)

The plan `.athanor/PLAN.md` must be organised in **waves**: a heading `## Wave 1`, `## Wave 2`... followed by the checkable milestones of that wave. Rules:
- Milestones within the same wave are **independent**: separate files, no dependency between them, each one doable alone by a worker who only knows the documents, the skeleton and its own milestone. Prefer 2 to 4 milestones per wave.
- Wave 1 contains a single milestone: the runnable skeleton, `check.sh` and the **contracts** (data structures, interfaces, conventions, shared stylesheet, shared header and footer) that the following waves depend on. Whatever is shared is built before being parallelised.
- A milestone that tests, reviews or polishes the work of other milestones (cross-cutting tests, a tester or critic pass, polish) goes in a wave **after** them, never in the same wave: in parallel it would run before what it checks exists.
- Every milestone states explicitly which files it creates or modifies. A file modified by two milestones of the same wave = plan error: move one of them to a later wave.
- The last waves (polish, delivery) often hold a single milestone: that is normal.
- **Additional requests** (sections "Additional request" of REQUEST.md, flagged by `.athanor/REQUEST_CHANGED`): turn each into milestones and fold them into the plan as waves: into an existing future wave when they are independent of it, otherwise into new waves; keep a single final delivery milestone at the very end, covering everything.
If the plan already exists without waves, reorganise it into waves without changing the milestones' content (commit), and stop there for this cycle.
EOF
}

parallele_consignes_ouvrier() {  # $1 = identifier, $2 = milestone line
  cat <<EOF

## You are a parallel worker

You carry out **only** this milestone, in this folder (git worktree of the project, branch \`auto/$1\`):

> $2

- Other workers are doing the other milestones of the wave at the same time, each in their own worktree. Touch only the files of your milestone; if you must modify a shared file (common stylesheet, header...), make the minimal change and flag it in your report.
- Do not modify \`.athanor/PLAN.md\` or \`.athanor/LOG.md\`: the project lead handles them after the merge. Do not launch any subagent.
- Check with \`bash .athanor/check.sh quick <pattern>\` (or your milestone's tests if there is no script), then \`git add -A && git commit -m "$1: ..."\` on your branch. Commit even partial work, and say so.
- End with a report of at most 10 lines: done / not done / files touched / what the lead needs to know to merge.
EOF
}

parallele_consignes_fusion() {  # $1 = list of identifiers
  cat <<EOF

## Merge (parallel mode)

Workers have just carried out milestones $1 in parallel, each on its own branch \`auto/<milestone>\`. Their reports are in \`.athanor/WORKER_REPORTS.md\`. For each branch, in plan order:
1. \`git merge --no-ff auto/<milestone>\`; on conflict, resolve it keeping the intent of both sides, then complete the merge.
2. Run \`bash .athanor/check.sh quick <pattern>\`; fix whatever the merge broke.
Then **one** full check \`bash .athanor/check.sh\`. In \`.athanor/PLAN.md\`, tick the milestones that really passed; leave the others unchecked and note in \`.athanor/BUGS.md\` what is missing (they will be redone). One log entry, one commit.
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
  { cat "$CONSIGNES"; parallele_consignes_ouvrier "$ident" "$ligne"; } > "$PRIVE/CLAUDE.ouvrier-$ident.md"
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
  [ -f "$PRIVE/mcp.json" ] && a+=(--mcp-config "$PRIVE/mcp.json")
  [ -n "$template" ] && a+=(--model "$template")
  [ -n "$FALLBACK_MODEL" ] && [ "$template" != "$FALLBACK_MODEL" ] && a+=(--fallback-model "$FALLBACK_MODEL")
  [ -n "$EFFORT" ] && a+=(--effort "$EFFORT")
  : > "$journal"
  setsid bash -c 'echo $$ > "$1"; f="$2"; d="$3"; b="$4"; PATH="$5"; export PROJECT="$d"; shift 5; cd "$d" && "$b" "$@" < /dev/null 2>&1; echo $? > "$f"' \
    _ "$PRIVE/ouvrier_$ident.sid" "$PRIVE/ouvrier_$ident.code" "$trav" "$CLAUDE_BIN" "$CHEMIN_CLAUDE" "${a[@]}" > "$journal" 9>&- &
  echo $! > "$PRIVE/ouvrier_$ident.pid"
}

# One full parallel cycle. Input: $1 = log prefix (without .jsonl), $2 = model.
# Output: fichier_log (the merge session's log) and the merge session's exit code.
parallele_cycle() {
  local prefixe="$1" template="$2" idents=() lignes=() ident ligne i debut maintenant derniere morts sid trav
  while IFS=$'\t' read -r ident ligne; do idents+=("$ident"); lignes+=("$ligne"); done < <(parallele_vague_courante)
  ETAT_OUVRIERS="${idents[*]}"
  log "Cycle $cycle (parallel): ${#idents[@]} workers on ${idents[*]}${template:+ with $template}"
  statut "Wave in parallel: ${idents[*]}"
  mkdir -p "$ETAT/work"
  # uncommitted changes in the main checkout (a lead session cut short) would make the merge fail:
  # they are committed first, the lead reviews them like the workers' branches
  if [ -n "$(g -C "$PROJECT" status --porcelain --untracked-files=no 2>/dev/null)" ]; then
    g -C "$PROJECT" add -u >/dev/null 2>&1 && g -C "$PROJECT" commit -q -m "Work in progress before the wave ${idents[*]} (lead)" >/dev/null 2>&1 && log "Uncommitted changes of the lead committed before the wave."
  fi
  rm -f "$PRIVE"/ouvrier_*.sid "$PRIVE"/ouvrier_*.code "$PRIVE"/ouvrier_*.pid
  local lances=()
  for i in "${!idents[@]}"; do
    parallele_lancer_ouvrier "${idents[$i]}" "${lignes[$i]}" "$prefixe-${idents[$i]}.jsonl" "$template" && lances+=("${idents[$i]}")
  done
  idents=("${lances[@]}"); ETAT_OUVRIERS="${idents[*]}"
  [ "${#idents[@]}" -gt 0 ] || { log "No worker could start: back to the serial mode for this cycle."; return 3; }
  # Watchdog: a worker silent for too long is stopped; the whole wave gets the maximum duration of one cycle.
  debut=$(date +%s)
  while :; do
    sleep 10 9>&- & wait $! 2>/dev/null
    maintenant=$(date +%s); morts=0
    for ident in "${idents[@]}"; do
      if ! kill -0 "$(pid_sur "$(cat "$PRIVE/ouvrier_$ident.pid" 2>/dev/null)")" 2>/dev/null; then morts=$(( morts + 1 )); continue; fi
      derniere=$(entier "$(stat -c %Y "$prefixe-$ident.jsonl" 2>/dev/null)" "$debut")
      if [ $(( maintenant - derniere )) -ge $(( IDLE_MAX_MIN * SECONDS_PER_MINUTE )) ] || [ $(( maintenant - debut )) -ge $(( CYCLE_MAX_MIN * SECONDS_PER_MINUTE )) ]; then
        log "Worker $ident stopped by the watchdog."
        sid=$(pid_sur "$(cat "$PRIVE/ouvrier_$ident.sid" 2>/dev/null)")
        [ -n "$sid" ] && { pkill -TERM -s "$sid" 2>/dev/null; sleep 5; pkill -KILL -s "$sid" 2>/dev/null; }
      fi
    done
    [ "$morts" -ge "${#idents[@]}" ] && break
  done
  for ident in "${idents[@]}"; do
    wait "$(pid_sur "$(cat "$PRIVE/ouvrier_$ident.pid" 2>/dev/null)")" 2>/dev/null
    sid=$(pid_sur "$(cat "$PRIVE/ouvrier_$ident.sid" 2>/dev/null)"); [ -n "$sid" ] && pkill -KILL -s "$sid" 2>/dev/null
  done
  assainir "$ETAT"
  for ident in "${idents[@]}"; do
    # Work left uncommitted (session cut, commit forgotten): recorded on the branch, never lost.
    trav="$ETAT/work/$ident"
    if [ -d "$trav" ] && [ -n "$(g -C "$trav" status --porcelain 2>/dev/null)" ]; then
      g -C "$trav" add -A >/dev/null 2>&1 && g -C "$trav" commit -qm "$ident: uncommitted worker work (recorded by the pilot)" >/dev/null 2>&1 \
        && log "Worker $ident: uncommitted work recorded on auto/$ident."
    fi
    enregistrer_stats "$prefixe-$ident.jsonl" "$([ "$(cat "$PRIVE/ouvrier_$ident.code" 2>/dev/null)" = 0 ] && echo ok || echo erreur)" "$cycle·$ident" "ref:auto/$ident"
  done
  # Reports for the lead, then merge by the lead in the project.
  {
    echo "# Worker reports ($(date '+%Y-%m-%d %H:%M'))"
    for ident in "${idents[@]}"; do
      echo; echo "## $ident (branch auto/$ident, exit code $(entier "$(cat "$PRIVE/ouvrier_$ident.code" 2>/dev/null)" '?'), commits: $(g -C "$PROJECT" rev-list --count HEAD.."auto/$ident" 2>/dev/null || echo 0))"
      jq -rR 'fromjson? | select(type == "object" and .type == "result") | .result // empty' "$prefixe-$ident.jsonl" 2>/dev/null | tail -n 40
    done
  } | publier "$ETAT/WORKER_REPORTS.md"
  for ident in "${idents[@]}"; do g -C "$PROJECT" worktree remove --force "$ETAT/work/$ident" >/dev/null 2>&1 || rm -rf "$ETAT/work/$ident"; done
  g -C "$PROJECT" worktree prune >/dev/null 2>&1
  ETAT_OUVRIERS=""
  log "Workers finished: the project lead merges ${idents[*]}."
  statut "Merging milestones ${idents[*]}"
  fichier_log="$prefixe.jsonl"
  local liste; liste=$(IFS=,; printf '%s' "${idents[*]}")
  local but="/goal The branches auto/{$liste} are merged into the current branch according to your merge instructions, bash .athanor/check.sh has been run and its output is shown, .athanor/PLAN.md ticks the milestones that passed, .athanor/LOG.md has a new entry, git log shows the merge commit. Or stop after ${MAX_TURNS_PER_CYCLE} turns."
  args=(-p "$but" --permission-mode bypassPermissions --settings "$REGLAGES" --permission-prompts none --output-format stream-json --verbose
        "${ARGS_SURS[@]}" --append-system-prompt "$(cat "$CONSIGNES"; parallele_consignes_fusion "${idents[*]}")")
  [ "$AGENTS_PAR_JSON" = 1 ] && args+=(--agents "$AGENTS_JSON")
  [ -f "$PRIVE/mcp.json" ] && args+=(--mcp-config "$PRIVE/mcp.json")
  [ -n "$template" ] && args+=(--model "$template")
  [ -n "$FALLBACK_MODEL" ] && [ "$template" != "$FALLBACK_MODEL" ] && args+=(--fallback-model "$FALLBACK_MODEL")
  [ -n "$EFFORT" ] && args+=(--effort "$EFFORT")
  local clore="$CLOSE_AFTER_MILESTONE"; CLOSE_AFTER_MILESTONE=0   # the merge ticks several milestones: do not close at the first one
  lancer_claude_surveille "$fichier_log"
  local code=$?
  CLOSE_AFTER_MILESTONE="$clore"
  # Merged branches: deleted; the others stay visible (git branch) until the next attempt.
  for ident in "${idents[@]}"; do
    g -C "$PROJECT" branch --merged HEAD 2>/dev/null | grep -qx "  auto/$ident" && g -C "$PROJECT" branch -d "auto/$ident" >/dev/null 2>&1
  done
  return "$code"
}
