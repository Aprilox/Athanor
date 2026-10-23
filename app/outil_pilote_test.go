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
	for _, d := range []string{"system", "template", "commands"} {
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

func executerPiloteMoteur(t *testing.T, kit, projet, fakeClaude, moteur string, extraEnv []string) {
	t.Helper()
	bash := "bash"
	if runtime.GOOS == "windows" {
		bash = trouverBashNatifPilote()
		if bash == "" {
			t.Skip("Git Bash not found on Windows")
		}
	}
	pilotSh := filepath.ToSlash(filepath.Join(kit, "system", "pilot.sh"))
	projArg := filepath.ToSlash(projet)
	cmd := exec.Command(bash, pilotSh, projArg)
	cmd.Dir = kit
	env := append(os.Environ(),
		"PILOT_ENGINE="+moteur,
		"CLAUDE_BIN="+filepath.ToSlash(fakeClaude),
		"CLAUDE_CODE_OAUTH_TOKEN=fake-token-for-test",
		"KEEP_AWAKE=0",
		"CLOSE_AFTER_MILESTONE=0",
		"WATCH_INTERVAL=1",
		"SECONDS_PER_MINUTE=1",
		"QUOTA_WAIT_MIN=1",
		"ERROR_WAIT_MIN=0",
	)
	env = append(env, extraEnv...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pilot (%s) failed: %v\nOutput:\n%s", moteur, err, string(out))
	}
}

func TestBancPiloteDeuxMoteurs(t *testing.T) {
	binTemp := filepath.Join(t.TempDir(), "athanor-test-bin")
	if runtime.GOOS == "windows" {
		binTemp += ".exe"
	}
	if _, err := exec.LookPath("go"); err == nil {
		if out, err := exec.Command("go", "build", "-o", binTemp, ".").CombinedOutput(); err != nil {
			t.Fatalf("go build failed: %v\n%s", err, string(out))
		}
	} else if st, err := os.Stat(filepath.Join("..", "athanor")); err == nil && !st.IsDir() {
		binTemp = filepath.Join("..", "athanor")
	} else {
		t.Skip("neither go nor ../athanor available")
	}

	t.Run("parcours_complet", func(t *testing.T) {
		type resMoteur struct {
			plan, rapport, hello string
			etat                 Etat
			lignesStats          int
			etatsStats           []string
			commits              []string
			aSolutions           bool
			aGitignore           bool
		}
		executer := func(moteur string) resMoteur {
			kit := preparerKitEssaiPilote(t, binTemp)
			fakeClaude := filepath.Join(kit, "fake-claude.sh")
			if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeComplet, "\r\n", "\n")), 0o755); err != nil {
				t.Fatal(err)
			}
			projet := filepath.Join(dossierEssaiPilote(t), "projet")
			_ = os.MkdirAll(filepath.Join(projet, ".athanor", "queue"), 0o755)
			_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# My Request\n\nBuild a greeting app.\n"), 0o644)
			_ = os.WriteFile(filepath.Join(projet, ".athanor", "queue", "01-extra.md"), []byte("# Extra\n\nAdd an extra line.\n"), 0o644)

			executerPiloteMoteur(t, kit, projet, fakeClaude, moteur, nil)

			p := Projet{Nom: "test", Dossier: projet}
			etat, ok := p.LireEtat()
			if !ok {
				t.Fatalf("[%s] STATE.json missing or invalid", moteur)
			}
			stats := p.LireStats()
			var etatsStats []string
			for _, s := range stats {
				etatsStats = append(etatsStats, s.Cycle+":"+s.Etat)
			}
			logOut, _ := gitP(projet, nil, "log", "--format=%s").Output()
			commits := strings.Split(strings.TrimSpace(string(logOut)), "\n")
			planB, _ := os.ReadFile(p.Plan())
			rapB, _ := os.ReadFile(filepath.Join(p.Atelier(), "FINAL_REPORT.md"))
			helloB, _ := os.ReadFile(filepath.Join(projet, "hello.txt"))
			giB, _ := os.ReadFile(filepath.Join(projet, ".gitignore"))
			return resMoteur{
				plan:        strings.TrimSpace(string(planB)),
				rapport:     strings.TrimSpace(string(rapB)),
				hello:       strings.TrimSpace(string(helloB)),
				etat:        etat,
				lignesStats: len(stats),
				etatsStats:  etatsStats,
				commits:     commits,
				aSolutions:  fichierRegulierPilote(filepath.Join(p.Atelier(), "SOLUTIONS.md")),
				aGitignore:  strings.Contains(string(giB), ".env"),
			}
		}

		rBash := executer("bash")
		rGo := executer("go")

		if rBash.plan != rGo.plan {
			t.Errorf("PLAN.md mismatch:\nbash:\n%s\ngo:\n%s", rBash.plan, rGo.plan)
		}
		if rBash.rapport != rGo.rapport {
			t.Errorf("FINAL_REPORT.md mismatch:\nbash:\n%s\ngo:\n%s", rBash.rapport, rGo.rapport)
		}
		if rBash.hello != rGo.hello {
			t.Errorf("hello.txt mismatch: bash=%q go=%q", rBash.hello, rGo.hello)
		}
		if rBash.etat.Phase != rGo.etat.Phase || rBash.etat.Etat != rGo.etat.Etat || rBash.etat.Cycle != rGo.etat.Cycle || rBash.etat.DemandesEnAttente != rGo.etat.DemandesEnAttente {
			t.Errorf("STATE.json mismatch:\nbash=%+v\ngo=%+v", rBash.etat, rGo.etat)
		}
		if strings.Join(rBash.etatsStats, ",") != strings.Join(rGo.etatsStats, ",") {
			t.Errorf("stats.tsv cycles/states mismatch: bash=%v go=%v", rBash.etatsStats, rGo.etatsStats)
		}
		if strings.Join(rBash.commits, "|") != strings.Join(rGo.commits, "|") {
			t.Errorf("git commits mismatch: bash=%v go=%v", rBash.commits, rGo.commits)
		}
		if !rGo.aSolutions || !rGo.aGitignore {
			t.Errorf("expected SOLUTIONS.md (%v) and .gitignore (%v)", rGo.aSolutions, rGo.aGitignore)
		}
	})

	t.Run("relais_et_quota", func(t *testing.T) {
		type resRelais struct {
			etatsStats   []string
			modelesStats []string
			relaySeen    bool
			fallbackSeen string
			etat         Etat
		}
		executer := func(moteur string) resRelais {
			kit := preparerKitEssaiPilote(t, binTemp)
			fakeClaude := filepath.Join(kit, "fake-claude-rq.sh")
			if err := os.WriteFile(fakeClaude, []byte(strings.ReplaceAll(scriptFauxClaudeRelaisQuota, "\r\n", "\n")), 0o755); err != nil {
				t.Fatal(err)
			}
			projet := filepath.Join(dossierEssaiPilote(t), "projet")
			_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
			_ = os.WriteFile(filepath.Join(projet, ".athanor", "REQUEST.md"), []byte("# Request\n\nBuild feature M1.\n"), 0o644)
			_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("# Plan\n\n- [ ] M1 : Core feature\n"), 0o644)

			executerPiloteMoteur(t, kit, projet, fakeClaude, moteur, []string{
				"MODEL=opus-principal",
				"FALLBACK_MODEL=sonnet-secours",
				"RELAY_CONTEXT_K=150",
			})

			p := Projet{Nom: "test", Dossier: projet}
			etat, _ := p.LireEtat()
			stats := p.LireStats()
			var etatsStats, modelesStats []string
			for _, s := range stats {
				etatsStats = append(etatsStats, s.Cycle+":"+s.Etat)
				modelesStats = append(modelesStats, s.Modele)
			}
			fbB, _ := os.ReadFile(filepath.Join(p.Atelier(), "fallback_seen"))
			return resRelais{
				etatsStats:   etatsStats,
				modelesStats: modelesStats,
				relaySeen:    fichierRegulierPilote(filepath.Join(p.Atelier(), "relay_seen")),
				fallbackSeen: strings.TrimSpace(string(fbB)),
				etat:         etat,
			}
		}

		rBash := executer("bash")
		rGo := executer("go")

		if strings.Join(rBash.etatsStats, ",") != strings.Join(rGo.etatsStats, ",") {
			t.Errorf("stats states mismatch: bash=%v go=%v", rBash.etatsStats, rGo.etatsStats)
		}
		if strings.Join(rBash.modelesStats, ",") != strings.Join(rGo.modelesStats, ",") {
			t.Errorf("stats models mismatch: bash=%v go=%v", rBash.modelesStats, rGo.modelesStats)
		}
		if !rBash.relaySeen || !rGo.relaySeen {
			t.Errorf("relay notes not passed to cycle 2: bash=%v go=%v", rBash.relaySeen, rGo.relaySeen)
		}
		if rBash.fallbackSeen != "sonnet-secours" || rGo.fallbackSeen != "sonnet-secours" {
			t.Errorf("fallback model not used on cycle 3: bash=%q go=%q", rBash.fallbackSeen, rGo.fallbackSeen)
		}
		if rBash.etat.Phase != rGo.etat.Phase || rBash.etat.Etat != rGo.etat.Etat {
			t.Errorf("final state mismatch: bash=%+v go=%+v", rBash.etat, rGo.etat)
		}
	})
}
