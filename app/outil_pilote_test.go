package main

import (
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
  sed -i 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md"
  echo "M1 completed." >> "$ETAT/LOG.md"
  git add -A && git commit -qm "M1: build greeting"
  emit_turn "Running check.sh: CHECK: PASSED"
  emit_result "M1 delivered"
  exit 0
fi

if grep -q '^- \[ \] M2 ' "$ETAT/PLAN.md"; then
  sed -i 's/^- \[ \] M2 /- [x] M2 /' "$ETAT/PLAN.md"
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
  sed -i 's/^- \[ \] Resume:/- [x] Resume:/' "$ETAT/PLAN.md"
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
sed -i 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md"
cat > "$ETAT/FINAL_REPORT.md" <<'EOF'
# Final Report
STATUS: DONE
EOF
git add -A && git commit -qm "M1: completed with fallback"
printf '{"type":"assistant","message":{"usage":{"input_tokens":2000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":200},"content":[{"type":"text","text":"CHECK: PASSED"}]}}\n'
printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Done"}\n'
exit 0
`

// executerPilote: the Go pilot on a throw-away kit and project, started as the application does (athanor
// demarrer, in the foreground here); its output, shown when a check fails.
func executerPilote(t *testing.T, bin, kit, projet, fakeClaude string, extraEnv []string) string {
	t.Helper()
	_ = os.WriteFile(filepath.Join(kit, ".env"), []byte("LANGUAGE=en\n"), 0o644)
	cmd := exec.Command(bin, "demarrer", projet)
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
		fakeClaude := filepath.Join(kit, "fake-claude.sh")
		if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeComplet, "\r\n", "\n")), 0o755); err != nil {
			t.Fatal(err)
		}
		projet := filepath.Join(dossierEssaiPilote(t), "projet")
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
		exclB, _ := os.ReadFile(filepath.Join(projet, ".git", "info", "exclude"))
		verifier(fichierRegulierPilote(filepath.Join(p.Atelier(), "SOLUTIONS.md")) && strings.Contains(string(exclB), ".env") && strings.Contains(string(exclB), ".athanor/"), "SOLUTIONS.md and .git/info/exclude expected")
		verifier(!strings.Contains(string(giB), "Athanor"), "the project's .gitignore untouched: %q", giB)
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
}
