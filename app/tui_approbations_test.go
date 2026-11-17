package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestTUIApprobations: For you's approvals in the terminal against a server: the card of each (short SHA, URL,
// prod, ticked accesses), y/r/t through POST /api/hub/approbations with Athanor's password (none
// refused, a wrong one refused, the right one accepted), a launch whose card changed since it was asked refused
// by its print, "never ask again" only where the web offers it, no witness value ever on screen, then ! the
// emergency stop, confirmed.
func TestTUIApprobations(t *testing.T) {
	s := serveurCoffreTest(t)
	natif(t, false)
	avecMdpPreuve(s)
	s.config.Langue = "fr"
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	demo := Projet{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}
	if err := os.MkdirAll(filepath.Join(demo.Atelier(), "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.config.Projets = []Projet{demo}
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	temoin := "temoin-tui-appr-" + aleatoire(6)
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "STRIPE_SECRET_KEY", Famille: "secret", Type: "stripe", Portee: "code"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	c := clientTUIPour(t, s)
	if err := c.appel("POST", "deverrouiller", map[string]string{"mot_de_passe": mdpPreuveTest}, nil); err != nil {
		t.Fatal(err)
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	demander := func(x Approbation) Approbation {
		t.Helper()
		y, err := s.demanderApprobation(x)
		if err != nil {
			t.Fatal(err)
		}
		return y
	}
	prod := demander(Approbation{Projet: "demo", Env: "prod", Agent: "relais", Action: actionDeploiement, Raison: "commit " + sha + " → martin.test",
		Details: map[string]string{"sha": sha, "env": "prod", "url": "https://martin.test", "hote": "martin.test", "serveurs": "vps-1"}})
	acces := demander(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "demander_acces", Details: map[string]string{"nom": "STRIPE_SECRET_KEY"}, Raison: "paiements de test"})

	a := nouvelleAppTUI(c, false)
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil })
	taper(a, "6")
	if a.page != pPourVous {
		t.Fatalf("6: page %d", a.page)
	}
	attendreTUI(t, a, func() bool { return len(a.apprAttente()) == 2 })
	n := 0
	ecran := func(l, h int) string {
		e := nouvelEcran(l, h)
		a.dessiner(e)
		txt := e.texteEcran()
		if strings.Contains(txt, temoin) {
			t.Fatalf("the witness value on screen:\n%s", txt)
		}
		if o := os.Getenv("TUI_OUT"); o != "" {
			n++
			_ = os.MkdirAll(o, 0o755)
			_ = os.WriteFile(filepath.Join(o, fmt.Sprintf("appr-%02d-%dx%d.txt", n, l, h)), []byte(txt), 0o644)
		}
		return txt
	}
	contient := func(quoi, txt string, l ...string) {
		t.Helper()
		for _, x := range l {
			if !strings.Contains(txt, x) {
				t.Fatalf("%s: no %q in\n%s", quoi, x, txt)
			}
		}
	}
	choisir := func(id string) {
		t.Helper()
		i := slices.IndexFunc(a.apprAttente(), func(x Approbation) bool { return x.ID == id })
		if i < 0 {
			t.Fatalf("%s not pending", id)
		}
		a.selTache = i
	}
	etat := func(id, e string) func() bool {
		return func() bool { return s.approbations.PourAgent(id) == e }
	}
	// prod: the short SHA, its URL; never a rule
	choisir(prod.ID)
	d := ecran(140, 40)
	contient("prod", d, a.t("Deploy {0} to production?", "martin.test"), a.t("Commit {0}", sha[:7]), "https://martin.test", a.t("Servers: {0}", "vps-1"),
		a.t("Production always asks you: one accord, one deployment."), strings.Fields(a.t("Any program of this PC can drive this window, so a decision asks for Athanor's password."))[0])
	if slices.ContainsFunc(a.raccourcisPage(), func(r raccourci) bool { return r.k == "t" }) {
		t.Fatal("never ask again offered for prod")
	}
	for _, taille := range [][2]int{{80, 24}, {40, 10}, {20, 5}} {
		_ = ecran(taille[0], taille[1])
	}
	// y: the password asked; empty, then wrong, refused (the form kept, the approval pending)
	taper(a, "y")
	if a.form == nil || len(a.form.champs) != 1 {
		t.Fatal("y: no password asked")
	}
	taper(a, "\r")
	attendreTUI(t, a, func() bool { return a.form != nil && a.form.erreur != "" })
	a.form.erreur = ""
	a.form.champs[0].s.mettreTexte("faux")
	taper(a, "\r")
	attendreTUI(t, a, func() bool { return a.form != nil && a.form.erreur != "" })
	if s.approbations.PourAgent(prod.ID) != "attente" {
		t.Fatal("decided without the right password")
	}
	a.form.champs[0].s.mettreTexte(mdpPreuveTest)
	taper(a, "\r")
	attendreTUI(t, a, etat(prod.ID, "acceptee"))
	// an access: the vault's key chosen (its name only), the variable proposed (no environment asked), then the password
	a.derniere["approbations"] = time.Time{}
	attendreTUI(t, a, func() bool { return a.form == nil && len(a.apprAttente()) == 1 })
	choisir(acces.ID)
	contient("access", ecran(140, 40), "demo · "+a.t("access requested"), a.t("proposed variable: {0}", "STRIPE_SECRET_KEY"), "paiements de test", "t "+a.t("Never ask again"))
	taper(a, "y")
	attendreTUI(t, a, func() bool { return a.dialogue != nil })
	contient("vault keys", ecran(140, 40), "STRIPE_SECRET_KEY", a.t("Vault key (code)"))
	taper(a, "\r")
	if a.form == nil || len(a.form.champs) != 2 || a.form.champs[0].s.texte() != "STRIPE_SECRET_KEY" {
		t.Fatalf("access form: %+v", a.form)
	}
	a.form.champs[1].s.mettreTexte(mdpPreuveTest)
	_ = ecran(140, 40)
	taper(a, "\x1bOQ") // F2: save (Enter goes to the next field)
	attendreTUI(t, a, etat(acces.ID, "acceptee"))
	if g := s.acces.Projet("demo").Acces; len(g) != 1 || g[0].Entree != e.ID || g[0].Env != "dev" {
		t.Fatalf("grant: %+v", g)
	}
	_ = ecran(140, 40)
	// r: refused with the password
	autre := demander(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "demander_acces", Details: map[string]string{"nom": "AUTRE_KEY"}, Raison: "autre"})
	a.derniere["approbations"] = time.Time{}
	attendreTUI(t, a, func() bool { l := a.apprAttente(); return len(l) == 1 && l[0].ID == autre.ID && a.form == nil })
	choisir(autre.ID)
	taper(a, "r")
	if a.form == nil || len(a.form.champs) != 1 {
		t.Fatal("r: no password asked")
	}
	a.form.champs[0].s.mettreTexte(mdpPreuveTest)
	taper(a, "\r")
	attendreTUI(t, a, etat(autre.ID, "refusee"))
	// !: confirmed first (Escape cancels), then the stop: the pending approvals refused, the vault locked
	reste := demander(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "demander_acces", Details: map[string]string{"nom": "ENCORE_KEY"}, Raison: "encore"})
	taper(a, "!")
	if a.dialogue == nil {
		t.Fatal("!: no confirmation")
	}
	taper(a, "\x1b")
	if s.coffre.Urgence() != "" || s.approbations.PourAgent(reste.ID) != "attente" {
		t.Fatal("stopped without the confirmation")
	}
	taper(a, "!")
	taper(a, "\r")
	attendreTUI(t, a, func() bool { return s.coffre.Urgence() != "" && s.approbations.PourAgent(reste.ID) == "refusee" })
	_ = ecran(140, 40)
	// a Servers plan, step by step: its class, its arguments in a stable order, why, then the critic
	var lp []string
	for _, l := range a.lignesPlanTUI(PlanServeur{Resume: "Installer Caddy", Etapes: []EtapePlan{{Outil: "ecrire_fichier", Classe: "destructrice",
		Lisible: `{"chemin":"/etc/caddy/Caddyfile","a":1}`, Pourquoi: "le site"}}, Critique: &CritiquePlan{Verdict: "risques", Lignes: []string{"écrase un fichier"}}}) {
		lp = append(lp, l.s)
	}
	contient("plan", strings.Join(lp, "\n"), "Installer Caddy\n1. ecrire_fichier ["+a.t("destructive")+"]\n   a 1\n   chemin /etc/caddy/Caddyfile\n   le site\n"+
		a.t("Security critic")+": "+a.t("Risks found")+"\n   écrase un fichier")
}
