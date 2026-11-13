package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAttenteQuotaPilote(t *testing.T) {
	loc := time.Local
	// Wednesday 14:00 local time
	now := time.Date(2026, 10, 21, 14, 0, 0, 0, loc)
	// "resets 3:30pm" -> 1h30m (5400s) + 120s = 5520s
	if got := attenteQuotaCalcul("You've hit your limit · resets 3:30pm", now, 20); got != 5520 {
		t.Fatalf("resets 3:30pm: got %d, want 5520", got)
	}
	// "resets at 1:30pm" (30 min in the past, < 6h) -> 120s
	if got := attenteQuotaCalcul("Usage limit reached, resets at 1:30pm", now, 20); got != 120 {
		t.Fatalf("resets at 1:30pm (just passed): got %d, want 120", got)
	}
	// Unknown format -> quotaWaitMin * 60
	if got := attenteQuotaCalcul("Rate limit exceeded", now, 25); got != 1500 {
		t.Fatalf("fallback wait: got %d, want 1500", got)
	}
}

func TestLignesEnDirectPilote(t *testing.T) {
	cas := []struct {
		json string
		veut string
	}{
		{
			`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello\nworld"}]}}`,
			"  💬 Hello world",
		},
		{
			`{"type":"assistant","parent_tool_use_id":"tu_1","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"pytest -q"}}]}}`,
			"      ↳ ▶ pytest -q",
		},
		{
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"src/main.go"}}]}}`,
			"  ✎ Write src/main.go",
		},
		{
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"README.md"}}]}}`,
			"  👁 Read README.md",
		},
		{
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Task","input":{"subagent_type":"tester","description":"Run suite"}}]}}`,
			"  👥 Sub-agent tester: Run suite",
		},
		{
			`{"type":"result","result":"Milestone M1 completed"}`,
			"  ■ End of cycle: Milestone M1 completed",
		},
	}
	for _, c := range cas {
		got := lignesEnDirectPilote(c.json)
		if len(got) != 1 || got[0] != c.veut {
			t.Errorf("lignesEnDirectPilote(%s) = %v, want [%q]", c.json, got, c.veut)
		}
	}
}

func TestPriveDePiloteParite(t *testing.T) {
	kit := "/kit"
	h1 := priveDePilote(kit, "/mnt/c/Users/Alice/proj", false)
	h2 := priveDePilote(kit, `C:\Users\Alice\proj`, true)
	h3 := priveDePilote(kit, "/c/Users/Alice/proj", true)
	if filepath.Base(h1) != filepath.Base(h2) || filepath.Base(h1) != filepath.Base(h3) {
		t.Fatalf("priveDePilote mismatch: wsl=%s win=%s gitbash=%s", h1, h2, h3)
	}
}

func dossierEssaiPilote(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return t.TempDir()
	}
	base := filepath.Join("..", "logs")
	_ = os.MkdirAll(base, 0o755)
	d, err := os.MkdirTemp(base, "test-pilote-*")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(abs) })
	return abs
}

// preparerKitEssaiPilote creates a self-contained copy of the kit with the compiled Athanor binary.
func preparerKitEssaiPilote(t *testing.T, binAthanor string) string {
	t.Helper()
	racineSrc, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	kit := filepath.Join(dossierEssaiPilote(t), "kit")
	for _, d := range []string{"template"} {
		if err := copierDossier(filepath.Join(racineSrc, d), filepath.Join(kit, d), true); err != nil {
			t.Fatalf("copy %s: %v", d, err)
		}
	}
	_ = os.MkdirAll(filepath.Join(kit, "logs"), 0o755)
	_ = os.MkdirAll(filepath.Join(kit, "tools", "bin"), 0o755)
	nomBin := "athanor"
	if runtime.GOOS == "windows" {
		nomBin = "Athanor.exe"
	}
	b, err := os.ReadFile(binAthanor)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	dstBin := filepath.Join(kit, nomBin)
	if err := os.WriteFile(dstBin, b, 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	return kit
}

const scriptFauxClaudeComplet = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help)
    echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"
    exit 0 ;;
  --version)
    echo "claude 2.1.285"
    exit 0 ;;
esac

but=""
while [ $# -gt 0 ]; do
  case "$1" in
    -p) but="$2"; shift 2 ;;
    *) shift ;;
  esac
done

ETAT="$PWD/.athanor"
mkdir -p "$ETAT/docs"

emit_turn() {
  local txt="$1" in_tok="${2:-1200}" out_tok="${3:-300}"
  printf '{"type":"assistant","message":{"usage":{"input_tokens":%s,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":%s},"content":[{"type":"text","text":"%s"}]}}\n' "$in_tok" "$out_tok" "$txt"
}

emit_result() {
  local txt="$1" err="${2:-false}" turns="${3:-2}" cost="${4:-0.05}"
  printf '{"type":"result","subtype":"success","is_error":%s,"num_turns":%s,"duration_ms":6000,"duration_api_ms":4000,"total_cost_usd":%s,"usage":{"input_tokens":1200,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":300},"result":"%s"}\n' "$err" "$turns" "$cost" "$txt"
}

if [ ! -f "$ETAT/PLAN.md" ]; then
  cat > "$ETAT/docs/REQUIREMENTS.md" <<'EOF'
# Requirements
Build the greeting app.
EOF
  cat > "$ETAT/docs/ARCHITECTURE.md" <<'EOF'
# Architecture
Single file hello.txt + check.sh.
EOF
  cat > "$ETAT/PLAN.md" <<'EOF'
# Plan

- [ ] M1 : Build greeting and check.sh
- [ ] M2 : Final delivery report
EOF
  echo "Framing completed." >> "$ETAT/LOG.md"
  emit_turn "Framing done: PLAN.md written."
  emit_result "Framing complete"
  exit 0
fi

if grep -q '^- \[ \] M1 ' "$ETAT/PLAN.md"; then
  echo "Hello Athanor" > "$PWD/hello.txt"
  cat > "$ETAT/check.sh" <<'EOF'
#!/usr/bin/env bash
echo "CHECK: PASSED"
EOF
  chmod +x "$ETAT/check.sh"
  sed -i.bak 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
  echo "M1 completed." >> "$ETAT/LOG.md"
  git add -A && git commit -qm "M1: build greeting"
  emit_turn "Running check.sh: CHECK: PASSED"
  emit_result "M1 delivered"
  exit 0
fi

if grep -q '^- \[ \] M2 ' "$ETAT/PLAN.md"; then
  sed -i.bak 's/^- \[ \] M2 /- [x] M2 /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
  cat > "$ETAT/FINAL_REPORT.md" <<'EOF'
# Final Report

Everything is tested and ready.
STATUS: DONE
EOF
  echo "M2 completed." >> "$ETAT/LOG.md"
  git add -A && git commit -qm "M2: initial delivery"
  emit_turn "CHECK: PASSED - Initial delivery ready"
  emit_result "M2 delivered"
  exit 0
fi

if grep -q '^- \[ \] Resume:' "$ETAT/PLAN.md"; then
  echo "Extra feature" >> "$PWD/hello.txt"
  sed -i.bak 's/^- \[ \] Resume:/- [x] Resume:/' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
  cat > "$ETAT/FINAL_REPORT.md" <<'EOF'
# Final Report

Extra request delivered too.
STATUS: DONE
EOF
  echo "Extra request completed." >> "$ETAT/LOG.md"
  git add -A && git commit -qm "M3: extra request delivered"
  emit_turn "CHECK: PASSED - Extra request delivered"
  emit_result "Extra request delivered"
  exit 0
fi

emit_turn "Nothing left"
emit_result "Done"
exit 0
`

const scriptFauxClaudeRelaisQuota = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help)
    echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"
    exit 0 ;;
  --version)
    echo "claude 2.1.285"
    exit 0 ;;
esac

modele=""
prompt_file=""
prompt_txt=""
while [ $# -gt 0 ]; do
  case "$1" in
    --model) modele="$2"; shift 2 ;;
    --append-system-prompt-file) prompt_file="$2"; shift 2 ;;
    --append-system-prompt) prompt_txt="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$prompt_file" ] && [ -f "$prompt_file" ] && prompt_txt=$(cat "$prompt_file")

ETAT="$PWD/.athanor"
COMPTEUR="$ETAT/appel_test_n"
n=0
[ -f "$COMPTEUR" ] && n=$(cat "$COMPTEUR")
n=$(( n + 1 ))
echo "$n" > "$COMPTEUR"

if [ "$n" = 1 ]; then
  trap 'exit 0' TERM INT
  echo "Step 1 done, step 2 remaining" > "$ETAT/HANDOFF.md"
  printf '{"type":"assistant","message":{"usage":{"input_tokens":160000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":500},"content":[{"type":"text","text":"Working on M1, context is now 160k tokens"}]}}\n'
  sleep 15 & wait $!
  exit 0
fi

if [ "$n" = 2 ]; then
  if printf '%s' "$prompt_txt" | grep -q "Step 1 done, step 2 remaining"; then
    echo "relay_ok" > "$ETAT/relay_seen"
  fi
  printf '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"model_limit"}}\n'
  printf '{"type":"result","subtype":"error","is_error":true,"num_turns":1,"duration_ms":1000,"duration_api_ms":500,"total_cost_usd":0.01,"usage":{"input_tokens":500,"output_tokens":20},"result":"You have hit your usage limit · resets in 5m"}\n'
  exit 1
fi

echo "$modele" > "$ETAT/fallback_seen"
echo "Finished on fallback" > "$PWD/app.txt"
sed -i.bak 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
cat > "$ETAT/FINAL_REPORT.md" <<'EOF'
# Final Report
STATUS: DONE
EOF
git add -A && git commit -qm "M1: completed with fallback"
printf '{"type":"assistant","message":{"usage":{"input_tokens":2000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":200},"content":[{"type":"text","text":"CHECK: PASSED"}]}}\n'
printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Done"}\n'
exit 0
`

// scriptFauxClaudeLivraison: every call builds every unticked milestone of the plan and delivers; it notes
// its call and whether the plan asked it to take up what changed in REQUEST.md.
const scriptFauxClaudeLivraison = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help)
    echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"
    exit 0 ;;
  --version)
    echo "claude 2.1.285"
    exit 0 ;;
esac
ETAT="$PWD/.athanor"
n=0
[ -f "$ETAT/appel_test_n" ] && n=$(cat "$ETAT/appel_test_n")
n=$(( n + 1 ))
echo "$n" > "$ETAT/appel_test_n"
if grep -q "^- \\[ \\] Resume: what changed in .athanor/REQUEST.md" "$ETAT/PLAN.md" && grep -q "New in REQUEST.md: .*dark mode" "$ETAT/PLAN.md"; then
  echo "$n reprise" >> "$ETAT/appels_vus"
else
  echo "$n" >> "$ETAT/appels_vus"
fi
echo "build $n" >> "$PWD/app.txt"
sed -i.bak 's/^- \[ \] /- [x] /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
printf '# Final Report\nSTATUS: DONE\n' > "$ETAT/FINAL_REPORT.md"
git add -A && git commit -qm "build $n"
printf '{"type":"assistant","message":{"usage":{"input_tokens":2000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":200},"content":[{"type":"text","text":"CHECK: PASSED"}]}}\n'
printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Done"}\n'
exit 0
`

// scriptFauxClaudeEco: call 1 (eco on) notes what it received and switches the eco mode off as the app
// would; call 2 notes it again and delivers.
const scriptFauxClaudeEco = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help)
    echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"
    exit 0 ;;
  --version)
    echo "claude 2.1.285"
    exit 0 ;;
esac
prompt_txt=""
prompt_file=""
while [ $# -gt 0 ]; do
  case "$1" in
    --append-system-prompt-file) prompt_file="$2"; shift 2 ;;
    --append-system-prompt) prompt_txt="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$prompt_file" ] && [ -f "$prompt_file" ] && prompt_txt=$(cat "$prompt_file")
ETAT="$PWD/.athanor"
n=0
[ -f "$ETAT/appel_test_n" ] && n=$(cat "$ETAT/appel_test_n")
n=$(( n + 1 ))
echo "$n" > "$ETAT/appel_test_n"
eco=non
printf '%s' "$prompt_txt" | grep -q "## Eco mode (active for this project)" && eco=oui
pony=non
printf '%s' "$prompt_txt" | grep -q "## Ponytail: the smallest complete change" && pony=oui
echo "$n $eco ${BASH_MAX_OUTPUT_LENGTH:-} $pony" >> "$ETAT/eco_vu"
if [ "$n" = 1 ]; then
  sed -i.bak 's/^ECO=1/ECO=0/' "$ETAT/.env" && rm -f "$ETAT/.env.bak"
  echo "part 1" > "$PWD/app.txt"
  git add -A && git commit -qm "M1: part 1"
  printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Part 1"}\n'
  exit 0
fi
echo "done" >> "$PWD/app.txt"
sed -i.bak 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
printf '# Final Report\nSTATUS: DONE\n' > "$ETAT/FINAL_REPORT.md"
git add -A && git commit -qm "M1: done"
printf '{"type":"assistant","message":{"usage":{"input_tokens":2000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":200},"content":[{"type":"text","text":"CHECK: PASSED"}]}}\n'
printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Done"}\n'
exit 0
`

// scriptFauxClaudeRapide: call 1 (fast and eco on) notes what it received and switches the fast mode off as
// the app would, the whole verification still owed; call 2 (verification owed) delivers without it; call 3
// pays the debt (deletes .athanor/FAST_DEBT) and delivers.
const scriptFauxClaudeRapide = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help)
    echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"
    exit 0 ;;
  --version)
    echo "claude 2.1.285"
    exit 0 ;;
esac
prompt_txt=""
prompt_file=""
while [ $# -gt 0 ]; do
  case "$1" in
    --append-system-prompt-file) prompt_file="$2"; shift 2 ;;
    --append-system-prompt) prompt_txt="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$prompt_file" ] && [ -f "$prompt_file" ] && prompt_txt=$(cat "$prompt_file")
ETAT="$PWD/.athanor"
n=0
[ -f "$ETAT/appel_test_n" ] && n=$(cat "$ETAT/appel_test_n")
n=$(( n + 1 ))
echo "$n" > "$ETAT/appel_test_n"
rapide=non
printf '%s' "$prompt_txt" | grep -q "## Fast mode (active for this project)" && rapide=oui
eco=non
printf '%s' "$prompt_txt" | grep -q "With the eco mode:" && eco=oui
due=non
printf '%s' "$prompt_txt" | grep -q "## Verification owed (the fast mode was switched off)" && due=oui
dette=non
[ -f "$ETAT/FAST_DEBT" ] && dette=oui
rapport=aucun
[ -f "$ETAT/FINAL_REPORT.md" ] && rapport=$(grep -o "STATUS: [A-Z]*" "$ETAT/FINAL_REPORT.md" | head -n 1)
echo "$n $rapide $eco $due $dette $rapport" >> "$ETAT/rapide_vu"
if [ "$n" = 1 ]; then
  sed -i.bak 's/^FAST=1/FAST=0/' "$ETAT/.env" && rm -f "$ETAT/.env.bak"
  echo "part 1" > "$PWD/app.txt"
  git add -A && git commit -qm "M1: part 1"
  printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Part 1"}\n'
  exit 0
fi
[ "$n" = 3 ] && rm -f "$ETAT/FAST_DEBT"
echo "done $n" >> "$PWD/app.txt"
sed -i.bak 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
printf '# Final Report\nSTATUS: DONE\n' > "$ETAT/FINAL_REPORT.md"
git add -A && git commit -qm "M1: done $n"
printf '{"type":"assistant","message":{"usage":{"input_tokens":2000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":200},"content":[{"type":"text","text":"CHECK: PASSED"}]}}\n'
printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Done"}\n'
exit 0
`

// scriptFauxClaudeParallele: the lead alone builds the skeleton (M1); a parallel worker notes what it received
// in a file of its milestone and commits on its branch; the lead's verification after the merges ticks the
// milestones; the lead alone then delivers
// (the fast mode's whole verification counted as done: .athanor/FAST_DEBT deleted).
const scriptFauxClaudeParallele = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help)
    echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"
    exit 0 ;;
  --version)
    echo "claude 2.1.285"
    exit 0 ;;
esac
prompt_txt=""
prompt_file=""
while [ $# -gt 0 ]; do
  case "$1" in
    --append-system-prompt-file) prompt_file="$2"; shift 2 ;;
    --append-system-prompt) prompt_txt="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$prompt_file" ] && [ -f "$prompt_file" ] && prompt_txt=$(cat "$prompt_file")
ETAT="$PWD/.athanor"
a() { printf '%s' "$prompt_txt" | grep -q -- "$1" && echo oui || echo non; }
eco=$(a "## Eco mode (active for this project)")
rapide=$(a "## Fast mode (active for this project)")
resultat() {
  printf '{"type":"assistant","message":{"usage":{"input_tokens":2000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":200},"content":[{"type":"text","text":"CHECK: PASSED"}]}}\n'
  printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":2000,"output_tokens":200},"result":"%s"}\n' "$1"
  exit 0
}
if [ "$(a "## You are a parallel worker")" = oui ]; then
  id=$(printf '%s' "$prompt_txt" | grep -o 'auto/M[0-9]*' | head -n 1 | sed 's|auto/||')
  echo "$id $eco $rapide ${BASH_MAX_OUTPUT_LENGTH:-}" > "$PWD/vu-$id.js"
  git add -A && git commit -qm "$id: built"
  resultat "Report of $id"
fi
if [ "$(a "## Verification after a parallel cycle")" = oui ]; then
  echo "verif $eco $(a "the full check waits for the final verification") $(a "one\*\* full check") ${BASH_MAX_OUTPUT_LENGTH:-}" >> "$ETAT/para_vu"
  for id in M2 M3 M4; do
    [ -f "$PWD/vu-$id.js" ] && sed -i.bak "s/^- \[ \] $id /- [x] $id /" "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
  done
  echo "verified" >> "$PWD/app.txt"
  git add -A && git commit -qm "Verify the parallel cycle"
  resultat "Verified"
fi
dette=non
[ -f "$ETAT/FAST_DEBT" ] && dette=oui
echo "chef $eco $rapide $dette" >> "$ETAT/para_vu"
if grep -q '^- \[ \] M1 ' "$ETAT/PLAN.md"; then
  echo "skeleton" > "$PWD/app.txt"
  sed -i.bak 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
  git add -A && git commit -qm "M1: Skeleton"
  resultat "Skeleton"
fi
rm -f "$ETAT/FAST_DEBT"
echo "delivered" >> "$PWD/app.txt"
printf '# Final Report\nSTATUS: DONE\n' > "$ETAT/FINAL_REPORT.md"
git add -A && git commit -qm "Deliver"
resultat "Done"
`

// executerPilote: the Go pilot on a throw-away kit and project, started as the application does (athanor
// demarrer, in the foreground here); its output, shown when a check fails.
func executerPilote(t *testing.T, bin, kit, projet, fakeClaude string, extraEnv []string) string {
	t.Helper()
	_ = os.WriteFile(filepath.Join(kit, ".env"), []byte("LANGUAGE=en\n"), 0o644)
	// (never longer than 5 minutes: a pilot that would hang says what it did instead)
	ctx, fin := context.WithTimeout(context.Background(), 5*time.Minute)
	defer fin()
	cmd := exec.CommandContext(ctx, bin, "demarrer", projet)
	cmd.WaitDelay = 10 * time.Second
	cmd.Dir = kit
	env := append(os.Environ(),
		"KIT="+kit,
		"CLAUDE_BIN="+fakeClaude,
		"CLAUDE_CODE_OAUTH_TOKEN=fake-token-for-test",
		"KEEP_AWAKE=0",
		"CLOSE_AFTER_MILESTONE=0",
		"WATCH_INTERVAL=1",
		"SECONDS_PER_MINUTE=1",
		"QUOTA_WAIT_MIN=1",
		"ERROR_WAIT_MIN=0",
	)
	cmd.Env = append(env, extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pilot failed: %v\nOutput:\n%s", err, string(out))
	}
	return string(out)
}

func TestBancPilote(t *testing.T) {
	if runtime.GOOS == "windows" && trouverBashNatifPilote() == "" {
		t.Skip("Git Bash not found on Windows (the fake Claude Code is a bash script)")
	}
	binTemp := filepath.Join(t.TempDir(), "athanor-test-bin")
	if runtime.GOOS == "windows" {
		binTemp += ".exe"
	}
	if _, err := exec.LookPath("go"); err == nil {
		if out, err := exec.Command("go", "build", "-o", binTemp, ".").CombinedOutput(); err != nil {
			t.Fatalf("go build failed: %v\n%s", err, string(out))
		}
	} else {
		t.Skip("go not available")
	}

	t.Run("parcours_complet", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		vrai := filepath.Join(kit, "fake-claude-complet.sh")
		if err := os.WriteFile(vrai, []byte(strings.ReplaceAll(scriptFauxClaudeComplet, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		// what the pilot says of its version while it runs (seen by Claude's session), then gone at the end
		vuVersion := filepath.Join(kit, "vu-version")
		fakeClaude := filepath.Join(kit, "fake-claude.sh")
		// each call's system prompt, one file per call (Ponytail: its lean work for the framing, its rules after)
		appels := filepath.Join(kit, "appels")
		_ = os.MkdirAll(appels, 0o755)
		if err := os.WriteFile(fakeClaude, []byte("#!/bin/bash\ncp '"+filepath.ToSlash(filepath.Join(projet, ".athanor", "pilot.version"))+"' '"+filepath.ToSlash(vuVersion)+"' 2>/dev/null\n"+
			"n=$(ls '"+filepath.ToSlash(appels)+"' | wc -l); f='"+filepath.ToSlash(appels)+"'/$((n+1)); : > \"$f\"\n"+
			"prec=; for a in \"$@\"; do [ \"$prec\" = --append-system-prompt-file ] && cat \"$a\" >> \"$f\"; [ \"$prec\" = --append-system-prompt ] && printf '%s' \"$a\" >> \"$f\"; prec=$a; done\n"+
			"exec bash '"+filepath.ToSlash(vrai)+"' \"$@\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(filepath.Join(projet, ".athanor", "queue"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# My Request\n\nBuild a greeting app.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "queue", "01-extra.md"), []byte("# Extra\n\nAdd an extra line.\n"), 0o644)
		sortie := executerPilote(t, binTemp, kit, projet, fakeClaude, nil)

		p := Projet{Nom: "test", Dossier: projet}
		etat, ok := p.LireEtat()
		if !ok {
			t.Fatalf("STATE.json missing or invalid\n%s", sortie)
		}
		var etatsStats []string
		for _, s := range p.LireStats() {
			etatsStats = append(etatsStats, s.Cycle+":"+s.Etat)
		}
		logOut, _ := gitP(projet, nil, "log", "--format=%s").Output()
		commits := strings.Split(strings.TrimSpace(string(logOut)), "\n")
		helloB, _ := os.ReadFile(filepath.Join(projet, "hello.txt"))
		giB, _ := os.ReadFile(filepath.Join(projet, ".gitignore"))
		rapB, _ := os.ReadFile(filepath.Join(p.Atelier(), "FINAL_REPORT.md"))
		echec := false
		verifier := func(ok bool, format string, a ...any) {
			if !ok {
				echec = true
				t.Errorf(format, a...)
			}
		}
		verifier(strings.Join(etatsStats, ",") == "1:ok,2:ok,3:ok,4:ok", "stats.tsv cycles/states: %v", etatsStats)
		verifier(etat.Phase == "entre_cycles" && strings.HasPrefix(etat.Etat, "DONE") && etat.Cycle == 4 && etat.DemandesEnAttente == 0, "STATE.json: %+v", etat)
		verifier(len(commits) >= 3 && commits[0] == "M3: extra request delivered", "git commits: %v", commits)
		verifier(strings.TrimSpace(string(helloB)) == "Hello Athanor\nExtra feature", "hello.txt: %q", helloB)
		verifier(strings.Contains(string(rapB), "STATUS: DONE"), "FINAL_REPORT.md: %q", rapB)
		vuB, _ := os.ReadFile(vuVersion)
		verifier(strings.TrimSpace(string(vuB)) == versionAthanor, "pilot.version while it ran: %q", vuB)
		verifier(!fichierExiste(filepath.Join(p.Atelier(), "pilot.version")), "pilot.version left after the pilot stopped")
		exclB, _ := os.ReadFile(filepath.Join(projet, ".git", "info", "exclude"))
		verifier(fichierRegulierPilote(filepath.Join(p.Atelier(), "SOLUTIONS.md")) && strings.Contains(string(exclB), ".env") && strings.Contains(string(exclB), ".athanor/"), "SOLUTIONS.md and .git/info/exclude expected")
		verifier(!strings.Contains(string(giB), "Athanor"), "the project's .gitignore untouched: %q", giB)
		var cadrageVu, constructionVu bool
		ls, _ := os.ReadDir(appels)
		for _, f := range ls {
			b, _ := os.ReadFile(filepath.Join(appels, f.Name()))
			if len(b) == 0 {
				continue // --help, --version
			}
			lean, code := strings.Contains(string(b), "## Ponytail: lean work"), strings.Contains(string(b), "## Ponytail: the smallest complete change")
			cadrageVu = cadrageVu || (lean && !code)
			constructionVu = constructionVu || (code && !lean)
		}
		verifier(cadrageVu && constructionVu, "Ponytail: the framing with its lean work, the building cycles with its rules")
		if echec {
			t.Logf("pilot output:\n%s", sortie)
		}
	})

	t.Run("relais_et_quota", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		fakeClaude := filepath.Join(kit, "fake-claude-rq.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeRelaisQuota, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild feature M1.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : Core feature\n"), 0o644)
		sortie := executerPilote(t, binTemp, kit, projet, fakeClaude, []string{"MODEL=opus-principal", "FALLBACK_MODEL=sonnet-secours", "RELAY_CONTEXT_K=150"})

		p := Projet{Nom: "test", Dossier: projet}
		etat, _ := p.LireEtat()
		var etatsStats, modelesStats []string
		for _, s := range p.LireStats() {
			etatsStats = append(etatsStats, s.Cycle+":"+s.Etat)
			modelesStats = append(modelesStats, s.Modele)
		}
		fbB, _ := os.ReadFile(filepath.Join(p.Atelier(), "fallback_seen"))
		echec := false
		verifier := func(ok bool, format string, a ...any) {
			if !ok {
				echec = true
				t.Errorf(format, a...)
			}
		}
		verifier(strings.Join(etatsStats, ",") == "1:relais,2:quota,3:ok", "stats states: %v", etatsStats)
		verifier(strings.Join(modelesStats, ",") == "opus-principal,opus-principal,sonnet-secours", "stats models: %v", modelesStats)
		verifier(fichierRegulierPilote(filepath.Join(p.Atelier(), "relay_seen")), "relay notes not passed to cycle 2")
		verifier(strings.TrimSpace(string(fbB)) == "sonnet-secours", "fallback model not used on cycle 3: %q", fbB)
		verifier(etat.Phase == "entre_cycles" && strings.HasPrefix(etat.Etat, "DONE"), "final state: %+v", etat)
		if echec {
			t.Logf("pilot output:\n%s", sortie)
		}
	})
	// API mode: the account's credit used up, a new key saved in Settings meanwhile: the pilot's long wait
	// cut short, the next cycle on the new key only (never the subscription's token), its cost written
	t.Run("api_credit_nouvelle_cle", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		fakeClaude := filepath.Join(kit, "fake-claude-credit.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeCredit, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild M1.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : Core feature\n"), 0o644)
		debut := time.Now()
		// LOGIN_WAIT_MIN=600: ten minutes of wait (one second a minute here) if the new key did not cut it short
		sortie := executerPilote(t, binTemp, kit, projet, fakeClaude, []string{"CLAUDE_AUTH=api", "ANTHROPIC_API_KEY=sk-ant-sans-credit", "LOGIN_WAIT_MIN=600"})
		p := Projet{Nom: "test", Dossier: projet}
		etat, _ := p.LireEtat()
		vues, _ := os.ReadFile(filepath.Join(p.Atelier(), "cles_vues"))
		journal, _ := os.ReadFile(filepath.Join(p.Atelier(), "pilot.log"))
		d, n := depenseAPI(kit, 0)
		echec := false
		verifier := func(ok bool, format string, a ...any) {
			if !ok {
				echec = true
				t.Errorf(format, a...)
			}
		}
		verifier(strings.TrimSpace(string(vues)) == "sk-ant-sans-credit|none\nsk-ant-nouvelle-cle|none", "the keys Claude Code got: %q", vues)
		verifier(strings.Contains(string(journal), "no credit left") && strings.Contains(string(journal), "Connection to Claude changed in Settings"), "pilot.log:\n%s", journal)
		verifier(time.Since(debut) < 3*time.Minute, "the wait was not cut short: %v", time.Since(debut))
		verifier(d == 0.03 && n == 1, "the API spend: %v (%d)", d, n)
		verifier(strings.HasPrefix(etat.Etat, "DONE"), "final state: %+v", etat)
		if echec {
			t.Logf("pilot output:\n%s", sortie)
		}
	})
	// API mode: 0.02 $ spent already, the automatic stop at 0.05 $: the first cycle (0.03 $) reaches it, the
	// pilot starts no other one and stops
	t.Run("api_arret", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		fakeClaude := filepath.Join(kit, "fake-claude-arret.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeArret, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild M1 to M3.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : One\n- [ ] M2 : Two\n- [ ] M3 : Three\n"), 0o644)
		sortie := executerPilote(t, binTemp, kit, projet, fakeClaude, []string{"CLAUDE_AUTH=api", "ANTHROPIC_API_KEY=sk-ant-essai", "API_SPENT_USD=0.02", "API_STOP_USD=0.05"})
		p := Projet{Nom: "test", Dossier: projet}
		plan, _ := os.ReadFile(filepath.Join(p.Atelier(), "PLAN.md"))
		journal, _ := os.ReadFile(filepath.Join(p.Atelier(), "pilot.log"))
		if strings.Count(string(plan), "- [x]") != 1 || !strings.Contains(string(journal), "API spend at 0.05 $, past the automatic stop at 0.05 $") {
			t.Errorf("plan:\n%s\npilot.log:\n%s\noutput:\n%s", plan, journal, sortie)
		}
	})
	t.Run("livre_puis_remarque", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		fakeClaude := filepath.Join(kit, "fake-claude-livraison.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeLivraison, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild feature M1.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : Core feature\n"), 0o644)
		p := Projet{Nom: "test", Dossier: projet}
		vus := func() string {
			b, _ := os.ReadFile(filepath.Join(p.Atelier(), "appels_vus"))
			return strings.TrimSpace(string(b))
		}
		echec := false
		verifier := func(ok bool, format string, a ...any) {
			if !ok {
				echec = true
				t.Errorf(format, a...)
			}
		}
		var sorties []string
		// 1. delivered
		sorties = append(sorties, executerPilote(t, binTemp, kit, projet, fakeClaude, nil))
		verifier(vus() == "1" && projetLivre(p) && fichierRegulierPilote(fichierLivraison(p.Atelier())), "delivery 1: %q", vus())
		// 2. started again, nothing new: it ends at once, Claude never called
		out := executerPilote(t, binTemp, kit, projet, fakeClaude, nil)
		sorties = append(sorties, out)
		verifier(vus() == "1" && strings.Contains(out, "To go on: add a request or a note"), "nothing new: %q", vus())
		// 3. a note: the pilot reopens the project, Claude takes it up, delivers again
		if err := p.AjouterRemarque("Add a dark mode", "## Notes", "(notes)"); err != nil {
			t.Fatal(err)
		}
		out = executerPilote(t, binTemp, kit, projet, fakeClaude, nil)
		sorties = append(sorties, out)
		plan, _ := os.ReadFile(filepath.Join(p.Atelier(), "PLAN.md"))
		verifier(vus() == "1\n2 reprise" && projetLivre(p) && strings.Contains(out, "Delivered project reopened: REQUEST.md changed"), "the note: %q", vus())
		verifier(strings.Contains(string(plan), "- [x] Resume: what changed in .athanor/REQUEST.md"), "plan: %q", plan)
		// 4. again nothing new
		sorties = append(sorties, executerPilote(t, binTemp, kit, projet, fakeClaude, nil))
		verifier(vus() == "1\n2 reprise", "after the second delivery: %q", vus())
		// 5. a milestone added by hand
		f, _ := os.OpenFile(filepath.Join(p.Atelier(), "PLAN.md"), os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString("- [ ] M9 : Export to CSV\n")
		f.Close()
		sorties = append(sorties, executerPilote(t, binTemp, kit, projet, fakeClaude, nil))
		verifier(vus() == "1\n2 reprise\n3" && projetLivre(p), "the milestone added: %q", vus())
		if echec {
			t.Logf("pilot outputs:\n%s", strings.Join(sorties, "\n=====\n"))
		}
	})
	t.Run("mode_eco", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		fakeClaude := filepath.Join(kit, "fake-claude-eco.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeEco, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild feature M1.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : Core feature\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", ".env"), []byte("ECO=1\n"), 0o644)
		sortie := executerPilote(t, binTemp, kit, projet, fakeClaude, nil)
		vu, _ := os.ReadFile(filepath.Join(projet, ".athanor", "eco_vu"))
		p := Projet{Nom: "test", Dossier: projet}
		etat, _ := p.LireEtat()
		echec := false
		verifier := func(ok bool, format string, a ...any) {
			if !ok {
				echec = true
				t.Errorf(format, a...)
			}
		}
		verifier(strings.TrimSpace(string(vu)) == "1 oui 8000 oui\n2 non 20000 oui", "what Claude received: %q", vu)
		var opts []string
		for _, st := range p.LireStats() {
			opts = append(opts, st.Options)
		}
		verifier(strings.Join(opts, "|") == "ponytail,eco|ponytail", "stats options: %q", opts)
		verifier(strings.Contains(sortie, "Ponytail "+versionPonytail+" on"), "Ponytail announced")
		verifier(strings.Contains(sortie, "Eco mode on") && !strings.Contains(sortie, "the parallel mode is off") && strings.Contains(sortie, "Eco mode off"), "eco messages in the pilot's log")
		verifier(strings.HasPrefix(etat.Etat, "DONE"), "final state: %+v", etat)
		if echec {
			t.Logf("pilot output:\n%s", sortie)
		}
	})
	// the fast mode with the eco mode: its rules reach Claude, the debt file is written, a delivery made while
	// the whole verification is owed is reopened, with the mode on or switched off
	t.Run("mode_rapide", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		fakeClaude := filepath.Join(kit, "fake-claude-rapide.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeRapide, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild feature M1.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : Core feature\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", ".env"), []byte("ECO=1\nFAST=1\n"), 0o644)
		sortie := executerPilote(t, binTemp, kit, projet, fakeClaude, nil)
		vu, _ := os.ReadFile(filepath.Join(projet, ".athanor", "rapide_vu"))
		p := Projet{Nom: "test", Dossier: projet}
		etat, _ := p.LireEtat()
		echec := false
		verifier := func(ok bool, format string, a ...any) {
			if !ok {
				echec = true
				t.Errorf(format, a...)
			}
		}
		// call: fast rules, their eco line, verification owed, debt file, state of the final report
		verifier(strings.TrimSpace(string(vu)) == "1 oui oui non oui aucun\n2 non non oui oui aucun\n3 non non oui oui STATUS: REOPENED", "what Claude received: %q", vu)
		var opts []string
		for _, st := range p.LireStats() {
			opts = append(opts, st.Options)
		}
		verifier(strings.Join(opts, "|") == "ponytail,eco,fast|ponytail,eco|ponytail,eco", "stats options: %q", opts)
		verifier(strings.Contains(sortie, "Fast mode on") && strings.Contains(sortie, "Fast mode off"), "fast messages in the pilot's log")
		verifier(strings.Count(sortie, "Delivery reopened: what was built in the fast mode") == 1, "the delivery without the verification is reopened once")
		verifier(strings.HasPrefix(etat.Etat, "DONE") && projetLivre(p), "final state: %+v", etat)
		verifier(!fichierRegulierPilote(filepath.Join(p.Atelier(), detteRapide)), "the debt file is gone")
		if echec {
			t.Logf("pilot output:\n%s", sortie)
		}
	})
	// the three modes together: the workers get the eco rules and its short outputs, the lead verifies the
	// merges with quick checks (the fast mode), then delivers alone once the whole verification is done
	t.Run("parallele_eco_rapide", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		fakeClaude := filepath.Join(kit, "fake-claude-parallele.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeParallele, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild M1 to M4.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : Skeleton — Files: app.txt. Depends on: none. Acceptance: it runs.\n"+
			"- [ ] M2 : Two — Files: vu-M2.js. Depends on: M1. Acceptance: the file exists.\n"+
			"- [ ] M3 : Three — Files: vu-M3.js. Depends on: M1. Acceptance: the file exists.\n"+
			"- [ ] M4 : Four — Files: vu-M4.js. Depends on: M1. Acceptance: the file exists.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", ".env"), []byte("PARALLEL=1\nPARALLEL_MAX=3\nECO=1\nFAST=1\n"), 0o644)
		sortie := executerPilote(t, binTemp, kit, projet, fakeClaude, nil)
		p := Projet{Nom: "test", Dossier: projet}
		etat, _ := p.LireEtat()
		echec := false
		verifier := func(ok bool, format string, a ...any) {
			if !ok {
				echec = true
				t.Errorf(format, a...)
			}
		}
		// each worker: the eco rules (the worker's own), not the lead's fast rules, the short outputs
		for _, id := range []string{"M2", "M3", "M4"} {
			vu, _ := os.ReadFile(filepath.Join(projet, "vu-"+id+".js"))
			verifier(strings.TrimSpace(string(vu)) == id+" oui non 8000", "worker %s received: %q", id, vu)
		}
		// the verification: the eco rules, the quick check instead of the full one; then the lead alone, with
		// the fast rules and the debt file
		vu, _ := os.ReadFile(filepath.Join(p.Atelier(), "para_vu"))
		verifier(strings.TrimSpace(string(vu)) == "chef oui oui oui\nverif oui oui non 8000\nchef oui oui oui", "what the lead received: %q", vu)
		plan, _ := os.ReadFile(filepath.Join(p.Atelier(), "PLAN.md"))
		verifier(strings.Count(string(plan), "- [x]") == 4, "plan:\n%s", plan)
		verifier(strings.Contains(sortie, "Eco mode on") && strings.Contains(sortie, "Fast mode on") && strings.Contains(sortie, "Workers finished"), "the modes and the workers in the pilot's log")
		verifier(!strings.Contains(sortie, "Delivery reopened"), "the delivery made after the verification is accepted")
		verifier(strings.HasPrefix(etat.Etat, "DONE") && projetLivre(p), "final state: %+v", etat)
		if echec {
			t.Logf("pilot output:\n%s", sortie)
		}
	})
	t.Run("sans_ponytail", func(t *testing.T) {
		kit := preparerKitEssaiPilote(t, binTemp)
		fakeClaude := filepath.Join(kit, "fake-claude-eco.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeEco, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
		_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild feature M1.\n"), 0o644)
		_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : Core feature\n"), 0o644)
		sortie := executerPilote(t, binTemp, kit, projet, fakeClaude, []string{"PONYTAIL=0"})
		vu, _ := os.ReadFile(filepath.Join(projet, ".athanor", "eco_vu"))
		p := Projet{Nom: "test", Dossier: projet}
		etat, _ := p.LireEtat()
		echec := false
		verifier := func(ok bool, format string, a ...any) {
			if !ok {
				echec = true
				t.Errorf(format, a...)
			}
		}
		verifier(strings.TrimSpace(string(vu)) == "1 non 20000 non\n2 non 20000 non", "what Claude received: %q", vu)
		var opts []string
		for _, st := range p.LireStats() {
			opts = append(opts, st.Options)
		}
		verifier(strings.Join(opts, "|") == "|", "stats options: %q", opts)
		verifier(!strings.Contains(sortie, "Ponytail") && !strings.Contains(sortie, "Eco mode"), "nothing announced")
		verifier(strings.HasPrefix(etat.Etat, "DONE"), "final state: %+v", etat)
		if echec {
			t.Logf("pilot output:\n%s", sortie)
		}
	})
}
