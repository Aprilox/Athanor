package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// piloteReglagesTest: a pilot of the project in this folder, started with the kit's models (MODEL=sonnet, no
// fallback, sub-agents like the lead, haiku for the simple tasks), as lancerPilote builds it from the .env.
func piloteReglagesTest(t *testing.T, s *Serveur, nom string) *pilote {
	t.Helper()
	p, ok := s.projetNomme(nom)
	if !ok {
		t.Fatal("unknown project " + nom)
	}
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		t.Fatal(err)
	}
	return &pilote{kit: s.config.Kit, projet: p.Dossier, etat: p.Atelier(), model: "sonnet", cheapModel: "haiku"}
}

// modelesPropres: the project's own models as its Working mode writes them (POST /api/mode, modeles_projet.go);
// "" puts one back to Settings.
func modelesPropres(t *testing.T, s *Serveur, nom string, m map[string]string) {
	t.Helper()
	p, _ := s.projetNomme(nom)
	if err := p.ecrireMode(ModeVue{Modeles: m}); err != nil {
		t.Fatal(err)
	}
}

// modelesCycle: what a cycle that starts now launches: the lead's --model and --fallback-model, the sub-agents'
// and the simple tasks' models (the variables preparerEquipe gives the team).
func modelesCycle(p *pilote) string {
	p.prendreReglagesProjet()
	return strings.Join([]string{p.model, p.fallbackModel, p.subagentModel, p.cheapModel}, ",")
}

// TestReglagesDeuxProjets (M290, note 01h48 §4; 2.53.0's Models): two projects, two lead models: each pilot launches
// its own, a project that chose nothing keeps the kit's; the automatic stop on quota is the project's rule in the
// place of the kit's, window by window; the chat and the team page follow the project's model.
func TestReglagesDeuxProjets(t *testing.T) {
	s, appel := reglagesTest(t)
	demo, autre := piloteReglagesTest(t, s, "demo"), piloteReglagesTest(t, s, "autre")
	definir := func(projet, cle, valeur string) {
		t.Helper()
		if code, v := appel("POST", projet, "", map[string]any{"cle": cle, "valeur": valeur}); code != 200 {
			t.Fatalf("%s %s=%s: %d %v", projet, cle, valeur, code, v)
		}
	}
	if a, b := modelesCycle(demo), modelesCycle(autre); a != "sonnet,,,haiku" || b != a {
		t.Fatalf("no setting of their own: demo %q, autre %q", a, b)
	}
	modelesPropres(t, s, "demo", map[string]string{"MODEL": "opus", "FALLBACK_MODEL": "sonnet", "SUBAGENT_MODEL": "auto"})
	modelesPropres(t, s, "autre", map[string]string{"CHEAP_MODEL": "sonnet"})
	if code, _ := appel("POST", "demo", "", map[string]any{"cle": "modele", "valeur": "opus"}); code == 200 {
		t.Error("a model is still a setting of This project's settings: its one place is the Working mode")
	}
	if a, b := modelesCycle(demo), modelesCycle(autre); a != "opus,sonnet,auto,haiku" || b != "sonnet,,,sonnet" {
		t.Errorf("two projects, two commands: demo %q, autre %q", a, b)
	}
	if v := variablesReglagesProjet(demo.projet)["MODEL"]; v != "opus" { // sessionChat and apiAgents read this
		t.Errorf("the chat of demo: model %q", v)
	}
	if v := variablesReglagesProjet(autre.projet); len(v) != 1 || v["CHEAP_MODEL"] != "sonnet" {
		t.Errorf("the variables of autre: %v", v)
	}
	if v := variablesReglagesProjet(t.TempDir()); len(v) != 0 { // a folder that is no project of the application
		t.Errorf("unknown folder: %v", v)
	}
	modelesPropres(t, s, "demo", map[string]string{"MODEL": ""})
	if a := modelesCycle(demo); a != "sonnet,sonnet,auto,haiku" {
		t.Errorf("back to the default: %q", a)
	}

	// quota: the kit stops everything at 90 % of the 5 hours and pauses at 80 % of the week
	regles := filepath.Join(s.config.Kit, "logs", "quota-arret.json")
	_ = os.MkdirAll(filepath.Dir(regles), 0o755)
	if err := os.WriteFile(regles, []byte(`{"five_hour":{"seuil":90,"action":"maintenant"},"seven_day":{"seuil":80,"action":"pause"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	definir("demo", "quota_five_hour", "0")         // the urgent project goes on
	definir("demo", "quota_modele", "50:apres")     // a rule the kit does not have
	definir("autre", "quota_seven_day", "95:apres") // another threshold
	rd, ra := reglesQuotaProjet(demo.projet, regles), reglesQuotaProjet(autre.projet, regles)
	if _, ok := rd["five_hour"]; ok || rd["seven_day"] != (regleSeuil{80, "pause"}) || rd["modele"] != (regleSeuil{50, "apres"}) || len(rd) != 2 {
		t.Errorf("rules of demo: %v", rd)
	}
	if ra["five_hour"] != (regleSeuil{90, "maintenant"}) || ra["seven_day"] != (regleSeuil{95, "apres"}) || len(ra) != 2 {
		t.Errorf("rules of autre: %v", ra)
	}
	_ = os.Remove(regles) // no rule in the kit: only the project's own
	if r := reglesQuotaProjet(demo.projet, regles); len(r) != 1 || r["modele"] != (regleSeuil{50, "apres"}) {
		t.Errorf("rules of demo without the kit's file: %v", r)
	}
	if demo.seuilQuota() != "" { // a rule and no log of a session: nothing to stop
		t.Error("seuilQuota without a log")
	}
}

// TestReglageTacheEnCours (M290): a setting changed while a cycle runs: this cycle keeps the model it started
// with, the next one takes the new value; the other project's pilot never sees it.
func TestReglageTacheEnCours(t *testing.T) {
	s, _ := reglagesTest(t)
	demo, autre := piloteReglagesTest(t, s, "demo"), piloteReglagesTest(t, s, "autre")
	if a := modelesCycle(demo); a != "sonnet,,,haiku" { // the cycle starts
		t.Fatalf("first cycle: %q", a)
	}
	demo.modeleCycle = demo.model
	modelesPropres(t, s, "demo", map[string]string{"MODEL": "opus"}) // changed during the cycle
	if demo.model != "sonnet" || demo.modeleCycle != "sonnet" {
		t.Errorf("the cycle under way changed its model: %q, %q", demo.model, demo.modeleCycle)
	}
	if a := modelesCycle(autre); a != "sonnet,,,haiku" {
		t.Errorf("the other project took the change: %q", a)
	}
	if a := modelesCycle(demo); a != "opus,,,haiku" { // the next cycle
		t.Errorf("next cycle: %q", a)
	}
	modelesPropres(t, s, "demo", map[string]string{"MODEL": ""})
	if a := modelesCycle(demo); a != "sonnet,,,haiku" { // what the pilot started with stays the default
		t.Errorf("back to the default: %q", a)
	}
}
