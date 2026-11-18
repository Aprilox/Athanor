package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A project's own models: written and read through /api/mode, only the keys sent change, "" goes back to
// Settings, a request without them (the terminal interface) keeps them, a bad name or key is refused; the
// pilot takes them at the start of each cycle over the ones of Settings, and says what it read.
func TestModelesParProjet(t *testing.T) {
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	kit := t.TempDir()
	_ = os.WriteFile(filepath.Join(kit, ".env"), []byte("MODEL=opus\nSUBAGENT_MODEL=\nCHEAP_MODEL=haiku\n"), 0o600)
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{p}, ProjetActif: "a", Langue: "en"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, corps string) (int, ModeVue) {
		r := httptest.NewRequest(methode, "/api/mode?p=a", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var m ModeVue
		if methode == "GET" {
			_ = json.Unmarshal(w.Body.Bytes(), &m)
		}
		return w.Code, m
	}
	_, m := appel("GET", "")
	if len(m.Modeles) != 0 || m.ModelesGlobaux["MODEL"] != "opus" || len(m.ModelesProposes) == 0 {
		t.Fatalf("nothing set: %+v", m)
	}
	if _, ok := m.ModelesGlobaux["WORKER_MODEL"]; ok {
		t.Fatal("a setting absent from Settings must not be sent (the pilot's default applies)")
	}
	if c, _ := appel("POST", `{"parallele_max":3,"modeles":{"MODEL":"sonnet","WORKER_MODEL":"haiku"}}`); c != 200 {
		t.Fatalf("two models: %d", c)
	}
	if c, _ := appel("POST", `{"parallele_max":3,"modeles":{"SUBAGENT_MODEL":"auto"}}`); c != 200 {
		t.Fatalf("auto for the sub-agents: %d", c)
	}
	if _, m := appel("GET", ""); m.Modeles["MODEL"] != "sonnet" || m.Modeles["WORKER_MODEL"] != "haiku" || m.Modeles["SUBAGENT_MODEL"] != "auto" || len(m.Modeles) != 3 {
		t.Fatalf("after saving: %+v", m.Modeles)
	}
	for _, corps := range []string{
		`{"parallele_max":3,"modeles":{"MODEL":"--dangerously-skip-permissions"}}`,
		`{"parallele_max":3,"modeles":{"MODEL":"auto"}}`,
		`{"parallele_max":3,"modeles":{"MODEL":"opus; rm -rf /"}}`,
		`{"parallele_max":3,"modeles":{"EFFORT":"high"}}`,
	} {
		if c, _ := appel("POST", corps); c != 400 {
			t.Errorf("accepted: %s (%d)", corps, c)
		}
	}
	if c, _ := appel("POST", `{"parallele_max":3,"eco":true}`); c != 200 {
		t.Fatal(c)
	}
	if mp := modelesProjet(p.envProjet()); len(mp) != 3 {
		t.Fatalf("left out of a request, the models changed: %v", mp)
	}

	// the pilot: the project's own over Settings', from the next cycle
	pl := &pilote{etat: p.Atelier(), prive: t.TempDir(), model: "opus", workerModel: "sonnet", cheapModel: "haiku", fallbackModel: "sonnet"}
	pl.appliquerModelesProjet(true)
	if pl.model != "sonnet" || pl.workerModel != "haiku" || pl.subagentModel != "auto" || pl.cheapModel != "haiku" || pl.fallbackModel != "sonnet" {
		t.Fatalf("the project's models not taken: %+v", pl.modelesGlobauxPilote())
	}
	b, _ := os.ReadFile(p.envProjet())
	pl.modesLus = modesBase(string(b)) + suffixeModeles(pl.modelesLus)
	if pl.modesLus != modesDemandes(string(b)) || !strings.Contains(pl.modesLus, "models=MODEL=sonnet,WORKER_MODEL=haiku,SUBAGENT_MODEL=auto") {
		t.Fatalf("what the pilot read: %q / %q", pl.modesLus, modesDemandes(string(b)))
	}
	if c, _ := appel("POST", `{"parallele_max":3,"modeles":{"MODEL":"","WORKER_MODEL":"","SUBAGENT_MODEL":""}}`); c != 200 {
		t.Fatal(c)
	}
	pl.appliquerModelesProjet(true)
	if pl.model != "opus" || pl.workerModel != "sonnet" || pl.subagentModel != "" || pl.modelesLus != "" {
		t.Fatalf("back to Settings: %+v %q", pl.modelesGlobauxPilote(), pl.modelesLus)
	}
	if g := modesDemandes(string(mustLire(t, p.envProjet()))); strings.Contains(g, "models") {
		t.Fatalf("no model of its own, yet: %q", g)
	}
	journal, _ := os.ReadFile(filepath.Join(pl.prive, "pilot.log"))
	if !strings.Contains(string(journal), "This project's own models") || !strings.Contains(string(journal), "own models removed") {
		t.Fatalf("log: %s", journal)
	}

	// a value Claude could write in the workshop: only model names are kept
	if mp := modelesProjetTexte("MODEL=-x\nWORKER_MODEL=auto\nCHEAP_MODEL=claude-haiku-4-5\nFALLBACK_MODEL=sonnet[1m]\nFRAMING_MODEL=us.anthropic.claude-opus-4-1:0\n"); len(mp) != 3 || mp["CHEAP_MODEL"] != "claude-haiku-4-5" || mp["FALLBACK_MODEL"] != "sonnet[1m]" {
		t.Fatalf("read: %v", mp)
	}

	// every text of the block is translated
	i18n := lireI18nEssai(t)
	for _, x := range []string{"this project's own; Like Settings follows Settings › Claude. Applies from the next cycle.", "Like Settings ({0})",
		"no fallback", "same as the sub-agents", "same as the lead", "Models saved for this project: a running pilot uses them from its next cycle.",
		"Unknown model name: letters, digits and . _ - : / @ [ ] only."} {
		cle, _ := json.Marshal(x)
		if n := strings.Count(i18n, string(cle)+": "); n < 5 {
			t.Errorf("translated in %d language(s) out of 5: %s", n, x)
		}
	}
}

func mustLire(t *testing.T, f string) []byte {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
