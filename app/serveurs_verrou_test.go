package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestVerrouEtatAssistant (M242a): the assistant's one state, and the lock of the owner's terminal only while one
// of its commands really runs there: never while it thinks (a mute assistant), waits for a decision, failed, was
// stopped, after Take over, on another terminal session (reload, reconnection), nor for a call whose end was lost.
func TestVerrouEtatAssistant(t *testing.T) {
	ctx, annuler := context.WithCancelCause(context.Background())
	maintenant := time.Now()
	enCours := ActionTache{N: 1, T: maintenant.UTC().Format(time.RFC3339), Outil: "ssh_commande", Quoi: "df -h", Etat: "en_cours"}
	e := &tacheEnCours{t: TacheServeurs{ID: "verrou1", Serveurs: []string{"ab12"}, Etat: "preparation", Direct: true, Terminal: "sess"}, annuler: annuler, ctx: ctx}
	tachesEnCours.Lock()
	tachesEnCours.m[e.t.ID] = e
	tachesEnCours.Unlock()
	t.Cleanup(func() {
		tachesEnCours.Lock()
		delete(tachesEnCours.m, "verrou1")
		tachesEnCours.Unlock()
	})
	cas := func(nom, etat string, actions []ActionTache, terminal, assistant string, verrou bool) {
		t.Helper()
		e.mu.Lock()
		e.t.Etat, e.t.Actions = etat, actions
		e.mu.Unlock()
		if c := e.copie(); c.Assistant != assistant || c.Verrou != verrou {
			t.Errorf("%s: assistant=%q verrou=%v, want %q %v", nom, c.Assistant, c.Verrou, assistant, verrou)
		}
		if got := tacheDirecte(terminal) != nil; got != verrou {
			t.Errorf("%s: typing locked=%v, want %v", nom, got, verrou)
		}
	}
	cas("mute assistant, no command", "preparation", nil, "sess", "travail", false)
	cas("command running", "application", []ActionTache{enCours}, "sess", "travail", true)
	if tacheDirecte("autre") != nil { // a reload or a reconnection opens another session
		t.Error("another terminal session is locked by the task's command")
	}
	cas("command ended", "application", []ActionTache{{N: 1, T: enCours.T, Etat: "ok"}}, "sess", "travail", false)
	cas("command failed", "application", []ActionTache{{N: 1, T: enCours.T, Etat: "erreur"}}, "sess", "travail", false)
	cas("refused", "application", []ActionTache{{N: 1, T: enCours.T, Etat: "refusee"}}, "sess", "travail", false)
	cas("waiting for a decision", "attente_accord", []ActionTache{enCours}, "sess", "attente_decision", false)
	cas("critique", "critique", nil, "sess", "travail", false)
	cas("task failed", "echec", []ActionTache{enCours}, "sess", "erreur", false)
	vieux := enCours
	vieux.T = maintenant.Add(-maxDureeVerrou - time.Minute).UTC().Format(time.RFC3339)
	cas("call whose end was lost", "application", []ActionTache{vieux}, "sess", "travail", false)

	// the guard: once a call locked the terminal, delaiClavierLibre without a command says the keyboard is free, once
	s := &Serveur{}
	e.mu.Lock()
	e.t.Etat, e.t.Actions, e.annonce, e.finAppel = "application", []ActionTache{enCours}, true, maintenant.Add(-time.Minute)
	e.mu.Unlock()
	if s.gardeFouVerrou(e, maintenant) {
		t.Error("guard: said free while a command runs")
	}
	e.mu.Lock()
	e.t.Actions, e.finAppel = []ActionTache{{N: 1, T: enCours.T, Etat: "ok"}}, maintenant.Add(-time.Second)
	e.mu.Unlock()
	if s.gardeFouVerrou(e, maintenant) {
		t.Error("guard: said free 1 s after the command ended")
	}
	if !s.gardeFouVerrou(e, maintenant.Add(delaiClavierLibre)) || s.gardeFouVerrou(e, maintenant.Add(2*delaiClavierLibre)) {
		t.Error("guard: the free keyboard is said once, delaiClavierLibre after the last command")
	}

	// a stop: unlocked at once, its late states never bring the task back
	cas("before the stop", "application", []ActionTache{enCours}, "sess", "travail", true)
	annuler(errTacheArretee)
	e.etat("application", "")
	cas("stopped, its session still dying", "application", []ActionTache{enCours}, "sess", "arretee", false)
	if e.t.Etat != "application" {
		t.Errorf("a late state was written after the stop: %s", e.t.Etat)
	}
}

// TestIATerminalArretLibere (M242a): mode "live in my terminal" with an assistant that never answers once a command
// of its runs (its session ignores the stop): Stop gives the keyboard and the files back in less than a second, says
// it on the terminal once, and the task reads stopped at once; before any command, its thinking locks nothing.
func TestIATerminalArretLibere(t *testing.T) {
	commence, libere := make(chan struct{}), make(chan struct{})
	b := ouvrirBancIA(t, func(cmd string, _ []byte) (string, int) {
		if strings.Contains(cmd, "df -h") {
			close(commence)
			<-libere
		}
		return "ok", 0
	})
	t.Cleanup(func() {
		select {
		case <-libere:
		default:
			close(libere)
		}
	})
	pense, fin := make(chan struct{}), make(chan struct{})
	ancien := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancien })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		jeton, _ := os.ReadFile(filepath.Join(dossier, "hub-jeton"))
		<-pense // it thinks: no command yet
		appelMCP(t, b.mux, "Bearer "+strings.TrimSpace(string(jeton)), appelOutil("ssh_commande", `{"serveur":"ab12","commande":"df -h /"}`))
		<-fin // mute: it never ends by itself, the stop included
		return context.Canceled
	}
	rep, v := b.requete("POST", "/api/serveurs/"+b.srv.ID+"/taches", `{"texte":"Regarde le disque","terminal":"`+b.session+`"}`)
	tache, _ := v["tache"].(map[string]any)
	if rep.StatusCode != http.StatusOK || tache == nil {
		t.Fatalf("launch live: %d %v", rep.StatusCode, v)
	}
	id := tache["id"].(string)
	if c := b.taper(`{"entree":"echo pendant\r"}`); c != http.StatusOK {
		t.Errorf("typing while the assistant thinks: %d, want 200", c)
	}
	close(pense)
	select {
	case <-commence:
	case <-time.After(20 * time.Second):
		t.Fatal("the command never reached the server")
	}
	if c := b.taper(`{"entree":"echo moi\r"}`); c != http.StatusLocked {
		t.Errorf("typing while its command runs: %d, want 423", c)
	}
	debut := time.Now()
	if c, _ := b.requete("POST", "/api/serveurs/"+b.srv.ID+"/taches/"+id+"/arreter", ""); c.StatusCode != http.StatusOK {
		t.Fatalf("stop: %d", c.StatusCode)
	}
	if c := b.taper(`{"entree":"echo libre\r"}`); c != http.StatusOK {
		t.Errorf("typing after Stop: %d, want 200", c)
	}
	if c, _ := b.requete("POST", "/api/serveurs/"+b.srv.ID+"/sftp/"+b.session, `{"supprimer":"/srv/rien"}`); c.StatusCode == http.StatusLocked {
		t.Error("files still locked after Stop")
	}
	if d := time.Since(debut); d > time.Second {
		t.Errorf("Stop gave the keyboard back in %v, want < 1 s", d)
	}
	var x TacheServeurs
	for _, y := range tachesServeur(b.srv.ID) {
		if y.ID == id {
			x = y
		}
	}
	if x.Etat != "arretee" || x.Assistant != "arretee" || x.Verrou {
		t.Errorf("task right after Stop: etat=%s assistant=%s verrou=%v", x.Etat, x.Assistant, x.Verrou)
	}
	b.l.attendre(t, "The assistant was stopped")
	if c, _ := b.requete("POST", "/api/serveurs/"+b.srv.ID+"/taches/"+id+"/arreter", ""); c.StatusCode != http.StatusOK {
		t.Errorf("Stop again while its session dies: %d", c.StatusCode)
	}
	close(fin)
	close(libere)
	for range 200 {
		tachesEnCours.Lock()
		_, enCours := tachesEnCours.m[id]
		tachesEnCours.Unlock()
		if !enCours {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if c, _ := b.requete("POST", "/api/serveurs/"+b.srv.ID+"/taches/"+id+"/arreter", ""); c.StatusCode != http.StatusOK {
		t.Errorf("Stop on an ended task (the page's view was late): %d", c.StatusCode)
	}
	if c, _ := b.requete("POST", "/api/serveurs/"+b.srv.ID+"/taches/"+id+"/main", ""); c.StatusCode != http.StatusOK {
		t.Errorf("Take over on an ended task: %d", c.StatusCode)
	}
	b.l.mu.Lock()
	ecran := b.l.b.String()
	b.l.mu.Unlock()
	if strings.Count(ecran, "The assistant was stopped") != 1 || strings.Contains(ecran, "task ended") {
		t.Errorf("the stop is said once on the terminal: %q", ecran)
	}
	b.taper(`{"fermer":true}`)
	b.l.attendreFin(t)
}
