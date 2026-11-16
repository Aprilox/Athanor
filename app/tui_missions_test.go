package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestTUIMissions: the Missions page (key E) against a server: the list and the home's block as /api/missions
// gives them, a mission's lanes, relay as text and 10 newest journal lines (translated), then p, r (wrong password
// refused, the right one accepted) and a (confirmed) through the API.
func TestTUIMissions(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	s.config.Langue = "fr"
	c := clientTUIPour(t, s)
	if err := c.appel("POST", "deverrouiller", map[string]string{"mot_de_passe": mdpPreuveTest}, nil); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	maintenant := time.Now().UTC()
	carte, _ := json.Marshal(CarteMission{Titre: "Site de Martin", Projet: ProjetCarte{Nom: "martin"},
		Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.martin.test"}, {Nom: "prod", URL: "https://martin.test"}}})
	m := Mission{ID: "tuimis01", Phrase: "Mettre le site de Martin en ligne", Etat: "lancee", Politique: "prod", Carte: carte,
		Voies: []VoieMission{{Module: "dev", Ref: "martin", Etat: "lancee"}}, Relais: &RelaisMission{Etape: "dev_verifie"}, Cree: maintenant.Format(formatCree)}
	for i := 1; i <= 12; i++ {
		m.Journal = append(m.Journal, EntreeJournal{ID: fmt.Sprintf("jalon:A%02d", i), T: maintenant.Add(time.Duration(i-12) * time.Minute).Format(time.RFC3339),
			Cle: "Milestone delivered: {0}", Params: []string{fmt.Sprintf("A%02d", i)}, Niveau: "info"})
	}
	ancienne, _ := json.Marshal(CarteMission{Titre: "Ancienne mission"})
	for _, x := range []Mission{m, {ID: "tuimis02", Phrase: "x", Etat: "terminee", Politique: "prod", Carte: ancienne, Cree: maintenant.Add(-time.Hour).Format(formatCree)}} {
		if err := enregistrerMission(x); err != nil {
			t.Fatal(err)
		}
	}
	a := nouvelleAppTUI(c, false)
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil && a.hub != nil && len(a.missions) == 2 })
	n := 0
	ecran := func(l, h int) string { // the screens go to TUI_OUT (a folder) when set
		e := nouvelEcran(l, h)
		a.dessiner(e)
		if o := os.Getenv("TUI_OUT"); o != "" {
			n++
			_ = os.MkdirAll(o, 0o755)
			_ = os.WriteFile(filepath.Join(o, fmt.Sprintf("missions-%02d-%dx%d.txt", n, l, h)), []byte(e.texteEcran()), 0o644)
		}
		return e.texteEcran()
	}
	contient := func(quoi string, txt string, l ...string) {
		t.Helper()
		for _, x := range l {
			if !strings.Contains(txt, x) {
				t.Fatalf("%s: no %q in\n%s", quoi, x, txt)
			}
		}
	}
	lancee := a.t("Launched")
	if lancee == "Launched" {
		t.Fatal("not translated")
	}
	// the home's block: the newest first, its relay step, See all
	contient("home", ecran(120, 40), "Site de Martin", "Ancienne mission", lancee, a.t("Relay: {0}", a.t("Your accord")), a.t("See all ({0})", 2))
	_ = ecran(80, 24)
	taper(a, "E")
	if a.page != pMissions {
		t.Fatalf("E: page %d", a.page)
	}
	contient("list", ecran(120, 40), "E Missions", "Site de Martin", "Ancienne mission", a.t("Finished"))
	taper(a, "\r")
	if a.misOuverte != "tuimis01" {
		t.Fatalf("Enter: %q", a.misOuverte)
	}
	d := ecran(120, 40)
	if strings.Contains(d, a.t("Policy: {0}", a.t("Ask me for production"))) { // M258: no policy to choose
		t.Fatalf("policy shown: %s", d)
	}
	contient("detail", d, a.t("Lanes"), a.t(moduleDev{}.Nom())+"  martin", a.t("Not found"),
		"✓ "+a.t("Milestone delivered")+"  ✓ "+a.t("Going online")+"  ✓ "+a.t("Verification")+"  ● "+a.t("Your accord")+"  ○ "+a.t("Production"),
		a.t("Online:")+" https://dev.martin.test", a.t("Milestone delivered: {0}", "A12"), a.t("Milestone delivered: {0}", "A03"), "p "+a.t("Pause"), "a "+a.t("Stop"))
	if strings.Contains(d, "A02") || strings.Contains(d, "https://martin.test") {
		t.Fatalf("an 11th journal line or the prod URL before its step:\n%s", d)
	}
	if i, j := strings.Index(d, "A12"), strings.Index(d, "A11"); i < 0 || j < i {
		t.Fatalf("the newest not first:\n%s", d)
	}
	for _, taille := range [][2]int{{80, 24}, {40, 10}, {20, 5}} {
		_ = ecran(taille[0], taille[1])
	}
	etat := func(e string) func() bool {
		return func() bool {
			mm := a.missionChoisie()
			return mm != nil && mm.Etat == e && a.form == nil
		}
	}
	// p: paused through the API
	taper(a, "p")
	attendreTUI(t, a, etat("en_pause"))
	if !slices.Contains(a.raccourcisMissions(), raccourci{"r", a.t("Resume")}) {
		t.Fatalf("keys of a paused mission: %v", a.raccourcisMissions())
	}
	// r: Athanor's password asked; a wrong one keeps the form open with the server's answer
	taper(a, "r")
	if a.form == nil {
		t.Fatal("r: no password asked")
	}
	taper(a, "faux\r")
	attendreTUI(t, a, func() bool { return a.form != nil && a.form.erreur != "" })
	if mm := a.missionChoisie(); mm.Etat != "en_pause" {
		t.Fatalf("resumed with a wrong password: %s", mm.Etat)
	}
	a.form.champs[0].s.mettreTexte(mdpPreuveTest)
	taper(a, "\r")
	attendreTUI(t, a, etat("lancee"))
	// a: confirmed first (Escape keeps it going), then stopped
	taper(a, "a")
	if a.dialogue == nil {
		t.Fatal("a: no confirmation")
	}
	taper(a, "\x1b")
	if a.dialogue != nil || a.page != pMissions || a.misOuverte == "" {
		t.Fatalf("Escape in the confirmation: page %d, open %q", a.page, a.misOuverte)
	}
	taper(a, "a")
	taper(a, "\r")
	attendreTUI(t, a, etat("arretee"))
	contient("stopped", ecran(120, 40), a.t("Stopped"), a.t("from this computer"))
	if r := a.raccourcisMissions(); len(r) != 3 || r[1].k != "r" || r[2].k != "d" { // M250: a stopped mission can be resumed; M261: deleted
		t.Fatalf("keys of a stopped mission: %v", r)
	}
	// M261 d: Cancel is focused (Enter keeps it); Tab then Enter deletes it and goes back to the list
	id := a.misOuverte
	taper(a, "d")
	if a.dialogue == nil || len(a.dialogue.choix) != 2 || a.dialogue.choix[0].id != "" {
		t.Fatalf("d: confirmation %+v", a.dialogue)
	}
	taper(a, "\r")
	if _, err := lireMission(id); err != nil {
		t.Fatalf("Enter on Cancel deleted the mission: %v", err)
	}
	taper(a, "d")
	taper(a, "\t")
	taper(a, "\r")
	attendreTUI(t, a, func() bool { _, err := lireMission(id); return err != nil && a.misOuverte == "" })
	attendreTUI(t, a, func() bool { return len(a.missions) == 1 })
	// Escape from the list (the deleted mission's detail closed): home
	if a.page != pMissions {
		t.Fatalf("after the deletion: page %d", a.page)
	}
	taper(a, "\x1b")
	if a.page != pAccueil {
		t.Fatalf("Escape from the list: %d", a.page)
	}
}
