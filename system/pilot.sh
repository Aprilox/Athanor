#!/usr/bin/env bash
# =============================================================================
#  Athanor (WSL2 / Linux, no Docker): autonomous work loop
#  Claude runs directly on the machine, inside the system sandbox
#  (Claude Code's sandbox): it only writes to the project folder and tools/.
#
#  Restarts Claude Code, cycle after cycle, until FINAL_REPORT.md contains
#  "STATUS: DONE". Each cycle completes one milestone of .athanor/PLAN.md.
#  Waits and resumes on its own after a quota limit, an error or a PC reboot:
#  all the state lives in the project files.
# =============================================================================
set -u

SYSTEME="$(cd "$(dirname "$0")" && pwd)"
KIT="$(dirname "$SYSTEME")"
# The project = the user's development folder (argument 1, or PROJECT, or KIT/projet).
# Everything used for piloting lives in PROJECT/.athanor (the "workshop"): the project stays clean.
PROJECT="${1:-${PROJECT:-$KIT/projet}}"
PROJECT="$(mkdir -p "$PROJECT" && cd "$PROJECT" && pwd)"   # logical path, stable (see start.sh)
TOOLS="$KIT/tools"
ETAT="$PROJECT/.athanor"
. "$SYSTEME/safety.sh"
migrer_atelier "$PROJECT"
# Pilot runtime state: inside the tool, out of Claude's reach (see safety.sh).
PRIVE="$(prive_de "$PROJECT")"
mkdir -p "$PRIVE"
REGLAGES="$PRIVE/claude_settings.json"
assainir "$ETAT"

# Settings: the tool's .env, read line by line, never executed. (The workshop's .env, editable by Claude,
# only provides the parallel mode, read separately and checked: see parallel.sh.)
if [ -f "$KIT/.env" ]; then
  while IFS= read -r ligne || [ -n "$ligne" ]; do
    ligne=${ligne%$'\r'}
    [[ "$ligne" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] || continue
    [ -z "${!BASH_REMATCH[1]+x}" ] && export "${BASH_REMATCH[1]}=${BASH_REMATCH[2]}"
  done < "$KIT/.env"
fi
# Settings written by an older version (French key names): still honoured. The app renames them on start.
for paire in MODELE=MODEL MODELE_SECOURS=FALLBACK_MODEL MODELE_SOUS_AGENTS=SUBAGENT_MODEL MODELE_TACHES_SIMPLES=CHEAP_MODEL LANGUE=LANGUAGE \
             DUREE_MAX_HEURES=MAX_HOURS PAUSE_QUOTA_MIN=QUOTA_WAIT_MIN INACTIVITE_MAX_MIN=IDLE_MAX_MIN CLORE_APRES_JALON=CLOSE_AFTER_MILESTONE; do
  v="${paire%%=*}"; n="${paire#*=}"
  [ -n "${!v+x}" ] && [ -z "${!n+x}" ] && export "$n=${!v}"
done
export TOOLS PROJECT
# tools/ is editable by Claude: never in the pilot's PATH (it could shadow python3, git... there).
# Claude gets it at the END of its PATH, and the pilot calls it by its absolute path.
export PATH="$SYSTEME/bin:$HOME/.local/bin:$PATH"
CLAUDE_BIN="${CLAUDE_BIN:-$(command -v claude)}"
CHEMIN_CLAUDE="$PATH:$TOOLS/bin"
mkdir -p "$ETAT/logs" "$TOOLS/bin" "$TOOLS/cache"
[ -n "${TZ:-}" ] && export TZ

# Settings (editable in .env)
MAX_HOURS="${MAX_HOURS:-168}"          # safety stop (168 h = 1 week)
QUOTA_WAIT_MIN="${QUOTA_WAIT_MIN:-20}"             # wait when the resume time is unknown
ERROR_WAIT_MIN="${ERROR_WAIT_MIN:-2}"            # 1st wait after an error (doubled afterwards)
ERROR_WAIT_MAX_MIN="${ERROR_WAIT_MAX_MIN:-60}"
LOGIN_WAIT_MIN="${LOGIN_WAIT_MIN:-30}"
MAX_CYCLES_WITHOUT_PROGRESS="${MAX_CYCLES_WITHOUT_PROGRESS:-3}"
MAX_TURNS_PER_CYCLE="${MAX_TURNS_PER_CYCLE:-30}"
MODEL="${MODEL:-}"
FALLBACK_MODEL="${FALLBACK_MODEL:-}"                 # used automatically when MODEL's quota is exhausted
EFFORT="${EFFORT:-}"                                 # low, medium, high, xhigh, max; empty = default setting
SUBAGENT_MODEL="${SUBAGENT_MODEL:-}"         # empty = same model as the project lead
CHEAP_MODEL="${CHEAP_MODEL-haiku}"  # "executor" sub-agent (mechanical tasks); empty = like the other sub-agents
IDLE_MAX_MIN="${IDLE_MAX_MIN:-30}"       # cycle stopped if its log stays silent that long
CYCLE_MAX_MIN="${CYCLE_MAX_MIN:-180}"                # cycle stopped if it runs longer than this
CLOSE_AFTER_MILESTONE="${CLOSE_AFTER_MILESTONE:-1}"          # 1 = cycle closed shortly after a milestone commit (fresh context for the next one)
WATCH_INTERVAL="${WATCH_INTERVAL:-30}"  # seconds between two checks
STAY_ALIVE_AT_END="${STAY_ALIVE_AT_END:-0}"  # 1 = the pilot stays on standby at the end
SECONDS_PER_MINUTE="${SECONDS_PER_MINUTE:-60}"      # only change to test the script
# A non-numeric setting falls back to its default (and says so), never evaluated as is.
for paire in MAX_HOURS=168 QUOTA_WAIT_MIN=20 ERROR_WAIT_MIN=2 ERROR_WAIT_MAX_MIN=60 LOGIN_WAIT_MIN=30 MAX_CYCLES_WITHOUT_PROGRESS=3 \
             MAX_TURNS_PER_CYCLE=30 IDLE_MAX_MIN=30 CYCLE_MAX_MIN=180 WATCH_INTERVAL=30 SECONDS_PER_MINUTE=60; do
  v="${paire%%=*}"; defaut="${paire#*=}"
  [[ "${!v}" =~ ^[0-9]{1,12}$ ]] || { echo "Invalid setting $v (\"${!v}\"): using default $defaut." >&2; printf -v "$v" '%s' "$defaut"; }
done

# With nobody at the screen, sub-agents and commands run in the foreground:
# Claude waits for their result directly instead of "waiting" in the background.
export CLAUDE_CODE_DISABLE_BACKGROUND_TASKS="${CLAUDE_CODE_DISABLE_BACKGROUND_TASKS:-1}"
# Overly long command output is truncated (30,000 characters by default): we cap it lower.
export BASH_MAX_OUTPUT_LENGTH="${BASH_MAX_OUTPUT_LENGTH:-20000}"
[ -n "$EFFORT" ] && export CLAUDE_CODE_EFFORT_LEVEL="$EFFORT"
CONFIG_CLAUDE="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"

# The pilot's output may go to a terminal or a pipe that disappears (window closed, application
# quit): writing to it must never kill the pilot (SIGPIPE ignored, the write simply fails).
trap '' PIPE

# ----------------------------------------------------------------------------- tools
log() {
  local m="[$(date '+%Y-%m-%d %H:%M:%S')] $*"
  echo "$m"
  echo "$m" >> "$PRIVE/pilot.log"
  tail -n 3000 "$PRIVE/pilot.log" | publier "$ETAT/pilot.log"
}

statut() {  # small file readable from the PC: .athanor/STATUS.txt (+ STATE.json for the application)
  {
    echo "Updated : $(date '+%Y-%m-%d %H:%M:%S')"
    echo "Cycle   : ${cycle:-0}"
    echo "State   : $*"
  } | publier "$ETAT/STATUS.txt"
  ETAT_PHASE="${ETAT_PHASE:-attente}"
  compter_file_attente
  jq -n --arg t "$(date '+%Y-%m-%dT%H:%M:%S')" --arg etat "$*" --arg phase "$ETAT_PHASE" --arg template "${modele_cycle:-$MODEL}" \
     --arg journal "${fichier_log:-}" --arg jalon "$(grep -m1 -E '^- \[ \] ' "$ETAT/PLAN.md" 2>/dev/null | cut -c7-200)" \
     --arg commit "$(g -C "$PROJECT" log -1 --pretty='%h %s' 2>/dev/null | cut -c1-160)" \
     --arg reprise "${ETAT_REPRISE:-}" --arg debut_cycle "${ETAT_DEBUT_CYCLE:-}" --arg ouvriers "${ETAT_OUVRIERS:-}" --argjson cycle "${cycle:-0}" --argjson pid "$$" \
     '{maj:$t, pid:$pid, phase:$phase, etat:$etat, cycle:$cycle, template:$template, journal:$journal, jalon_en_cours:$jalon, dernier_commit:$commit, reprise_prevue:$reprise, debut_cycle:$debut_cycle, ouvriers:$ouvriers,
       demandes_en_attente:(env.FILE_ATTENTE_N // "0" | tonumber)}' 2>/dev/null | publier "$ETAT/STATE.json"
}

compter_file_attente() {  # requests dropped in queue/ (.md files), read in name order
  FILE_ATTENTE_N=$(ls "$ETAT/queue"/*.md 2>/dev/null | wc -l); export FILE_ATTENTE_N
}

prochaine_demande() {  # the current request is done: move on to the first one in the queue
  local f; f=$(ls "$ETAT/queue"/*.md 2>/dev/null | sort | head -n 1)
  [ -n "$f" ] || return 1
  local nom; nom=$(basename "$f" .md)
  # First remove the request from the queue (otherwise it would be picked up again and again), then append it to REQUEST.md.
  local faite; faite="$ETAT/done/$(date '+%Y%m%d-%H%M%S')-$nom.md"
  mkdir -p "$ETAT/done" && mv -f "$f" "$faite" || { log "Queue: cannot remove \"$nom\" ($f)."; return 1; }
  {
    printf '\n\n## Next request (%s, %s)\n\n' "$(date '+%Y-%m-%d %H:%M')" "$nom"
    tr -d '\r' < "$faite"
    printf '\n'
  } >> "$ETAT/REQUEST.md"
  if [ -f "$ETAT/FINAL_REPORT.md" ]; then
    # same pattern as est_termine: "**STATUS: DONE**" is reopened too
    sed -i -E 's/STATUS: *DONE[^*]*/STATUS: REOPENED (next request in REQUEST.md)/' "$ETAT/FINAL_REPORT.md"
  fi
  if [ -f "$ETAT/PLAN.md" ]; then
    printf '\n- [ ] Resume: new request "%s" ("Next request" section of .athanor/REQUEST.md)\n  - Re-read this request, turn it into verifiable milestones appended to this plan, the last one being a new delivery (.athanor/FINAL_REPORT.md with STATUS: DONE).\n' "$nom" >> "$ETAT/PLAN.md"
  fi
  synchroniser_demande
  log "Request done: moving on to \"$nom\" (queue: $(( $(ls "$ETAT/queue"/*.md 2>/dev/null | wc -l) )) remaining)."
  return 0
}

enregistrer_stats() {  # one line per cycle in .athanor/stats.tsv (read by the application); $3 = cycle label, $4 = git folder
  local f="$1" etat="$2" etiquette="${3:-$cycle}" depot="${4:-$PROJECT}" res
  [ -s "$PRIVE/stats.tsv" ] || printf 'horodatage\tcycle\tmodele\tetat\tminutes\tminutes_api\ttours\tcout_usd\tentree_tokens\tcache_lu\tcache_ecrit\tsortie_tokens\tcontexte_max\tcommit\n' > "$PRIVE/stats.tsv"
  res=$(jq -cR 'fromjson? | select(type=="object" and .type=="result")' "$f" 2>/dev/null | tail -n 1)
  if [ -z "$res" ]; then
    # Cycle closed by the pilot (no "result" line): add up the project lead's turns.
    local debut_s; debut_s=$(date -d "${ETAT_DEBUT_CYCLE:-now}" +%s 2>/dev/null || date +%s)
    res=$(jq -sR --argjson d "$(( $(date +%s) - debut_s ))" '[split("\n")[] | fromjson? | select(type=="object" and .type=="assistant" and .parent_tool_use_id==null) | .message.usage]
      | {duration_ms:($d*1000), num_turns:length, usage:{input_tokens:(map(.input_tokens//0)|add//0), cache_read_input_tokens:(map(.cache_read_input_tokens//0)|add//0),
         cache_creation_input_tokens:(map(.cache_creation_input_tokens//0)|add//0), output_tokens:(map(.output_tokens//0)|add//0)}}' "$f" 2>/dev/null)
  fi
  local pic; pic=$(jq -rR 'fromjson? | select(type=="object" and .type=="assistant" and .parent_tool_use_id==null) | .message.usage | ((.input_tokens//0)+(.cache_read_input_tokens//0)+(.cache_creation_input_tokens//0))' "$f" 2>/dev/null | sort -n | tail -n 1)
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$(date '+%Y-%m-%dT%H:%M:%S')" "$etiquette" "${modele_cycle:-defaut}" "$etat" \
    "$(printf '%s' "$res" | jq -r '((.duration_ms//0)/60000*10|round/10)' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '((.duration_api_ms//0)/60000*10|round/10)' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.num_turns//0' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '((.total_cost_usd//0)*100|round/100)' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.usage.input_tokens//0' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.usage.cache_read_input_tokens//0' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.usage.cache_creation_input_tokens//0' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.usage.output_tokens//0' 2>/dev/null)" \
    "${pic:-0}" "$(case "$depot" in ref:*) g -C "$PROJECT" log -1 --pretty=%h "${depot#ref:}" 2>/dev/null ;; *) g -C "$depot" log -1 --pretty=%h 2>/dev/null ;; esac)" >> "$PRIVE/stats.tsv"
  publier "$ETAT/stats.tsv" < "$PRIVE/stats.tsv"
}

dormir() {  # $1 = real seconds
  local s=$(( $1 * SECONDS_PER_MINUTE / 60 ))
  [ "$s" -gt 0 ] && { sleep "$s" 9>&- & wait $! 2>/dev/null; }
  return 0
}

est_termine() {
  [ -f "$ETAT/FINAL_REPORT.md" ] && grep -Eq "STATUS: *DONE" "$ETAT/FINAL_REPORT.md"
}

terminer() {
  log "$1"
  ETAT_PHASE="${ETAT_PHASE:-termine}"
  statut "$1"
  if [ "$STAY_ALIVE_AT_END" = "1" ]; then
    log "The pilot stays on standby. To stop it: stop"
    while [ ! -f "$ETAT/STOP" ]; do sleep 60 9>&- & wait $! 2>/dev/null; done
    log "Stop requested (STOP file)."
  fi
  exit 0
}

connexion_presente() {
  [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ] || [ -n "${ANTHROPIC_API_KEY:-}" ] || [ -f "$CONFIG_CLAUDE/.credentials.json" ]
}

synchroniser_demande() {  # flags a change of .athanor/REQUEST.md (Claude re-reads it and adapts the plan)
  [ -f "$ETAT/REQUEST.md" ] || return 0
  local nouveau ancien
  nouveau=$(sha256sum "$ETAT/REQUEST.md" | cut -d' ' -f1)
  ancien=$(cat "$PRIVE/demande.sha" 2>/dev/null || true)
  if [ "$nouveau" != "$ancien" ]; then
    echo "$nouveau" > "$PRIVE/demande.sha"
    if [ -n "$ancien" ]; then
      date '+%Y-%m-%d %H:%M' > "$ETAT/REQUEST_CHANGED"
      log "REQUEST.md has changed: Claude will take it into account on the next cycle."
    fi
  fi
}

afficher_en_direct() {  # turns Claude's JSON stream into readable lines
  jq --unbuffered -rR '
    def court(n): tostring | gsub("\\s+"; " ") | if length > n then .[0:n] + "…" else . end;
    (fromjson? // empty) | select(type == "object")
    | (if .parent_tool_use_id then "      ↳ " else "  " end) as $p
    | if .type == "assistant" then
        .message.content[]? |
        if .type == "text" and (.text | length) > 0 then $p + "💬 " + (.text | court(300))
        elif .type == "tool_use" then
          $p + (
            if .name == "Bash" then "▶ " + (.input.command | court(200))
            elif .name == "Edit" or .name == "Write" or .name == "MultiEdit" then "✎ " + .name + " " + (.input.file_path // "" | tostring)
            elif .name == "Read" then "👁 Read " + (.input.file_path // "" | tostring)
            elif .name == "Agent" or .name == "Task" then "👥 Sub-agent " + (.input.subagent_type // "?" | tostring) + ": " + (.input.description // "" | court(120))
            else "• " + .name + " " + (.input | tostring | court(120))
            end)
        else empty end
      elif .type == "result" then "  ■ End of cycle: " + (.result // .subtype // "" | court(300))
      else empty end
  ' 2>/dev/null || cat > /dev/null
}

coches_plan() {  # number of ticked milestones in the workshop (.athanor/PLAN.md, outside git)
  grep -c '^- \[[xX]\]' "$ETAT/PLAN.md" 2>/dev/null || true
}

lancer_claude_surveille() {  # $1 = log; stops the cycle if it stays silent or runs too long
  local journal="$1" f_sid="$PRIVE/claude_sid" f_code="$PRIVE/claude_code"
  local pid_flux sid debut maintenant derniere motif="" tete coches_debut jalon_vu=0 h c fin_vu=0
  rm -f "$f_sid" "$f_code" "$PRIVE/cycle_arrete" "$PRIVE/cycle_clos"
  tete=$(g -C "$PROJECT" rev-parse HEAD 2>/dev/null || echo aucun)
  coches_debut=$(coches_plan); coches_debut=${coches_debut:-0}
  : > "$journal"
  # setsid: Claude and everything it launches (engine, browser...) form one group that can be stopped at once.
  # (9>&-: the pilot's lock must not be inherited by Claude nor by the display pipeline)
  { setsid bash -c 'echo $$ > "$1"; f="$2"; b="$3"; PATH="$4"; shift 4; "$b" "$@" < /dev/null 2>&1; echo $? > "$f"' \
    _ "$f_sid" "$f_code" "$BASH" "$CHEMIN_CLAUDE" "$SYSTEME/ecran.sh" lancer "$CLAUDE_BIN" "${args[@]}" | tee "$journal" | afficher_en_direct; } 9>&- &
  pid_flux=$!
  debut=$(date +%s)
  while kill -0 "$pid_flux" 2>/dev/null; do
    sleep "$WATCH_INTERVAL" 9>&- & wait $! 2>/dev/null   # interruptible: immediate stop
    maintenant=$(date +%s)
    derniere=$(entier "$(stat -c %Y "$journal" 2>/dev/null)" "$debut")
    # Claude has finished but a program it launched keeps the output open: we do not wait for the watchdog.
    if [ -s "$f_code" ]; then [ "$fin_vu" = 0 ] && fin_vu=$maintenant; fi
    if [ "$fin_vu" != 0 ] && [ $(( maintenant - fin_vu )) -ge 30 ]; then
      sid=$(pid_sur "$(cat "$f_sid" 2>/dev/null)"); [ -n "$sid" ] && pkill -KILL -s "$sid" 2>/dev/null
      break
    elif [ $(( maintenant - derniere )) -ge $(( IDLE_MAX_MIN * SECONDS_PER_MINUTE )) ]; then
      motif="no activity for $IDLE_MAX_MIN min"
    elif [ $(( maintenant - debut )) -ge $(( CYCLE_MAX_MIN * SECONDS_PER_MINUTE )) ]; then
      motif="cycle longer than $CYCLE_MAX_MIN min"
    elif [ "$CLOSE_AFTER_MILESTONE" = "1" ]; then
      # One milestone per cycle: as soon as a commit ticks a new milestone, we allow 2 min to finish
      # (log, answer), then close the cycle (10 min at most if files remain modified).
      # The next one starts with a fresh context, hence much cheaper turns.
      if [ "$jalon_vu" = 0 ]; then
        h=$(g -C "$PROJECT" rev-parse HEAD 2>/dev/null || echo aucun)
        c=$(coches_plan)
        if [ "$h" != "$tete" ] && [ "${c:-0}" -gt "$coches_debut" ]; then
          jalon_vu=$maintenant
        fi
      fi
      if [ "$jalon_vu" != 0 ]; then
        if [ $(( maintenant - jalon_vu )) -ge 600 ] \
           || { [ $(( maintenant - jalon_vu )) -ge 120 ] && timeout 20 bash -c '. "$1"; shift; g "$@"' _ "$SYSTEME/safety.sh" -C "$PROJECT" diff --quiet --no-ext-diff --no-textconv HEAD 2>/dev/null; }; then
          motif="milestone delivered and committed"; : > "$PRIVE/cycle_clos"
        fi
      fi
    fi
    if [ -n "$motif" ]; then
      echo "$motif" > "$PRIVE/cycle_arrete"
      sid=$(pid_sur "$(cat "$f_sid" 2>/dev/null)")
      [ -n "$sid" ] && pkill -TERM -s "$sid" 2>/dev/null
      sleep 10
      [ -n "$sid" ] && pkill -KILL -s "$sid" 2>/dev/null
      break
    fi
  done
  wait "$pid_flux" 2>/dev/null
  # Cleanup: no program launched during this cycle may keep running.
  sid=$(pid_sur "$(cat "$f_sid" 2>/dev/null)")
  [ -n "$sid" ] && pkill -KILL -s "$sid" 2>/dev/null
  assainir "$ETAT"
  if [ -s "$f_code" ]; then return "$(entier "$(cat "$f_code")" 1)"; fi
  return 124
}

preparer_cycle() {  # non-AI tools: archives and project map, before Claude reads anything
  command -v athanor >/dev/null 2>&1 || return 0
  local sortie
  # Archive only if PLAN.md and LOG.md have no change in progress.
  sortie=$(PROJECT="$PROJECT" athanor archive 2>&1) && printf '%s' "$sortie" | grep -q " archived" && log "Archiving: $(printf '%s' "$sortie" | tr '\n' ' ')"
  PROJECT="$PROJECT" athanor map > /dev/null 2>&1 || true
  # Windows firewall kept up to date for Unreal and the project's programs (administrator task, no window),
  # otherwise Windows pops up "Allow access" as soon as a program waits for players.
  (cd /mnt/c 2>/dev/null && schtasks.exe /run /tn "Athanor - installations" >/dev/null 2>&1) || true
  # Unreal: the editor must be open so that Claude has its MCP tools during the cycle.
  if [ "${UNREAL_MCP:-0}" = "1" ]; then
    local uproject; uproject=$(find "$PROJECT" -maxdepth 2 -name '*.uproject' -print -quit 2>/dev/null)
    if [ -n "$uproject" ] && ! curl -s -o /dev/null --max-time 3 "${UNREAL_MCP_URL:-http://127.0.0.1:8000/mcp}"; then
      log "Opening the Unreal editor ($(basename "$uproject")) for the MCP server…"
      PROJECT="$PROJECT" unreal-editor --wait-mcp "$uproject" >> "$PRIVE/pilot.log" 2>&1 || log "Unreal MCP unavailable for this cycle."
    fi
  fi
}

attente_quota() {  # reads "resets 3:45pm" or "resets Mon 12:00am" when possible
  local texte="$1" quand cible maintenant
  maintenant=$(date +%s)
  quand=$(printf '%s' "$texte" | grep -Eio 'resets? (at )?[A-Za-z]{0,3} ?[0-9]{1,2}(:[0-9]{2})? ?(am|pm)?' | head -n1 | sed -E 's/^resets? (at )?//I')
  if [ -n "$quand" ] && cible=$(date -d "$quand" +%s 2>/dev/null); then
    if [ "$cible" -le "$maintenant" ]; then
      if [ $(( maintenant - cible )) -lt 21600 ]; then
        echo 120   # the resume time has just passed: retry in 2 min
        return
      elif printf '%s' "$quand" | grep -Eqi '^[a-z]{3} '; then
        cible=$(( cible + 7 * 86400 ))   # "Mon 12:00am": next week
      else
        cible=$(( cible + 86400 ))       # "3:45pm": tomorrow
      fi
    fi
    local attente=$(( cible - maintenant + 120 ))
    # Never sleep more than 6 h at once: at worst, one more attempt.
    [ "$attente" -gt 21600 ] && attente=21600
    [ "$attente" -lt 60 ] && attente=60
    echo "$attente"
    return
  fi
  echo $(( QUOTA_WAIT_MIN * 60 ))
}

# Parallel mode (simultaneous workers on a wave of milestones, then merge by the lead): separate functions.
. "$SYSTEME/parallel.sh"
parallele_nettoyer_worktrees   # leftovers of a wave interrupted by a stop or a crash

# ----------------------------------------------------------------------------- startup
cd "$PROJECT" || { echo "Folder $PROJECT not accessible"; exit 1; }
mkdir -p "$ETAT/logs" "$ETAT/queue" "$ETAT/done"

# One pilot per project (double click on Start, automatic startup...).
exec 9>"$PRIVE/verrou"
flock -n 9 || { echo "The pilot is already running on this project."; exit 0; }
echo $$ > "$PRIVE/pilot.pid"
echo $$ | publier "$ETAT/pilot.pid"
# Heartbeat: the pid is republished every 20 s in the workshop; the application considers the pilot
# dead if the file is older than 90 s (WSL or the PC shut down before the pilot could exit).
( while kill -0 $$ 2>/dev/null && [ -f "$PRIVE/pilot.pid" ]; do echo $$ | publier "$ETAT/pilot.pid"; sleep 20; done ) 9>&- &
BATTEMENT_PID=$!
arreter_tout() {  # Ctrl+C, window closed or stop.sh: Claude is stopped too
  local sid f; sid=$(pid_sur "$(cat "$PRIVE/claude_sid" 2>/dev/null)")
  [ -n "$sid" ] && pkill -TERM -s "$sid" 2>/dev/null
  for f in "$PRIVE"/ouvrier_*.sid; do [ -f "$f" ] && sid=$(pid_sur "$(cat "$f")") && [ -n "$sid" ] && pkill -KILL -s "$sid" 2>/dev/null; done
  parallele_nettoyer_worktrees
  [ -n "${PONT_PID:-}" ] && kill "$PONT_PID" 2>/dev/null
  [ -n "${BATTEMENT_PID:-}" ] && kill "$BATTEMENT_PID" 2>/dev/null
  log "Pilot stopped."; ETAT_PHASE=arrete; statut "Stopped (restart with start)"; rm -f "$PRIVE/pilot.pid" "$ETAT/pilot.pid"; exit 0
}
trap arreter_tout TERM INT HUP
# Any end of the pilot (STOP, DONE, error) also stops the bridge.
trap '[ -n "${PONT_PID:-}" ] && kill "$PONT_PID" 2>/dev/null; [ -n "${BATTEMENT_PID:-}" ] && kill "$BATTEMENT_PID" 2>/dev/null; [ -n "${ECRAN_PID:-}" ] && kill "$ECRAN_PID" 2>/dev/null; rm -f "$PRIVE/pilot.pid" "$ETAT/pilot.pid" "$PRIVE/ecran"' EXIT

[ -n "$CLAUDE_BIN" ] && [ -x "$CLAUDE_BIN" ] || { log "Claude Code not found: run install.sh first."; exit 1; }
# History from a previous pilot (before the private folder): taken over once, without following links.
for f in pilot.log stats.tsv; do [ -f "$PRIVE/$f" ] || { [ -f "$ETAT/$f" ] && [ ! -L "$ETAT/$f" ] && cp "$ETAT/$f" "$PRIVE/$f"; }; done

log "=== Athanor starting ==="

# Until the request is written, we wait.
demande_remplie() {  # .athanor/REQUEST.md contains something other than headings and parenthesised lines
  local f="$ETAT/REQUEST.md"
  [ -f "$f" ] || return 1
  grep -v -E '^\s*(#|\(|$)' "$f" | grep -q '[[:alnum:]]'
}
deja_signale=0
until demande_remplie; do
  if [ "$deja_signale" = "0" ]; then
    log "No request: write it in the application (or in .athanor/REQUEST.md), Claude will start on its own."
    statut "Waiting for your request in REQUEST.md"
    deja_signale=1
  fi
  dormir 60
done

# Instructions and team: always the kit's version, copied into the workshop (never into the project itself).
CONSIGNES="$PRIVE/CLAUDE.md"
cp -f "$KIT/template/CLAUDE.md" "$CONSIGNES"
# The language also goes at the end of each cycle's goal: the goal is written in English, and Claude
# answers in the language of the message it receives unless told otherwise.
case "${LANGUAGE:-en}" in
  fr) PHRASE_LANGUE="Écris tous tes messages en français (ils s'affichent dans le journal de l'application), ainsi que les documents, les fichiers .athanor et les consignes des sous-agents." ;;
  es) PHRASE_LANGUE="Escribe todos tus mensajes en español (se muestran en el registro de la aplicación), así como los documentos, los archivos .athanor y las instrucciones de los subagentes." ;;
  de) PHRASE_LANGUE="Schreibe alle deine Nachrichten auf Deutsch (sie erscheinen im Protokoll der Anwendung), ebenso die Dokumente, die .athanor-Dateien und die Anweisungen an die Sub-Agenten." ;;
  it) PHRASE_LANGUE="Scrivi tutti i tuoi messaggi in italiano (compaiono nel registro dell'applicazione), così come i documenti, i file .athanor e le istruzioni dei sub-agenti." ;;
  pt) PHRASE_LANGUE="Escreve todas as tuas mensagens em português (aparecem no registo da aplicação), assim como os documentos, os ficheiros .athanor e as instruções dos subagentes." ;;
  *)  PHRASE_LANGUE="Write all your messages in English (they show in the application's log), as well as the documents, the .athanor files and the sub-agents' briefs." ;;
esac
case "${LANGUAGE:-en}" in
  fr) printf '\n## Langue\n\n' ;; es|pt) printf '\n## Idioma\n\n' ;; de) printf '\n## Sprache\n\n' ;; it) printf '\n## Lingua\n\n' ;; *) printf '\n## Language\n\n' ;;
esac >> "$CONSIGNES"
printf '%s\n' "$PHRASE_LANGUE" >> "$CONSIGNES"
[ -f "$KIT/template/PC_TOOLS_GUIDE.md" ] && cp -f "$KIT/template/PC_TOOLS_GUIDE.md" "$ETAT/PC_TOOLS_GUIDE.md"
rm -rf "$ETAT/agents" "$ETAT/CLAUDE.md" "$ETAT/CLAUDE.cycle.md" "$ETAT/mcp.json" "$ETAT/claude_settings.json"   # older versions
rm -rf "$PRIVE/agents" && mkdir -p "$PRIVE/agents" && cp "$KIT/template/.claude/agents/"*.md "$PRIVE/agents/" 2>/dev/null
# Sub-agents are passed to Claude Code as JSON (--agents); failing that, copied into PROJECT/.claude/agents.
# Model of each sub-agent: CHEAP_MODEL for "executor", SUBAGENT_MODEL for the others
# (empty = the lead's), unless a model is set in the agent file. Also written into the .md copies.
AGENTS_JSON=$(SUBAGENT_MODEL="$SUBAGENT_MODEL" CHEAP_MODEL="$CHEAP_MODEL" python3 - "$PRIVE/agents" <<'PY'
import json, os, re, sys
d = sys.argv[1]; out = {}
sous, simple = os.environ.get("SUBAGENT_MODEL", ""), os.environ.get("CHEAP_MODEL", "")
for f in sorted(os.listdir(d)):
    if not f.endswith(".md"): continue
    t = open(os.path.join(d, f), encoding="utf-8").read()
    m = re.match(r"^---\n(.*?)\n---\n(.*)$", t, re.S)
    if not m: continue
    meta = dict((l.split(":", 1)[0].strip(), l.split(":", 1)[1].strip()) for l in m.group(1).splitlines() if ":" in l)
    a = {"description": meta.get("description", ""), "prompt": m.group(2).strip()}
    if meta.get("tools"): a["tools"] = [x.strip() for x in meta["tools"].split(",")]
    nom = meta.get("name", f[:-3])
    template = meta.get("model") or (simple if nom == "executor" else "") or sous
    if template:
        a["model"] = template
        if not meta.get("model"):  # .md copy (if --agents does not exist): same model
            entete = m.group(1) + "\nmodel: " + template
            open(os.path.join(d, f), "w", encoding="utf-8").write("---\n" + entete + "\n---\n" + m.group(2))
    out[nom] = a
print(json.dumps(out, ensure_ascii=False))
PY
)
AIDE_CLAUDE=$("$CLAUDE_BIN" --help 2>/dev/null)
# Settings and MCP servers: only the pilot's, never those a project file might declare.
ARGS_SURS=()
printf '%s' "$AIDE_CLAUDE" | grep -q -- '--strict-mcp-config' && ARGS_SURS+=(--strict-mcp-config)
printf '%s' "$AIDE_CLAUDE" | grep -q -- '--setting-sources' && ARGS_SURS+=(--setting-sources user)
if printf '%s' "$AIDE_CLAUDE" | grep -q -- '--agents'; then
  AGENTS_PAR_JSON=1
else
  AGENTS_PAR_JSON=0
  mkdir -p "$PROJECT/.claude/agents" && cp -f "$PRIVE/agents/"*.md "$PROJECT/.claude/agents/" 2>/dev/null
fi
synchroniser_demande

# PC tools, sandbox settings, Unreal MCP
"$SYSTEME/detect-tools.sh"
# The skills of the Skills & MCP page are copied there (read-only for Claude: settings.py and the guard).
export ATHANOR_PLUGIN="$PRIVE/plugin"
python3 "$SYSTEME/settings.py" "$KIT" "$REGLAGES" > /dev/null || { log "Cannot write Claude's settings."; exit 1; }
if [ "${UNREAL_MCP:-0}" = "1" ] && [ -f "$SYSTEME/unreal_relay.txt" ]; then
  # "Mirrored" networking unavailable: go through the Windows relay (unreal-network.cmd), seen from WSL
  # at the gateway address (it changes on every WSL start).
  passerelle=$(ip route show default 2>/dev/null | awk '{print $3; exit}')
  [ -n "$passerelle" ] && export UNREAL_MCP_URL="http://$passerelle:$(tr -dc '0-9' < "$SYSTEME/unreal_relay.txt")/mcp"
fi
export UNREAL_MCP_URL="${UNREAL_MCP_URL:-http://127.0.0.1:8000/mcp}"
[ "${UNREAL_MCP:-0}" = "1" ] && log "Unreal MCP: $UNREAL_MCP_URL"
export UNREAL_MCP

# Skills and MCP servers chosen in the app (Skills & MCP page), plus Unreal MCP: rebuilt at the start of
# every cycle, so a change made in the app applies from the next cycle. Local MCP servers run in a cage
# (system/mcp-jail.sh); the skills form a plugin loaded with --plugin-dir.
PLUGIN_DIR_OK=0
printf '%s' "$AIDE_CLAUDE" | grep -q -- '--plugin-dir' && PLUGIN_DIR_OK=1
EXTENSIONS_AVANT=""
preparer_extensions() {
  local r; r=$(python3 "$SYSTEME/extensions.py" preparer "$KIT" "$PROJECT" "$PRIVE" 2>> "$PRIVE/pilot.log")
  PLUGIN_ATHANOR=""
  if [ -f "$PRIVE/plugin/.claude-plugin/plugin.json" ]; then
    if [ "$PLUGIN_DIR_OK" = 1 ]; then PLUGIN_ATHANOR="$PRIVE/plugin"
    else r="$r (skills ignored: this Claude Code version has no --plugin-dir, update it with repair)"; fi
  fi
  [ -n "$r" ] && [ "$r" != "$EXTENSIONS_AVANT" ] && log "Skills & MCP: $r"
  EXTENSIONS_AVANT="$r"
}
preparer_extensions

# Claude's screen (Linux side, see system/ecran.sh): with the "Claude's desktop" option, a private X server
# per pilot. Claude, its tools and its MCP servers see only that server: the Linux windows they open
# (a game under test, Godot, a headed browser) never reach the user's screen nor take the focus.
ECRAN_PID=""; ECRAN_SIGNALE=""
ECRAN_VARIABLES=(DISPLAY WAYLAND_DISPLAY XAUTHORITY DBUS_SESSION_BUS_ADDRESS XDG_SESSION_TYPE GDK_BACKEND QT_QPA_PLATFORM SDL_VIDEODRIVER ELECTRON_OZONE_PLATFORM_HINT)
declare -A ECRAN_AVANT=()
for v in "${ECRAN_VARIABLES[@]}"; do [ -n "${!v+x}" ] && ECRAN_AVANT[$v]="${!v}"; done
arreter_ecran() {
  [ -n "$ECRAN_PID" ] && kill "$ECRAN_PID" 2>/dev/null
  ECRAN_PID=""
  rm -f "$PRIVE/ecran"
  if [ -n "${ATHANOR_X11_DIR:-}" ]; then   # back to the user's display
    unset ATHANOR_X11_DIR
    local v
    for v in "${ECRAN_VARIABLES[@]}"; do
      if [ -n "${ECRAN_AVANT[$v]+x}" ]; then export "$v=${ECRAN_AVANT[$v]}"; else unset "$v"; fi
    done
  fi
}
preparer_ecran() {
  if [ ! -f "$KIT/logs/bureau-isole" ] || [ -n "${ECRAN_IMPOSSIBLE:-}" ]; then arreter_ecran; return; fi
  if ! command -v Xvfb > /dev/null 2>&1 || ! command -v bwrap > /dev/null 2>&1; then
    [ -z "$ECRAN_SIGNALE" ] && log "Claude's screen: Xvfb is missing (Setup & health -> Install everything): Linux windows open on your screen."
    ECRAN_SIGNALE=1; arreter_ecran; return
  fi
  local n="" dir="" i
  if [ -z "$ECRAN_PID" ] || ! kill -0 "$ECRAN_PID" 2>/dev/null; then
    arreter_ecran
    # The socket folder is private (mounted over /tmp/.X11-unix for Claude only) and there is no lock file
    # (-nolock): any number works; one no other X server uses avoids any confusion.
    dir="${TMPDIR:-/tmp}/athanor-ecran-$(id -u)-$(basename "$PRIVE")"
    rm -rf "$dir" && mkdir -m 700 "$dir" || { log "Claude's screen: cannot create $dir."; return; }
    for n in $(seq 90 189); do [ -e "/tmp/.X$n-lock" ] || break; done
    bwrap --dev-bind / / --bind "$dir" /tmp/.X11-unix --die-with-parent -- \
      Xvfb ":$n" -screen 0 1920x1080x24 -nolisten tcp -nolisten local -nolock >> "$PRIVE/ecran.log" 2>&1 9>&- &
    ECRAN_PID=$!
    for i in $(seq 1 50); do [ -S "$dir/X$n" ] && break; sleep 0.1; done
    if [ ! -S "$dir/X$n" ]; then
      log "Claude's screen: Xvfb did not start (see $PRIVE/ecran.log): Linux windows open on your screen."
      kill "$ECRAN_PID" 2>/dev/null; ECRAN_PID=""; ECRAN_IMPOSSIBLE=1; return
    fi
    # Claude's own sandbox (bubblewrap) must still work inside: otherwise no Claude screen, Claude first.
    if ! ATHANOR_X11_DIR="$dir" "$BASH" "$SYSTEME/ecran.sh" lancer bwrap --ro-bind / / --dev /dev --proc /proc --unshare-net -- true 2>> "$PRIVE/ecran.log"; then
      log "Claude's screen: the system sandbox cannot run inside it on this system: Linux windows open on your screen."
      kill "$ECRAN_PID" 2>/dev/null; ECRAN_PID=""; ECRAN_IMPOSSIBLE=1; return
    fi
    printf '%s %s\n' "$n" "$dir" > "$PRIVE/ecran"
    log "Claude's screen ready: Linux windows open there (:$n), never on your screen."
  fi
  read -r n dir < "$PRIVE/ecran"
  export ATHANOR_X11_DIR="$dir" DISPLAY=":$n" XDG_SESSION_TYPE=x11 GDK_BACKEND=x11 QT_QPA_PLATFORM=xcb \
         SDL_VIDEODRIVER=x11 ELECTRON_OZONE_PLATFORM_HINT=x11
  unset WAYLAND_DISPLAY XAUTHORITY DBUS_SESSION_BUS_ADDRESS   # the user's session stays out of reach
}
preparer_ecran

# Bridge to the PC tools (outside the sandbox): the gateways Claude runs go through it.
python3 "$SYSTEME/bridge.py" "$KIT" "$PROJECT" >> "$PRIVE/pont.log" 2>&1 9>&- &
PONT_PID=$!

if [ ! -d "$PROJECT/.git" ]; then
  g -C "$PROJECT" init -q -b main
fi
# The workshop must never enter the project's history.
grep -qx '.athanor/' "$PROJECT/.git/info/exclude" 2>/dev/null || { mkdir -p "$PROJECT/.git/info"; echo '.athanor/' >> "$PROJECT/.git/info/exclude"; }
grep -qx '.claude/' "$PROJECT/.git/info/exclude" 2>/dev/null || echo '.claude/' >> "$PROJECT/.git/info/exclude"
# Empty files the system sandbox creates at the project root (protection mount points):
# they make "git add -A" fail; excluded from git without touching the project's .gitignore.
for motif in /.bashrc /.bash_profile /.profile /.zshrc /.zprofile /.gitconfig /.gitmodules /.ripgreprc /.idea /.vscode /.mcp.json; do
  grep -qxF "$motif" "$PROJECT/.git/info/exclude" 2>/dev/null || echo "$motif" >> "$PROJECT/.git/info/exclude"
done
g -C "$PROJECT" config user.name  >/dev/null 2>&1 || g -C "$PROJECT" config user.name  "Athanor"
g -C "$PROJECT" config user.email >/dev/null 2>&1 || g -C "$PROJECT" config user.email "athanor@localhost"
if ! g -C "$PROJECT" rev-parse HEAD >/dev/null 2>&1; then
  g -C "$PROJECT" add -A >/dev/null 2>&1 && g -C "$PROJECT" commit -qm "Start (Athanor)" >/dev/null 2>&1 || g -C "$PROJECT" commit -q --allow-empty -m "Start (Athanor)"
fi

# Project start (MAX_HOURS safety stop): in the private folder, taken over from an older version
# if valid. A non-numeric value is never evaluated.
[ -f "$PRIVE/debut" ] || entier "$(cat "$ETAT/debut" 2>/dev/null)" "$(date +%s)" > "$PRIVE/debut"
DEBUT=$(entier "$(cat "$PRIVE/debut" 2>/dev/null)" "$(date +%s)")

BUT="/goal The next unticked milestone of .athanor/PLAN.md is completed and verified according to your instructions: bash .athanor/check.sh has been run and its output, showing success, is displayed in this conversation; .athanor/PLAN.md ticks this milestone; .athanor/LOG.md has a new entry; git log shows the commit. If .athanor/PLAN.md does not exist yet, this milestone is the framing phase described in your instructions, with no check.sh required. If all milestones are already ticked, the goal is reached when .athanor/FINAL_REPORT.md exists and contains the line STATUS: DONE. Or stop after ${MAX_TURNS_PER_CYCLE} turns. ${PHRASE_LANGUE}"

cycle=$(entier "$(cat "$PRIVE/cycle" 2>/dev/null)" 0)   # continues across restarts (stats.tsv, logs)
# installations older than this counter: continue after the last cycle recorded in the statistics
[ "$cycle" = 0 ] && [ -s "$PRIVE/stats.tsv" ] && cycle=$(entier "$(cut -f2 "$PRIVE/stats.tsv" | sed 's/·.*//' | grep -E '^[0-9]+$' | sort -n | tail -n 1)" 0)
erreurs=0
sans_progres=0

# ----------------------------------------------------------------------------- loop
while true; do
  compter_file_attente
  [ -f "$ETAT/STOP" ] && { ETAT_PHASE=arrete; terminer "Stop requested (STOP file). Delete it, then restart with start."; }
  ecoule_h=$(( ( $(date +%s) - DEBUT ) / 3600 ))
  if [ "$ecoule_h" -ge "$MAX_HOURS" ]; then
    terminer "Maximum duration reached (${MAX_HOURS} h). The work in progress is in the project folder."
  fi
  if est_termine; then
    # moment of the delivery: the requests moved to done/ before it are delivered, the later ones are
    # only added to the plan (the app shows the difference)
    date '+%Y%m%d-%H%M%S' > "$ETAT/done/.last-delivery" 2>/dev/null
    # delivered: not restarted when the application opens again (.athanor/auto, written by the app)
    prochaine_demande || { rm -f "$ETAT/auto"; terminer "DONE: the deliverable and FINAL_REPORT.md are in the project folder."; }
  fi

  if ! connexion_presente; then
    log "NO CONNECTION to Claude: put your token in .env (run configure), then restart."
    statut "Waiting for connection (token missing in .env)"
    dormir $(( LOGIN_WAIT_MIN * 60 ))
    continue
  fi

  synchroniser_demande
  preparer_cycle
  cycle=$(( cycle + 1 )); echo "$cycle" > "$PRIVE/cycle"
  fichier_log="$ETAT/logs/cycle-$(date '+%Y%m%d-%H%M%S')-$(printf '%04d' "$cycle").jsonl"
  avant=$(g -C "$PROJECT" rev-parse HEAD 2>/dev/null || echo aucun)
  coches_cycle_debut=$(coches_plan); coches_cycle_debut=${coches_cycle_debut:-0}

  # Cycle model: the main one, or the fallback model while the main one's quota is exhausted.
  modele_cycle="$MODEL"
  if [ -n "$FALLBACK_MODEL" ] && [ -f "$PRIVE/secours_jusqua" ]; then
    if [ "$(date +%s)" -lt "$(entier "$(cat "$PRIVE/secours_jusqua" 2>/dev/null)" 0)" ]; then
      modele_cycle="$FALLBACK_MODEL"
    else
      rm -f "$PRIVE/secours_jusqua"
      log "Main model quota reopened: back to ${MODEL:-default model}."
    fi
  fi
  log "Cycle $cycle: Claude is working${modele_cycle:+ with $modele_cycle} (log: .athanor/logs/$(basename "$fichier_log"))"
  ETAT_PHASE=travail; ETAT_REPRISE=""; ETAT_DEBUT_CYCLE=$(date '+%Y-%m-%dT%H:%M:%S')
  preparer_ecran
  preparer_extensions
  statut "Claude is working (cycle $cycle)"

  # Serial mode (one lead, one milestone) or parallel (a wave of workers, then merge by the lead).
  parallele_lire_reglage
  if [ "$PARALLEL" = 1 ]; then parallele_absorber_file; synchroniser_demande; fi
  mode=serie; but_cycle="$BUT"; consignes="$CONSIGNES"; plan_seul=0
  if [ "$PARALLEL" = 1 ]; then
    { cat "$CONSIGNES"; parallele_consignes; } > "$PRIVE/CLAUDE.cycle.md"; consignes="$PRIVE/CLAUDE.cycle.md"
    # a changed request (an additional one, or edited by hand) is first folded into the plan by the lead
    # alone, without any code: the milestones themselves are for the workers of the next cycles
    if [ -f "$ETAT/PLAN.md" ] && [ -f "$ETAT/REQUEST_CHANGED" ] && parallele_plan_a_des_vagues; then
      but_cycle="/goal The change of .athanor/REQUEST.md (flagged by .athanor/REQUEST_CHANGED) is folded into .athanor/PLAN.md as waves according to your parallel-mode instructions, the change is noted in .athanor/LOG.md, .athanor/REQUEST_CHANGED is deleted and the plan is committed, without writing any code. Or stop after 15 turns. ${PHRASE_LANGUE}"
      plan_seul=1; log "Parallel mode: the request changed, the lead updates the plan first."
    elif [ -f "$ETAT/PLAN.md" ] && [ ! -f "$ETAT/RECHECK" ] && [ ! -f "$ETAT/REQUEST_CHANGED" ]; then
      if parallele_plan_a_des_vagues; then
        [ "$(parallele_vague_courante | wc -l)" -ge 2 ] && mode=parallele
      else
        but_cycle="/goal .athanor/PLAN.md is reorganised into waves (## Wave 1, ## Wave 2…) according to your parallel-mode instructions, without writing any code. Or stop after 15 turns. ${PHRASE_LANGUE}"
        log "Parallel mode: the plan has no waves yet, the lead reorganises it first."
      fi
    fi
  fi
  if [ "$mode" = parallele ]; then
    parallele_cycle "${fichier_log%.jsonl}" "$modele_cycle"
    code=$?
    [ "$code" = 3 ] && mode=serie
  fi
  if [ "$mode" = serie ]; then
    args=(-p "$but_cycle" --permission-mode bypassPermissions --settings "$REGLAGES" --permission-prompts none --output-format stream-json --verbose
          "${ARGS_SURS[@]}" --append-system-prompt "$(cat "$consignes")")
    [ "$AGENTS_PAR_JSON" = 1 ] && args+=(--agents "$AGENTS_JSON")
    [ -f "$PRIVE/mcp.json" ] && args+=(--mcp-config "$PRIVE/mcp.json")
    [ -n "$PLUGIN_ATHANOR" ] && args+=(--plugin-dir "$PLUGIN_ATHANOR")
    [ -n "$modele_cycle" ] && args+=(--model "$modele_cycle")
    # Model overloaded or unavailable: Claude Code switches to the fallback model on its own.
    [ -n "$FALLBACK_MODEL" ] && [ "$modele_cycle" != "$FALLBACK_MODEL" ] && args+=(--fallback-model "$FALLBACK_MODEL")
    [ -n "$EFFORT" ] && args+=(--effort "$EFFORT")
    # The full log goes to the file; a readable version is displayed live
    # (visible in the pilot window or with: tail -f .athanor/pilot.log).
    lancer_claude_surveille "$fichier_log"
    code=$?
  fi
  ETAT_PHASE=entre_cycles
  # the plan-only cycle had its chance: the flag never blocks the workers a second time
  [ "$plan_seul" = 1 ] && rm -f "$ETAT/REQUEST_CHANGED"

  apres=$(g -C "$PROJECT" rev-parse HEAD 2>/dev/null || echo aucun)
  resultat=$(jq -cR 'fromjson? | select(type == "object" and .type == "result")' "$fichier_log" 2>/dev/null | tail -n 1)
  texte_resultat=$(printf '%s' "$resultat" | jq -r '.result // empty' 2>/dev/null)
  en_erreur=$(printf '%s' "$resultat" | jq -r '.is_error // false' 2>/dev/null)
  [ -z "$resultat" ] && en_erreur=true
  fin_log=$(grep -v -E '"type":"(rate_limit_event|tool_progress)"' "$fichier_log" | tail -n 15)
  reprise_quota=$(jq -rR 'fromjson? | select(type == "object" and .type == "rate_limit_event" and .rate_limit_info.status == "rejected") | .rate_limit_info.resetsAt // empty' "$fichier_log" 2>/dev/null | tail -n 1)
  motif_arret=$(cat "$PRIVE/cycle_arrete" 2>/dev/null || true)
  rm -f "$PRIVE/cycle_arrete"
  clos=0; [ -f "$PRIVE/cycle_clos" ] && clos=1; rm -f "$PRIVE/cycle_clos"

  if printf '%s' "$texte_resultat" | grep -Eqi "hit your .*limit"; then
    etat=quota
  elif [ -n "$reprise_quota" ] && { [ "$code" -ne 0 ] || [ "$en_erreur" = "true" ]; }; then
    etat=quota
  elif [ "$clos" = 1 ]; then
    etat=ok
  elif [ -n "$motif_arret" ]; then
    etat=bloque
  elif [ "$code" -eq 0 ] && [ "$en_erreur" != "true" ]; then
    etat=ok
  elif printf '%s\n%s' "$texte_resultat" "$fin_log" | grep -Eqi "hit your .*limit|usage limit|limit reached|rate[ _-]?limit|spend limit"; then
    etat=quota
  elif printf '%s\n%s' "$texte_resultat" "$fin_log" | grep -Eqi "not logged in|/login|invalid api key|oauth token|authentication_failed|invalid authentication|login expired"; then
    etat=connexion
  else
    etat=erreur
  fi

  enregistrer_stats "$fichier_log" "$etat"
  # Safeguard: a milestone ticked without "CHECK: PASSED" seen in the cycle log -> recheck first on the next cycle.
  coches_fin=$(coches_plan); coches_fin=${coches_fin:-0}
  if [ "$avant" != "$apres" ] && [ "$coches_fin" -gt "${coches_cycle_debut:-0}" ] \
     && [ -f "$ETAT/check.sh" ] && ! grep -q "CHECK: PASSED" "$fichier_log"; then
    date '+%Y-%m-%d %H:%M' > "$ETAT/RECHECK"
    log "Warning: milestone ticked without a visible successful full check; Claude will recheck first on the next cycle."
  fi

  case "$etat" in
    ok)
      erreurs=0
      if [ "$avant" = "$apres" ]; then
        sans_progres=$(( sans_progres + 1 ))
        log "Cycle $cycle finished without a new commit ($sans_progres/$MAX_CYCLES_WITHOUT_PROGRESS)."
        if [ "$sans_progres" -ge "$MAX_CYCLES_WITHOUT_PROGRESS" ]; then
          {
            echo "# Alert: no progress"
            echo "$(date '+%Y-%m-%d %H:%M'): $sans_progres cycles in a row without a commit."
            echo "Look at .athanor/LOG.md, BLOCKERS.md and logs/. You can refine your request."
          } > "$ETAT/ALERT.md"
          log "ALERT: $sans_progres cycles without progress, 30 min pause (see .athanor/ALERT.md)."
          ETAT_PHASE=erreur; statut "Paused after $sans_progres cycles without progress"
          sans_progres=0
          dormir 1800
        fi
      else
        sans_progres=0
        rm -f "$ETAT/ALERT.md"
        # The screenshots of this successful cycle become the reference for the next one.
        command -v athanor >/dev/null 2>&1 && PROJECT="$PROJECT" athanor screenshots --accept > /dev/null 2>&1
        log "Cycle $cycle succeeded: $(g -C "$PROJECT" log -1 --pretty=%s 2>/dev/null)"
        [ "$clos" = 1 ] && log "(cycle closed by the pilot after the milestone: the next one starts with a fresh context)"
      fi
      ;;
    quota)
      if printf '%s' "$reprise_quota" | grep -Eq '^[0-9]+$'; then
        attente=$(( reprise_quota - $(date +%s) + 120 ))
        [ "$attente" -gt 21600 ] && attente=21600
        [ "$attente" -lt 60 ] && attente=60
      else
        attente=$(attente_quota "$texte_resultat $fin_log")
      fi
      reprise=$(date -d "@$(( $(date +%s) + attente ))" '+%Y-%m-%d %H:%M')
      ETAT_PHASE=quota; ETAT_REPRISE="$reprise"
      if [ -n "$FALLBACK_MODEL" ] && [ "$modele_cycle" != "$FALLBACK_MODEL" ]; then
        # Main model quota exhausted: carry on with the fallback model until it reopens.
        echo $(( $(date +%s) + attente )) > "$PRIVE/secours_jusqua"
        log "Quota of ${modele_cycle:-default model} reached: switching to $FALLBACK_MODEL until about $reprise."
        statut "Fallback model $FALLBACK_MODEL until $reprise"
        dormir 60
      else
        log "Quota reached: pausing until about $reprise, then resuming automatically."
        statut "Quota reached, resuming around $reprise"
        dormir "$attente"
      fi
      ;;
    connexion)
      ETAT_PHASE=connexion
      log "Connection problem (token expired or invalid?). Retrying in $LOGIN_WAIT_MIN min."
      statut "Connection problem: check the token in .env"
      dormir $(( LOGIN_WAIT_MIN * 60 ))
      ;;
    bloque)
      erreurs=$(( erreurs + 1 ))
      log "Cycle $cycle stopped by the watchdog ($motif_arret). The work already done stays in the folder; resuming in $ERROR_WAIT_MIN min."
      ETAT_PHASE=erreur; statut "Cycle stopped ($motif_arret), resuming in $ERROR_WAIT_MIN min"
      dormir $(( ERROR_WAIT_MIN * 60 ))
      ;;
    erreur)
      ETAT_PHASE=erreur
      erreurs=$(( erreurs + 1 ))
      pause=$ERROR_WAIT_MIN
      for (( i = 1; i < erreurs && pause < ERROR_WAIT_MAX_MIN; i++ )); do pause=$(( pause * 2 )); done
      [ "$pause" -gt "$ERROR_WAIT_MAX_MIN" ] && pause=$ERROR_WAIT_MAX_MIN
      log "Error on cycle $cycle (code $code, $erreurs in a row). Retrying in $pause min."
      statut "Error, retrying in $pause min"
      dormir $(( pause * 60 ))
      ;;
  esac
done
