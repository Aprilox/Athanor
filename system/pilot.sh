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
. "$SYSTEME/roblox.sh"
migrer_atelier "$PROJECT"
# what the application's quick folder check unticked is Claude's to judge now (REVERIFY, CLAUDE.md)
rm -f "$ETAT/verification.json"
# Pilot runtime state: inside the tool, out of Claude's reach (see safety.sh).
PRIVE="$(prive_de "$PROJECT")"
# a GitHub token of the user's environment never reaches Claude (the GitHub push uses its own key, github.sh)
unset GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN GITHUB_ENTERPRISE_TOKEN
mkdir -p "$PRIVE"
REGLAGES="$PRIVE/claude_settings.json"
# The kit's texts (instructions, guides, sub-agents): template/, where a file of local/template/ takes the
# place of the same one (a repair made on this PC: an update of the kit never overwrites local/).
MODELES="$PRIVE/template"
rm -rf "$MODELES" && cp -r "$KIT/template" "$MODELES"
if [ -d "$KIT/local/template" ] && [ ! -L "$KIT/local/template" ]; then cp -r "$KIT/local/template/." "$MODELES/" 2>/dev/null; fi
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
if [ "$NATIF" = 1 ]; then
  # the shims of system/natif are the pilot's: Claude's shell keeps the real programs (its python3 writes
  # binary output, its pkill kills by name…)
  CHEMIN_CLAUDE=$(printf '%s' "$CHEMIN_CLAUDE" | tr ':' '\n' | grep -vxF "$SYSTEME/natif" | paste -sd: -)
  # Claude Code's Bash tool and hooks: this very Git Bash
  [ -n "${CLAUDE_CODE_GIT_BASH_PATH:-}" ] || export CLAUDE_CODE_GIT_BASH_PATH="$(cygpath -w /usr/bin/bash.exe 2>/dev/null)"
fi
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
FRAMING_MODEL="${FRAMING_MODEL:-}"           # the framing cycle (the plan): empty = MODEL
WORKER_MODEL="${WORKER_MODEL-sonnet}"        # parallel workers (a well-written milestone sheet is execution work); empty = MODEL
FRAMING_MAX_MIN="${FRAMING_MAX_MIN:-25}"     # time box of the framing: the plan as it stands goes ahead after that
RELAY_CONTEXT_K="${RELAY_CONTEXT_K:-150}"    # context (thousands of tokens) past which a fresh session takes over; 0 = never
CHEAP_MODEL="${CHEAP_MODEL-haiku}"  # "executor" sub-agent (mechanical tasks); empty = like the other sub-agents
IDLE_MAX_MIN="${IDLE_MAX_MIN:-30}"       # cycle stopped if its log stays silent that long
CYCLE_MAX_MIN="${CYCLE_MAX_MIN:-180}"                # cycle stopped if it runs longer than this
CLOSE_AFTER_MILESTONE="${CLOSE_AFTER_MILESTONE:-1}"          # 1 = cycle closed shortly after a milestone commit (fresh context for the next one)
WATCH_INTERVAL="${WATCH_INTERVAL:-30}"  # seconds between two checks
STAY_ALIVE_AT_END="${STAY_ALIVE_AT_END:-0}"  # 1 = the pilot stays on standby at the end
SECONDS_PER_MINUTE="${SECONDS_PER_MINUTE:-60}"      # only change to test the script
KEEP_AWAKE="${KEEP_AWAKE:-1}"   # 1 = the PC stays awake while the pilot runs (the screen too for Roblox Studio and Unreal); screen = always the screen too; 0 = off
case "$KEEP_AWAKE" in 0|1|screen) ;; *) echo "Invalid setting KEEP_AWAKE (\"$KEEP_AWAKE\"): using default 1." >&2; KEEP_AWAKE=1 ;; esac
# A non-numeric setting falls back to its default (and says so), never evaluated as is.
for paire in MAX_HOURS=168 QUOTA_WAIT_MIN=20 ERROR_WAIT_MIN=2 ERROR_WAIT_MAX_MIN=60 LOGIN_WAIT_MIN=30 MAX_CYCLES_WITHOUT_PROGRESS=3 \
             MAX_TURNS_PER_CYCLE=30 IDLE_MAX_MIN=30 CYCLE_MAX_MIN=180 WATCH_INTERVAL=30 SECONDS_PER_MINUTE=60 FRAMING_MAX_MIN=25 RELAY_CONTEXT_K=150; do
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

# The helpers that moved to Go (the move of the kit to Go, phase 1): the sub-commands of the kit's
# executable when it runs on this system and knows them (Athanor.exe in native Windows, athanor on Linux
# and in WSL), else the Python scripts they replace, as before. ATHANOR_OUTILS=python forces the scripts.
ATHANOR_GO=""
if [ "${ATHANOR_OUTILS:-}" != python ]; then
  if [ "$NATIF" = 1 ]; then candidat="$KIT/Athanor.exe"; else candidat="$KIT/athanor"; fi
  if [ -f "$candidat" ] && [ -x "$candidat" ] && aide_go=$(timeout 15 "$candidat" aide < /dev/null 2> /dev/null) \
     && ( for c in relais cout quota hook reglages; do printf '%s' "$aide_go" | grep -q "^  $c " || exit 1; done ); then
    ATHANOR_GO="$candidat"
  fi
  unset candidat aide_go
fi
if [ -n "$ATHANOR_GO" ]; then
  OUTIL_RELAIS=("$ATHANOR_GO" relais); OUTIL_COUT=("$ATHANOR_GO" cout); OUTIL_QUOTA=("$ATHANOR_GO" quota); OUTIL_REGLAGES=("$ATHANOR_GO" reglages)
else
  OUTIL_RELAIS=(python3 -I "$SYSTEME/relais.py"); OUTIL_COUT=(python3 -I "$SYSTEME/cout.py"); OUTIL_QUOTA=(python3 -I "$SYSTEME/quota.py"); OUTIL_REGLAGES=(python3 "$SYSTEME/settings.py")
fi

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
  local f="$1" etat="$2" etiquette="${3:-$cycle}" depot="${4:-$PROJECT}" res estime=0 tarifs="$KIT/logs/tarifs.json"
  [ -s "$PRIVE/stats.tsv" ] || printf 'horodatage\tcycle\tmodele\tetat\tminutes\tminutes_api\ttours\tcout_usd\tentree_tokens\tcache_lu\tcache_ecrit\tsortie_tokens\tcontexte_max\tcommit\testimation\n' > "$PRIVE/stats.tsv"
  res=$(jq -cR 'fromjson? | select(type=="object" and .type=="result")' "$f" 2>/dev/null | tail -n 1)
  if [ -n "$res" ]; then
    # its cost per model teaches this PC's prices, for the cycles that end without one
    timeout 20 "${OUTIL_COUT[@]}" apprendre "$tarifs" "$f" 2>> "$PRIVE/pilot.log"
  else
    # Cycle closed by the pilot or stopped (no "result" line, so no cost from Claude Code): the lead's
    # turns, each message counted once, at the learned prices, plus its sub-agents' turns, from the
    # transcripts Claude Code keeps of them (CONFIG_CLAUDE/projects/*/<session>/subagents)
    local debut_s; debut_s=$(date -d "${ETAT_DEBUT_CYCLE:-now}" +%s 2>/dev/null || date +%s)
    res=$(timeout 60 "${OUTIL_COUT[@]}" estimer "$tarifs" "$f" "$CONFIG_CLAUDE" 2>> "$PRIVE/pilot.log" \
      | jq -c --argjson d "$(( $(date +%s) - debut_s ))" '. + {duration_ms: ($d*1000)}' 2>/dev/null)
    [ -n "$res" ] && estime=1
  fi
  local pic; pic=$(jq -rR 'fromjson? | select(type=="object" and .type=="assistant" and .parent_tool_use_id==null) | .message.usage | ((.input_tokens//0)+(.cache_read_input_tokens//0)+(.cache_creation_input_tokens//0))' "$f" 2>/dev/null | sort -n | tail -n 1)
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$(date '+%Y-%m-%dT%H:%M:%S')" "$etiquette" "${modele_cycle:-defaut}" "$etat" \
    "$(printf '%s' "$res" | jq -r '((.duration_ms//0)/60000*10|round/10)' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '((.duration_api_ms//0)/60000*10|round/10)' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.num_turns//0' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '((.total_cost_usd//0)*100|round/100)' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.usage.input_tokens//0' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.usage.cache_read_input_tokens//0' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.usage.cache_creation_input_tokens//0' 2>/dev/null)" \
    "$(printf '%s' "$res" | jq -r '.usage.output_tokens//0' 2>/dev/null)" \
    "${pic:-0}" "$(case "$depot" in ref:*) g -C "$PROJECT" log -1 --pretty=%h "${depot#ref:}" 2>/dev/null ;; *) g -C "$depot" log -1 --pretty=%h 2>/dev/null ;; esac)" "$estime" >> "$PRIVE/stats.tsv"
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

# Tasks for the user (system/taches.py; page "For you" of the application): what Claude cannot do itself.
taches_etat() {
  local j; j=$(timeout 20 python3 -B "$SYSTEME/taches.py" etat "$PROJECT" 2>/dev/null 9>&-)
  POUR_VOUS=$(entier "$(printf '%s' "$j" | jq -r '.pour_vous // 0' 2>/dev/null)" 0)
  A_TRAITER=$(entier "$(printf '%s' "$j" | jq -r '.a_traiter // 0' 2>/dev/null)" 0)
}
signaler_reponses() {  # before a cycle: tasks the user answered are checked first (FOR_YOU_ANSWERED, CLAUDE.md)
  taches_etat
  if [ "$A_TRAITER" -gt 0 ]; then
    [ -f "$ETAT/FOR_YOU_ANSWERED" ] || log "You answered $A_TRAITER task(s): Claude checks them first."
    date '+%Y-%m-%d %H:%M' | publier "$ETAT/FOR_YOU_ANSWERED"
  else
    rm -f "$ETAT/FOR_YOU_ANSWERED"
  fi
  if [ -f "$ETAT/WAITING_FOR_YOU" ] && { [ "$POUR_VOUS" = 0 ] || [ "$A_TRAITER" -gt 0 ]; }; then rm -f "$ETAT/WAITING_FOR_YOU"; fi
  return 0
}
attendre_utilisateur() {  # every remaining milestone waits for the user: no cycle until an answer
  taches_etat
  if [ "$POUR_VOUS" = 0 ] || [ "$A_TRAITER" -gt 0 ]; then rm -f "$ETAT/WAITING_FOR_YOU"; return 0; fi
  log "Waiting for you: $POUR_VOUS task(s) on the For you page. The pilot resumes as soon as you answer one."
  ETAT_PHASE=attente_vous; statut "Waiting for you ($POUR_VOUS task(s))"
  local sha debut_attente=$(date +%s) n=0
  sha=$(sha256sum "$ETAT/REQUEST.md" 2>/dev/null | cut -d' ' -f1)
  while [ -f "$ETAT/WAITING_FOR_YOU" ] && [ ! -f "$ETAT/STOP" ]; do
    dormir 10
    taches_etat
    { [ "$A_TRAITER" -gt 0 ] || [ "$POUR_VOUS" = 0 ]; } && break
    [ "$(sha256sum "$ETAT/REQUEST.md" 2>/dev/null | cut -d' ' -f1)" != "$sha" ] && break   # a note added to the request
    n=$(( n + 1 )); [ $(( n % 30 )) = 0 ] && statut "Waiting for you ($POUR_VOUS task(s))"
  done
  rm -f "$ETAT/WAITING_FOR_YOU"
  # the time spent waiting for the user does not count towards MAX_HOURS
  DEBUT=$(( DEBUT + $(date +%s) - debut_attente )); echo "$DEBUT" > "$PRIVE/debut"
  [ -f "$ETAT/STOP" ] || log "Resuming: an answer arrived (or the request changed)."
  ETAT_PHASE=entre_cycles
  return 0
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

# The framing is done once .athanor/PLAN.md holds milestones: until then, every cycle is a framing cycle.
plan_pret() { grep -qE '^- \[[ xX]\] ' "$ETAT/PLAN.md" 2>/dev/null; }
jalons_plan() { grep -cE '^- \[[ xX]\] ' "$ETAT/PLAN.md" 2>/dev/null || true; }

# The quota stopped this session ($1 = its log)? Only a session that ended in error counts: a "rejected"
# rate-limit event, or the limit message in its result. (A session that went on and succeeded is not one.)
journal_quota() {
  local res; res=$(jq -cR 'fromjson? | select(type == "object" and .type == "result")' "$1" 2>/dev/null | tail -n 1)
  [ -n "$res" ] && [ "$(printf '%s' "$res" | jq -r '.is_error // false' 2>/dev/null)" != true ] && return 1
  jq -e -R 'fromjson? | select(type == "object" and .type == "rate_limit_event" and .rate_limit_info.status == "rejected")' "$1" >/dev/null 2>&1 \
    || printf '%s' "$res" | jq -r '.result // empty' 2>/dev/null | grep -Eqi "hit your .*limit|usage limit"
}

# Shared solutions: one line per problem solved, read by every agent at the start (CLAUDE.md). One file
# for the project (.athanor/SOLUTIONS.md), one for this PC and its tools ($TOOLS/SOLUTIONS.md). Claude
# writes them, so the pilot (outside the sandbox) never follows a link there, never blocks on a pipe,
# reads 256 KB at most and writes through a new file of its own: it keeps them short (duplicates out,
# the 60 latest lines), and merges in what a parallel worker added to its copy.
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
ranger_solutions() {  # $1 = file, $2 = what it is for
  timeout 20 python3 -I -c "$SOLUTIONS_PY" ranger "$1" "$2" 9>&- 2>/dev/null
  return 0
}
fusionner_solutions() {  # $1 = the main file, $2 = a parallel worker's copy
  timeout 20 python3 -I -c "$SOLUTIONS_PY" fusionner "$1" "$2" 9>&- 2>/dev/null
  return 0
}

# A successful check.sh run seen for this cycle: in Claude's log, or in a check output file written during
# the cycle (the lead often sends the long output to .athanor/.verif/*.log and only shows its tail; the
# last "CHECK:" line of the newest one counts). $1 = log, $2 = start of the cycle (seconds).
check_reussi() {
  grep -q "CHECK: PASSED" "$1" 2>/dev/null && return 0
  local f
  f=$(find "$ETAT/.verif" "$ETAT/check-logs" -maxdepth 2 -type f -newermt "@$2" -size -20M -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 1 | cut -d' ' -f2-)
  [ -n "$f" ] && grep -a "CHECK: " "$f" 2>/dev/null | tail -n 1 | grep -q "CHECK: PASSED"
}
# The commits between $1 and $2 change tracked files (the product). A cycle that only updated the plan
# (the workshop is outside git) needs no check.
produit_change() {
  [ "$1" != "$2" ] || return 1
  [ "$1" = aucun ] && return 0
  ! g -C "$PROJECT" diff --quiet "$1" "$2" 2>/dev/null
}

# Native Windows: Claude is a Windows program. The pilot's paths become Windows paths, the long texts go
# through files (a Windows command line holds 32,767 characters at most), and Git Bash is told not to
# rewrite any other argument (EXCL_CLAUDE): the goal starts with "/goal", which it would take for a path.
# $1 = name of the argument array, $2 = prefix of the files written for it. Nothing to do in WSL.
EXCL_CLAUDE=""
[ "$NATIF" = 1 ] && EXCL_CLAUDE="*"
args_natifs() {
  [ "$NATIF" = 1 ] || return 0
  local -n _a="$1"
  local -a n=()
  local i=0 k=0 o v f
  while [ "$i" -lt "${#_a[@]}" ]; do
    o="${_a[$i]}"; v="${_a[$((i + 1))]:-}"
    case "$o" in
      --settings|--mcp-config|--plugin-dir|--append-system-prompt-file)
        n+=("$o" "$(cygpath -m "$v")"); i=$((i + 2)) ;;
      --append-system-prompt)
        if [ "${APPEND_FICHIER_OK:-0}" = 1 ]; then
          k=$((k + 1)); f="$2.consignes$k.md"; printf '%s' "$v" > "$f"
          n+=(--append-system-prompt-file "$(cygpath -m "$f")")
        else n+=("$o" "$v"); fi
        i=$((i + 2)) ;;
      --agents)
        if [ "${AGENTS_FICHIER_OK:-0}" = 1 ]; then f="$2.agents.json"; printf '%s' "$v" > "$f"; n+=(--agents "$(cygpath -m "$f")")
        else n+=("$o" "$v"); fi
        i=$((i + 2)) ;;
      *) n+=("$o"); i=$((i + 1)) ;;
    esac
  done
  _a=("${n[@]}")
}

# The program to start for Claude. Native Windows: through Athanor.exe, which puts Claude in a Windows job:
# stopping it stops everything Claude started, and what is left when Claude ends stops with it (no dev
# server or browser keeps the cycle's output open); on Claude's desktop when that option is on.
# $1 = file where Athanor.exe writes its Windows PID (arreter_session stops the cycle with it).
prog_claude() {
  PROG_CLAUDE=("$CLAUDE_BIN")
  if [ "$NATIF" = 1 ] && [ -f "$KIT/Athanor.exe" ]; then
    PROG_CLAUDE=("$KIT/Athanor.exe" --isole --fin-tout)
    [ -n "${1:-}" ] && { rm -f "$1"; PROG_CLAUDE+=(--pid "$(cygpath -w "$1")"); }
    [ -f "$KIT/logs/bureau-isole" ] || PROG_CLAUDE+=(--ici)
    PROG_CLAUDE+=(-- "$(cygpath -w "$CLAUDE_BIN")")
  fi
}

lancer_claude_surveille() {  # $1 = log; stops the cycle if it stays silent or runs too long
  local journal="$1" f_sid="$PRIVE/claude_sid" f_code="$PRIVE/claude_code"
  local pid_flux debut maintenant derniere motif="" tete coches_debut jalon_vu=0 h c fin_vu=0 ecart verifie
  local relais_vu=0 dernier_ctx=0 ctx dernier_quota=0 lq f_win="$PRIVE/claude_winpid" attente dernier_verrou=0
  args_natifs args "$PRIVE/claude"
  prog_claude "$f_win"
  rm -f "$f_sid" "$f_code" "$PRIVE/cycle_arrete" "$PRIVE/cycle_clos" "$PRIVE/cycle_relais"
  tete=$(g -C "$PROJECT" rev-parse HEAD 2>/dev/null || echo aucun)
  coches_debut=$(coches_plan); coches_debut=${coches_debut:-0}
  : > "$journal"
  # setsid: Claude and everything it launches (engine, browser...) form one group that can be stopped at once.
  # (9>&-: the pilot's lock must not be inherited by Claude nor by the display pipeline)
  { MSYS2_ARG_CONV_EXCL="$EXCL_CLAUDE" setsid bash -c 'echo $$ > "$1"; f="$2"; b="$3"; PATH="$4"; shift 4; "$b" "$@" < /dev/null 2>&1; echo $? > "$f"' \
    _ "$f_sid" "$f_code" "$BASH" "$CHEMIN_CLAUDE" "$SYSTEME/ecran.sh" lancer "${PROG_CLAUDE[@]}" "${args[@]}" | tee "$journal" | afficher_en_direct; } 9>&- &
  pid_flux=$!
  debut=$(date +%s)
  while kill -0 "$pid_flux" 2>/dev/null; do
    sleep "$WATCH_INTERVAL" 9>&- & wait $! 2>/dev/null   # interruptible: immediate stop
    maintenant=$(date +%s)
    derniere=$(entier "$(stat -c %Y "$journal" 2>/dev/null)" "$debut")
    # Claude has finished but a program it launched keeps the output open: we do not wait for the watchdog.
    if [ -s "$f_code" ]; then [ "$fin_vu" = 0 ] && fin_vu=$maintenant; fi
    if [ "$fin_vu" != 0 ] && [ $(( maintenant - fin_vu )) -ge 30 ]; then
      arreter_session KILL "$f_sid" "$f_win"
      break
    elif [ $(( maintenant - derniere )) -ge $(( IDLE_MAX_MIN * SECONDS_PER_MINUTE )) ]; then
      motif="no activity for $IDLE_MAX_MIN min"
    elif [ $(( maintenant - debut )) -ge $(( ${LIMITE_CYCLE_MIN:-$CYCLE_MAX_MIN} * SECONDS_PER_MINUTE )) ]; then
      if [ -n "${LIMITE_CYCLE_MIN:-}" ]; then motif="framing time box of $LIMITE_CYCLE_MIN min spent"; else motif="cycle longer than $CYCLE_MAX_MIN min"; fi
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
        # a milestone can be ticked and committed before its full check has run: the cycle is closed once
        # the check is visible (or the commits only touched the plan), so that the next cycle does not
        # spend its start on a recheck; 20 min at most
        ecart=$(( maintenant - jalon_vu )); verifie=1
        if [ -f "$ETAT/check.sh" ] && produit_change "$tete" "$(g -C "$PROJECT" rev-parse HEAD 2>/dev/null || echo aucun)" && ! check_reussi "$journal" "$debut"; then
          verifie=0
        fi
        if [ "$ecart" -ge 1200 ] || { [ "$verifie" = 1 ] && { [ "$ecart" -ge 600 ] \
           || { [ "$ecart" -ge 120 ] && timeout 20 bash -c '. "$1"; shift; g "$@"' _ "$SYSTEME/safety.sh" -C "$PROJECT" diff --quiet --no-ext-diff --no-textconv HEAD 2>/dev/null; }; }; }; then
          motif="milestone delivered and committed"; : > "$PRIVE/cycle_clos"
        fi
      fi
    fi
    # Context full: a fresh session takes over the milestone (cheaper turns, and no automatic compaction
    # that would blur the thread). Only between two steps (no tool running), or 10 min later at most; the
    # relay notes say where the session was. Not in the framing, nor in a verification after workers.
    if [ -z "$motif" ] && [ "$RELAY_CONTEXT_K" -gt 0 ] && [ "${RELAIS_OK:-1}" = 1 ] && [ "${cadrage:-0}" != 1 ] && [ "$jalon_vu" = 0 ] \
       && [ $(( maintenant - dernier_ctx )) -ge 60 ]; then
      dernier_ctx=$maintenant
      ctx=$(entier "$(timeout 20 "${OUTIL_RELAIS[@]}" contexte "$journal" 2>/dev/null)" 0)
      if [ "$ctx" -ge $(( RELAY_CONTEXT_K * 1000 )) ]; then
        [ "$relais_vu" = 0 ] && relais_vu=$maintenant
        if timeout 20 "${OUTIL_RELAIS[@]}" sur "$journal" 2>/dev/null || [ $(( maintenant - relais_vu )) -ge 600 ]; then
          timeout 30 "${OUTIL_RELAIS[@]}" resume "$journal" "$ETAT/HANDOFF.md" > "$PRIVE/relais.md" 2>/dev/null
          echo "$(( ctx / 1000 ))" > "$PRIVE/relais_k"; : > "$PRIVE/cycle_relais"
          motif="context full ($(( ctx / 1000 ))k tokens): a fresh session takes over"
        fi
      fi
    fi
    # A locked Windows session draws nothing: Roblox Studio and Unreal captures and play tests fail until the
    # user unlocks it. Said once in the log (each time it locks), so that a quiet night is understood.
    if { [ "${ROBLOX_MCP:-0}" = 1 ] || [ "${UNREAL_MCP:-0}" = 1 ] || [ "${BLENDER_MCP:-0}" = 1 ]; } && [ $(( maintenant - dernier_verrou )) -ge 300 ]; then
      dernier_verrou=$maintenant
      if windows_verrouille; then
        [ "$verrou_vu" = 0 ] && log "Windows session locked: Roblox Studio, Unreal and Blender draw nothing until you unlock it (captures, play tests). Claude goes on with what needs no image."
        verrou_vu=1
      elif [ "$verrou_vu" = 1 ]; then
        verrou_vu=0; log "Windows session unlocked: the programs draw again."
      fi
    fi
    # Quota threshold with "stop now" (the application's auto stop): the cycle is cut; the loop then stops
    if [ -z "$motif" ] && [ -s "$REGLES_QUOTA" ] && [ $(( maintenant - dernier_quota )) -ge 60 ]; then
      dernier_quota=$maintenant
      lq=$(seuil_quota)
      case "$lq" in maintenant$'\t'*) motif="quota threshold reached: ${lq##*$'\t'}" ;; esac
    fi
    if [ -n "$motif" ]; then
      echo "$motif" > "$PRIVE/cycle_arrete"
      arreter_session TERM "$f_sid" "$f_win"
      sleep 10
      arreter_session KILL "$f_sid" "$f_win"
      break
    fi
  done
  # Claude must really be gone before anything else runs in the project: as long as its output stays
  # open, the pilot tries again every minute and says so (it never starts a second Claude beside it).
  attente=0
  while kill -0 "$pid_flux" 2>/dev/null; do
    sleep 5 9>&- & wait $! 2>/dev/null
    attente=$(( attente + 5 ))
    if [ $(( attente % 60 )) = 0 ]; then
      [ "$attente" = 60 ] && log "Cycle $cycle: Claude is still running after the stop${motif:+ ($motif)}; the pilot keeps stopping it (taskkill output: arret.log in the pilot's folder)."
      arreter_session KILL "$f_sid" "$f_win"
    fi
  done
  wait "$pid_flux" 2>/dev/null
  [ "$attente" -ge 60 ] && log "Cycle $cycle: Claude stopped after $(( attente / 60 )) min of attempts."
  # Cleanup: no program launched during this cycle may keep running.
  arreter_session KILL "$f_sid" "$f_win"
  rm -f "$f_win"
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
  (cd /mnt/c 2>/dev/null; MSYS2_ARG_CONV_EXCL='*' schtasks.exe /run /tn "Athanor - installations" >/dev/null 2>&1) || true
  # Unreal: the editor must be open so that Claude has its MCP tools during the cycle.
  if [ "${UNREAL_MCP:-0}" = "1" ]; then
    local uproject; uproject=$(find "$PROJECT" -maxdepth 2 -name '*.uproject' -print -quit 2>/dev/null)
    if [ -n "$uproject" ] && ! curl -s -o /dev/null --max-time 3 "${UNREAL_MCP_URL:-http://127.0.0.1:8000/mcp}"; then
      log "Opening the Unreal editor ($(basename "$uproject")) for the MCP server…"
      PROJECT="$PROJECT" unreal-editor --wait-mcp "$uproject" >> "$PRIVE/pilot.log" 2>&1 || log "Unreal MCP unavailable for this cycle."
    fi
  fi
  # Roblox Studio: its MCP server drives the open Studio. A Studio already open (yours included) is used
  # as it is; otherwise the pilot opens the project's first place, on Claude's desktop when that option is on.
  if [ "${ROBLOX_MCP:-0}" = "1" ]; then
    local place="" rojo_f
    # Rojo project: the files are the place (system/roblox.sh). Studio works on .athanor/studio/NAME.rbxl,
    # rebuilt from them whenever Studio is closed, and gets the scripts live (SYNC.luau) while it is open.
    rojo_f=$(roblox_projet_rojo)
    if [ -n "$rojo_f" ]; then
      roblox_sync_demarrer
      place=$(roblox_place_studio "$rojo_f")
      if ! roblox_studio_ouvert && ! roblox_construire "$rojo_f" "$place" 2>> "$PRIVE/pilot.log"; then
        log "Roblox Studio: rojo build failed (see the pilot's log), Studio opens on the place built last."
        [ -f "$place" ] || place=""
      fi
    fi
    [ -n "$place" ] || place=$(find "$PROJECT" -maxdepth 3 \( -name .athanor -o -name .git -o -name node_modules \) -prune -o \( -iname '*.rbxl' -o -iname '*.rbxlx' \) -type f -print -quit 2>/dev/null)
    if [ -n "$place" ]; then
      local sortie; sortie=$(PROJECT="$PROJECT" roblox-studio --wait "$place" 2>&1)
      case "$sortie" in
        *"already open"*) ;;
        *started*) log "Roblox Studio opened on $(basename "$place") for its MCP server." ;;
        *) log "Roblox Studio unavailable for this cycle: $(printf '%s' "$sortie" | tail -n 1 | cut -c1-200)" ;;
      esac
    elif [ "${ROBLOX_SANS_PLACE:-}" != 1 ]; then
      ROBLOX_SANS_PLACE=1
      log "Roblox Studio MCP: no .rbxl/.rbxlx place in the project, the pilot does not open Studio (open it yourself, or let Claude save the place into the project)."
    fi
  fi
  # Blender MCP: its tools act on an open Blender whose MCP add-on runs (its "Auto Start" on): opened
  # before the cycle on the project's first .blend, unless a Blender already runs (the user's included).
  if [ "${BLENDER_MCP:-0}" = "1" ]; then
    local blend sortie_b
    blend=$(find "$PROJECT" -maxdepth 3 \( -name .athanor -o -name .git -o -name node_modules \) -prune -o -iname '*.blend' -type f -print -quit 2>/dev/null)
    sortie_b=$(PROJECT="$PROJECT" blender --open ${blend:+"$blend"} 2>&1)
    case "$sortie_b" in
      *"already open"*) ;;
      *started*) log "Blender opened${blend:+ on $(basename "$blend")} for its MCP server." ;;
      *) sortie_b=$(printf '%s' "$sortie_b" | tail -n 1 | cut -c1-200)
         [ "$sortie_b" != "${BLENDER_ECHEC:-}" ] && log "Blender unavailable for this cycle: $sortie_b"
         BLENDER_ECHEC=$sortie_b; return 0 ;;
    esac
    BLENDER_ECHEC=""
  fi
}

# The live sync of a Rojo project's scripts into the open Studio (roblox-sync.py): one server for the
# pilot's whole life, outside Claude's sandbox; it writes .athanor/studio/SYNC.luau, the code Claude runs.
SYNC_PID=""
roblox_sync_demarrer() {
  [ -n "$SYNC_PID" ] && kill -0 "$SYNC_PID" 2>/dev/null && return 0
  python3 "$SYSTEME/roblox-sync.py" serve "$PROJECT" "$PRIVE/pilot.pid" >> "$PRIVE/roblox-sync.log" 2>&1 9>&- &
  SYNC_PID=$!
  [ -n "${SYNC_ANNONCE:-}" ] && return 0
  SYNC_ANNONCE=1
  log "Roblox Studio: Rojo project, the scripts reach the open Studio live (.athanor/studio/SYNC.luau), without restarting it."
}

# Automatic stops on the quota (Overview -> Subscription quota -> Auto stop): the rules the application
# writes in the kit's logs/quota-arret.json, checked against the latest quota report of the cycle logs.
# Read at each check: a rule changed in the application applies at once, without restarting the pilot.
REGLES_QUOTA="$KIT/logs/quota-arret.json"
seuil_quota() {  # prints "action<TAB>reset<TAB>message" when a rule is met
  [ -s "$REGLES_QUOTA" ] || return 0
  local js
  mapfile -t js < <(ls -t "$ETAT/logs"/*.jsonl 2>/dev/null | head -n 6)
  [ ${#js[@]} -gt 0 ] || return 0
  timeout 20 "${OUTIL_QUOTA[@]}" verifier "$REGLES_QUOTA" "${js[@]}" 2>/dev/null | head -n 1
}
appliquer_seuil_quota() {  # before a cycle: the pilot stops, or pauses until the window resets
  local l action reinit message reprise
  l=$(seuil_quota); [ -n "$l" ] || return 0
  IFS=$'\t' read -r action reinit message <<< "$l"
  reinit=$(entier "$reinit" 0)
  if [ "$action" = pause ] && [ "$reinit" -gt 0 ]; then
    reprise=$(date -d "@$(( reinit + 60 ))" '+%Y-%m-%d %H:%M')
    ETAT_PHASE=quota; ETAT_REPRISE="$reprise"
    log "Quota threshold reached: $message. Pausing until about $reprise, then resuming automatically."
    statut "Quota threshold reached ($message), resuming around $reprise"
    while [ "$(date +%s)" -lt $(( reinit + 60 )) ] && [ ! -f "$ETAT/STOP" ]; do
      dormir 300
      [ -n "$(seuil_quota)" ] || break   # the rule was lifted or changed in the application
    done
    ETAT_PHASE=""; ETAT_REPRISE=""
    [ -f "$ETAT/STOP" ] || log "Quota threshold: resuming."
    return 0
  fi
  ETAT_PHASE=arrete
  terminer "Quota threshold reached: $message. The pilot stopped (Start to resume)."
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
if [ "$NATIF" = 1 ] && ! [[ "$PROJECT" =~ ^/[a-zA-Z](/|$) ]]; then
  echo "Native Windows mode: the project must be in a folder of a Windows drive (C:\..., D:\...), not $PROJECT. Move it, or use the WSL mode."; exit 1
fi
mkdir -p "$ETAT/logs" "$ETAT/queue" "$ETAT/done"

# One pilot per project (double click on Start, automatic startup...).
exec 9>"$PRIVE/verrou"
flock -n 9 || { echo "The pilot is already running on this project."; exit 0; }
echo $$ > "$PRIVE/pilot.pid"
echo $$ | publier "$ETAT/pilot.pid"
# Heartbeat: the pid is republished every 20 s in the workshop; the application considers the pilot
# dead if the file is older than 90 s (WSL or the PC shut down before the pilot could exit).
EVEIL="$PRIVE/eveil"
verrou_vu=0   # the Windows session was seen locked (lancer_claude_surveille)
export JOURNAL_ARRET="$PRIVE/arret.log"   # what taskkill said when a cycle was stopped (native Windows)
( while kill -0 $$ 2>/dev/null && [ -f "$PRIVE/pilot.pid" ]; do echo $$ | publier "$ETAT/pilot.pid"; [ -f "$EVEIL" ] && touch "$EVEIL"; sleep 20; done ) 9>&- &
BATTEMENT_PID=$!
# The PC stays awake while the pilot runs (KEEP_AWAKE), even with the application closed: a sleeping PC
# stops every cycle until someone wakes it. Windows (native and WSL): Athanor.exe --eveil, which ends by
# itself once the pilot stops touching $EVEIL (the heartbeat above); Linux: systemd-inhibit. "screen" in
# $EVEIL also keeps the screen on, so that the screen timeout does not lock the session: a locked Windows
# session draws nothing (Roblox Studio, Unreal: captures and play tests fail).
rm -f "$EVEIL"
demarrer_eveil() {
  [ "$KEEP_AWAKE" = 0 ] && return 0
  echo system > "$EVEIL"
  if [ -f "$KIT/Athanor.exe" ] && { [ "$NATIF" = 1 ] || grep -qi microsoft /proc/version 2>/dev/null; }; then
    (cd /mnt/c 2>/dev/null; exec "$KIT/Athanor.exe" --eveil "$(wslpath -w "$EVEIL")") < /dev/null > /dev/null 2>&1 9>&- &
  elif command -v systemd-inhibit > /dev/null 2>&1; then
    systemd-inhibit --what=sleep:idle --who=Athanor --why="Athanor: a pilot is running" --mode=block \
      tail --pid=$$ -f /dev/null < /dev/null > /dev/null 2>&1 9>&- &
  else
    rm -f "$EVEIL"
  fi
}
# Before each cycle: the screen too when the cycle drives a Windows program's window.
mode_eveil() {
  [ -f "$EVEIL" ] || return 0
  local m=system
  if [ "$KEEP_AWAKE" = screen ] || { [ "$KEEP_AWAKE" = 1 ] && { [ "${ROBLOX_MCP:-0}" = 1 ] || [ "${UNREAL_MCP:-0}" = 1 ] || [ "${BLENDER_MCP:-0}" = 1 ]; }; }; then m=screen; fi
  [ "$(cat "$EVEIL" 2>/dev/null)" = "$m" ] || echo "$m" > "$EVEIL"
}
demarrer_eveil
arreter_tout() {  # Ctrl+C, window closed or stop.sh: Claude is stopped too
  local f
  arreter_session TERM "$PRIVE/claude_sid" "$PRIVE/claude_winpid"
  for f in "$PRIVE"/ouvrier_*.sid; do [ -f "$f" ] && arreter_session KILL "$f" "${f%.sid}.winpid"; done
  parallele_nettoyer_worktrees
  [ -n "${PONT_PID:-}" ] && kill "$PONT_PID" 2>/dev/null
  [ -n "${BATTEMENT_PID:-}" ] && kill "$BATTEMENT_PID" 2>/dev/null
  [ -n "${SYNC_PID:-}" ] && kill "$SYNC_PID" 2>/dev/null
  log "Pilot stopped."; ETAT_PHASE=arrete; statut "Stopped (restart with start)"; rm -f "$PRIVE/pilot.pid" "$ETAT/pilot.pid"; exit 0
}
trap arreter_tout TERM INT HUP
# Any end of the pilot (STOP, DONE, error) also stops the bridge.
trap '[ -n "${PONT_PID:-}" ] && kill "$PONT_PID" 2>/dev/null; [ -n "${SYNC_PID:-}" ] && kill "$SYNC_PID" 2>/dev/null; [ -n "${BATTEMENT_PID:-}" ] && kill "$BATTEMENT_PID" 2>/dev/null; [ -n "${ECRAN_PID:-}" ] && kill "$ECRAN_PID" 2>/dev/null; rm -f "$PRIVE/pilot.pid" "$ETAT/pilot.pid" "$PRIVE/ecran" "$EVEIL"' EXIT

[ -n "$CLAUDE_BIN" ] && [ -x "$CLAUDE_BIN" ] || { log "Claude Code not found: run install.sh first."; exit 1; }
# History from a previous pilot (before the private folder): taken over once, without following links.
for f in pilot.log stats.tsv; do [ -f "$PRIVE/$f" ] || { [ -f "$ETAT/$f" ] && [ ! -L "$ETAT/$f" ] && cp "$ETAT/$f" "$PRIVE/$f"; }; done

log "=== Athanor starting ==="
if [ -n "$ATHANOR_GO" ]; then log "Helpers: Go ($(basename "$ATHANOR_GO") $(timeout 10 "$ATHANOR_GO" version < /dev/null 2> /dev/null | tr -d "\r"))."; else log "Helpers: Python scripts."; fi

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
cp -f "$MODELES/CLAUDE.md" "$CONSIGNES"
# SUBAGENT_MODEL=auto: the lead chooses the model of each sub-agent call (the Agent tool's "model"
# parameter wins over the agent's own model); the defaults stay for a call that sets none
[ "${SUBAGENT_MODEL,,}" = auto ] && cat "$MODELES/guides/SUBAGENT_MODELS.md" >> "$CONSIGNES" 2>/dev/null
# native Windows mode: what changes for Claude (Git Bash, Windows programs, no sandbox)
[ "$NATIF" = 1 ] && cat "$MODELES/guides/NATIVE.md" >> "$CONSIGNES" 2>/dev/null
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
[ -f "$MODELES/PC_TOOLS_GUIDE.md" ] && cp -f "$MODELES/PC_TOOLS_GUIDE.md" "$ETAT/PC_TOOLS_GUIDE.md"
# the parts of the instructions only some cycles need (framing, the user's tasks, the final report): read
# when they apply rather than sent with every turn
mkdir -p "$ETAT/guides" && cp -f "$MODELES/guides/"*.md "$ETAT/guides/" 2>/dev/null
rm -rf "$ETAT/CLAUDE.md" "$ETAT/CLAUDE.cycle.md" "$ETAT/mcp.json" "$ETAT/claude_settings.json"   # older versions
rm -rf "$PRIVE/agents" && mkdir -p "$PRIVE/agents" && cp "$MODELES/.claude/agents/"*.md "$PRIVE/agents/" 2>/dev/null
# Sub-agents are passed to Claude Code as JSON (--agents); failing that, copied into PROJECT/.claude/agents.
# Model of each sub-agent: CHEAP_MODEL for "executor", SUBAGENT_MODEL for the others
# (empty = the lead's), unless a model is set in the agent file. Also written into the .md copies.
# The team (system/equipe.py): Athanor's sub-agents, plus the ones the user created in the app and the
# ones the lead proposed (.athanor/agents/, imported before each cycle). Rebuilt before every cycle.
preparer_equipe() {
  local l
  while IFS= read -r l; do [ -n "$l" ] && log "$l"; done < <(python3 "$SYSTEME/equipe.py" importer "$KIT" "$PROJECT" 2>/dev/null)
  AGENTS_JSON=$(SUBAGENT_MODEL="$SUBAGENT_MODEL" CHEAP_MODEL="$CHEAP_MODEL" python3 "$SYSTEME/equipe.py" json "$KIT" "$PROJECT" "$PRIVE/agents" 2>/dev/null)
  [ -n "$AGENTS_JSON" ] || AGENTS_JSON='{}'
  python3 "$SYSTEME/equipe.py" fiche "$KIT" "$PROJECT" "$PRIVE/agents" 2>/dev/null
  return 0
}
# copies of Athanor's agents left in the workshop by older versions (the folder now holds the lead's proposals)
for f in architect creator developer tester critic executor scout; do rm -f "$ETAT/agents/$f.md"; done
preparer_equipe
AIDE_CLAUDE=$("$CLAUDE_BIN" --help 2>/dev/null)
# Settings and MCP servers: only the pilot's, never those a project file might declare.
ARGS_SURS=()
printf '%s' "$AIDE_CLAUDE" | grep -q -- '--strict-mcp-config' && ARGS_SURS+=(--strict-mcp-config)
printf '%s' "$AIDE_CLAUDE" | grep -q -- '--setting-sources' && ARGS_SURS+=(--setting-sources user)
# native Windows: the instructions go through a file (a Windows command line holds 32,767 characters)
APPEND_FICHIER_OK=0
printf '%s' "$AIDE_CLAUDE" | grep -q -- '--append-system-prompt-file' && APPEND_FICHIER_OK=1
AGENTS_FICHIER_OK=0
v_claude=$("$CLAUDE_BIN" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n 1)
[ -n "$v_claude" ] && [ "$(printf '%s\n2.1.281\n' "$v_claude" | sort -V | head -n 1)" = 2.1.281 ] && AGENTS_FICHIER_OK=1
# --help does not list every option: a version that reads --agents from a file has this one too
[ "$AGENTS_FICHIER_OK" = 1 ] && APPEND_FICHIER_OK=1
if [ "$NATIF" = 1 ] && [ "$APPEND_FICHIER_OK" = 0 ]; then
  log "Native Windows mode: Claude Code ${v_claude:-?} cannot read its instructions from a file; update it (Setup & health -> Install everything), long instructions may not fit on a Windows command line."
fi
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
export ATHANOR_PLUGINS="$PRIVE/plugins"   # the plugins switched on for the project (Plugins tab), read-only too
"${OUTIL_REGLAGES[@]}" "$KIT" "$REGLAGES" > /dev/null || { log "Cannot write Claude's settings."; exit 1; }
# Unreal MCP: the "unreal" server of the Skills & MCP page, switched on per project (its address, relay
# included, comes from system/extensions.py; the .env keys of older versions are migrated by the app).
UNREAL_MCP=0; export UNREAL_MCP UNREAL_MCP_URL
unreal_depuis_mcp() {
  UNREAL_MCP=0
  if [ -f "$PRIVE/mcp.json" ] && jq -e '.mcpServers.unreal.url' "$PRIVE/mcp.json" >/dev/null 2>&1; then
    UNREAL_MCP=1; UNREAL_MCP_URL=$(jq -r '.mcpServers.unreal.url' "$PRIVE/mcp.json")
    [ "$UNREAL_MCP_URL" != "${UNREAL_PRECEDENT:-}" ] && log "Unreal MCP: $UNREAL_MCP_URL"
    UNREAL_PRECEDENT="$UNREAL_MCP_URL"
  fi
  export UNREAL_MCP UNREAL_MCP_URL
}
# Roblox Studio MCP: a server of the Skills & MCP page that drives Studio (the verified catalog's
# "roblox-studio", or Studio's configuration pasted by hand), switched on for the project.
ROBLOX_MCP=0; export ROBLOX_MCP
roblox_depuis_mcp() {
  ROBLOX_MCP=0
  [ -f "$PRIVE/mcp.json" ] && jq -e '.mcpServers | to_entries[] | select((.key | test("roblox"; "i")) or ((.value.args // []) | map(tostring) | join(" ") | test("Roblox\\\\mcp\\.bat"; "i")))' "$PRIVE/mcp.json" >/dev/null 2>&1 \
    && ROBLOX_MCP=1
  export ROBLOX_MCP
}

# Blender MCP: the verified catalog's "blender" (Blender Lab's server), or a server whose name says Blender.
BLENDER_MCP=0; export BLENDER_MCP
blender_depuis_mcp() {
  BLENDER_MCP=0
  [ -f "$PRIVE/mcp.json" ] && jq -e '.mcpServers | keys[] | select(test("blender"; "i"))' "$PRIVE/mcp.json" >/dev/null 2>&1 && BLENDER_MCP=1
  export BLENDER_MCP
}

# Skills and MCP servers chosen in the app (Skills & MCP page), plus Unreal MCP: rebuilt at the start of
# every cycle, so a change made in the app applies from the next cycle. Local MCP servers run in a cage
# (system/mcp-jail.sh); the skills form a plugin loaded with --plugin-dir.
PLUGIN_DIR_OK=0
printf '%s' "$AIDE_CLAUDE" | grep -q -- '--plugin-dir' && PLUGIN_DIR_OK=1
EXTENSIONS_AVANT=""
preparer_extensions() {
  local r; r=$(python3 "$SYSTEME/extensions.py" preparer "$KIT" "$PROJECT" "$PRIVE" 2>> "$PRIVE/pilot.log")
  PLUGIN_ATHANOR=""; PLUGINS_DIRS=()
  if [ -f "$PRIVE/plugin/.claude-plugin/plugin.json" ]; then
    if [ "$PLUGIN_DIR_OK" = 1 ]; then PLUGIN_ATHANOR="$PRIVE/plugin"
    else r="$r (skills ignored: this Claude Code version has no --plugin-dir, update it with repair)"; fi
  fi
  local d
  for d in "$PRIVE/plugins"/*/; do
    [ -f "$d.claude-plugin/plugin.json" ] && [ ! -L "${d%/}" ] && [ "$PLUGIN_DIR_OK" = 1 ] && PLUGINS_DIRS+=("${d%/}")
  done
  [ -n "$r" ] && [ "$r" != "$EXTENSIONS_AVANT" ] && log "Skills & MCP: $r"
  EXTENSIONS_AVANT="$r"
  unreal_depuis_mcp
  roblox_depuis_mcp
  blender_depuis_mcp
  mode_eveil
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
  # native Windows: no Linux windows (Claude's desktop, on the Windows side, keeps its programs away)
  if [ "$NATIF" = 1 ] || [ ! -f "$KIT/logs/bureau-isole" ] || [ -n "${ECRAN_IMPOSSIBLE:-}" ]; then arreter_ecran; return; fi
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
# (native Windows: no sandbox, the gateways run directly, PONT_DIRECT in settings.py)
PONT_PID=""
if [ "$NATIF" != 1 ]; then
  python3 "$SYSTEME/bridge.py" "$KIT" "$PROJECT" >> "$PRIVE/pont.log" 2>&1 9>&- &
  PONT_PID=$!
fi

if [ ! -d "$PROJECT/.git" ]; then
  g -C "$PROJECT" init -q -b main
fi
if [ "$NATIF" = 1 ]; then
  # the same repository as in WSL: Git for Windows must neither rewrite the line ends nor see every
  # file as changed because of the executable bits NTFS does not keep
  g -C "$PROJECT" config core.autocrlf false; g -C "$PROJECT" config core.fileMode false
fi
# The workshop must never enter the project's history.
grep -qx '.athanor/' "$PROJECT/.git/info/exclude" 2>/dev/null || { mkdir -p "$PROJECT/.git/info"; echo '.athanor/' >> "$PROJECT/.git/info/exclude"; }
grep -qx '.claude/' "$PROJECT/.git/info/exclude" 2>/dev/null || echo '.claude/' >> "$PROJECT/.git/info/exclude"
# Empty files the system sandbox creates at the project root (protection mount points):
# they make "git add -A" fail; excluded from git without touching the project's .gitignore.
for motif in /.bashrc /.bash_profile /.profile /.zshrc /.zprofile /.gitconfig /.gitmodules /.ripgreprc /.idea /.vscode /.mcp.json; do
  grep -qxF "$motif" "$PROJECT/.git/info/exclude" 2>/dev/null || echo "$motif" >> "$PROJECT/.git/info/exclude"
done
# Local secrets (.env and its variants, never their templates) never enter the history: excluded here
# (this computer, before the first commit), and listed in the project's .gitignore when it does not
# already mention them (a copy of the project elsewhere ignores them too).
MOTIFS_SECRETS=(.env '.env.*' '!.env.example' '!.env.sample' '!.env.template' '!.env.dist')
for motif in "${MOTIFS_SECRETS[@]}"; do
  grep -qxF -- "$motif" "$PROJECT/.git/info/exclude" 2>/dev/null || printf '%s\n' "$motif" >> "$PROJECT/.git/info/exclude"
done
gi="$PROJECT/.gitignore"
if [ ! -L "$gi" ] && { [ ! -e "$gi" ] || [ -f "$gi" ]; } \
   && ! grep -Eq '^[[:space:]]*!?(\*\*/|/)?\*?\.env\*?[[:space:]]*$' "$gi" 2>/dev/null; then
  { if [ -s "$gi" ]; then [ -n "$(tail -c 1 "$gi")" ] && echo; echo; fi
    echo "# Local secrets (added by Athanor)"; printf '%s\n' "${MOTIFS_SECRETS[@]}"; } >> "$gi" \
    && log "Local secrets (.env) added to the project's .gitignore."
fi
unset gi
# Author of the commits: the one chosen in the GitHub block (Manage page), else the user's git settings;
# and Claude as co-author or not. Applied again before every cycle (a change in the app takes effect then).
IDENTITE_PRECEDENTE=""
appliquer_identite() {
  local l nom email sans
  l=$(bash "$SYSTEME/github.sh" identite-pilote "$PROJECT" 2>/dev/null)
  IFS=$'\t' read -r nom email sans <<< "$l"
  if [ -n "$nom" ] && [ -n "$email" ]; then
    export GIT_AUTHOR_NAME="$nom" GIT_COMMITTER_NAME="$nom" GIT_AUTHOR_EMAIL="$email" GIT_COMMITTER_EMAIL="$email"
    g -C "$PROJECT" config user.name "$nom"; g -C "$PROJECT" config user.email "$email"
  else
    unset GIT_AUTHOR_NAME GIT_COMMITTER_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL
    g -C "$PROJECT" config user.name  >/dev/null 2>&1 || g -C "$PROJECT" config user.name  "Athanor"
    g -C "$PROJECT" config user.email >/dev/null 2>&1 || g -C "$PROJECT" config user.email "athanor@localhost"
  fi
  if [ "$sans" = 1 ]; then export ATHANOR_SANS_COAUTEUR=1; else unset ATHANOR_SANS_COAUTEUR; fi
  bash "$SYSTEME/github.sh" crochet "$PROJECT" "${sans:-0}" 2>/dev/null
  if [ "$l" != "$IDENTITE_PRECEDENTE" ]; then
    [ -n "${REGLAGES:-}" ] && [ -f "$REGLAGES" ] && "${OUTIL_REGLAGES[@]}" "$KIT" "$REGLAGES" > /dev/null
    [ -n "$nom" ] && log "Commits by $nom <$email>$([ "$sans" = 1 ] && echo ", without Claude as co-author")."
    IDENTITE_PRECEDENTE="$l"
  fi
}
appliquer_identite
# Files of the piloting that versions before 2.8.0 put in the project itself (the working documents, a
# .athanor/ line in the project's .gitignore): Claude moves them into the workshop on its next cycle
# (.athanor/TIDY, CLAUDE.md), never a document of the user's own. Looked for once per project.
if [ ! -f "$PRIVE/rangement" ]; then
  a_ranger=""
  for f in docs/REQUIREMENTS.md docs/DESIGN.md docs/ARCHITECTURE.md docs/DECISIONS.md; do
    [ -f "$PROJECT/$f" ] && [ ! -L "$PROJECT/$f" ] && a_ranger+="- \`$f\`"$'\n'
  done
  if [ -f "$PROJECT/.gitignore" ] && [ ! -L "$PROJECT/.gitignore" ] && grep -Eq '^[[:space:]]*/?\.(athanor|claude)(/|/\*)?[[:space:]]*$' "$PROJECT/.gitignore"; then
    a_ranger+="- the \`.athanor/\` or \`.claude/\` line of \`.gitignore\`"$'\n'
  fi
  if [ -n "$a_ranger" ]; then
    printf '# Files of the piloting to take out of the project

%s' "$a_ranger" | publier "$ETAT/TIDY" \
      && log "Files of the piloting found in the project: Claude moves them into the workshop first (.athanor/TIDY)."
  fi
  date '+%Y-%m-%d %H:%M' > "$PRIVE/rangement"
fi
# Local secret files git already follows (committed by hand, or by a version before 2.14.0): taken out of
# git, kept on disk. Already pushed somewhere, their values stay readable in that history: Claude asks
# the user to change them (.athanor/SECRETS_PUBLISHED.md, CLAUDE.md).
mapfile -d '' SECRETS_SUIVIS < <(g -C "$PROJECT" ls-files -z 2>/dev/null \
  | grep -zE '(^|/)\.env(\.[^/]+)?$' | grep -zvE '(^|/)\.env\.(example|sample|template|dist)$' \
  | g -C "$PROJECT" check-ignore -z --stdin --no-index 2>/dev/null)   # a "!.env" of the user's .gitignore wins
if [ "${#SECRETS_SUIVIS[@]}" -gt 0 ]; then
  liste=$(printf '%s\n' "${SECRETS_SUIVIS[@]}" | head -n 20 | sed 's/^/- `/; s/$/`/')
  if GIT_LITERAL_PATHSPECS=1 g -C "$PROJECT" rm -q --cached -- "${SECRETS_SUIVIS[@]}" >/dev/null 2>&1; then
    # the commit holds this removal only, never what the user had staged: built on a temporary index
    if ancien=$(g -C "$PROJECT" rev-parse -q --verify 'HEAD^{commit}' 2>/dev/null); then
      idx="$PRIVE/index.secrets.$$"; rm -f "$idx"
      GIT_INDEX_FILE="$idx" g -C "$PROJECT" read-tree "$ancien" 2>/dev/null \
        && GIT_INDEX_FILE="$idx" GIT_LITERAL_PATHSPECS=1 g -C "$PROJECT" rm -q --cached -- "${SECRETS_SUIVIS[@]}" >/dev/null 2>&1 \
        && arbre=$(GIT_INDEX_FILE="$idx" g -C "$PROJECT" write-tree 2>/dev/null) \
        && nouveau=$(g -C "$PROJECT" commit-tree "$arbre" -p "$ancien" -m "Stop tracking local secret files" 2>/dev/null) \
        && g -C "$PROJECT" update-ref -m "athanor: stop tracking local secret files" HEAD "$nouveau" "$ancien" 2>/dev/null \
        || log "Local secret files: taken out of git, but the commit failed (Claude commits it with its next change)."
      rm -f "$idx"; unset idx arbre nouveau
    fi
    unset ancien
    log "Local secret files taken out of git (still on disk, now ignored): $(printf '%s ' "${SECRETS_SUIVIS[@]}" | cut -c1-300)"
    if [ -n "$(GIT_LITERAL_PATHSPECS=1 g -C "$PROJECT" log --remotes -1 --format=%H -- "${SECRETS_SUIVIS[@]}" 2>/dev/null)" ] \
       || bash "$SYSTEME/github.sh" publie "$PROJECT" "${SECRETS_SUIVIS[@]}" >/dev/null 2>&1; then
      printf '# Secret files that were published\n\nThese files hold local secrets and were in commits already pushed (to GitHub or another remote):\n\n%s\n\nThe pilot took them out of git (they are still on disk, and now ignored), but their values stay readable in the published history: only changing them protects the project.\n\n1. Add one task for the user (`athanor todo add`): regenerate (revoke, then replace) each key, token or password these files hold, at the service that issued it, then put the new value back in the file. Name the files and the services or variable names concerned, never a value.\n2. Note it in `.athanor/LOG.md`, then delete this file.\n' "$liste" \
        | publier "$ETAT/SECRETS_PUBLISHED.md" \
        && log "These secret files were already pushed: Claude asks you to change their values (For you page)."
    fi
  else
    log "Local secret files followed by git could not be taken out of it: $(printf '%s ' "${SECRETS_SUIVIS[@]}" | cut -c1-300)"
  fi
  unset liste
fi
if ! g -C "$PROJECT" rev-parse HEAD >/dev/null 2>&1; then
  g -C "$PROJECT" add -A >/dev/null 2>&1 && g -C "$PROJECT" commit -qm "Initial commit" >/dev/null 2>&1 || g -C "$PROJECT" commit -q --allow-empty -m "Initial commit"
fi

# Project start (MAX_HOURS safety stop): in the private folder, taken over from an older version
# if valid. A non-numeric value is never evaluated.
[ -f "$PRIVE/debut" ] || entier "$(cat "$ETAT/debut" 2>/dev/null)" "$(date +%s)" > "$PRIVE/debut"
DEBUT=$(entier "$(cat "$PRIVE/debut" 2>/dev/null)" "$(date +%s)")

BUT="/goal The next unticked milestone of .athanor/PLAN.md is completed and verified according to your instructions: bash .athanor/check.sh has been run and its output, showing success, is displayed in this conversation; .athanor/PLAN.md ticks this milestone; .athanor/LOG.md has a new entry; git log shows the commit. If .athanor/PLAN.md does not exist yet, this milestone is the framing phase described in your instructions, with no check.sh required. If all milestones are already ticked, the goal is reached when .athanor/FINAL_REPORT.md exists and contains the line STATUS: DONE. A milestone that waits for a task of the user (athanor todo list) is passed over for the next one; if .athanor/FOR_YOU_ANSWERED exists, every answered task is handled first (athanor todo ok, missing or resolved); if every unticked milestone waits for the user, the goal is reached once athanor todo wait has run. Or stop after ${MAX_TURNS_PER_CYCLE} turns. ${PHRASE_LANGUE}"

# The framing (no plan yet): its own goal, its own instructions and a time box (FRAMING_MAX_MIN). Past the
# time box, the plan as it stands goes ahead; with no plan at all, one short cycle writes it from what
# the framing already wrote. Two time boxes missed in a row: the next framing runs without one.
FORMAT_JALON='each milestone one line starting with "- [ ] M1 : " (the pilot reads only those lines) and written as a self-contained sheet (Files, Depends on, Acceptance, Read)'
but_cadrage() {  # $1 = 1 for the closing cycle, $2 = time box in minutes (empty: none)
  if [ "$1" = 1 ]; then
    printf '%s' "/goal The framing is closed now: .athanor/PLAN.md is written from .athanor/REQUEST.md and the documents already in .athanor/docs (no more reading of attachments, no sub-agent, no code), $FORMAT_JALON, and .athanor/LOG.md has a new entry. Or stop after 12 turns. ${PHRASE_LANGUE}"
  else
    printf '%s' "/goal The framing is done according to the framing steps of your instructions: .athanor/PLAN.md exists, $FORMAT_JALON; .athanor/docs/REQUIREMENTS.md and .athanor/docs/ARCHITECTURE.md exist; .athanor/LOG.md has a new entry. No code, no installation, no demo content in this cycle.${2:+ The pilot gives this cycle $2 minutes: write the plan well before that.} Or stop after ${MAX_TURNS_PER_CYCLE} turns. ${PHRASE_LANGUE}"
  fi
}
consignes_cadrage() {  # $1 = 1 for the closing cycle, $2 = time box in minutes (empty: none)
  if [ "$1" = 1 ]; then
    printf '\n## This cycle: closing the framing\n\nThe framing cycle ended before the plan was written. Write `.athanor/PLAN.md` now, yourself, from `.athanor/REQUEST.md` and what is already in `.athanor/docs/`: each milestone one line starting with `- [ ] M1 : `. Do not read the attachments again, do not call any sub-agent, write no code. The missing details become milestones of the plan.\n'
  elif [ -n "$2" ]; then
    printf '\n## This cycle: the framing (time box of %s min)\n\nThis cycle only writes the plan, with the steps below: no code, no installation, no test of a tool, no demo content. The pilot stops it after %s minutes and starts the plan as it stands then: have `.athanor/PLAN.md` written within the first half of that time, refine after.\n\n' "$2" "$2"
    cat "$MODELES/guides/FRAMING.md" 2>/dev/null
  else
    printf '\n## This cycle: the framing\n\nThis cycle only writes the plan, with the steps below: no code, no installation, no test of a tool, no demo content.\n\n'
    cat "$MODELES/guides/FRAMING.md" 2>/dev/null
  fi
}

# GitHub (system/github.sh): when the user linked the project to a repository with automatic push,
# its commits go there after each successful milestone and at the delivery. Silent when not linked;
# a failure is logged and never stops the pilot. GITHUB_CODE: 0 pushed, 3 nothing to do, 4 secret found.
github_pousser() {
  GITHUB_CODE=3
  [ -f "$SYSTEME/github.sh" ] || return 0
  local sortie
  sortie=$(timeout 900 bash "$SYSTEME/github.sh" pousser "$PROJECT" --auto 2>&1 9>&-); GITHUB_CODE=$?
  [ "$GITHUB_CODE" = 3 ] || log "$(printf '%s' "$sortie" | tail -n 1 | cut -c1-300)"
  return 0
}

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
    github_pousser
    if [ "$GITHUB_CODE" = 4 ] && [ -f "$ETAT/SECRETS_FOUND.md" ]; then
      # never delivered with a secret in the commits: Claude removes it first (CLAUDE.md), then delivers again
      sed -i -E 's/STATUS: *DONE[^*]*/STATUS: REOPENED (a secret must leave the commits before the GitHub push)/' "$ETAT/FINAL_REPORT.md"
      log "Delivery reopened: a secret must leave the commits before the GitHub push (.athanor/SECRETS_FOUND.md)."
    else
      # delivered: not restarted when the application opens again (.athanor/auto, written by the app)
      prochaine_demande || { rm -f "$ETAT/auto"; terminer "DONE: the deliverable and FINAL_REPORT.md are in the project folder."; }
    fi
  fi

  if ! connexion_presente; then
    log "NO CONNECTION to Claude: put your token in .env (run configure), then restart."
    statut "Waiting for connection (token missing in .env)"
    dormir $(( LOGIN_WAIT_MIN * 60 ))
    continue
  fi

  appliquer_seuil_quota
  [ -f "$ETAT/STOP" ] && continue   # stopped while pausing: the loop's start ends the pilot
  synchroniser_demande
  signaler_reponses
  ranger_solutions "$ETAT/SOLUTIONS.md" "this project"
  ranger_solutions "$TOOLS/SOLUTIONS.md" "this PC and its tools, every project"
  # the MCP servers of this cycle first: a server switched on in the app since the last cycle has its
  # program (Unreal, Roblox Studio, Blender) opened by preparer_cycle right away
  preparer_ecran
  preparer_extensions
  preparer_cycle
  appliquer_identite
  preparer_equipe
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
  cadrage=0; cadrage_fin=0; LIMITE_CYCLE_MIN=""
  if ! plan_pret; then
    cadrage=1
    [ -f "$PRIVE/cadrage_fin" ] && cadrage_fin=1
    # the framing model, unless the fallback replaces the main one for now
    [ -n "$FRAMING_MODEL" ] && [ "$modele_cycle" = "$MODEL" ] && modele_cycle="$FRAMING_MODEL"
    # FRAMING_MAX_MIN=0: no time box; two missed in a row: none either (the request needs more)
    if [ "$FRAMING_MAX_MIN" -gt 0 ] && [ "$(entier "$(cat "$PRIVE/cadrage_depasse" 2>/dev/null)" 0)" -lt 2 ]; then
      if [ "$cadrage_fin" = 1 ]; then LIMITE_CYCLE_MIN=$(( FRAMING_MAX_MIN < 15 ? FRAMING_MAX_MIN : 15 )); else LIMITE_CYCLE_MIN=$FRAMING_MAX_MIN; fi
    fi
  fi
  log "Cycle $cycle: Claude is working${modele_cycle:+ with $modele_cycle}$([ "$cadrage" = 1 ] && echo ", framing${LIMITE_CYCLE_MIN:+ (time box $LIMITE_CYCLE_MIN min)}") (log: .athanor/logs/$(basename "$fichier_log"))"
  ETAT_PHASE=travail; ETAT_REPRISE=""; ETAT_DEBUT_CYCLE=$(date '+%Y-%m-%dT%H:%M:%S')
  statut "Claude is working (cycle $cycle)"

  # Serial mode (one lead, one milestone) or parallel (a wave of workers, then merge by the lead).
  parallele_lire_reglage
  if [ "$PARALLEL" = 1 ]; then parallele_absorber_file; synchroniser_demande; fi
  mode=serie; but_cycle="$BUT"; consignes="$CONSIGNES"; plan_seul=0
  # the user answered tasks: how to handle the answers comes with this cycle
  if [ -f "$ETAT/FOR_YOU_ANSWERED" ] && [ -f "$MODELES/guides/FOR_YOU.md" ]; then
    { cat "$consignes"; printf '\n\n'; cat "$MODELES/guides/FOR_YOU.md"; } > "$PRIVE/CLAUDE.reponses.md"; consignes="$PRIVE/CLAUDE.reponses.md"
  fi
  if [ "${ATHANOR_SANS_COAUTEUR:-0}" = 1 ]; then
    # the user does not want Claude as co-author: no such line in the commit messages, whatever the habit
    { cat "$CONSIGNES"; printf '\n## Commit messages\n\nNever add a `Co-Authored-By` line, a "Generated with" line or any mention of Claude or of an AI tool to a commit message: the commits are signed by the user alone.\n'; } > "$PRIVE/CLAUDE.auteur.md"
    consignes="$PRIVE/CLAUDE.auteur.md"
  fi
  if [ "$PARALLEL" = 1 ]; then
    { cat "$consignes"; parallele_consignes; } > "$PRIVE/CLAUDE.cycle.md"; consignes="$PRIVE/CLAUDE.cycle.md"
    # a changed request (an additional one, or edited by hand) is first folded into the plan by the lead
    # alone, without any code: the milestones themselves are for the workers of the next cycles
    if [ -f "$ETAT/PLAN.md" ] && [ -f "$ETAT/REQUEST_CHANGED" ] && parallele_plan_a_des_vagues; then
      but_cycle="/goal The change of .athanor/REQUEST.md (flagged by .athanor/REQUEST_CHANGED) is folded into .athanor/PLAN.md as waves according to your parallel-mode instructions, the change is noted in .athanor/LOG.md, .athanor/REQUEST_CHANGED is deleted and the plan is committed, without writing any code. Or stop after 15 turns. ${PHRASE_LANGUE}"
      plan_seul=1; log "Parallel mode: the request changed, the lead updates the plan first."
    elif [ -f "$ETAT/PLAN.md" ] && [ ! -f "$ETAT/RECHECK" ] && [ ! -f "$ETAT/REQUEST_CHANGED" ] && [ ! -f "$ETAT/REVERIFY" ] && [ ! -f "$ETAT/SECRETS_FOUND.md" ] && [ ! -f "$ETAT/SECRETS_PUBLISHED.md" ] && [ ! -f "$ETAT/TIDY" ] && [ ! -f "$ETAT/FOR_YOU_ANSWERED" ] && [ ! -f "$ETAT/SAVE_CHECK" ] && [ ! -s "$PRIVE/relais.md" ]; then
      if parallele_plan_a_des_vagues; then
        # the pilot picks the mode of each cycle: workers only when they pay off (parallel.sh)
        raison_serie=$(parallele_raison_serie)
        if [ -z "$raison_serie" ]; then
          mode=parallele
        elif [ "$raison_serie" != "$(cat "$PRIVE/raison_serie" 2>/dev/null)" ]; then
          log "Parallel mode: the lead works alone this cycle ($raison_serie)."
        fi
        printf '%s' "$raison_serie" > "$PRIVE/raison_serie"
      else
        but_cycle="/goal .athanor/PLAN.md is reorganised into waves (## Wave 1, ## Wave 2…) according to your parallel-mode instructions, without writing any code. Or stop after 15 turns. ${PHRASE_LANGUE}"
        log "Parallel mode: the plan has no waves yet, the lead reorganises it first."
      fi
    fi
  fi
  # the framing: its own goal and instructions (serial, whatever the mode)
  if [ "$cadrage" = 1 ]; then
    mode=serie
    but_cycle=$(but_cadrage "$cadrage_fin" "$LIMITE_CYCLE_MIN")
    { cat "$consignes"; consignes_cadrage "$cadrage_fin" "$LIMITE_CYCLE_MIN"; } > "$PRIVE/CLAUDE.cadrage.md"; consignes="$PRIVE/CLAUDE.cadrage.md"
  fi
  # after a quota stop with a switch to the fallback model: one serial cycle first. When the limit hit was
  # the account's, it stops at once and costs nothing; a wave of workers would all fail on it together.
  if [ "$mode" = parallele ] && [ "$(date +%s)" -lt "$(entier "$(cat "$PRIVE/serie_jusqua" 2>/dev/null)" 0)" ]; then
    mode=serie
  elif [ "$mode" = parallele ] && [ -f "$PRIVE/sonde_quota" ]; then
    mode=serie; log "After the quota stop: one milestone by the lead first, to check that the quota is back before starting workers."
  fi
  QUOTA_OUVRIERS=0 MODELE_VERIF=""
  if [ "$mode" = parallele ]; then
    parallele_cycle "${fichier_log%.jsonl}" "$modele_cycle"
    code=$?
    [ "$code" = 3 ] && mode=serie
  fi
  # serial while workers left partial work on their branches (quota, watchdog): the lead resumes it rather than redo it
  if [ "$mode" = serie ] && [ "$cadrage" != 1 ]; then
    partiels=""
    while read -r br; do partiels+="${partiels:+, }\`auto/$br\`"; done < <(parallele_branches_partielles | head -n 20)
    if [ -n "$partiels" ]; then
      { cat "$consignes"; printf '\n\n## Partial work left by workers\n\nThese branches hold work on a milestone started by a parallel worker that was stopped: %s. When you take one of these milestones, merge its branch first (`git merge --no-ff auto/<milestone>`) and carry on from there, never redo it; once the milestone is ticked, delete the branch (`git branch -D auto/<milestone>`).\n' "$partiels"; } > "$PRIVE/CLAUDE.serie.md"
      consignes="$PRIVE/CLAUDE.serie.md"
    fi
  fi
  if [ "$mode" = serie ] && [ "$cadrage" != 1 ] && [ -s "$PRIVE/relais.md" ]; then
    { cat "$consignes"; printf '\n\n'; cat "$PRIVE/relais.md"; } > "$PRIVE/CLAUDE.relais.md"; consignes="$PRIVE/CLAUDE.relais.md"
    rm -f "$PRIVE/relais.md"
  fi
  # how to save what was done in the programs driven by the MCP servers of this cycle (system/extensions.py)
  if [ "$mode" = serie ] && [ -f "$PRIVE/mcp.json" ] && [ -s "$PRIVE/mcp.json.sauvegarde.md" ]; then
    { cat "$consignes"; printf '\n\n'; cat "$PRIVE/mcp.json.sauvegarde.md"; } > "$PRIVE/CLAUDE.mcp.md"; consignes="$PRIVE/CLAUDE.mcp.md"
  fi
  if [ "$mode" = serie ]; then
    args=(-p "$but_cycle" --permission-mode bypassPermissions --settings "$REGLAGES" --permission-prompts none --output-format stream-json --verbose
          "${ARGS_SURS[@]}" --append-system-prompt "$(cat "$consignes")")
    [ "$AGENTS_PAR_JSON" = 1 ] && args+=(--agents "$AGENTS_JSON")
    [ -f "$PRIVE/mcp.json" ] && args+=(--mcp-config "$PRIVE/mcp.json")
    [ -n "$PLUGIN_ATHANOR" ] && args+=(--plugin-dir "$PLUGIN_ATHANOR")
    for dp in "${PLUGINS_DIRS[@]}"; do args+=(--plugin-dir "$dp"); done
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
  relais=0; [ -f "$PRIVE/cycle_relais" ] && relais=1; rm -f "$PRIVE/cycle_relais"

  if printf '%s' "$texte_resultat" | grep -Eqi "hit your .*limit"; then
    etat=quota
  elif [ -n "$reprise_quota" ] && { [ "$code" -ne 0 ] || [ "$en_erreur" = "true" ]; }; then
    etat=quota
  elif [ "$clos" = 1 ]; then
    etat=ok
  elif [ "$relais" = 1 ]; then
    etat=relais
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

  # The framing: done as soon as the plan exists (the workshop is outside git: no commit to expect);
  # its time box spent without a plan, a short closing cycle follows at once.
  issue_cadrage=""
  if [ "$cadrage" = 1 ] && [ "$etat" != quota ] && [ "$etat" != connexion ]; then
    if plan_pret; then
      issue_cadrage=fait; etat=ok
    else
      # time box spent, turns used up, or the session simply stopped: the plan is written now, in a short cycle
      issue_cadrage=a_conclure; etat=bloque
      [ -f "$ETAT/PLAN.md" ] && log "Framing: .athanor/PLAN.md has no milestone line the pilot can read (each one must start with \"- [ ] M1 : \")."
    fi
  fi
  modele_cycle="${MODELE_VERIF:-$modele_cycle}" enregistrer_stats "$fichier_log" "$etat"
  # Safeguard: a milestone ticked without "CHECK: PASSED" seen in the cycle log -> recheck first on the next cycle.
  coches_fin=$(coches_plan); coches_fin=${coches_fin:-0}
  if [ "$coches_fin" -gt "${coches_cycle_debut:-0}" ]; then
    rm -f "$ETAT/HANDOFF.md"   # the milestone is done: its handover notes with it
    # MCP servers drive programs that keep their work in memory (an editor, an engine…): used for a
    # milestone whose commit changed no file, that work may exist nowhere yet. Claude saves it first.
    serveurs=$(jq -rR 'fromjson? | select(type == "object" and .type == "assistant") | .message.content[]? | select(.type == "tool_use") | .name // empty' "$fichier_log" 2>/dev/null \
      | awk -F'__' '/^mcp__/ && $2 != "" {print $2}' | sort -u | head -n 10 | tr '\n' ' ')
    if [ -n "$serveurs" ] && ! produit_change "$avant" "$apres"; then
      printf '%s\n' $serveurs > "$ETAT/SAVE_CHECK"
      log "The milestone used MCP servers (${serveurs% }) but its commit changed no file: Claude first saves their work into the project on the next cycle."
    fi
  fi
  if produit_change "$avant" "$apres" && [ "$coches_fin" -gt "${coches_cycle_debut:-0}" ] \
     && [ -f "$ETAT/check.sh" ] && ! check_reussi "$fichier_log" "$(date -d "$ETAT_DEBUT_CYCLE" +%s 2>/dev/null || echo 0)"; then
    date '+%Y-%m-%d %H:%M' > "$ETAT/RECHECK"
    log "Warning: milestone ticked without a visible successful full check; Claude will recheck first on the next cycle."
  fi

  # a real wait only: a task waits for the user and no answer is pending (never a way to dodge the
  # "cycles without progress" alert)
  attente_vous=0
  if [ "$etat" = ok ] && [ -f "$ETAT/WAITING_FOR_YOU" ]; then
    taches_etat
    if [ "$POUR_VOUS" -gt 0 ] && [ "$A_TRAITER" = 0 ]; then attente_vous=1; else rm -f "$ETAT/WAITING_FOR_YOU"; fi
  fi
  [ "$etat" = ok ] || rm -f "$ETAT/WAITING_FOR_YOU"
  case "$etat" in quota|connexion) ;; *) rm -f "$PRIVE/sonde_quota" ;; esac
  if [ "$issue_cadrage" = fait ]; then
    erreurs=0; sans_progres=0
    rm -f "$PRIVE/cadrage_fin" "$ETAT/ALERT.md"
    if printf '%s' "$motif_arret" | grep -q "framing time box"; then
      echo $(( $(entier "$(cat "$PRIVE/cadrage_depasse" 2>/dev/null)" 0) + 1 )) > "$PRIVE/cadrage_depasse"
      log "Framing time box reached: the plan as it stands goes ahead ($(jalons_plan) milestones)."
    else
      rm -f "$PRIVE/cadrage_depasse"
      log "Framing done in $(( ( $(date +%s) - $(date -d "$ETAT_DEBUT_CYCLE" +%s 2>/dev/null || date +%s) ) / 60 )) min: $(jalons_plan) milestones in the plan."
    fi
    etat=cadre
  elif [ "$issue_cadrage" = a_conclure ]; then
    erreurs=0
    echo $(( $(entier "$(cat "$PRIVE/cadrage_depasse" 2>/dev/null)" 0) + 1 )) > "$PRIVE/cadrage_depasse"
    if [ "$cadrage_fin" = 1 ]; then
      rm -f "$PRIVE/cadrage_fin"
      log "The closing cycle wrote no plan either: the next framing runs without a time box."
    else
      : > "$PRIVE/cadrage_fin"
      log "Framing ended with no plan yet ($( [ -n "$motif_arret" ] && echo "$motif_arret" || echo "the session stopped")): a short cycle writes it now from the documents already there."
    fi
    # framings that never produce a plan count as cycles without progress (alert and pause, as for milestones)
    sans_progres=$(( sans_progres + 1 ))
    if [ "$sans_progres" -ge "$MAX_CYCLES_WITHOUT_PROGRESS" ]; then
      { echo "# Alert: no plan"; echo "$(date '+%Y-%m-%d %H:%M'): $sans_progres framing cycles in a row without a plan."; echo "Look at .athanor/LOG.md and logs/. You can refine your request."; } > "$ETAT/ALERT.md"
      log "ALERT: $sans_progres framing cycles without a plan, 30 min pause (see .athanor/ALERT.md)."
      ETAT_PHASE=erreur; statut "Paused after $sans_progres framing cycles without a plan"
      sans_progres=0
      dormir 1800
    fi
    etat=cadre
  fi
  case "$etat" in
    cadre) ;;
    ok)
      erreurs=0
      if [ "$avant" = "$apres" ] && [ "$attente_vous" = 1 ]; then
        sans_progres=0   # a cycle that only handed tasks to the user is not a cycle without progress
      elif [ "$avant" = "$apres" ]; then
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
        github_pousser
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
      # the kind of limit hit: the account's own (5 hours, the week) is the fallback model's too
      type_limite=$(jq -rR 'fromjson? | select(type == "object" and .type == "rate_limit_event" and .rate_limit_info.status == "rejected") | .rate_limit_info.rateLimitType // empty' "$fichier_log" 2>/dev/null | tail -n 1)
      rm -f "$PRIVE/sonde_quota"
      if [ "${QUOTA_OUVRIERS:-0}" = 1 ] && ! [[ "$type_limite" =~ ^(five_hour|seven_day)$ ]] && [ -n "${WORKER_MODEL:-}" ] && [ "$WORKER_MODEL" != "$modele_cycle" ]; then
        # the workers' own model ran out, not the lead's: the lead goes on alone, without workers, until the reset
        echo $(( $(date +%s) + attente )) > "$PRIVE/serie_jusqua"
        log "Quota of the workers' model $WORKER_MODEL reached: the lead goes on alone, without workers, until about $reprise."
        statut "Workers paused until $reprise"
        dormir 60
      elif [ -n "$FALLBACK_MODEL" ] && [ "$modele_cycle" != "$FALLBACK_MODEL" ] && [[ "$type_limite" =~ ^(five_hour|seven_day)$ ]]; then
        log "Quota of the account reached ($( [ "$type_limite" = five_hour ] && echo "5-hour limit" || echo "weekly limit")): the fallback model shares it, pausing until about $reprise, then resuming automatically."
        statut "Quota reached, resuming around $reprise"
        : > "$PRIVE/sonde_quota"
        dormir "$attente"
      elif [ -n "$FALLBACK_MODEL" ] && [ "$modele_cycle" != "$FALLBACK_MODEL" ]; then
        # Main model quota exhausted: carry on with the fallback model until it reopens.
        echo $(( $(date +%s) + attente )) > "$PRIVE/secours_jusqua"
        : > "$PRIVE/sonde_quota"
        log "Quota of ${modele_cycle:-default model} reached: switching to $FALLBACK_MODEL until about $reprise."
        statut "Fallback model $FALLBACK_MODEL until $reprise"
        dormir 60
      else
        log "Quota reached: pausing until about $reprise, then resuming automatically."
        statut "Quota reached, resuming around $reprise"
        : > "$PRIVE/sonde_quota"
        dormir "$attente"
      fi
      ;;
    relais)
      erreurs=0
      log "Cycle $cycle: context full ($(cat "$PRIVE/relais_k" 2>/dev/null)k tokens). A fresh session takes over the milestone now, from the relay notes."
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
  [ "$attente_vous" = 1 ] && attendre_utilisateur
done
