package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestMissionsSuivies (3.1): the New mission form launches a new project with its server, key and permission
// choice, or adds a request to an existing project; the frame reads them back; Edit, Pause, Stop, Resume and
// Delete work in any state; a vault value written in the text never reaches the mission file.
func TestMissionsSuivies(t *testing.T) {
	b := nouveauBancCarteProjet(t, `{"type":"projet","projet":"vitrine","texte":"Un site vitrine.","plan":"1. accueil","deploiement":{"serveurs":["vps"],"apis":["Clé Stripe"]},"autonome":true}`)
	s := b.s
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	arrets := []string{}
	ancien := arreterPiloteProjet
	arreterPiloteProjet = func(_ *Serveur, p Projet) error { arrets = append(arrets, p.Nom); return nil }
	t.Cleanup(func() { arreterPiloteProjet = ancien })
	appel := func(methode, chemin string, corps any) (int, map[string]any) {
		t.Helper()
		j, _ := json.Marshal(corps)
		r := httptest.NewRequest(methode, chemin, strings.NewReader(string(j)))
		r.SetPathValue("id", strings.Split(strings.TrimPrefix(chemin, "/api/missions/")+"/", "/")[0])
		if i := strings.Count(chemin, "/"); i == 4 {
			r.SetPathValue("action", chemin[strings.LastIndex(chemin, "/")+1:])
		}
		w := httptest.NewRecorder()
		switch {
		case methode == "POST" && chemin == "/api/missions":
			s.apiLancerMission(w, r)
		case methode == "POST" && strings.Count(chemin, "/") == 4:
			s.apiAgirMission(w, r)
		case methode == "POST":
			s.apiModifierMission(w, r)
		case methode == "DELETE":
			s.apiSupprimerMission(w, r)
		default:
			s.apiMissions(w, r)
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}

	// the home's proposal fills the form: server and key by id, autonomous, the plan
	c := s.cartesConversation(lireConversation(s.kitCourant(), projetAccueil(s.kitCourant())))["1"]
	if m := c.Mission; m == nil || m.Serveur != b.vps || m.API != b.api || !m.Autonome || m.Plan != "1. accueil" || m.Projet != "vitrine" {
		t.Fatalf("proposal: %+v", c.Mission)
	}

	// refused: no text, an unknown server, no name
	for _, x := range []struct {
		quoi  string
		corps MissionDemande
		code  int
	}{
		{"no text", MissionDemande{Projet: "x"}, 400},
		{"unknown server", MissionDemande{Texte: "x", Projet: "x", Serveur: "fantome"}, 400},
		{"no name", MissionDemande{Texte: "x"}, 400},
	} {
		if code, v := appel("POST", "/api/missions", x.corps); code != x.code {
			t.Errorf("%s: %d %v", x.quoi, code, v)
		}
	}
	if len(lireMissions()) != 0 || len(b.pilotes) != 0 {
		t.Fatal("a refused mission was written or started")
	}

	// a new project, launched from the home's proposal (index 1): the vault value in the text is masked
	i := 1
	code, v := appel("POST", "/api/missions", MissionDemande{Texte: "Un site vitrine, clé valeur-Clé Stripe.", Plan: "1. accueil", Projet: "vitrine", Dossier: b.dossier("vitrine"),
		Serveur: "vps", API: "Clé Stripe", Autonome: true, Index: &i})
	if code != 200 {
		t.Fatalf("launch: %d %v", code, v)
	}
	l := lireMissions()
	if len(l) != 1 || l[0].Projet != "vitrine" || l[0].Serveur != b.vps || l[0].API != b.api || !l[0].Autonome || strings.Contains(l[0].Phrase, "valeur-Clé Stripe") {
		t.Fatalf("mission written: %+v", l)
	}
	id := l[0].ID
	e := s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")]
	if !slices.Equal(e.Serveurs, []string{b.vps}) || !slices.Equal(e.APIs, []string{b.api}) || modeProjet("vitrine") != "auto" || !slices.Equal(b.pilotes, []string{"vitrine"}) {
		t.Fatalf("project: %+v, mode %s, pilots %v", e, modeProjet("vitrine"), b.pilotes)
	}
	if p, _ := s.projetLocalChat("vitrine"); !strings.Contains(lireTexte(p.Demande()), "## Plan") {
		t.Error("the plan is not in the project's request")
	}
	if c := s.cartesConversation(lireConversation(s.kitCourant(), projetAccueil(s.kitCourant())))["1"]; c.Etat != "lancee" || c.Mission != nil {
		t.Errorf("the home's proposal is not marked launched: %+v", c)
	}
	if code, _ := appel("POST", "/api/missions", MissionDemande{Texte: "encore", Projet: "autre", Index: &i}); code != http.StatusConflict {
		t.Errorf("a proposal launched twice: %d", code)
	}

	// an existing project: a new request, its pilot started, no project created
	file := len(b.site.LireFileAttente())
	if code, v := appel("POST", "/api/missions", MissionDemande{Texte: "Ajoute une page contact", Projet: "SITE"}); code != 200 {
		t.Fatalf("existing project: %d %v", code, v)
	}
	if len(b.site.LireFileAttente()) != file+1 && lireTexte(b.site.Demande()) == "" || !slices.Contains(b.pilotes, "site") {
		t.Errorf("existing project: queue %d → %d, pilots %v", file, len(b.site.LireFileAttente()), b.pilotes)
	}

	// the frame: server and key named, the project's state
	_, v = appel("GET", "/api/missions", nil)
	j, _ := json.Marshal(v)
	if !strings.Contains(string(j), `"serveur_nom":"vps"`) || !strings.Contains(string(j), `"api_nom":"Clé Stripe"`) || !strings.Contains(string(j), `"suivie":true`) || strings.Contains(string(j), "valeur-") {
		t.Errorf("list: %s", j)
	}

	// Edit: needs me, a new text becomes a request
	if code, v := appel("POST", "/api/missions/"+id, MissionDemande{Texte: "Un site vitrine avec un blog", Serveur: b.vps, API: b.api}); code != 200 {
		t.Fatalf("edit: %d %v", code, v)
	}
	if m, _ := lireMission(id); m.Autonome || modeProjet("vitrine") != "manuel" || m.Phrase != "Un site vitrine avec un blog" {
		t.Errorf("edit: %+v, mode %s", m, modeProjet("vitrine"))
	}

	// Pause, Stop, Resume in any state; an unknown action
	for _, a := range []string{"pause", "arreter", "serveur-arreter", "reprendre"} {
		if code, v := appel("POST", "/api/missions/"+id+"/"+a, nil); code != 200 {
			t.Errorf("%s: %d %v", a, code, v)
		}
	}
	if !slices.Equal(arrets, []string{"vitrine", "vitrine"}) || lireRelais()["vitrine"].Pause {
		t.Errorf("stops %v, paused %v", arrets, lireRelais()["vitrine"].Pause)
	}
	if code, _ := appel("POST", "/api/missions/"+id+"/exploser", nil); code != 404 {
		t.Errorf("unknown action: %d", code)
	}

	// Delete with its project: the folder goes, never a key nor a server
	if code, v := appel("DELETE", "/api/missions/"+id, OptionsSuppression{Projet: true}); code != 200 {
		t.Fatalf("delete: %d %v", code, v)
	}
	if _, err := lireMission(id); err == nil {
		t.Error("mission still there")
	}
	if _, ok := s.projetLocalChat("vitrine"); ok {
		t.Error("its project is still in Studio")
	}
	if l, _ := lireServeurs(); len(l) != 2 || len(s.coffre.Liste()) != 2 {
		t.Error("a server or a key went with the mission")
	}
	if j := journalAuditTexte(t); !strings.Contains(j, "mission lancée") || !strings.Contains(j, "mission supprimée") || strings.Contains(j, "valeur-Clé") {
		t.Error("audit: launch or deletion missing, or a vault value in it")
	}
}

func lireTexte(f string) string { b, _ := os.ReadFile(f); return string(b) }
