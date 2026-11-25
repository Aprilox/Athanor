package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Settings › Claude › Connections: .env's connection taken into the list (its unused key kept, off), the order, the
// project's choice, the switch at a limit, .env's own connection changed elsewhere, and never a secret in an answer.
func TestConnexionsListeEtBascule(t *testing.T) {
	kit := t.TempDir()
	env := filepath.Join(kit, ".env")
	ecrireFichier(t, env, "CLAUDE_AUTH=subscription\nCLAUDE_CODE_OAUTH_TOKEN=jeton-abonnement-1\nANTHROPIC_API_KEY=sk-cle-api-0001\nMODEL=sonnet\n", 0o600)
	s := nouveauServeur(Config{Kit: kit})
	poster := func(corps map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(corps)
		w := httptest.NewRecorder()
		s.apiConnexionsModifier(w, httptest.NewRequest("POST", "/api/connexions", strings.NewReader(string(b))))
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "jeton-abonnement") || strings.Contains(w.Body.String(), "sk-cle-api") {
			t.Fatalf("%v: %d %s", corps, w.Code, w.Body.String())
		}
		var r map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		return r
	}
	choisir := func(permises []string, bascule bool, actuel string) ChoixConnexion {
		return choisirConnexion(LireEnv(env), kit, nil, permises, bascule, actuel)
	}
	if c := choisir(nil, true, ""); c.Connexion.ID != "" || c.Connexion.api() || c.Connexion.secretChoisi() != "jeton-abonnement-1" {
		t.Fatalf("no list: .env's connection as before, %+v", c)
	}

	poster(map[string]any{"action": "ajouter", "nom": "Max 2", "type": "subscription", "secret": "jeton-abonnement-2"})
	l := lireListeConnexions(kit)
	if len(l) != 3 || l[0].Secret != "jeton-abonnement-1" || !l[0].Actif || l[1].Secret != "sk-cle-api-0001" || l[1].Actif || l[2].Nom != "Max 2" || !l[2].Actif {
		t.Fatalf(".env's connection first, its unused key off, the new one last: %+v", l)
	}
	if st, err := os.Stat(fichierConnexions(kit)); err != nil || (st.Mode().Perm()&0o077 != 0 && !estWindows) {
		t.Fatalf("the list's file: %v %v", st, err)
	}

	// no rule switches: the first one, whatever its limit
	marquerConnexion(kit, l[0].ID, time.Now().Unix()+3600, "test")
	if c := choisir(nil, false, ""); c.Connexion.Jeton != "jeton-abonnement-1" || c.ALimite {
		t.Fatalf("without switching: %+v", c)
	}
	// the rules switch: the first at its limit, the next one on (the API key is off)
	if c := choisir(nil, true, l[0].ID); c.Connexion.Jeton != "jeton-abonnement-2" || c.Connexion.ID != l[2].ID || c.ALimite {
		t.Fatalf("switch: %+v", c)
	}
	// every one at its limit: the current one kept, the earliest reset
	tot := time.Now().Unix() + 600
	marquerConnexion(kit, l[2].ID, tot, "test")
	if c := choisir(nil, true, l[2].ID); !c.ALimite || c.Connexion.ID != l[2].ID || c.Reprise != tot {
		t.Fatalf("all at their limit: %+v", c)
	}
	// the project's own choice; ids gone are all of them
	marquerConnexion(kit, l[0].ID, 0, "")
	marquerConnexion(kit, l[2].ID, 0, "")
	if c := choisir([]string{l[2].ID}, true, ""); c.Connexion.ID != l[2].ID {
		t.Fatalf("the project's choice: %+v", c)
	}
	if c := choisir([]string{"partie"}, true, ""); c.Connexion.ID != l[0].ID {
		t.Fatalf("a choice gone: %+v", c)
	}

	// the order: the new one moved up twice, first, so copied into .env
	poster(map[string]any{"action": "monter", "id": l[2].ID})
	poster(map[string]any{"action": "monter", "id": l[2].ID})
	if e := connexionDesLignes(LireEnv(env), nil); e.Jeton != "jeton-abonnement-2" || e.Cle != "" || e.api() {
		t.Fatalf(".env after the order changed: %+v", e)
	}
	if b, _ := os.ReadFile(env); !strings.Contains(string(b), "MODEL=sonnet") {
		t.Fatalf("the other settings of .env kept: %s", b)
	}

	// .env's connection changed elsewhere (configurer, the terminal): shown first, never lost
	if err := EcrireEnv(env, map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "jeton-terminal-3"}); err != nil {
		t.Fatal(err)
	}
	v := s.vueConnexions()
	if len(v) != 4 || !v[0].Fichier || !v[0].Utilisee || v[0].Apercu != "…al-3" {
		t.Fatalf("the list with .env's own: %+v", v)
	}

	// every one off: no connection at all, as asked
	for _, x := range listeAEcrire(kit) {
		poster(map[string]any{"action": "modifier", "id": x.ID, "actif": false})
	}
	if c := choisir(nil, true, ""); c.Connexion.presente() {
		t.Fatalf("all off: %+v", c.Connexion)
	}
	if r := poster(map[string]any{"action": "supprimer", "id": l[0].ID}); len(r["liste"].([]any)) != 3 {
		t.Fatalf("removed: %v", r["liste"])
	}
}

// A connection's key changed in place (its limit forgotten, .env following), and each subscription's own quota for
// the Overview, from the logs of the cycles it ran (a parallel worker's log under its lead's cycle).
func TestConnexionsCleEtQuotas(t *testing.T) {
	kit := t.TempDir()
	ecrireFichier(t, filepath.Join(kit, ".env"), "MODEL=sonnet\n", 0o600)
	ecrireFichier(t, fichierConnexions(kit), `{"liste":[{"id":"un","nom":"Max 1","type":"subscription","secret":"jeton-un-0001","actif":true},{"id":"deux","nom":"Max 2","type":"subscription","secret":"jeton-deux-0002","actif":true}]}`, 0o600)
	s := nouveauServeur(Config{Kit: kit})
	marquerConnexion(kit, "un", time.Now().Unix()+3600, "test")
	w := httptest.NewRecorder()
	s.apiConnexionsModifier(w, httptest.NewRequest("POST", "/api/connexions", strings.NewReader(`{"action":"modifier","id":"un","secret":"jeton-renouvele-0003"}`)))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "jeton-renouvele") || lireListeConnexions(kit)[0].Secret != "jeton-renouvele-0003" || lireEtatsConnexions(kit)["un"].Jusqua != 0 {
		t.Fatalf("new key: %d %s %+v", w.Code, w.Body.String(), lireEtatsConnexions(kit))
	}
	if e := connexionDesLignes(LireEnv(filepath.Join(kit, ".env")), nil); e.Jeton != "jeton-renouvele-0003" {
		t.Fatalf(".env after the new key: %+v", e)
	}

	d := t.TempDir()
	a, b := fauxProjet(t, d, "a", 2, 0), fauxProjet(t, d, "b", 2, 0)
	evt := func(u float64) string {
		return fmt.Sprintf(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":%g,"resetsAt":%d}}}}`+"\n", u, time.Now().Unix()+3600)
	}
	ecrire := func(p Projet, nom, contenu string, age time.Duration, id string) {
		f := filepath.Join(p.LogsDir(), nom)
		ecrireFichier(t, f, contenu, 0o644)
		_ = os.Chtimes(f, time.Now().Add(-age), time.Now().Add(-age))
		noterConnexionCycle(f, id)
	}
	ecrire(a, "cycle-20261125-100000-0001.jsonl", evt(0.10), 3*time.Hour, "un")
	ecrire(a, "cycle-20261125-120000-0002.jsonl", "", 2*time.Hour, "deux")
	ecrire(a, "cycle-20261125-120000-0002-M3.jsonl", evt(0.42), 2*time.Hour, "")
	ecrire(b, "cycle-20261125-130000-0001.jsonl", evt(0.77), time.Hour, "un")
	quotasConnMu.Lock()
	quotasConnQuand = time.Time{} // not another test's cache
	quotasConnMu.Unlock()
	q := quotasParConnexion([]Projet{a, b})
	u := func(id string) float64 {
		if q[id] == nil || len(q[id].Fenetres) == 0 || q[id].Fenetres[0].Utilisation == nil {
			return -1
		}
		return *q[id].Fenetres[0].Utilisation
	}
	if u("un") != 0.77 || u("deux") != 0.42 {
		t.Fatalf("each connection's newest report: un %v, deux %v", u("un"), u("deux"))
	}
}

// A connection's own limit, with no quota rule at all: an API key's own budget (its own runs only, counted from when
// it was given, and again from Count again from now) hands over to the next one; a subscription's own percent.
func TestConnexionsLimitesPropres(t *testing.T) {
	kit := t.TempDir()
	env := filepath.Join(kit, ".env")
	ecrireFichier(t, env, "MODEL=sonnet\n", 0o600)
	ecrireFichier(t, fichierConnexions(kit), `{"liste":[{"id":"cle1","nom":"API 1","type":"api","secret":"sk-ant-api-0001","actif":true},{"id":"abo","nom":"Max","type":"subscription","secret":"jeton-abo-0002","actif":true}]}`, 0o600)
	s := nouveauServeur(Config{Kit: kit})
	poster := func(corps string) int {
		t.Helper()
		w := httptest.NewRecorder()
		s.apiConnexionsModifier(w, httptest.NewRequest("POST", "/api/connexions", strings.NewReader(corps)))
		return w.Code
	}
	choisir := func() connexionClaude { return choisirConnexion(LireEnv(env), kit, nil, nil, false, "").Connexion }
	if c := choisir(); c.ID != "cle1" || limitesPropres(kit) {
		t.Fatalf("no limit: the first one, %+v", c)
	}
	for _, mauvais := range []string{`{"action":"modifier","id":"cle1","budget":-1}`, `{"action":"modifier","id":"abo","seuil":101}`} {
		if code := poster(mauvais); code != http.StatusBadRequest {
			t.Errorf("%s: %d", mauvais, code)
		}
	}
	if code := poster(`{"action":"modifier","id":"cle1","budget":5}`); code != http.StatusOK || lireListeConnexions(kit)[0].BudgetDepuis == 0 || !limitesPropres(kit) {
		t.Fatalf("a budget for the key: %d %+v", code, lireListeConnexions(kit))
	}
	noterDepense(kit, modeConnexionAPI, "cle1", 3, "p", "cycle 1")
	noterDepense(kit, modeConnexionAPI, "autre", 9, "p", "cycle 2") // another key's: not this one's budget
	if c := choisir(); c.ID != "cle1" || c.BudgetPropre != 5 {
		t.Fatalf("3 $ of 5: still the key, %+v", c)
	}
	noterDepense(kit, modeConnexionAPI, "cle1", 2.5, "p", "cycle 3")
	if c := choisir(); c.ID != "abo" {
		t.Fatalf("its budget spent: the subscription takes over, %+v", c)
	}
	if v := s.vueConnexions(); v[0].Depense != 5.5 || v[0].Budget != 5 {
		t.Fatalf("the page: %+v", v[0])
	}
	time.Sleep(time.Until(time.Unix(time.Now().Unix()+1, 0))) // the runs above in an earlier second than the new count
	if code := poster(`{"action":"zero","id":"cle1"}`); code != http.StatusOK || choisir().ID != "cle1" || s.vueConnexions()[0].Depense != 0 {
		t.Fatalf("counted again from now: the key first again (%d)", code)
	}
	if code := poster(`{"action":"modifier","id":"abo","seuil":60}`); code != http.StatusOK {
		t.Fatalf("a subscription's own percent: %d", code)
	}
	if c := choisirConnexion(LireEnv(env), kit, nil, []string{"abo"}, false, ""); c.Connexion.SeuilPropre != 60 {
		t.Fatalf("its percent reaches the pilot: %+v", c.Connexion)
	}
}

// scriptFauxClaudePlein: Claude Code refusing the subscription jeton-plein (its 5-hour quota full, back in an hour),
// building M1 with any other token.
const scriptFauxClaudePlein = `#!/usr/bin/env bash
set -u
case "${1:-}" in
  --help) echo "--strict-mcp-config --setting-sources --append-system-prompt-file --agents --plugin-dir"; exit 0 ;;
  --version) echo "claude 2.1.285"; exit 0 ;;
esac
ETAT="$PWD/.athanor"
echo "${CLAUDE_CODE_OAUTH_TOKEN:-none}" >> "$ETAT/cles_vues"
if [ "${CLAUDE_CODE_OAUTH_TOKEN:-}" = "jeton-plein" ]; then
  printf '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":%d,"rateLimitType":"five_hour","utilization":1}}\n' $(( $(date +%s) + 3600 ))
  printf '{"type":"result","subtype":"error","is_error":true,"num_turns":1,"duration_ms":500,"duration_api_ms":100,"usage":{"input_tokens":10,"output_tokens":0},"result":"Claude usage limit reached."}\n'
  exit 1
fi
echo "built" > "$PWD/app.txt"
sed -i.bak 's/^- \[ \] M1 /- [x] M1 /' "$ETAT/PLAN.md" && rm -f "$ETAT/PLAN.md.bak"
printf '# Final Report\nSTATUS: DONE\n' > "$ETAT/FINAL_REPORT.md"
git add -A && git commit -qm "M1: built on the second subscription"
printf '{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":3000,"duration_api_ms":2000,"usage":{"input_tokens":2000,"output_tokens":200},"result":"Done"}\n'
exit 0
`
