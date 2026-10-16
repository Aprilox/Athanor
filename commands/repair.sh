#!/usr/bin/env bash
# Claude repair session launched by the app ("Installation & health" tab).
# Reads the instruction from file $1, works in the kit folder OUTSIDE the sandbox (so it can
# install and fix the tooling itself), without sudo. Output: stream-json flow (the app translates it).
cd "$(dirname "$0")/.." || exit 1
export PATH="$HOME/.local/bin:$PWD/system/bin:$PATH"
if [ -f .env ]; then
  while IFS= read -r l || [ -n "$l" ]; do l=${l%$'\r'}
    [[ "$l" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] && export "${BASH_REMATCH[1]}=${BASH_REMATCH[2]}"; done < .env
fi
[ -f "${1:-}" ] || { echo "Instruction file NOT FOUND: ${1:-}"; exit 1; }
command -v claude >/dev/null 2>&1 || { echo "Claude Code NOT FOUND: repair the \"Claude Code\" line first."; exit 1; }
mkdir -p logs
MODELE_REP="${REPAIR_MODEL:-${MODEL:-}}"
args=(-p "$(cat "$1")" --permission-mode bypassPermissions --permission-prompts none --output-format stream-json --verbose --max-turns 80)
[ -n "$MODELE_REP" ] && args+=(--model "$MODELE_REP")
[ -n "${FALLBACK_MODEL:-}" ] && [ "$MODELE_REP" != "${FALLBACK_MODEL:-}" ] && args+=(--fallback-model "$FALLBACK_MODEL")
export CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1
echo "[reparer] Claude session${MODELE_REP:+ ($MODELE_REP)}: $(basename "$1")"
timeout 1800 claude "${args[@]}" < /dev/null 2>&1 | tee -a logs/reparations.jsonl
code=${PIPESTATUS[0]}
[ -f logs/TODO_FOR_YOU.md ] && { echo; echo "[reparer] Claude left steps for you to do: logs/TODO_FOR_YOU.md"; }
exit "$code"
