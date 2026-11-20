package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMissionsSuivies (3.1): the New mission form launches a new project with its server, key and permission
// choice, or adds a request to an existing project; the frame reads them back; Edit, Pause, Stop, Resume and
// Delete work in any state; a vault value written in the text never reaches the mission file.
func TestMissionsSuivies(t *testing.T) {
	b := nouveauBancCarteProjet(t, `{"type":"projet","projet":"vitrine","texte":"Un site vitrine.","plan":"1. accueil","deploiement":{"serveurs":["vps"],"apis":["Clé Stripe"],"plan":"nginx sur vps"},"autonome":true}`)
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
	if m := c.Mission; m == nil || !slices.Equal(m.Serveurs, []string{b.vps}) || !slices.Equal(m.APIs, []string{b.api}) || m.Mode != "auto" || m.Plan != "1. accueil" || m.PlanDeploiement != "nginx sur vps" || m.Projet != "vitrine" {
		t.Fatalf("proposal: %+v", c.Mission)
	}
	if c.Noms[b.vps] != "vps" || c.Noms[b.api] != "Clé Stripe" { // the summary under the answer names them
		t.Errorf("names for the summary: %v", c.Noms)
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

	// other servers without one the project is deployed on: refused
	if code, _ := appel("POST", "/api/missions", MissionDemande{Texte: "x", Projet: "x", Autres: []string{"base"}}); code != 400 {
		t.Errorf("other servers without a site's server: %d", code)
	}
	inventaire, _ := lireServeurs()
	base := inventaire[slices.IndexFunc(inventaire, func(x ServeurInventaire) bool { return x.Nom == "base" })].ID
	// the assistant names a role by its server's name: the form gets it by id
	if d, _ := s.missionProposee(ActionChat{Type: "projet", Projet: "x", Deploiement: &DeploiementDemande{Serveurs: []string{"vps"}, Autres: []string{"base"}, Roles: map[string]string{"Base": "PostgreSQL"}}}); d.Roles[base] != "PostgreSQL" {
		t.Errorf("roles of the proposal: %v", d.Roles)
	}
	// a server of another role says what it does, in 200 characters at most
	for _, roles := range []map[string]string{nil, {"base": "  "}, {"base": strings.Repeat("é", maxRoleServeur+1)}} {
		if code, v := appel("POST", "/api/missions", MissionDemande{Texte: "x", Projet: "x", Serveurs: []string{"vps"}, Autres: []string{"base"}, Roles: roles}); code != 400 {
			t.Errorf("another role %v: %d %v", roles, code, v)
		}
	}

	// a new project, launched from the home's proposal (index 1): the site on vps, the database on base; the
	// vault value in the text is masked
	i := 1
	code, v := appel("POST", "/api/missions", MissionDemande{Texte: "Un site vitrine, clé valeur-Clé Stripe.", Plan: "1. accueil", Projet: "vitrine", Dossier: b.dossier("vitrine"),
		Serveurs: []string{"vps"}, Autres: []string{"base", "vps"}, Roles: map[string]string{"@base": " PostgreSQL, clé valeur-Clé Stripe ", "fantome": "rien"}, APIs: []string{"Clé Stripe"},
		PlanDeploiement: "nginx sur vps, postgres sur base", Mode: "auto", Index: &i})
	if code != 200 {
		t.Fatalf("launch: %d %v", code, v)
	}
	l := lireMissions()
	if len(l) != 1 || l[0].Projet != "vitrine" || !slices.Equal(l[0].Serveurs, []string{b.vps}) || !slices.Equal(l[0].Autres, []string{base}) || !slices.Equal(l[0].APIs, []string{b.api}) ||
		l[0].Mode != "auto" || strings.Contains(l[0].Phrase, "valeur-Clé Stripe") || len(l[0].Roles) != 1 || !strings.HasPrefix(l[0].Roles[base], "PostgreSQL, clé ") || strings.Contains(l[0].Roles[base], "valeur-") {
		t.Fatalf("mission written: %+v", l)
	}
	id := l[0].ID
	e := s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")]
	if !slices.Equal(e.Serveurs, []string{b.vps}) || !slices.Equal(e.Autres, []string{base}) || !slices.Equal(e.APIs, []string{b.api}) || e.Plan != "nginx sur vps, postgres sur base" || e.Roles[base] != l[0].Roles[base] ||
		modeProjet("vitrine") != "auto" || !slices.Equal(b.pilotes, []string{"vitrine"}) {
		t.Fatalf("project: %+v, mode %s, pilots %v", e, modeProjet("vitrine"), b.pilotes)
	}
	// Discuss: a project with servers asks code or servers, and adds a remark for the servers to their plan
	pv, _ := s.projetLocalChat("vitrine")
	if c := s.consigneServeursChat(pv); !strings.Contains(c, "vps") || !strings.Contains(c, "<serveurs>") {
		t.Errorf("Discuss's servers instruction: %q", c)
	}
	if ps, _ := s.projetLocalChat("site"); s.consigneServeursChat(ps) != "" || s.ajouterPlanServeurs(ps, "x", "test") == nil {
		t.Error("a project without a server gets the servers instruction or a servers' plan")
	}
	if err := s.ajouterPlanServeurs(pv, " Un pare-feu qui ne laisse que 80 et 443. ", "test"); err != nil ||
		s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")].Plan != "nginx sur vps, postgres sur base\n\nUn pare-feu qui ne laisse que 80 et 443." {
		t.Errorf("a remark for the servers added to their plan: %v %q", err, s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")].Plan)
	}
	// the lead tells the server manager how the project runs: kept with the deployment, secrets masked, in its briefs
	if r, err := outilInformerServeurs(s, SessionHub{Projet: "vitrine"}, json.RawMessage(`{"note":"Site statique dans dist/, npm run build. Clé valeur-Clé Stripe."}`)); err != nil || !strings.HasPrefix(r, "noted") {
		t.Fatalf("informer_serveurs: %q %v", r, err)
	}
	ev := s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")]
	if !strings.Contains(ev.NotesChef, "npm run build") || strings.Contains(ev.NotesChef, "valeur-") || !strings.Contains(s.briefPreparationProjet(pv, ev), "npm run build") ||
		!strings.Contains(briefPremierProjet(pv, ev, "site", strings.Repeat("a", 40), true), "npm run build") {
		t.Errorf("the lead's notes: %q", ev.NotesChef)
	}
	if _, err := outilInformerServeurs(s, SessionHub{Projet: "site"}, json.RawMessage(`{"note":"x"}`)); err == nil {
		t.Error("a project without a server took notes for the servers")
	}
	// Start relaunches a first deployment that did not go through, never one not tried yet nor one at work
	for _, x := range []struct {
		e      Environnement
		r      RelaisJalon
		essais int
		ok     bool
	}{{Environnement{Serveurs: []string{"v"}}, RelaisJalon{}, 1, true}, {Environnement{Serveurs: []string{"v"}}, RelaisJalon{}, 0, false},
		{Environnement{Serveurs: []string{"v"}}, RelaisJalon{Etat: "preparation"}, 1, false}, {Environnement{Serveurs: []string{"v"}, Deploiement: &ConfigDeploiement{}}, RelaisJalon{}, 1, false},
		{Environnement{}, RelaisJalon{}, 1, false}} {
		if premierARelancer(x.e, x.r, x.essais) != x.ok {
			t.Errorf("relaunch %+v %+v %d: want %v", x.e, x.r, x.essais, x.ok)
		}
	}
	// Studio shows its work on the servers step by step: prepared, first deployment, each version, the next one
	_ = noterHistorique(Deploiement{ID: "d1", Projet: "vitrine", Env: s.envJalon("vitrine"), Commit: "59c2a7f3c89a", Etat: "echec", Erreur: "Caddy is not installed", Debut: "2026-11-20T12:19:00Z"})
	if j := s.jalonsDeploiement("vitrine"); len(j) != 4 || j[0].Titre != "Prepare the servers" || j[0].Etat != "fait" || j[1].Titre != "First deployment" || j[1].Etat != "echec" ||
		j[2].Titre != "Version {0} failed" || j[2].Args[0] != "59c2a7f" || j[2].Detail != "Caddy is not installed" || j[3].Titre != "Next deployment: at delivery" {
		t.Errorf("deployment milestones: %+v", j)
	}
	// the servers' plan shows the acceptance criteria its last check found, as a milestone's
	_ = os.MkdirAll(dossierTaches(b.vps), 0o700)
	tv, _ := json.Marshal(TacheServeurs{ID: "c1a2b3c4d5e6", Env: "vitrine/site", Serveurs: []string{b.vps}, Etat: "finie", Cree: "2026-11-20T18:40:00Z",
		Verification: &VerificationTache{Verdict: "echec", T: "2026-11-20T18:48:00Z", Criteres: []CritereVerifie{{Texte: "nginx écoute sur 5050", OK: true}, {Texte: "https://vitrine répond 200", OK: false}}}})
	_ = os.WriteFile(filepath.Join(dossierTaches(b.vps), "c1a2b3c4d5e6.json"), tv, 0o600)
	if j := s.jalonsDeploiement("vitrine"); len(j) != 6 || !j[3].Critere || j[3].Titre != "nginx écoute sur 5050" || j[3].Etat != "fait" || j[4].Etat != "echec" {
		t.Errorf("the servers' plan without its criteria: %+v", j)
	}
	// the moment chosen is the one the servers' plan says, right after it is saved
	ev2 := s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")]
	for moment, titre := range map[string]string{"jalon": "Next deployment: after the next milestone", "livraison": "Next deployment: at delivery"} {
		if err := s.definirDeploiementProjet("vitrine", DeploiementDemande{Serveurs: ev2.Serveurs, URL: ev2.URL, Plan: ev2.Plan, Moment: moment, APIs: ev2.APIs}, "test"); err != nil {
			t.Fatal(err)
		}
		if j := s.jalonsDeploiement("vitrine"); j[len(j)-1].Titre != titre {
			t.Errorf("moment %s: the servers' plan says %q", moment, j[len(j)-1].Titre)
		}
	}
	if j := s.jalonsDeploiement("site"); j != nil {
		t.Errorf("a project without a server has deployment milestones: %+v", j)
	}
	if u := s.acces.UtilisationsServeurs(); len(u[base]) != 1 || len(u[b.vps]) != 1 {
		t.Errorf("the servers page does not say both servers serve the project: %v", u)
	}
	brief := s.briefPreparationProjet(Projet{Nom: "vitrine"}, e)
	if !strings.Contains(brief, "the project is deployed on vps") || !strings.Contains(brief, "other servers (base (PostgreSQL, clé ") || strings.Contains(brief, "valeur-") {
		t.Errorf("the preparation does not say the servers' roles: %s", brief)
	}
	// the Deployment tab does not show the other servers: saving it keeps them
	if err := s.definirDeploiementProjet("vitrine", DeploiementDemande{Serveurs: []string{b.vps}, APIs: []string{b.api}, URL: "", Plan: e.Plan, Moment: "livraison"}, "test"); err != nil ||
		!slices.Equal(s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")].Autres, []string{base}) || s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")].Roles[base] == "" {
		t.Errorf("the Deployment tab lost the other servers or their roles: %v", err)
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
	if !strings.Contains(string(j), `"nom":"vps","site":true`) || !strings.Contains(string(j), `"nom":"base","role":"PostgreSQL, clé ***"`) || !strings.Contains(string(j), `"api_noms":["Clé Stripe"]`) || !strings.Contains(string(j), `"suivie":true`) || strings.Contains(string(j), "valeur-") {
		t.Errorf("list: %s", j)
	}

	// Edit: needs me, a new text becomes a request
	if code, v := appel("POST", "/api/missions/"+id, MissionDemande{Texte: "Un site vitrine avec un blog", Serveur: b.vps, API: b.api}); code != 200 {
		t.Fatalf("edit: %d %v", code, v)
	}
	if m, _ := lireMission(id); m.Mode != "manuel" || modeProjet("vitrine") != "manuel" || m.Phrase != "Un site vitrine avec un blog" {
		t.Errorf("edit: %+v, mode %s", m, modeProjet("vitrine"))
	}

	if m, _ := lireMission(id); len(m.Autres) != 0 || len(m.Roles) != 0 || len(s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")].Autres) != 0 ||
		len(s.acces.Environnements("vitrine")[s.deploiementProjet("vitrine")].Roles) != 0 {
		t.Errorf("edit without other servers kept them: %+v", m.Autres)
	}

	// full autonomy (ignorer): confirmed with Athanor's password when chosen, never again while it stays
	plein := MissionDemande{Texte: "Un site vitrine avec un blog", Serveur: b.vps, API: b.api, Mode: "ignorer"}
	avecMdp := func(confirmer bool, mdp string) map[string]any {
		j, _ := json.Marshal(plein)
		var x map[string]any
		_ = json.Unmarshal(j, &x)
		x["confirmer"], x["mot_de_passe"] = confirmer, mdp
		return x
	}
	if code, _ := appel("POST", "/api/missions/"+id, avecMdp(true, mdpPreuveTest)); code != http.StatusForbidden || modeProjet("vitrine") != "manuel" {
		t.Errorf("full autonomy without a password for Athanor: %d, mode %s", code, modeProjet("vitrine"))
	}
	avecMdpPreuve(s)
	for _, x := range []struct {
		quoi string
		c    map[string]any
		code int
	}{{"not confirmed", avecMdp(false, mdpPreuveTest), http.StatusConflict}, {"wrong password", avecMdp(true, "faux"), http.StatusUnauthorized}} {
		if code, _ := appel("POST", "/api/missions/"+id, x.c); code != x.code || modeProjet("vitrine") != "manuel" {
			t.Errorf("full autonomy %s: %d (want %d), mode %s", x.quoi, code, x.code, modeProjet("vitrine"))
		}
	}
	if code, v := appel("POST", "/api/missions/"+id, avecMdp(true, mdpPreuveTest)); code != 200 || modeProjet("vitrine") != "ignorer" {
		t.Fatalf("full autonomy with the password: %d %v, mode %s", code, v, modeProjet("vitrine"))
	}
	if code, v := appel("POST", "/api/missions/"+id, plein); code != 200 { // kept: not asked again
		t.Errorf("full autonomy kept: %d %v", code, v)
	}
	if code, _ := appel("POST", "/api/missions", MissionDemande{Texte: "x", Projet: "autonome-sans-mdp", Serveur: b.vps, Mode: "ignorer"}); code != http.StatusConflict {
		t.Errorf("a new mission in full autonomy, not confirmed: %d", code)
	}
	if _, ok := s.projetLocalChat("autonome-sans-mdp"); ok {
		t.Error("a refused full autonomy created its project")
	}
	if code, _ := appel("POST", "/api/missions/"+id, MissionDemande{Texte: "x", Mode: "modifications"}); code != http.StatusBadRequest {
		t.Errorf("a mode a mission does not offer: %d", code)
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

// TestMissionsSansProjet: a mission without a Studio project is one Servers task on its servers and keys; its frame
// follows the task; Stop and Launch again in any state; Edit keeps the change for the next launch; Delete stops it.
func TestMissionsSansProjet(t *testing.T) {
	b := nouveauBancCarteProjet(t, `{"type":"serveur","serveurs":["vps","base"],"apis":["Clé Stripe"],"texte":"Installe nginx et relie-les.","permission":"auto"}`)
	s := b.s
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	ancienne := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancienne })
	lente := make(chan struct{})
	sessionTacheServeurs = func(_ *Serveur, ctx context.Context, dossier string) error { // the fake operator: works until stopped or released
		select {
		case <-lente:
		case <-ctx.Done():
		}
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"nginx installé"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	t.Cleanup(func() {
		for range 400 { // the tasks end (their goroutines must not outlive the test)
			tachesEnCours.Lock()
			n := len(tachesEnCours.m)
			tachesEnCours.Unlock()
			if n == 0 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	appel := func(methode, chemin string, corps any) (int, map[string]any) {
		t.Helper()
		j, _ := json.Marshal(corps)
		r := httptest.NewRequest(methode, chemin, strings.NewReader(string(j)))
		parts := strings.Split(strings.TrimPrefix(chemin, "/api/missions/"), "/")
		r.SetPathValue("id", parts[0])
		if len(parts) == 2 {
			r.SetPathValue("action", parts[1])
		}
		w := httptest.NewRecorder()
		switch {
		case chemin == "/api/missions":
			s.apiLancerMission(w, r)
		case methode == "DELETE":
			s.apiSupprimerMission(w, r)
		case len(parts) == 2:
			s.apiAgirMission(w, r)
		default:
			s.apiModifierMission(w, r)
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	vue := func(id string) VueMission { m, _ := lireMission(id); return s.vueMission(m) }

	// the home's server task proposal fills the form as a mission without a project
	c := s.cartesConversation(lireConversation(s.kitCourant(), projetAccueil(s.kitCourant())))["1"]
	if m := c.Mission; m == nil || !m.SansProjet || len(m.Serveurs) != 2 || !slices.Equal(m.APIs, []string{b.api}) || m.Mode != "auto" {
		t.Fatalf("proposal: %+v", c.Mission)
	}
	if code, _ := appel("POST", "/api/missions", MissionDemande{SansProjet: true, Texte: "x"}); code != 400 {
		t.Errorf("no server and no key: %d", code)
	}
	i := 1
	code, v := appel("POST", "/api/missions", MissionDemande{SansProjet: true, Texte: "Installe nginx et relie-les.", Serveurs: []string{"vps"}, Autres: []string{"base"}, APIs: []string{b.api}, Roles: map[string]string{"base": "la base"},
		PlanDeploiement: "nginx sur vps, postgres sur base", Mode: "auto", Index: &i})
	if code != 200 {
		t.Fatalf("launch: %d %v", code, v)
	}
	l := lireMissions()
	if len(l) != 1 || l[0].Projet != "" || l[0].Tache == "" || len(l[0].Serveurs) != 2 || l[0].PlanServeurs != "nginx sur vps, postgres sur base" || len(b.pilotes) != 0 {
		t.Fatalf("mission: %+v, pilots %v", l, b.pilotes)
	}
	id, tache := l[0].ID, l[0].Tache
	if x, ok := tacheMission(tache); !ok || !strings.Contains(x.Texte, "## Servers\n\n- base: la base\n\n## Servers plan") || len(x.Serveurs) != 2 || !slices.Equal(x.APIs, []string{b.api}) {
		t.Fatalf("its task: %+v", x)
	}
	if w := vue(id); !w.Suivie || w.Statut != "en_cours" || !w.ServeurTache || len(w.Machines) != 2 || w.Machines[0].Site || w.Machines[1].Role != "la base" || w.Machines[0].Role != "" || !slices.Equal(w.APINoms, []string{"Clé Stripe"}) {
		t.Fatalf("frame while it works: %+v", w)
	}
	if code, _ := appel("POST", "/api/missions/"+id+"/reprendre", nil); code != http.StatusConflict {
		t.Errorf("launched again while it works: %d", code)
	}
	if code, v := appel("POST", "/api/missions/"+id+"/arreter", nil); code != 200 {
		t.Fatalf("stop: %d %v", code, v)
	}
	for k := 0; k < 100 && vue(id).Statut == "en_cours"; k++ {
		time.Sleep(50 * time.Millisecond)
	}
	if w := vue(id); w.Statut != "arretee" || w.ServeurTache {
		t.Fatalf("frame once stopped: %+v", w)
	}
	// Edit: kept for the next launch; Launch again: a new task with the change
	if code, v := appel("POST", "/api/missions/"+id, MissionDemande{Texte: "Installe nginx et caddy.", Serveurs: []string{b.vps}, Mode: "manuel"}); code != 200 {
		t.Fatalf("edit: %d %v", code, v)
	}
	close(lente)
	if code, v := appel("POST", "/api/missions/"+id+"/serveur-relancer", nil); code != 200 {
		t.Fatalf("launch again: %d %v", code, v)
	}
	m, _ := lireMission(id)
	if x, ok := tacheMission(m.Tache); m.Tache == tache || !ok || !strings.Contains(x.Texte, "caddy") || !slices.Equal(x.Serveurs, []string{b.vps}) {
		t.Fatalf("launched again: %+v %+v", m, x)
	}
	for k := 0; k < 200 && vue(id).Statut == "en_cours"; k++ {
		time.Sleep(50 * time.Millisecond)
	}
	if w := vue(id); w.Statut != "terminee" || !strings.Contains(w.Bilan, "nginx") {
		t.Errorf("frame once over: %+v", w)
	}
	if code, v := appel("DELETE", "/api/missions/"+id, OptionsSuppression{}); code != 200 {
		t.Fatalf("delete: %d %v", code, v)
	}
	if _, err := lireMission(id); err == nil || len(b.pilotes) != 0 {
		t.Error("mission still there, or a pilot started")
	}
}
