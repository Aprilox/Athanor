package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestHubChatActionsServeur (M296): the `serveur` card of the home runs a task on servers, on API keys alone or on
// both, at the card's permission mode; nothing starts before Launch; its servers and keys are the proposal's.
func TestHubChatActionsServeur(t *testing.T) {
	b := nouveauBancCarteProjet(t,
		`{"type":"serveur","serveurs":["@VPS"],"texte":"Installe nginx.","permission":"auto"}`,                   // 1
		`{"type":"serveur","apis":["clé stripe"],"texte":"Liste les clés."}`,                                     // 2
		`{"type":"serveur","serveurs":["vps"],"apis":["Clé Stripe"],"texte":"Les deux.","permission":"ignorer"}`, // 3
		`{"type":"serveur","serveurs":["nulle-part"],"texte":"x"}`,                                               // 4
		`{"type":"serveur","texte":"x"}`,                                                                         // 5
		`{"type":"serveur","serveurs":["vps"],"texte":"x","permission":"tout"}`,                                  // 6
		`{"type":"serveur","serveurs":["vps"]}`,                                                                  // 7
		`{"type":"serveur","apis":["nulle-part"],"texte":"x"}`,                                                   // 8
	)
	s := b.s
	ancienne := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancienne })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error { // the fake chef ops: no plan
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"nothing to do"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	finies := func() {
		t.Helper()
		for range 400 { // the tasks end (their goroutines must not outlive the test)
			tachesEnCours.Lock()
			n := len(tachesEnCours.m)
			tachesEnCours.Unlock()
			if n == 0 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("a task never ended")
	}
	t.Cleanup(finies)
	taches := func() int { return len(tachesServeur(b.vps)) + len(tachesServeur(serveurAPI)) }

	// the cards: what, on what (by name), with which accesses, why not
	for i, v := range map[int][3]string{
		1: {"Run a task on your servers", "vps", ""},
		2: {"Run a task with your API keys", "Clé Stripe", ""},
		3: {"Run a task on your servers, with your API keys", "vps, Clé Stripe", ""},
		4: {"Run a task on your servers", "nulle-part", "Unknown server"},
		5: {"Run a task on your servers", "", "A task needs at least one server or one API"},
		6: {"Run a task on your servers", "vps", "Unknown permission mode (manuel, auto, modifications or ignorer)"},
		7: {"Run a task on your servers", "vps", "Write what it must do."},
		8: {"Run a task with your API keys", "nulle-part", "Unknown vault entry"},
	} {
		if c := b.carte(i); c.Quoi != v[0] || c.Sur != v[1] || c.Erreur != v[2] || c.Empreinte == "" || c.Projet != "" {
			t.Errorf("card %d: %q on %q, error %q, fingerprint %q", i, c.Quoi, c.Sur, c.Erreur, c.Empreinte)
		}
	}
	if c := b.carte(3); !slices.Equal(c.Serveurs, []string{b.vps}) || !slices.Equal(c.APIs, []string{b.api}) || len(c.Acces) != 3 || !strings.HasPrefix(c.Acces[2], "Ignore permissions") {
		t.Fatalf("card on both: %+v", c)
	}
	if taches() != 0 {
		t.Fatal("a task started before Launch")
	}

	// refused: an unknown server, no text, an older view, Ignore permissions without its confirmation
	for i, code := range map[int]int{4: 400, 5: 400, 6: 400, 7: 400, 8: 400, 3: 409} {
		if c, corps := b.lancer(t, i, "", nil); c != code {
			t.Errorf("card %d launched: %d %s", i, c, corps)
		}
	}
	if c, corps := b.lancer(t, 1, "", map[string]any{"empreinte": "ancienne"}); c != 409 {
		t.Errorf("older view launched: %d %s", c, corps)
	}
	if c, corps := b.lancer(t, 3, "tel", map[string]any{"confirmer": true}); c != 403 {
		t.Errorf("Ignore permissions from a device: %d %s", c, corps)
	}
	if taches() != 0 {
		t.Fatal("a refused card started a task")
	}

	// a server alone, at the card's mode; the request cannot add a server or a key the card did not show
	autre := b.carte(1).ActionChat
	autre.APIs, autre.Texte = []string{b.api}, "Installe nginx et caddy."
	if c, corps := b.lancer(t, 1, "", map[string]any{"action": autre}); c != 200 {
		t.Fatalf("server card: %d %s", c, corps)
	}
	if l := tachesServeur(b.vps); len(l) != 1 || l[0].Mode != "auto" || len(l[0].APIs) != 0 || !slices.Equal(l[0].Serveurs, []string{b.vps}) || l[0].Texte != "Installe nginx et caddy." {
		t.Fatalf("server task: %+v", l)
	}
	// an API alone: no server, the mode of Settings › Permissions (manuel)
	if c, corps := b.lancer(t, 2, "", nil); c != 200 {
		t.Fatalf("API card: %d %s", c, corps)
	}
	if l := tachesServeur(serveurAPI); len(l) != 1 || l[0].Mode != "manuel" || len(l[0].Serveurs) != 0 || !slices.Equal(l[0].APIs, []string{b.api}) {
		t.Fatalf("API task: %+v", l)
	}
	if c, _ := b.lancer(t, 2, "", nil); c != 409 {
		t.Errorf("launched twice: %d", c)
	}
	finies()

	// the decided card: launched, its task's state and answer read at each view; the audit log
	w := httptest.NewRecorder()
	s.apiHubChat(w, httptest.NewRequest("GET", "/api/hub/chat", nil))
	var vue struct{ Actions map[string]CarteActionChat }
	_ = json.Unmarshal(w.Body.Bytes(), &vue)
	if c := vue.Actions["2"]; c.Etat != "lancee" || c.Resultat != "Task started on {0}." || c.Args[0] != "Clé Stripe" || c.EtatTache == "" || c.Bilan != "nothing to do" {
		t.Errorf("decided card: %+v", c)
	}
	var audit []string
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if strings.HasPrefix(l.Action, "chat d'accueil") {
			audit = append(audit, l.Action+" → "+l.Resultat)
		}
	}
	if j := strings.Join(audit, "\n"); !strings.Contains(j, "Run a task on your servers (vps) → lancée") || !strings.Contains(j, "Run a task with your API keys (Clé Stripe) → lancée") || !strings.Contains(j, "refusée : ") {
		t.Errorf("audit:\n%s", j)
	}

	// the assistant is told about the `serveur` card, no longer about missions; every word is translated
	if !strings.Contains(consigneActionsAccueil, `{"type": "serveur"`) || strings.Contains(consigneActionsAccueil, `"mission"`) {
		t.Error("the card instructions still propose a mission, or no `serveur` card")
	}
	cles := []string{"Task started on {0}.", "Unknown server", "Unknown vault entry", "A task needs at least one server or one API", "Native Windows mode has no barrier: this card starts no task here. Switch to WSL in Settings › Options, or tick “Don't show the native Windows mode warnings anymore” there, knowing the risk."}
	for _, i := range []int{1, 2, 3} {
		c := b.carte(i)
		cles = append(append(cles, c.Quoi), c.Acces...)
	}
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		for _, c := range cles {
			if _, ok := dico[c]; !ok {
				t.Errorf("%s: no translation of %q", l, c)
			}
		}
	}
}
