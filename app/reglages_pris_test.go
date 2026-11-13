package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestEmpreintesReglages: what counts in the settings a pilot takes at its start, and in the working modes it
// reads at each cycle.
func TestEmpreintesReglages(t *testing.T) {
	base := empreinteReglages("# comment\nMODEL=opus\nEFFORT=high\n\nCLAUDE_CODE_OAUTH_TOKEN=abc\n")
	if g := empreinteReglages("EFFORT=high\r\nMODEL=opus\r\nCLAUDE_CODE_OAUTH_TOKEN=xyz\nANTHROPIC_API_KEY=k\n# other\n"); g != base {
		t.Fatalf("order, comments, line ends and the connection must not count: %s / %s", g, base)
	}
	if g := empreinteReglages("MODEL=sonnet\nEFFORT=high\n"); g == base {
		t.Fatal("a changed model must count")
	}
	if g := empreinteReglages("MODEL=opus\nEFFORT=high\nRELAY_CONTEXT_K=160\n"); g == base {
		t.Fatal("a new setting must count")
	}
	for texte, attendu := range map[string]string{
		"":                               "parallel=0 max=- eco=0 fast=0",
		"ECO=1\nFAST=1\n":                "parallel=0 max=- eco=1 fast=1",
		"PARALLEL=1\r\nPARALLEL_MAX=6\n": "parallel=1 max=6 eco=0 fast=0",
		"PARALLELE=1\nPARALLELE_MAX=x\n": "parallel=1 max=3 eco=0 fast=0", // the old names, an invalid number
		"PARALLEL=0\nPARALLEL_MAX=8\n":   "parallel=0 max=- eco=0 fast=0", // the workers only count with the mode
		"FAST=1\nFAST=0\nECO=yes\n":      "parallel=0 max=- eco=0 fast=0", // the last line wins, only 1 switches on
	} {
		if g := modesDemandes(texte); g != attendu {
			t.Errorf("modesDemandes(%q) = %q, want %q", texte, g, attendu)
		}
	}
}

// TestChangementsEnAttente: what a running pilot has not taken yet is told from what the pilot itself says it
// read. New settings ask a restart at the end of the cycle, once per pilot and per state of the settings: a
// cancelled restart is not asked again, a pilot that restarted without the latest change is asked again.
func TestChangementsEnAttente(t *testing.T) {
	kit := t.TempDir()
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		t.Fatal(err)
	}
	ecrireF := func(f, texte string) {
		t.Helper()
		if err := os.WriteFile(f, []byte(texte), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	envKit, stop := filepath.Join(kit, ".env"), filepath.Join(p.Atelier(), "STOP")
	ecrireF(envKit, "MODEL=opus\nCLAUDE_CODE_OAUTH_TOKEN=abc\n")
	ecrireF(p.envProjet(), "ECO=1\n")
	// the pilot of process pid, running, that read these settings and these modes
	pilote := func(pid int, reglages, modes string) {
		t.Helper()
		ecrireF(filepath.Join(p.Atelier(), "pilot.pid"), fmt.Sprint(pid)+"\n")
		b, _ := json.Marshal(map[string]any{"pid": pid, "phase": "travail", "reglages": reglages, "modes": modes})
		ecrireF(p.EtatJSON(), string(b))
	}
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{p}, ProjetActif: "a", Langue: "en"})
	demandes := make(chan string, 8)
	s.arretTest = func(p Projet) (string, error) {
		ecrireF(stop, "")
		demandes <- p.Nom
		return "", nil
	}
	demande := func() bool {
		select {
		case <-demandes:
			return true
		case <-time.After(300 * time.Millisecond):
			return false
		}
	}
	relance := func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.relancer["a"] }
	voir := func(attendu ...string) {
		t.Helper()
		if g := s.changementsEnAttente(p, p.PiloteActif()); !reflect.DeepEqual(g, attendu) && (len(g) > 0 || len(attendu) > 0) {
			t.Fatalf("waiting: %v, want %v", g, attendu)
		}
	}
	lus := empreinteReglagesKit(kit)
	pilote(4242, lus, modesDemandes("ECO=1\n"))

	// 1. nothing changed: nothing waits, nothing is asked
	voir()
	if demande() || relance() {
		t.Fatal("a restart asked with nothing changed")
	}
	// 2. a working mode switched on during the cycle: it waits for the next cycle, no restart
	ecrireF(p.envProjet(), "ECO=1\nFAST=1\n")
	voir("modes")
	if demande() || relance() {
		t.Fatal("a restart asked for a working mode")
	}
	// 3. a setting changed: the restart is asked by itself, once
	ecrireF(envKit, "MODEL=sonnet\nCLAUDE_CODE_OAUTH_TOKEN=abc\n")
	voir("modes", "reglages")
	if !demande() || !relance() || !fichierExiste(stop) {
		t.Fatal("the restart for the new settings is not asked")
	}
	voir("modes", "reglages")
	if demande() {
		t.Fatal("the restart is asked twice")
	}
	// 4. a second setting changed before the end of the cycle: the stop already asked covers it (the pilot
	// reads everything when it starts again)
	ecrireF(envKit, "MODEL=sonnet\nEFFORT=low\nCLAUDE_CODE_OAUTH_TOKEN=abc\n")
	voir("modes", "reglages")
	if demande() || !relance() {
		t.Fatal("the second change must ride the restart already asked")
	}
	// 5. the user cancels that restart: not asked again for the same settings, still told as waiting
	_ = os.Remove(stop)
	s.mu.Lock()
	delete(s.relancer, "a")
	s.mu.Unlock()
	voir("modes", "reglages")
	if demande() || relance() {
		t.Fatal("a cancelled restart is asked again")
	}
	// ... but asked again as soon as the settings change again
	ecrireF(envKit, "MODEL=sonnet\nEFFORT=medium\nCLAUDE_CODE_OAUTH_TOKEN=abc\n")
	voir("modes", "reglages")
	if !demande() || !relance() {
		t.Fatal("the restart is not asked for the settings changed after the cancel")
	}
	// 6. the pilot restarted (new process), but it read the settings before the last change, saved while it
	// started: it does not pass for up to date, and is asked to restart again
	_ = os.Remove(stop)
	s.mu.Lock()
	delete(s.relancer, "a")
	s.mu.Unlock()
	avantDernier := empreinteReglagesKit(kit)
	ecrireF(envKit, "MODEL=sonnet\nEFFORT=medium\nRELAY_CONTEXT_K=160\nCLAUDE_CODE_OAUTH_TOKEN=abc\n")
	pilote(4343, avantDernier, modesDemandes("ECO=1\nFAST=1\n"))
	voir("reglages")
	if !demande() || !relance() {
		t.Fatal("a pilot restarted without the last change must restart again")
	}
	// 7. restarted with everything: nothing waits any more
	_ = os.Remove(stop)
	s.mu.Lock()
	delete(s.relancer, "a")
	s.mu.Unlock()
	pilote(4444, empreinteReglagesKit(kit), modesDemandes("ECO=1\nFAST=1\n"))
	voir()
	if demande() || relance() {
		t.Fatal("a restart asked for a pilot that is up to date")
	}
	// 8. a new token alone: the pilot reads the connection at every cycle, no restart
	ecrireF(envKit, "MODEL=sonnet\nEFFORT=medium\nRELAY_CONTEXT_K=160\nCLAUDE_CODE_OAUTH_TOKEN=other\n")
	voir()
	// 9. the state file of the pilot before (the new one has not written its own yet): nothing is told
	ecrireF(envKit, "MODEL=haiku\n")
	ecrireF(filepath.Join(p.Atelier(), "pilot.pid"), "4545\n")
	voir()
	if demande() {
		t.Fatal("a restart asked from another pilot's state")
	}
	// 10. a stop the user asked is left as a stop
	pilote(4545, empreinteReglages("MODEL=old\n"), modesDemandes("ECO=1\nFAST=1\n"))
	ecrireF(stop, "")
	voir("reglages")
	if demande() || relance() {
		t.Fatal("the user's stop was turned into a restart")
	}
	// 11. saving the settings looks at the running pilots at once, without waiting for a page to refresh;
	// and the Overview tells what waits
	_ = os.Remove(stop)
	pilote(4646, empreinteReglagesKit(kit), modesDemandes("ECO=1\n"))
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	r := httptest.NewRequest("POST", "/api/env", strings.NewReader(`{"MODEL":"opus"}`))
	r.Header.Set("X-Athanor", "jeton")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "restarts with them at the end of its cycle") {
		t.Fatalf("saving the settings: %d %s", w.Code, w.Body.String())
	}
	if !demande() || !relance() {
		t.Fatal("saving the settings does not ask the running pilot to restart")
	}
	r = httptest.NewRequest("GET", "/api/etat?p=a", nil)
	r.Header.Set("X-Athanor", "jeton")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var vue struct {
		EnAttente    []string `json:"en_attente"`
		ArretDemande string   `json:"arret_demande"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &vue)
	if !reflect.DeepEqual(vue.EnAttente, []string{"modes", "reglages"}) || vue.ArretDemande != "relance" {
		t.Fatalf("the Overview: %+v\n%s", vue, w.Body.String())
	}
	// 12. a stopped pilot: nothing waits
	vieux := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(filepath.Join(p.Atelier(), "pilot.pid"), vieux, vieux)
	voir()
}
