package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestClesSurServeur: a key the owner gave a task goes on its server only through {{coffre:NAME}} in fichier_ecrire's
// contenu: another task's key or an unknown name is refused, an infra key is a destructive step, a command line never
// carries one, the value is put in at run time only, and the agent is told the names, never a value.
func TestClesSurServeur(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	tGH, tApp, tAutre := "ghp_temoin"+aleatoire(8), "app-temoin-"+aleatoire(8), "autre-temoin-"+aleatoire(8)
	gh := ajouter(t, s.coffre, "GitHub", "secret", "github", "infra", tGH)
	app := ajouter(t, s.coffre, "STRIPE_KEY", "secret", "stripe", "code", tApp)
	ajouter(t, s.coffre, "AUTRE", "secret", "jeton", "code", tAutre) // in the vault, not given to the task
	x := SessionHub{APIs: []string{gh.ID, app.ID}}
	etape := func(outil string, a map[string]any) Etape {
		b, _ := json.Marshal(a)
		return Etape{Outil: outil, Arguments: b, Classe: ClasseChangement, Raison: "writes a file"}
	}
	if l := clesNommees("a {{coffre:GitHub}} b {{coffre: GitHub }} {{coffre:STRIPE_KEY}}"); strings.Join(l, ",") != "GitHub,STRIPE_KEY" {
		t.Errorf("names: %v", l)
	}
	e := etape("fichier_ecrire", map[string]any{"chemin": "/root/.git-credentials", "contenu": "https://x-access-token:{{coffre:github}}@github.com\n"})
	if err := s.classerCles(x, "fichier_ecrire", &e); err != nil || e.Classe != ClasseDestructrice || !strings.Contains(e.Raison, "infra key GitHub") {
		t.Errorf("an infra key: %v %v %q", err, e.Classe, e.Raison)
	}
	e = etape("fichier_ecrire", map[string]any{"chemin": "/srv/app/.env", "contenu": "STRIPE_KEY={{coffre:STRIPE_KEY}}\n"})
	if err := s.classerCles(x, "fichier_ecrire", &e); err != nil || e.Classe != ClasseChangement || !strings.Contains(e.Raison, "key STRIPE_KEY") {
		t.Errorf("a code key: %v %v %q", err, e.Classe, e.Raison)
	}
	for _, c := range []Etape{etape("fichier_ecrire", map[string]any{"chemin": "/a", "contenu": "{{coffre:AUTRE}}"}), etape("fichier_ecrire", map[string]any{"chemin": "/a", "contenu": "{{coffre:INCONNUE}}"})} {
		if err := s.classerCles(x, "fichier_ecrire", &c); err == nil {
			t.Errorf("a key outside the task accepted: %s", c.Arguments)
		}
	}
	c := etape("ssh_commande", map[string]any{"commande": "echo {{coffre:STRIPE_KEY}} > /tmp/k"})
	if err := s.classerCles(x, "ssh_commande", &c); err == nil {
		t.Error("a key in a command line accepted")
	}
	// at run time: the value in the file's content, the other arguments untouched; nothing else is touched
	brut, _ := json.Marshal(map[string]any{"chemin": "/root/.git-credentials", "contenu": "https://x-access-token:{{coffre:GitHub}}@github.com\n", "sudo": true})
	args, cles, err := s.avecCles(x, "fichier_ecrire", brut)
	var a argsOutilServeur
	_ = json.Unmarshal(args, &a)
	if err != nil || !cles || a.Contenu == nil || *a.Contenu != "https://x-access-token:"+tGH+"@github.com\n" || a.Chemin != "/root/.git-credentials" || !a.Sudo {
		t.Fatalf("the value put in: %v %v %+v", err, cles, a)
	}
	if b, cles, err := s.avecCles(x, "ssh_commande", json.RawMessage(`{"commande":"ls"}`)); err != nil || cles || string(b) != `{"commande":"ls"}` {
		t.Errorf("another tool: %s %v %v", b, cles, err)
	}
	if _, _, err := s.avecCles(SessionHub{APIs: []string{app.ID}}, "fichier_ecrire", brut); err == nil {
		t.Error("another task's key put in")
	}
	if m := s.masquerSecrets(string(args)); strings.Contains(m, tGH) {
		t.Error("the audit line would show the value")
	}
	l := s.lignesClesTache(x.APIs, nil)
	if !strings.Contains(l, "GitHub (github, infra)") || !strings.Contains(l, "STRIPE_KEY (stripe, code)") || strings.Contains(l, "AUTRE") ||
		strings.Contains(l, tGH) || strings.Contains(l, tApp) || !strings.Contains(l, "{{coffre:NAME}}") {
		t.Errorf("what the agent is told: %s", l)
	}
}
