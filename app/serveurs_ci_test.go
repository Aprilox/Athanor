package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestServeursCIConteneur (M100): what the CI job servers-ssh checks on the test container, through a Servers
// agent's real MCP route: the addition, the sheet, a read, a real change by an accepted plan, a destructive
// step without an accord refused, bash -c and sudo masking refused, an unconfirmed firewall rule undone (10 s).
func TestServeursCIConteneur(t *testing.T) {
	s, _, id, _, direct := serveurConteneurTest(t)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	avant := delaiAnnulation
	delaiAnnulation = 10 * time.Second
	t.Cleanup(func() { delaiAnnulation = avant })
	const fichier = "/tmp/athanor-ci.txt"
	t.Cleanup(func() {
		direct("ssh_commande", map[string]any{"commande": "rm -f " + fichier + "*; sudo -n ufw --force delete allow 8383/tcp; sudo -n rm -rf /var/backups/athanor"})
	})
	_, appel, _ := serveursSessionTest(t, s, id)
	outil := func(nom, args string) ResultatOutil {
		t.Helper()
		txt, e := appel(nom, `{"serveur":"`+id+`"`+args+`}`)
		var r ResultatOutil
		if e || json.Unmarshal([]byte(jsonDeDonnees(txt)), &r) != nil {
			t.Fatalf("%s %s: %v %q", nom, args, e, txt)
		}
		return r
	}
	refuse := func(nom, args, motif string) {
		t.Helper()
		if txt, e := appel(nom, `{"serveur":"`+id+`"`+args+`}`); !e || !strings.Contains(txt, motif) {
			t.Errorf("%s %s: %v %q (expected %q)", nom, args, e, txt, motif)
		}
	}
	proposer := func(etapes string) string {
		t.Helper()
		txt, e := appel("proposer_plan", `{"serveur":"`+id+`","resume":"essai CI","etapes":[`+etapes+`]}`)
		var r struct{ Plan string }
		if e || json.Unmarshal([]byte(txt), &r) != nil || r.Plan == "" {
			t.Fatalf("proposer_plan: %v %s", e, txt)
		}
		return r.Plan
	}
	accepter := func(plan string) {
		t.Helper()
		if _, _, err := s.deciderApprobation("fenetre", plan, "accepter", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}

	// the sheet and a read, with no plan
	if txt, e := appel("serveur_fiche", `{"serveur":"`+id+`"}`); e || !strings.Contains(strings.ToLower(txt), "debian") {
		t.Errorf("sheet: %v %q", e, txt)
	}
	if r := outil("ssh_commande", `,"commande":"cat /etc/os-release"`); r.Code != 0 || !strings.Contains(r.Sortie, "ID=debian") {
		t.Errorf("read command: %+v", r)
	}

	// a real change by an accepted plan
	ecrire := `,"chemin":"` + fichier + `","contenu":"ci\n"`
	refuse("fichier_ecrire", ecrire, "plan required")
	accepter(proposer(`{"outil":"fichier_ecrire","arguments":{"chemin":"` + fichier + `","contenu":"ci\n"},"pourquoi":"essai"}`))
	if r := outil("fichier_ecrire", ecrire); r.Code != 0 {
		t.Errorf("accepted step: %+v", r)
	}
	if r := outil("fichier_lire", `,"chemin":"`+fichier+`"`); r.Sortie != "ci\n" {
		t.Errorf("file written by the plan: %q", r.Sortie)
	}

	// destructive without an accord, then masked behind bash -c and sudo: refused, the file still there
	proposer(`{"outil":"ssh_commande","arguments":{"commande":"rm -f ` + fichier + `"},"pourquoi":"place","sauvegarde":["` + fichier + `"]}`)
	refuse("ssh_commande", `,"commande":"rm -f `+fichier+`"`, "waits for the owner")
	refuse("ssh_commande", `,"commande":"bash -c 'rm -f `+fichier+`'"`, "plan required")
	refuse("ssh_commande", `,"commande":"sudo rm -f `+fichier+`"`, "plan required")
	refuse("ssh_commande", `,"commande":"sudo -n bash -c 'rm -f `+fichier+`'"`, "plan required")
	if r := outil("ssh_commande", `,"commande":"cat `+fichier+`"`); r.Sortie != "ci\n" {
		t.Errorf("a refused step touched the file: %+v", r)
	}

	// a firewall rule by an accepted plan, left unconfirmed: the server puts it back after 10 s
	regle := `,"action":"allow","port":8383,"proto":"tcp"`
	accepter(proposer(`{"outil":"pare_feu","arguments":{"action":"allow","port":8383,"proto":"tcp"},"pourquoi":"essai"}`))
	r := outil("pare_feu", regle)
	if r.Code != 0 || r.Annulation == "" {
		t.Fatalf("firewall rule without its rollback: %+v", r)
	}
	if !strings.Contains(outil("pare_feu", `,"action":"status"`).Sortie, "8383/tcp") {
		t.Error("firewall rule missing")
	}
	etat := ""
	for fin := time.Now().Add(30 * time.Second); time.Now().Before(fin); time.Sleep(time.Second) {
		etat = direct("ssh_commande", map[string]any{"commande": "sudo -n ufw show added; sudo -n cat /var/backups/athanor/annul-" + r.Annulation + ".log"}).Sortie
		if !strings.Contains(etat, "8383") && strings.Contains(etat, "rolled back") {
			break
		}
	}
	if strings.Contains(etat, "8383") || !strings.Contains(etat, "rolled back") {
		t.Errorf("the unconfirmed rule after the delay: %q", etat)
	}
	time.Sleep(500 * time.Millisecond) // Athanor's own timer, a little after the server's
	if l := annulationsDe(id); len(l) != 0 {
		t.Errorf("still pending: %+v", l)
	}
	if j := journalAuditTexte(t); !strings.Contains(j, "non confirmé : remis par le serveur") {
		t.Error("the rollback is not in the audit log")
	}
}
