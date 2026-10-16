#!/usr/bin/env bash
# =============================================================================
#  Athanor (WSL2 / Linux): configuration assistant
#  Asks a few questions and writes .env (settings shared by all projects). The app
#  drives it. Run it again whenever you want: your current answers are
#  offered as defaults (Enter key).
#  Windows: double-click configure.cmd · Linux: ./configure.sh
# =============================================================================
set -u
cd "$(dirname "$0")/.." || exit 1
export PATH="$HOME/.local/bin:$PATH"   # Claude Code (native install)

ENV_FICHIER=.env
DEMANDE_VIERGE_SHA="5f5cdf3e03d8222b40cba308cfbe2338a2e2e022eb567c3758d4a32adc88de30"
GRAS=$'\033[1m'; VERT=$'\033[32m'; JAUNE=$'\033[33m'; GRIS=$'\033[90m'; FIN=$'\033[0m'
[ -t 1 ] || { GRAS=""; VERT=""; JAUNE=""; GRIS=""; FIN=""; }

# ----------------------------------------------------------------------------- tools
valeur_actuelle() {  # $1 = key; current value in .env (empty if absent)
  [ -f "$ENV_FICHIER" ] || return 0
  sed -n "s/^$1=//p" "$ENV_FICHIER" | tail -n 1 | tr -d '\r'
}

titre() { printf '\n%s%s%s\n' "$GRAS" "$1" "$FIN" >&2; [ -n "${2:-}" ] && printf '%s%s%s\n' "$GRIS" "$2" "$FIN" >&2; return 0; }

# choisir "current value" "value|label" ... -> REPONSE
# "__autre__" as the value = free input.
choisir() {
  local actuelle="$1"; shift
  local opts=("$@") i defaut=0 rep n
  for i in "${!opts[@]}"; do [ "${opts[$i]%%|*}" = "$actuelle" ] && defaut=$((i + 1)); done
  [ -f "$ENV_FICHIER" ] || defaut=0
  if [ "$defaut" = 0 ] && [ -n "$actuelle" ]; then
    opts=("$actuelle|Keep the current value: $actuelle" "${opts[@]}"); defaut=1
  fi
  if [ "$defaut" = 0 ]; then
    for i in "${!opts[@]}"; do case "${opts[$i]}" in *"(recommended)"*) defaut=$((i + 1)); break ;; esac; done
  fi
  [ "$defaut" = 0 ] && defaut=1
  n=${#opts[@]}
  for i in "${!opts[@]}"; do
    if [ $((i + 1)) = "$defaut" ]; then
      printf '  %s%2d) %s%s  %s← Enter%s\n' "$VERT" $((i + 1)) "${opts[$i]#*|}" "$FIN" "$GRIS" "$FIN" >&2
    else
      printf '  %2d) %s\n' $((i + 1)) "${opts[$i]#*|}" >&2
    fi
  done
  while :; do
    printf 'Your choice [%s]: ' "$defaut" >&2
    IFS= read -r rep || rep=""
    rep=${rep:-$defaut}
    if [[ "$rep" =~ ^[0-9]+$ ]] && [ "$rep" -ge 1 ] && [ "$rep" -le "$n" ]; then
      REPONSE=${opts[$((rep - 1))]%%|*}
      if [ "$REPONSE" = "__autre__" ]; then
        printf 'Enter the value: ' >&2; IFS= read -r REPONSE || REPONSE=""
        REPONSE=$(printf '%s' "$REPONSE" | tr -d '[:space:]')
      fi
      return 0
    fi
    printf '  %sType a number between 1 and %s.%s\n' "$JAUNE" "$n" "$FIN" >&2
  done
}

demander() {  # $1 = question, $2 = default -> REPONSE
  printf '%s [%s]: ' "$1" "$2" >&2
  IFS= read -r REPONSE || REPONSE=""
  REPONSE=$(printf '%s' "${REPONSE:-$2}" | tr -d '\r')
}

oui_non() {  # $1 = question, $2 = o|n as default; exit code 0 if yes
  local rep invite="[y/N]"; [ "$2" = o ] && invite="[Y/n]"
  printf '%s %s: ' "$1" "$invite" >&2
  IFS= read -r rep || rep=""
  rep=${rep:-$2}
  case "$rep" in [oOyY]*) return 0 ;; *) return 1 ;; esac
}

# ----------------------------------------------------------------------------- questions
printf '%s=== Athanor: configuration ===%s\n' "$GRAS" "$FIN" >&2
[ -f "$ENV_FICHIER" ] && printf '%sA .env file exists: its values are offered as defaults.%s\n' "$GRIS" "$FIN" >&2

# 1. Token
titre "1. Login token for your Claude subscription" "Obtained with \"claude setup-token\" (Claude Code installed on this PC), valid for 1 year."
JETON=$(valeur_actuelle CLAUDE_CODE_OAUTH_TOKEN)
if [ -n "$JETON" ]; then
  printf 'Current token: %s...%s\n' "${JETON:0:14}" "${JETON: -4}" >&2
  invite="Paste a new token, or Enter to keep the current one: "
else
  if command -v claude >/dev/null 2>&1 && [ -t 0 ] && oui_non "Run \"claude setup-token\" now?" o; then
    claude setup-token
  fi
  invite="Paste the token (it is not echoed): "
fi
while :; do
  printf '%s' "$invite" >&2
  IFS= read -rs nouveau || { echo >&2; break; }   # end of input: keep the current token
  echo >&2
  nouveau=$(printf '%s' "$nouveau" | tr -d '[:space:]')
  [ -n "$nouveau" ] && JETON=$nouveau
  if [ -z "$JETON" ]; then
    oui_non "No token. Continue without one (to add later in .env)?" n && break
    continue
  fi
  case "$JETON" in sk-ant-*) break ;; esac
  oui_non "${JAUNE}This token does not start with \"sk-ant-\". Keep it anyway?${FIN}" n && break
  JETON=""
done

# 2. Main model
titre "2. Project lead model" "It plans, codes and judges the result. The more powerful it is, the faster the quota goes."
choisir "$(valeur_actuelle MODEL)" \
  "claude-opus-5-5|Opus 5.5: best quality for a complete project (recommended)" \
  "claude-sonnet-5-5|Sonnet 5.5: very good and more frugal, the quota lasts longer" \
  "claude-haiku-5-5|Haiku 5.5: fast and very frugal, for simple projects" \
  "claude-fable-5-1|Fable 5.1: the most powerful, uses the most quota (Max subscription; plan a fallback model)" \
  "opus|Always the latest Opus (alias \"opus\")" \
  "sonnet|Always the latest Sonnet (alias \"sonnet\")" \
  "|Your account's default model" \
  "__autre__|Another identifier (an older version, for example)"
MODEL=$REPONSE

# 2b. Fallback model
titre "2b. Fallback model" "Used automatically when the main model's quota is exhausted, until it reopens, then back to the main one. Essential with Fable."
case "$MODEL" in *fable*) conseil=claude-opus-5-5 ;; *opus*) conseil=claude-sonnet-5-5 ;; *) conseil="" ;; esac
opts=()
for o in "|None: pause until the quota reopens" "claude-opus-5-5|Opus 5.5" "claude-sonnet-5-5|Sonnet 5.5" "claude-haiku-5-5|Haiku 5.5" "__autre__|Another identifier"; do
  [ "${o%%|*}" = "$conseil" ] && o="$o (recommended)"
  opts+=("$o")
done
choisir "$(valeur_actuelle FALLBACK_MODEL)" "${opts[@]}"
FALLBACK_MODEL=$REPONSE
if [ -n "$FALLBACK_MODEL" ] && [ "$FALLBACK_MODEL" = "$MODEL" ]; then
  printf '%sThe fallback model is the same as the main one: no fallback.%s\n' "$JAUNE" "$FIN" >&2
  FALLBACK_MODEL=""
fi

# 3. Effort
titre "3. Thinking effort" "How much the model thinks before acting. More effort = slower and more quota."
choisir "$(valeur_actuelle EFFORT)" \
  "|Normal: Claude Code's default setting (recommended)" \
  "low|Low: the fastest, for simple tasks" \
  "medium|Medium: faster and more frugal, good quality" \
  "high|High: thinks more" \
  "xhigh|Extra: thinks a lot, noticeably slower" \
  "max|Maximum: the slowest and most expensive"
EFFORT=$REPONSE

# 4. Subagents
titre "4. Subagent model" "They receive the big chunks of work (a level, a module) from the project lead."
choisir "$(valeur_actuelle SUBAGENT_MODEL)" \
  "|The same as the project lead" \
  "claude-sonnet-5-5|Sonnet 5.5: saves quota, Opus keeps the lead (recommended)" \
  "claude-opus-5-5|Opus 5.5" \
  "claude-haiku-5-5|Haiku 5.5: very frugal" \
  "__autre__|Another identifier"
SUBAGENT_MODEL=$REPONSE

# 5. Duration
titre "5. Maximum working time" "Safety stop, counted from the project's first start (quota pauses included)."
choisir "$(valeur_actuelle MAX_HOURS)" \
  "24|1 day" "72|3 days" "168|1 week (recommended)" "336|2 weeks" "__autre__|Other (in hours)"
DUREE=$REPONSE
[[ "$DUREE" =~ ^[0-9]+$ ]] || DUREE=168

# 6. RTK
titre "6. Command output compression (RTK)" "Reduces what Claude reads for git, ls, grep, find... No risk for the code."
rtk_defaut=o; [ "$(valeur_actuelle RTK)" = 0 ] && rtk_defaut=n
if oui_non "Enable RTK?" "$rtk_defaut"; then RTK=1; else RTK=0; fi

# 7. Time zone
titre "7. Time zone of the logs"
tz_defaut=$(valeur_actuelle TZ)
if [ -z "$tz_defaut" ]; then
  tz_defaut=$( (timedatectl show -p Timezone --value 2>/dev/null || cat /etc/timezone 2>/dev/null) | head -n 1)
fi
demander "Time zone" "${tz_defaut:-UTC}"; TZ_CHOISI=$REPONSE

# 8. Unreal Engine
titre "8. Driving the Unreal editor (MCP)" "Unreal Engine 5.8: enable the \"Unreal MCP\" and \"All Toolsets\" plugins, and the server's automatic start (Editor Preferences > Model Context Protocol). On Windows, run unreal-network.cmd once (\"mirrored\" network or local relay)."
PROJECT="$PWD/logs/detection" system/detect-tools.sh >/dev/null 2>&1
ue_defaut=n; [ "$(valeur_actuelle UNREAL_MCP)" = 1 ] && ue_defaut=o
grep -q "^UE_DIR='.\+'" system/pc_tools.env 2>/dev/null && { printf '%sUnreal detected on this PC.%s\n' "$VERT" "$FIN" >&2; [ -f "$ENV_FICHIER" ] || ue_defaut=o; }
if oui_non "Let Claude drive the open Unreal editor?" "$ue_defaut"; then UNREAL_MCP=1; else UNREAL_MCP=0; fi

# ----------------------------------------------------------------------------- write
PAUSE_QUOTA=$(valeur_actuelle QUOTA_WAIT_MIN); INACT=$(valeur_actuelle IDLE_MAX_MIN); CMAX=$(valeur_actuelle CYCLE_MAX_MIN)
GERES=" CLAUDE_CODE_OAUTH_TOKEN MODEL FALLBACK_MODEL SUBAGENT_MODEL EFFORT MAX_HOURS RTK TZ UNREAL_MCP QUOTA_WAIT_MIN IDLE_MAX_MIN CYCLE_MAX_MIN "
[ -f "$ENV_FICHIER" ] && cp "$ENV_FICHIER" "$ENV_FICHIER.sauvegarde" && chmod 600 "$ENV_FICHIER.sauvegarde" 2>/dev/null

{
  echo "# Written by configure.sh on $(date '+%Y-%m-%d %H:%M'). Run it again to change these settings."
  echo "# Never share this file: the token gives access to your Claude subscription."
  echo
  echo "# Login token (valid for 1 year), obtained with: claude setup-token"
  echo "CLAUDE_CODE_OAUTH_TOKEN=$JETON"
  echo
  echo "# Project lead model (empty = account default)"
  echo "MODEL=$MODEL"
  echo "# Fallback model when the main one's quota is exhausted (empty = pause)"
  echo "FALLBACK_MODEL=$FALLBACK_MODEL"
  echo "# Subagent model (empty = the same)"
  echo "SUBAGENT_MODEL=$SUBAGENT_MODEL"
  echo "# Thinking effort: empty = normal; low, medium, high, xhigh, max"
  echo "EFFORT=$EFFORT"
  echo
  echo "# Safety stop after this number of hours"
  echo "MAX_HOURS=$DUREE"
  echo "# Wait when the quota reset time cannot be read (minutes)"
  echo "QUOTA_WAIT_MIN=${PAUSE_QUOTA:-20}"
  echo "# Watchdog: a cycle is stopped if it stays silent or runs too long (minutes)"
  echo "IDLE_MAX_MIN=${INACT:-30}"
  echo "CYCLE_MAX_MIN=${CMAX:-180}"
  echo
  echo "# Command output compression (1 = on, 0 = off)"
  echo "RTK=$RTK"
  echo "# Time zone of the logs"
  echo "TZ=$TZ_CHOISI"
  echo "# Driving the Unreal editor through its MCP server (1 = yes)"
  echo "UNREAL_MCP=$UNREAL_MCP"
  if [ -f "$ENV_FICHIER.sauvegarde" ]; then
    autres=$(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$ENV_FICHIER.sauvegarde" | tr -d '\r' | while IFS= read -r l; do
      case "$GERES" in *" ${l%%=*} "*) ;; *) echo "$l" ;; esac; done)
    if [ -n "$autres" ]; then echo; echo "# Other settings kept"; echo "$autres"; fi
  fi
} > "$ENV_FICHIER.tmp" && mv "$ENV_FICHIER.tmp" "$ENV_FICHIER"
chmod 600 "$ENV_FICHIER" 2>/dev/null

titre "Configuration saved to .env"
printf '  Model: %s | Fallback: %s | Effort: %s | Subagents: %s\n' "${MODEL:-account default}" "${FALLBACK_MODEL:-none}" "${EFFORT:-normal}" "${SUBAGENT_MODEL:-the same}" >&2
printf '  Max time: %s h | RTK: %s | Time zone: %s | Unreal MCP: %s\n' "$DUREE" "$RTK" "$TZ_CHOISI" "$UNREAL_MCP" >&2
[ -f "$ENV_FICHIER.sauvegarde" ] && printf '%s  Previous version kept in .env.sauvegarde%s\n' "$GRIS" "$FIN" >&2
[ -z "$JETON" ] && printf '%s  Remember to add the token to .env before starting.%s\n' "$JAUNE" "$FIN" >&2

# ----------------------------------------------------------------------------- next
echo "Requests are made per project, in the app (Athanor.exe): Requests tab."

titre "Starting"
if grep -qi microsoft /proc/version 2>/dev/null; then
  printf 'To start: double-click %sstart.cmd%s (a minimised window keeps the pilot running).\n' "$GRAS" "$FIN" >&2
  printf 'To test the sandbox first (recommended the first time): test-sandbox.cmd\n' >&2
elif oui_non "Start the pilot now, in the background?" o; then
  commands/start.sh --background
else
  printf 'To start later: commands/start.sh (or commands/start.sh --background)\n' >&2
fi
