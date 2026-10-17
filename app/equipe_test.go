package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAgentsPerso(t *testing.T) {
	kit := t.TempDir()
	a, b := Projet{Nom: "a", Dossier: t.TempDir()}, Projet{Nom: "b", Dossier: t.TempDir()}
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{a, b}, ProjetActif: "a", Langue: "en"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(projet string, corps any) (int, map[string]any) {
		j, _ := json.Marshal(corps)
		r := httptest.NewRequest("POST", "/api/equipe/agents?p="+projet, bytes.NewReader(j))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	ag := func(nom string) AgentPerso {
		return AgentPerso{Nom: nom, Description: "Translates the texts.", Consignes: "Translate faithfully.", Modele: "eco"}
	}
	if c, v := appel("a", map[string]any{"action": "enregistrer", "agent": ag(" Traducteur ")}); c != 200 {
		t.Fatalf("save: %d %v", c, v)
	}
	for _, mauvais := range []AgentPerso{ag("tester"), ag("x"), ag("a b"), {Nom: "vide", Description: "d"}, {Nom: "mod", Description: "d", Consignes: "c", Modele: "gpt"}} {
		if c, _ := appel("a", map[string]any{"action": "enregistrer", "agent": mauvais}); c != 400 {
			t.Errorf("refused %q: %d", mauvais.Nom, c)
		}
	}
	if c, v := appel("a", map[string]any{"action": "enregistrer", "agent": ag("traducteur")}); c != 400 || v["erreur_key"] != "A sub-agent already has this name." {
		t.Errorf("collision: %d %v", c, v)
	}
	// the same name in another project is fine; one for every project is seen by both
	if c, _ := appel("b", map[string]any{"action": "enregistrer", "agent": ag("traducteur")}); c != 200 {
		t.Errorf("other project: %d", c)
	}
	if c, _ := appel("b", map[string]any{"action": "enregistrer", "agent": ag("chercheur"), "tous": true}); c != 200 {
		t.Errorf("every project: %d", c)
	}
	if l, _ := s.agentsDuProjet(a); len(l) != 2 || l[0].Nom != "traducteur" || l[0].Origine != "utilisateur" || l[1].Projet != "" {
		t.Fatalf("project a: %+v", l)
	}
	// a proposal of the lead: editing keeps its label, removing it refuses the name for this project
	f := lireAgents(s.kitCourant())
	f.Agents = append(f.Agents, AgentPerso{Nom: "level-designer", Description: "d", Consignes: "c", Projet: a.Dossier, Origine: "chef", Cree: "2026-01-01"})
	ecrireJSONFichier(cheminAgents(s.kitCourant()), f, 0o644)
	e := ag("level-designer")
	e.LectureSeule = true
	if c, _ := appel("a", map[string]any{"action": "enregistrer", "ancien": "level-designer", "agent": e}); c != 200 {
		t.Fatalf("edit: %d", c)
	}
	if l, _ := s.agentsDuProjet(a); l[2].Origine != "chef" || l[2].Cree != "2026-01-01" || !l[2].LectureSeule {
		t.Errorf("edited proposal: %+v", l[2])
	}
	if c, _ := appel("a", map[string]any{"action": "supprimer", "ancien": "level-designer"}); c != 200 {
		t.Fatalf("remove: %d", c)
	}
	if c, _ := appel("a", map[string]any{"action": "supprimer", "ancien": "level-designer"}); c != 404 {
		t.Errorf("remove twice: %d", c)
	}
	if l, refus := s.agentsDuProjet(a); len(l) != 2 || len(refus) != 1 || refus[0] != "level-designer" {
		t.Errorf("after removal: %+v %v", l, refus)
	}
	if _, refus := s.agentsDuProjet(b); len(refus) != 0 {
		t.Errorf("refused in b: %v", refus)
	}
	// created again by the user: no longer refused
	appel("a", map[string]any{"action": "enregistrer", "agent": ag("level-designer")})
	if _, refus := s.agentsDuProjet(a); len(refus) != 0 {
		t.Errorf("still refused: %v", refus)
	}
	// renaming onto a taken name is refused; a removed user agent is not refused
	if c, _ := appel("a", map[string]any{"action": "enregistrer", "ancien": "level-designer", "agent": ag("traducteur")}); c != 400 {
		t.Errorf("rename collision: %d", c)
	}
	appel("a", map[string]any{"action": "supprimer", "ancien": "level-designer"})
	if _, refus := s.agentsDuProjet(a); len(refus) != 0 {
		t.Errorf("a user agent got refused: %v", refus)
	}
	// limit
	for i := 0; i < 20; i++ {
		appel("a", map[string]any{"action": "enregistrer", "agent": ag("agent-" + string(rune('a'+i)))})
	}
	if l, _ := s.agentsDuProjet(a); len(l) != agentsParProjetMax {
		t.Errorf("limit: %d", len(l))
	}
	// roster: the project's own agents follow Athanor's, with their model
	os.WriteFile(s.kitCourant().Env(), []byte("CHEAP_MODEL=sonnet\n"), 0o600)
	r := httptest.NewRequest("GET", "/api/agents?p=a", nil)
	r.Header.Set("X-Athanor", "jeton")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var v struct {
		Equipe []MembreVue `json:"equipe"`
	}
	json.Unmarshal(w.Body.Bytes(), &v)
	vu := false
	for _, m := range v.Equipe {
		if m.Nom == "traducteur" {
			vu = m.Perso != nil && m.Modele == "sonnet" && m.AppelePar == "chef_ouvriers"
		}
	}
	if !vu {
		t.Errorf("roster: %s", w.Body.String())
	}
	// an unreadable file is never overwritten
	os.WriteFile(cheminAgents(s.kitCourant()), []byte("{oops"), 0o644)
	if c, _ := appel("a", map[string]any{"action": "enregistrer", "agent": ag("zz")}); c != 409 {
		t.Errorf("broken file: %d", c)
	}
	if b, _ := os.ReadFile(cheminAgents(s.kitCourant())); string(b) != "{oops" {
		t.Error("broken file overwritten")
	}
}
