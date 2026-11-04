package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLireConnexionClaude(t *testing.T) {
	kit := t.TempDir()
	ecrireFichier(t, filepath.Join(kit, ".env"), "CLAUDE_AUTH=api\nCLAUDE_CODE_OAUTH_TOKEN=jeton-abonnement-1234567890\nANTHROPIC_API_KEY=sk-ant-api-cle\nAPI_BUDGET_USD=100,50 $\nAPI_BUDGET_SINCE=1700000000\n", 0o600)
	c := lireConnexionClaude(kit, nil)
	if !c.api() || c.Cle != "sk-ant-api-cle" || c.Budget != 100.5 || c.Depuis != 1700000000 || !c.presente() {
		t.Fatalf("%+v", c)
	}
	// only the chosen connection reaches Claude Code
	env := c.environnement([]string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=x", "anthropic_api_key=y", "AUTRE=1"})
	if strings.Join(env, " ") != "PATH=/bin AUTRE=1 ANTHROPIC_API_KEY=sk-ant-api-cle" {
		t.Fatalf("api: %v", env)
	}
	ecrireFichier(t, filepath.Join(kit, ".env"), "CLAUDE_AUTH=subscription\nCLAUDE_CODE_OAUTH_TOKEN=jeton-abonnement-1234567890\nANTHROPIC_API_KEY=sk-ant-api-cle\n", 0o600)
	c2 := lireConnexionClaude(kit, nil)
	if env := c2.environnement([]string{"ANTHROPIC_API_KEY=z", "A=1"}); strings.Join(env, " ") != "A=1 CLAUDE_CODE_OAUTH_TOKEN=jeton-abonnement-1234567890" {
		t.Fatalf("subscription: %v", env)
	}
	if c2.empreinte() == c.empreinte() {
		t.Fatal("the mode is part of the connection")
	}
	// nothing in .env: the environment the process got; the default mode is the subscription
	ecrireFichier(t, filepath.Join(kit, ".env"), "MODEL=\n", 0o600)
	c3 := lireConnexionClaude(kit, map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "du-processus"})
	if c3.api() || c3.Jeton != "du-processus" || c3.Budget != 0 {
		t.Fatalf("%+v", c3)
	}
	for s, v := range map[string]float64{"": 0, "abc": 0, "-5": 0, "$20": 20, "12.5 USD": 12.5} {
		if lireMontant(s) != v {
			t.Errorf("%q: %v", s, lireMontant(s))
		}
	}
}

func TestDepenseAPI(t *testing.T) {
	kit := t.TempDir()
	noterDepense(kit, modeConnexionAbonnement, 3, "p", "cycle 1") // the subscription's: not the API's spend
	noterDepense(kit, modeConnexionAPI, 1.25, "p\tx", "cycle 2")
	noterDepense(kit, modeConnexionAPI, 0, "p", "nothing")
	ancien := time.Now().Add(-time.Hour).Unix()
	b, _ := os.ReadFile(fichierDepenses(kit))
	b = append(b, []byte(strconv.FormatInt(ancien, 10)+"\tapi\t50\tp\tbefore the count\n")...)
	_ = os.WriteFile(fichierDepenses(kit), b, 0o644)
	if d, n := depenseAPI(kit, ancien+1); d != 1.25 || n != 1 {
		t.Fatalf("%v %d", d, n)
	}
	if d, n := depenseAPI(kit, 0); d != 51.25 || n != 2 {
		t.Fatalf("all: %v %d", d, n)
	}
	ecrireFichier(t, filepath.Join(kit, ".env"), "CLAUDE_AUTH=api\nANTHROPIC_API_KEY=sk-ant-x\nAPI_BUDGET_USD=1\nAPI_BUDGET_SINCE="+strconv.FormatInt(ancien+1, 10)+"\n", 0o600)
	e := etatDepenseKit(kit)
	if e == nil || e.Depense != 1.25 || e.Budget != 1 || !e.Atteint || !e.Cle {
		t.Fatalf("%+v", e)
	}
	ecrireFichier(t, filepath.Join(kit, ".env"), "CLAUDE_AUTH=subscription\n", 0o600)
	if etatDepenseKit(kit) != nil {
		t.Fatal("subscription: the quota, not the spend")
	}
}

// Saving Settings: a new API key or a first budget start the count again; a budget raised keeps it.
func TestReglagesConnexionEnregistres(t *testing.T) {
	kit := t.TempDir()
	ecrireFichier(t, filepath.Join(kit, ".env"), "CLAUDE_AUTH=api\nANTHROPIC_API_KEY=sk-ant-a\n", 0o600)
	v := map[string]string{"API_BUDGET_USD": "100"}
	reglagesConnexionEnregistres(kit, v)
	if v["API_BUDGET_SINCE"] == "" {
		t.Fatal("a first budget: counted from now")
	}
	ecrireFichier(t, filepath.Join(kit, ".env"), "CLAUDE_AUTH=api\nANTHROPIC_API_KEY=sk-ant-a\nAPI_BUDGET_USD=100\nAPI_BUDGET_SINCE=5\n", 0o600)
	v = map[string]string{"API_BUDGET_USD": "150", "ANTHROPIC_API_KEY": "sk-ant-a"}
	reglagesConnexionEnregistres(kit, v)
	if _, ok := v["API_BUDGET_SINCE"]; ok {
		t.Fatal("a budget raised, the same key: the count goes on")
	}
	v = map[string]string{"ANTHROPIC_API_KEY": "sk-ant-b"}
	reglagesConnexionEnregistres(kit, v)
	if v["API_BUDGET_SINCE"] == "" {
		t.Fatal("a new key: counted from now")
	}
}

// essaiPiloteConnexion: a pilot of nothing, enough for its waits and its connection.
func essaiPiloteConnexion(t *testing.T, env string) *pilote {
	t.Helper()
	d := t.TempDir()
	kit := filepath.Join(d, "kit")
	ecrireFichier(t, filepath.Join(kit, ".env"), env, 0o600)
	p := &pilote{kit: kit, projet: filepath.Join(d, "projet"), etat: filepath.Join(d, "projet", ".athanor"), prive: filepath.Join(d, "prive"), secondsPerMinute: 60}
	_ = os.MkdirAll(p.etat, 0o755)
	_ = os.MkdirAll(p.prive, 0o755)
	p.connexion = lireConnexionClaude(kit, nil)
	ancien := pasConnexion
	pasConnexion = 20 * time.Millisecond
	t.Cleanup(func() { pasConnexion = ancien })
	return p
}

// A wait is cut short by a new connection saved in Settings; what waited on the old account is let go.
func TestDormirNouvelleConnexion(t *testing.T) {
	p := essaiPiloteConnexion(t, "CLAUDE_CODE_OAUTH_TOKEN=ancien-jeton-0123456789\n")
	ecrireFichier(t, filepath.Join(p.prive, "secours_jusqua"), "9999999999\n", 0o644)
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(p.kit, ".env"), []byte("CLAUDE_AUTH=api\nANTHROPIC_API_KEY=sk-ant-nouvelle\n"), 0o600)
	}()
	debut := time.Now()
	if !p.dormir(3600) || time.Since(debut) > 30*time.Second {
		t.Fatalf("not woken: %v", time.Since(debut))
	}
	if c := p.connexionActuelle(); !c.api() || c.Cle != "sk-ant-nouvelle" || existeP(filepath.Join(p.prive, "secours_jusqua")) {
		t.Fatalf("%+v", c)
	}
	if env := strings.Join(p.envClaude(), "\n"); !strings.Contains(env, "ANTHROPIC_API_KEY=sk-ant-nouvelle") || strings.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=") {
		t.Fatal("the next run of Claude: the new key only")
	}
	if p.dormir(0) || p.relireConnexion() {
		t.Fatal("nothing changed since")
	}
}

// The budget spent: the pilot waits until it is raised (or counted again, or another key).
func TestAttendreBudget(t *testing.T) {
	p := essaiPiloteConnexion(t, "CLAUDE_AUTH=api\nANTHROPIC_API_KEY=sk-ant-a\nAPI_BUDGET_USD=1\n")
	if p.attendreBudget() {
		t.Fatal("nothing spent yet")
	}
	noterDepense(p.kit, modeConnexionAPI, 1.5, "projet", "cycle 1")
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(p.kit, ".env"), []byte("CLAUDE_AUTH=api\nANTHROPIC_API_KEY=sk-ant-a\nAPI_BUDGET_USD=5\n"), 0o600)
	}()
	p.secondsPerMinute = 1 // dormir(60): one second at a time
	debut := time.Now()
	if !p.attendreBudget() || time.Since(debut) > 30*time.Second || p.etatPhase != "" {
		t.Fatalf("%v %q", time.Since(debut), p.etatPhase)
	}
	b, _ := os.ReadFile(filepath.Join(p.etat, "pilot.log"))
	if !strings.Contains(string(b), "API budget reached: 1.50 $ spent of 1.00 $") || !strings.Contains(string(b), "room again") {
		t.Fatalf("%s", b)
	}
}

// the fake Claude Code of the bench's api_credit: the API key it is given, each call; the old key has no
// credit left (and "the user" saves a new one in Settings meanwhile), the new one builds M1
const scriptFauxClaudeCredit = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help) echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"; exit 0 ;;
  --version) echo "claude 2.1.285"; exit 0 ;;
esac
ETAT="$PWD/.athanor"
echo "${ANTHROPIC_API_KEY:-none}|${CLAUDE_CODE_OAUTH_TOKEN:-none}" >> "$ETAT/cles_vues"
if [ "${ANTHROPIC_API_KEY:-}" = "sk-ant-sans-credit" ]; then
  printf 'ANTHROPIC_API_KEY=sk-ant-nouvelle-cle\n' >> "$KIT/.env"
  printf '{"type":"result","subtype":"error","is_error":true,"num_turns":1,"duration_ms":500,"duration_api_ms":100,"usage":{"input_tokens":10,"output_tokens":0},"result":"Credit balance is too low"}\n'
  exit 1
fi
echo "built" > "$PWD/app.txt"
sed -i.bak 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
printf '# Final Report\nSTATUS: DONE\n' > "$ETAT/FINAL_REPORT.md"
git add -A && git commit -qm "M1: built with the new key"
printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"total_cost_usd":0.03,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Done"}\n'
exit 0
`
