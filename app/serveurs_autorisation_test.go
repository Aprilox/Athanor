package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAutorisationModes (M244f): the 4 modes of the terminal's assistant × a safe and a dangerous command, decided
// in Go on the task's calls and plans; the sheet's tasks keep the plan rules; "ignorer" needs a confirmation.
func TestAutorisationModes(t *testing.T) {
	// the table: what each mode does with each class of the one list (classerCommande)
	attendu := map[string][3]string{
		"manuel":        {decisionProprietaire, decisionProprietaire, decisionProprietaire},
		"auto":          {decisionLibre, decisionLibre, decisionProprietaire},
		"modifications": {decisionLibre, decisionPlan, decisionProprietaire},
		"ignorer":       {decisionLibre, decisionLibre, decisionLibre},
		"":              {decisionProprietaire, decisionProprietaire, decisionProprietaire},
	}
	for m, d := range attendu {
		for c := ClasseLecture; c <= ClasseDestructrice; c++ {
			if got := decisionMode(m, c); got != d[c] {
				t.Errorf("mode %q, %s: %s, want %s", m, c, got, d[c])
			}
		}
	}
	for cmd, c := range map[string]Classe{"uptime": ClasseLecture, "systemctl restart nginx": ClasseChangement, "rm -rf /srv/vieux": ClasseDestructrice,
		"chmod 640 /etc/app.conf": ClasseChangement, "chmod 777 /etc/shadow": ClasseDestructrice, "chmod o+w /etc/passwd": ClasseDestructrice,
		"chmod a+rwx /srv": ClasseDestructrice, "chmod +w x": ClasseDestructrice, "chmod 1777 /srv/tmp": ClasseDestructrice, "chmod 755 /usr/local/bin/x": ClasseChangement,
		"chmod u+w,g-w x.sh": ClasseChangement, "chmod go-w x": ClasseChangement, "ufw allow 22": ClasseDestructrice, "shutdown -h now": ClasseDestructrice, "mkfs.ext4 /dev/sdb1": ClasseDestructrice} {
		if got, _ := classerCommande(cmd); got != c {
			t.Errorf("classerCommande(%q) = %s, want %s", cmd, got, c)
		}
	}

	signer, prive := nouvelleCleSSH(t)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, _ []byte) (string, int) { return "ok:" + cmd, 0 })
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	jeton, err := s.jetons.CreerSession(SessionHub{Projet: prefixeTache + "m244f", Mission: "m244f", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{srv.ID}})
	if err != nil {
		t.Fatal(err)
	}
	e := &tacheEnCours{t: TacheServeurs{ID: "m244f", Serveurs: []string{srv.ID}, Panneau: true}, hache: hacherJeton(jeton)}
	tachesEnCours.Lock()
	tachesEnCours.m[e.t.ID] = e
	tachesEnCours.Unlock()
	t.Cleanup(func() { tachesEnCours.Lock(); delete(tachesEnCours.m, "m244f"); tachesEnCours.Unlock() })
	appel := func(nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args))
		if rep.Error != nil {
			return rep.Error.Message, true
		}
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	choisir := func(mode string, confirmer bool) int {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"mode": mode, "confirmer": confirmer})
		r := httptest.NewRequest("POST", "/api/serveurs/"+srv.ID+"/autorisation", strings.NewReader(string(b)))
		r.SetPathValue("id", srv.ID)
		w := httptest.NewRecorder()
		s.apiAutorisationServeur(w, r)
		return w.Code
	}
	commande := func(cmd string) bool { // true: it ran on the server
		t.Helper()
		txt, e := appel("ssh_commande", `{"serveur":"`+srv.ID+`","commande":"`+cmd+`"}`)
		if e && !strings.Contains(txt, "plan required") {
			t.Fatalf("%s: unexpected error %s", cmd, txt)
		}
		return !e
	}
	plan := func(cmd string) string {
		t.Helper()
		txt, e := appel("proposer_plan", `{"serveur":"`+srv.ID+`","resume":"r","etapes":[{"outil":"ssh_commande","arguments":{"commande":"`+cmd+`"},"pourquoi":"p","sans_sauvegarde":"un dossier vide"}]}`)
		var r struct{ Etat string }
		if e || json.Unmarshal([]byte(txt), &r) != nil {
			t.Fatalf("proposer_plan %s: %s", cmd, txt)
		}
		return r.Etat
	}

	if c := choisir("ignorer", false); c != http.StatusConflict {
		t.Errorf("ignorer without confirmation: %d", c)
	}
	if c := choisir("tout", true); c != http.StatusBadRequest {
		t.Errorf("unknown mode: %d", c)
	}
	// never chosen: manuel, a read waits for a plan too
	if commande("uptime") {
		t.Error("manuel by default: a read ran without a plan")
	}
	for _, m := range []struct {
		mode                string
		sure, dangereuse    bool   // ran at once
		planSur, planDanger string // a proposed plan's state
	}{
		{"manuel", false, false, "attente", "attente"},
		{"auto", true, false, "acceptee", "attente"},
		{"modifications", true, false, "acceptee", "attente"},
		{"ignorer", true, true, "acceptee", "acceptee"},
	} {
		if c := choisir(m.mode, true); c != 200 {
			t.Fatalf("choose %s: %d", m.mode, c)
		}
		if l, _ := lireServeurs(); l[0].Autorisation != m.mode {
			t.Fatalf("mode not kept: %q", l[0].Autorisation)
		}
		if got := commande("uptime"); got != m.sure {
			t.Errorf("%s, safe command: ran %v", m.mode, got)
		}
		if got := commande("rm -rf /srv/vieux"); got != m.dangereuse {
			t.Errorf("%s, dangerous command: ran %v", m.mode, got)
		}
		if got := plan("systemctl restart nginx"); got != m.planSur {
			t.Errorf("%s, plan of a change: %s", m.mode, got)
		}
		if got := plan("rm -rf /srv/vieux"); got != m.planDanger {
			t.Errorf("%s, dangerous plan: %s", m.mode, got)
		}
	}
	// modifications: a change runs only as a step of a plan (accepted at once)
	choisir("modifications", false)
	if commande("systemctl restart caddy") {
		t.Error("modifications: a change ran without a plan")
	}
	// a task of the sheet (not the panel): the plan rules, whatever the mode
	choisir("ignorer", true)
	e.mu.Lock()
	e.t.Panneau = false
	e.mu.Unlock()
	if !commande("uptime") || commande("rm -rf /srv/autre") {
		t.Error("a sheet's task follows the assistant's mode")
	}
	// the audit names the mode, never more than the class
	b := journalAuditTexte(t)
	if !strings.Contains(b, "mode de l'assistant") || !strings.Contains(b, "mode « ignorer »") {
		t.Errorf("audit: mode changes or grants missing")
	}
}
