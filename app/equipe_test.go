package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
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
	// the library: a name is unique in it, and another project switches it on
	if c, _ := appel("b", map[string]any{"action": "enregistrer", "agent": ag("traducteur")}); c != 400 {
		t.Errorf("the same name from another project: %d", c)
	}
	if c, _ := appel("b", map[string]any{"action": "activer", "ancien": "traducteur", "actif": true}); c != 200 {
		t.Errorf("switched on in b: %d", c)
	}
	if c, _ := appel("b", map[string]any{"action": "enregistrer", "agent": ag("chercheur")}); c != 200 {
		t.Errorf("created in b: %d", c)
	}
	if l, _ := s.agentsDuProjet(a); len(l) != 1 || l[0].Nom != "traducteur" || l[0].Origine != "utilisateur" || len(l[0].Projets) != 2 {
		t.Fatalf("project a: %+v", l)
	}
	if l, _ := s.agentsDuProjet(b); len(l) != 2 {
		t.Fatalf("project b: %+v", l)
	}
	if l := s.bibliothequeAgents(a); len(l) != 2 || !l[0].Actif || l[1].Actif || l[1].Nom != "chercheur" {
		t.Fatalf("library seen from a: %+v", l)
	}
	if c, _ := appel("a", map[string]any{"action": "activer", "ancien": "inconnu", "actif": true}); c != 404 {
		t.Errorf("switching an unknown one: %d", c)
	}
	// a proposal of the lead, as the pilot imports it (its project in "projet"): switched on in its project;
	// editing keeps its label and its projects, switching it off or deleting it refuses the name there
	f := lireAgents(s.kitCourant())
	f.Agents = append(f.Agents, AgentPerso{Nom: "level-designer", Description: "d", Consignes: "c", Projet: a.Dossier, Origine: "chef", Cree: "2026-01-01"})
	ecrireJSONFichier(cheminAgents(s.kitCourant()), f, 0o644)
	e := ag("level-designer")
	e.LectureSeule = true
	if c, _ := appel("a", map[string]any{"action": "enregistrer", "ancien": "level-designer", "agent": e}); c != 200 {
		t.Fatalf("edit: %d", c)
	}
	if l, _ := s.agentsDuProjet(a); len(l) != 2 || l[1].Origine != "chef" || l[1].Cree != "2026-01-01" || !l[1].LectureSeule || len(l[1].Projets) != 1 {
		t.Errorf("edited proposal: %+v", l)
	}
	if c, _ := appel("a", map[string]any{"action": "activer", "ancien": "level-designer", "actif": false}); c != 200 {
		t.Fatalf("switch off: %d", c)
	}
	if l, refus := s.agentsDuProjet(a); len(l) != 1 || len(refus) != 1 || len(s.bibliothequeAgents(a)) != 3 {
		t.Errorf("switched off: %+v %v", l, refus)
	}
	if c, _ := appel("a", map[string]any{"action": "activer", "ancien": "level-designer", "actif": true}); c != 200 {
		t.Fatalf("switch on: %d", c)
	}
	if l, refus := s.agentsDuProjet(a); len(l) != 2 || len(refus) != 0 {
		t.Errorf("switched on again: %+v %v", l, refus)
	}
	if c, _ := appel("a", map[string]any{"action": "supprimer", "ancien": "level-designer"}); c != 200 {
		t.Fatalf("delete: %d", c)
	}
	if c, _ := appel("a", map[string]any{"action": "supprimer", "ancien": "level-designer"}); c != 404 {
		t.Errorf("delete twice: %d", c)
	}
	if l, refus := s.agentsDuProjet(a); len(l) != 1 || len(refus) != 1 || refus[0] != "level-designer" {
		t.Errorf("after deletion: %+v %v", l, refus)
	}
	if _, refus := s.agentsDuProjet(b); len(refus) != 0 {
		t.Errorf("refused in b: %v", refus)
	}
	// created again by the user: no longer refused
	appel("a", map[string]any{"action": "enregistrer", "agent": ag("level-designer")})
	if _, refus := s.agentsDuProjet(a); len(refus) != 0 {
		t.Errorf("still refused: %v", refus)
	}
	// renaming onto a taken name is refused; a deleted user agent is not refused; deleting leaves every project
	if c, _ := appel("a", map[string]any{"action": "enregistrer", "ancien": "level-designer", "agent": ag("chercheur")}); c != 400 {
		t.Errorf("rename collision: %d", c)
	}
	appel("a", map[string]any{"action": "supprimer", "ancien": "level-designer"})
	if _, refus := s.agentsDuProjet(a); len(refus) != 0 {
		t.Errorf("a user agent got refused: %v", refus)
	}
	appel("a", map[string]any{"action": "supprimer", "ancien": "traducteur"})
	if la, _ := s.agentsDuProjet(a); len(la) != 0 {
		t.Errorf("deleted, still in a: %+v", la)
	}
	if lb, _ := s.agentsDuProjet(b); len(lb) != 1 || lb[0].Nom != "chercheur" {
		t.Errorf("deleted, still in b: %+v", lb)
	}
	// no limit, in the library or in a project
	for i := 0; i < 30; i++ {
		if c, _ := appel("a", map[string]any{"action": "enregistrer", "agent": ag(fmt.Sprintf("agent-%02d", i))}); c != 200 {
			t.Fatalf("agent %d: %d", i, c)
		}
	}
	if c, _ := appel("a", map[string]any{"action": "activer", "ancien": "chercheur", "actif": true}); c != 200 {
		t.Errorf("a 31st one switched on: %d", c)
	}
	if l, _ := s.agentsDuProjet(a); len(l) != 31 {
		t.Errorf("team of a: %d", len(l))
	}
	appel("b", map[string]any{"action": "enregistrer", "agent": ag("agent-z1")})
	if l := s.bibliothequeAgents(b); len(l) != 32 {
		t.Errorf("library: %d", len(l))
	}
	// the project moves: its sub-agents follow; it leaves Athanor: the library keeps them
	k := s.kitCourant()
	dossierAgents(k, b.Dossier, b.Dossier+"-2")
	if l, _ := s.agentsDuProjet(Projet{Dossier: b.Dossier + "-2"}); len(l) != 2 {
		t.Errorf("moved: %+v", l)
	}
	dossierAgents(k, b.Dossier+"-2", "")
	if l := lireAgents(k).Agents; len(l) != 32 || l[0].duDossier(b.Dossier+"-2") || l[0].Projets == nil {
		t.Errorf("removed project: %+v", l[0])
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
		if m.Nom == "agent-00" {
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

// a sub-agent proposed by Claude in the chat, checked by the user on its card, joins the team
func TestAgentDepuisLeChat(t *testing.T) {
	kit := t.TempDir()
	a := Projet{Nom: "a", Dossier: t.TempDir()}
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{a}, ProjetActif: "a", Langue: "en"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	k := s.kitCourant()
	ecrireConversation(k, a, conversationChat{Messages: []MessageChat{
		{Role: "vous", Texte: "a Blender specialist please"},
		{Role: "claude", Texte: "Here it is.\n<agent>{\"nom\":\"blender-artist\"}</agent>"},
		{Role: "claude", Texte: "No agent here."},
	}})
	appel := func(corps any) (int, map[string]any) {
		j, _ := json.Marshal(corps)
		r := httptest.NewRequest("POST", "/api/chat/agent?p=a", bytes.NewReader(j))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	ag := AgentPerso{Nom: " Blender-Artist ", Description: "Models and textures the game's props in Blender.", Consignes: "Model in Blender, export glTF.", Modele: "fort"}
	if c, _ := appel(map[string]any{"index": 2, "agent": ag}); c != 400 {
		t.Fatalf("a message without a proposal accepted: %d", c)
	}
	if c, _ := appel(map[string]any{"index": 1, "agent": AgentPerso{Nom: "x", Description: "d", Consignes: "c"}}); c != 400 {
		t.Fatalf("an invalid sub-agent accepted: %d", c)
	}
	if c, v := appel(map[string]any{"index": 1, "agent": ag}); c != 200 || v["message_key"] != "Sub-agent saved: it joins the team at the next cycle." {
		t.Fatalf("add: %d %v", c, v)
	}
	l, _ := s.agentsDuProjet(a)
	if len(l) != 1 || l[0].Nom != "blender-artist" || l[0].Origine != "utilisateur" || l[0].Modele != "fort" || len(l[0].Projets) != 1 {
		t.Fatalf("team: %+v", l)
	}
	if m := lireConversation(k, a).Messages[1]; m.Agent != "blender-artist" {
		t.Fatalf("message not marked: %+v", m)
	}
	// the same name again: refused; as a change of it (ancien), saved
	if c, _ := appel(map[string]any{"index": 1, "agent": ag}); c != 400 {
		t.Fatal("a second sub-agent of the same name")
	}
	ag.Consignes = "Model in Blender, export glTF, keep under 5k triangles."
	if c, v := appel(map[string]any{"index": 1, "ancien": "blender-artist", "agent": ag}); c != 200 {
		t.Fatalf("change: %d %v", c, v)
	}
	l, _ = s.agentsDuProjet(a)
	if len(l) != 1 || len(l[0].Projets) != 1 || !strings.Contains(l[0].Consignes, "5k") {
		t.Fatalf("after the change: %+v", l)
	}
}

// Before 2.49.9 a sub-agent was for one project or for all of them: at startup each becomes a sub-agent of
// the library, switched on where it was used; the pilot reads both forms the same way.
func TestMigrerAgents(t *testing.T) {
	kit := t.TempDir()
	a, b := Projet{Nom: "a", Dossier: t.TempDir()}, Projet{Nom: "b", Dossier: t.TempDir()}
	ag := func(nom, consignes, projet string) map[string]any {
		return map[string]any{"nom": nom, "description": "d", "consignes": consignes, "modele": "", "projet": projet, "origine": "utilisateur"}
	}
	ancien, _ := json.Marshal(map[string]any{"agents": []any{
		ag("tous", "c", ""),
		ag("trad", "c", a.Dossier), ag("trad", "c", b.Dossier), // the same: one sub-agent
		ag("rev", "c1", a.Dossier), ag("rev", "c2", b.Dossier), // different: renamed, past the taken rev-2
		ag("rev-2", "c3", b.Dossier),
	}, "refuses": map[string]any{}})
	f := filepath.Join(kit, "extensions", "agents.json")
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, ancien, 0o644)
	// the pilot, before the app migrated the file
	vide := t.TempDir()
	equipe := func(p Projet) string {
		var m map[string]any
		json.Unmarshal([]byte(equipeJSON(kit, p.Dossier, vide, false)), &m)
		var noms []string
		for n := range m {
			noms = append(noms, n)
		}
		sort.Strings(noms)
		return strings.Join(noms, " ")
	}
	avant := equipe(b)
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{a, b}, ProjetActif: "a", Langue: "en"})
	var noms []string
	for _, x := range lireAgents(s.kitCourant()).Agents {
		noms = append(noms, fmt.Sprintf("%s:%d", x.Nom, len(x.Projets)))
	}
	if strings.Join(noms, " ") != "tous:2 trad:2 rev:1 rev-3:1 rev-2:1" {
		t.Fatalf("migrated: %v", noms)
	}
	if g := equipe(a); g != "rev tous trad" {
		t.Errorf("the pilot in a: %q", g)
	}
	if g := equipe(b); g != "rev-2 rev-3 tous trad" || avant != "rev rev-2 tous trad" {
		t.Errorf("the pilot in b: %q, before %q", g, avant)
	}
	// once migrated, nothing is written again; a new project has none
	st, _ := os.Stat(f)
	os.Chtimes(f, st.ModTime().Add(-time.Hour), st.ModTime().Add(-time.Hour))
	st, _ = os.Stat(f)
	nouveauServeur(Config{Kit: kit, Projets: []Projet{a, b, {Nom: "c", Dossier: t.TempDir()}}, Langue: "en"})
	if st2, _ := os.Stat(f); !st2.ModTime().Equal(st.ModTime()) {
		t.Error("written again")
	}
}

// The pilot: a proposal of the lead whose name the library already has (another project's) is refused;
// a new one is switched on in its project only; a sub-agent switched off everywhere is in no team.
func TestEquipeImporterBibliotheque(t *testing.T) {
	kit, projet, vide := t.TempDir(), t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(kit, "extensions"), 0o755)
	os.WriteFile(filepath.Join(kit, "extensions", "agents.json"), []byte(`{"agents": [
  {"nom": "autre", "description": "d", "consignes": "c", "projets": ["/ailleurs"], "origine": "utilisateur"},
  {"nom": "nulle-part", "description": "d", "consignes": "c", "projets": [], "origine": "utilisateur"}], "refuses": {}}`), 0o644)
	d := filepath.Join(projet, ".athanor", "agents")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "a.md"), []byte("---\nname: autre\ndescription: d\n---\nc\n"), 0o644)
	os.WriteFile(filepath.Join(d, "b.md"), []byte("---\nname: neuf\ndescription: d\n---\nc\n"), 0o644)
	l := strings.Join(equipeImporter(kit, projet), "\n")
	if !strings.Contains(l, "refused (a.md): a sub-agent already has this name") || !strings.Contains(l, "added to the team: neuf") {
		t.Fatalf("import: %s", l)
	}
	if g := equipeJSON(kit, projet, vide, false); !strings.Contains(g, `"neuf"`) || strings.Contains(g, `"autre"`) || strings.Contains(g, `"nulle-part"`) {
		t.Errorf("team: %s", g)
	}
	if g := equipeJSON(kit, "/ailleurs", vide, false); strings.Contains(g, `"neuf"`) || !strings.Contains(g, `"autre"`) {
		t.Errorf("the other project: %s", g)
	}
	// the app sees it switched on in its project (under WSL, the folder as the Windows app knows it)
	if x := lireAgents(Kit{kit}).Agents; len(x) != 3 || !x[2].duDossier(cleProjetApp(projet)) || len(x[2].Projets) != 1 {
		t.Errorf("app: %+v", x)
	}
}
