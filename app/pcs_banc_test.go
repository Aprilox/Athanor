package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// the fake Claude Code of a team: frames the plan when there is none, builds the milestone its goal names,
// writes the final report when every milestone is ticked
const scriptFauxClaudeEquipe = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help) echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"; exit 0 ;;
  --version) echo "claude 2.1.285"; exit 0 ;;
esac
but=""
while [ $# -gt 0 ]; do
  case "$1" in
    -p) but="$2"; shift 2 ;;
    *) shift ;;
  esac
done
ETAT="$PWD/.athanor"
qui=$(git config user.name)
fin() {
  printf '{"type":"assistant","message":{"usage":{"input_tokens":1200,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":300},"content":[{"type":"text","text":"%s"}]}}\n' "$1"
  printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.02,"usage":{"input_tokens":1200,"output_tokens":300},"result":"%s"}\n' "$1"
  exit 0
}
sleep 0.3
if [ -f "$ETAT/MERGE_NEEDED.md" ]; then
  cat "$ETAT/MERGE_NEEDED.md" >> "$ETAT/fake-unexpected.txt"
  git merge -q -X ours --no-edit refs/remotes/athanor/main || true
  rm -f "$ETAT/MERGE_NEEDED.md"
fi
if [ ! -f "$ETAT/PLAN.md" ]; then
  mkdir -p "$ETAT/docs"
  echo "# Requirements" > "$ETAT/docs/REQUIREMENTS.md"
  echo "# Architecture" > "$ETAT/docs/ARCHITECTURE.md"
  printf '#!/usr/bin/env bash\necho "CHECK: PASSED"\n' > "$ETAT/check.sh"; chmod +x "$ETAT/check.sh"
  cat > "$ETAT/PLAN.md" <<'PLAN'
# Plan

- [ ] M1 : Models — Needs: gpu.
- [ ] M2 : Menus — Needs: blender.
- [ ] M3 : Downloads — Needs: network.
- [ ] M4 : Sounds
- [ ] M5 : Polish — Depends on: M1, M2, M3, M4.
PLAN
  echo "- framing by $qui" >> "$ETAT/LOG.md"
  git add -A && git commit -qm "Framing by $qui"
  fin "Framing done by $qui"
fi
id=$(printf '%s' "$but" | sed -n 's/^\/goal Milestone \(M[0-9]*\) of .*/\1/p')
if [ -n "$id" ] && [ "$qui" = "PC-GPU" ] && grep -q "^- \[ \] $id .*Needs: blender" "$ETAT/PLAN.md"; then
  # Blender does not start on this computer: one quick try, then the others
  printf 'tool: blender\nwhy: Blender crashes at start on %s\n' "$qui" > "$ETAT/CANNOT_HERE.md"
  echo "- $id: blender fails on $qui" >> "$ETAT/LOG.md"
  git add -A && git commit -qm "$id: blender does not work on $qui"
  fin "Blender does not work here"
fi
if [ -n "$id" ]; then
  if [ "$id" = "M1" ]; then echo "- 2026-01-01 10:00 · $qui: the armor interface is now Armor.paint(color)" >> "$ETAT/TEAM_NOTES.md"; fi
  if [ "$id" = "M5" ] && [ -f "$ETAT/TEAM_NOTES.md" ]; then cp "$ETAT/TEAM_NOTES.md" "$PWD/work/notes-seen.txt" 2>/dev/null || { mkdir -p work; cp "$ETAT/TEAM_NOTES.md" "$PWD/work/notes-seen.txt"; }; fi
  mkdir -p work
  echo "$id by $qui" > "work/$id.txt"
  sed -i.bak "s/^- \[ \] $id /- [x] $id /" "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
  echo "- $id by $qui" >> "$ETAT/LOG.md"
  bash "$ETAT/check.sh"
  git add -A && git commit -qm "$id: done by $qui"
  fin "CHECK: PASSED $id by $qui"
fi
if ! grep -q '^- \[ \]' "$ETAT/PLAN.md"; then
  printf '# Final Report\n\nDelivered by %s.\nSTATUS: DONE\n' "$qui" > "$ETAT/FINAL_REPORT.md"
  echo "- delivery by $qui" >> "$ETAT/LOG.md"
  git add -A && git commit -qm "Delivery by $qui"
  fin "CHECK: PASSED delivery by $qui"
fi
echo "unexpected goal: $but" >> "$ETAT/fake-unexpected.txt"
fin "Nothing to do"
`

// TestBancEquipe: two real pilots (athanor demarrer) build one project together through a local "GitHub":
// the framing once, the milestones shared out by what each computer has, the delivery once, both stop.
func TestBancEquipe(t *testing.T) {
	if runtime.GOOS == "windows" && trouverBashNatifPilote() == "" {
		t.Skip("Git Bash not found on Windows (the fake Claude Code is a bash script)")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}
	bin := filepath.Join(t.TempDir(), "athanor-essai")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-tags", "essai", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	fins := []bool{false, true}
	if runtime.GOOS == "windows" {
		fins = []bool{true} // as Git for Windows installs it (and the bench is slower there)
	}
	for _, crlf := range fins {
		nom := map[bool]string{false: "LF", true: "CRLF"}[crlf]
		t.Run(nom, func(t *testing.T) {
			conf := filepath.Join(t.TempDir(), "gitconfig")
			ecrireEssai(t, conf, map[bool]string{false: "", true: "[core]\n\tautocrlf = true\n"}[crlf])
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", conf)
			bancEquipe(t, bin)
		})
	}
}

func bancEquipe(t *testing.T, bin string) {
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien := depotEssaiGH
	depotEssaiGH = depot
	defer func() { depotEssaiGH = ancien }()

	// A starts the project with its request, pushed; B joins from a new folder
	a := nouveauPCEssai(t, "PC-GPU", "gpu, blender", "RTX 4090", depot, false) // it says it has Blender: it is meant to build M2
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "REQUEST.md"), "# Request\n\nBuild the game.\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "The request")
	a.faire(t, func() {
		var o strings.Builder
		if code := pousserGH([]string{a.projet}, &o); code != 0 {
			t.Fatalf("A's first push: %s", o.String())
		}
	})
	b := pcVideEssai(t, "PC-Fibre", nil)
	b.faire(t, func() {
		var o strings.Builder
		if code := pcsMachineGH([]string{"--mots", "network"}, &o); code != 0 {
			t.Fatal(o.String())
		}
	})
	if code, out := activerEssai(t, b); code != 0 || !strings.Contains(out, "starts from the other computers' work") {
		t.Fatalf("B's switch: %d %s", code, out)
	}
	gitEssai(t, b.projet, "config", "user.name", "PC-Fibre")
	gitEssai(t, b.projet, "config", "user.email", "fibre@example.com")

	sorties := make([]string, 2)
	var wg sync.WaitGroup
	for i, m := range []pcEssai{a, b} {
		kit := preparerKitEssaiPilote(t, bin)
		faux := filepath.Join(kit, "fake-claude.sh")
		if err := os.WriteFile(faux, []byte(scriptFauxClaudeEquipe), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(kit, ".env"), []byte("LANGUAGE=en\n"), 0o644)
		wg.Add(1)
		go func(i int, m pcEssai, kit, faux string) {
			defer wg.Done()
			ctx, fin := context.WithTimeout(context.Background(), 5*time.Minute)
			defer fin()
			c := exec.CommandContext(ctx, bin, "demarrer", m.projet)
			c.WaitDelay = 10 * time.Second
			c.Dir = kit
			c.Env = append(os.Environ(), "KIT="+kit, "CLAUDE_BIN="+faux, "CLAUDE_CODE_OAUTH_TOKEN=fake-token-for-test",
				"KEEP_AWAKE=0", "CLOSE_AFTER_MILESTONE=0", "WATCH_INTERVAL=1", "SECONDS_PER_MINUTE=1", "QUOTA_WAIT_MIN=1",
				"ERROR_WAIT_MIN=0", "ATHANOR_GITHUB_DIR="+m.donnees, "ATHANOR_DEPOT_ESSAI="+depot)
			out, err := c.CombinedOutput()
			sorties[i] = string(out)
			if err != nil {
				sorties[i] += "\n(exit: " + err.Error() + ")"
			}
		}(i, m, kit, faux)
		time.Sleep(300 * time.Millisecond)
	}
	wg.Wait()
	journal := "\n--- A ---\n" + sorties[0] + "\n--- B ---\n" + sorties[1]
	for _, m := range []pcEssai{a, b} {
		if x, err := os.ReadFile(filepath.Join(m.projet, ".athanor", "fake-unexpected.txt")); err == nil {
			journal += "\n--- " + m.nom + "'s Claude saw ---\n" + string(x)
		}
	}
	for i, s := range sorties {
		if !strings.Contains(s, "DONE") || strings.Contains(s, "(exit:") {
			t.Fatalf("pilot %d did not end with the delivery%s", i, journal)
		}
	}

	// GitHub has the whole project: each milestone once, the framing and the delivery once
	plan := gitEssai(t, depot, "show", "main:.athanor/PLAN.md")
	if strings.Contains(plan, "- [ ]") {
		t.Fatalf("plan on GitHub:\n%s%s", plan, journal)
	}
	log := gitEssai(t, depot, "show", "main:.athanor/LOG.md")
	compte := func(motif string) int { return len(regexp.MustCompile(motif).FindAllString(log, -1)) }
	if compte(`- framing by`) != 1 || compte(`- delivery by`) != 1 {
		t.Fatalf("framing or delivery twice:\n%s%s", log, journal)
	}
	parQui := map[string]string{}
	for _, id := range []string{"M1", "M2", "M3", "M4", "M5"} {
		if n := compte(`- ` + id + ` by`); n != 1 {
			t.Fatalf("%s built %d times:\n%s%s", id, n, log, journal)
		}
		parQui[id] = strings.TrimSpace(strings.TrimPrefix(gitEssai(t, depot, "show", "main:work/"+id+".txt"), id+" by "))
	}
	if parQui["M1"] != "PC-GPU" || parQui["M3"] != "PC-Fibre" || parQui["M2"] != "PC-Fibre" {
		t.Fatalf("milestones not given by what each computer has: %v%s", parQui, journal)
	}
	if !strings.Contains(gitEssai(t, depot, "show", "main:.athanor/FINAL_REPORT.md"), "STATUS: DONE") {
		t.Fatal("no final report on GitHub" + journal)
	}
	for _, m := range []pcEssai{a, b} {
		if _, err := os.Stat(filepath.Join(m.projet, ".athanor", "fake-unexpected.txt")); err == nil {
			b, _ := os.ReadFile(filepath.Join(m.projet, ".athanor", "fake-unexpected.txt"))
			t.Fatalf("%s's Claude got an unexpected goal: %s%s", m.nom, b, journal)
		}
	}
	// the program that failed on PC-GPU worked on PC-Fibre: PC-Fibre has its word now, PC-GPU said it
	var motsB []string
	b.faire(t, func() { motsB = lireMachinePC().Mots })
	if !strings.Contains(strings.Join(motsB, ","), "blender") {
		t.Fatalf("PC-Fibre did not learn blender: %v%s", motsB, journal)
	}
	if !strings.Contains(sorties[0], "blender does not work on this computer (Blender crashes at start on PC-GPU)") || !strings.Contains(sorties[1], "blender works on this computer") {
		t.Fatalf("the relay is not in the logs%s", journal)
	}
	// the note PC-GPU left with M1 reached the computer that built M5, and is on GitHub
	if !strings.Contains(gitEssai(t, depot, "show", "main:.athanor/TEAM_NOTES.md"), "Armor.paint(color)") || !strings.Contains(gitEssai(t, depot, "show", "main:work/notes-seen.txt"), "Armor.paint(color)") {
		t.Fatalf("team notes not shared%s", journal)
	}
	t.Logf("milestones: %v", parQui)
}
