package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// missionAncienne: a mission file as Athanor 3.0.0 left it (its card computed, its relay, its journal), on project.
func missionAncienne(id, etat, projet, cree string) string {
	return `{
  "id": "` + id + `",
  "phrase": "Mets @` + projet + ` en ligne",
  "mentions": {"projets": ["` + projet + `"], "serveurs": [], "cles": []},
  "etat": "` + etat + `",
  "carte": {"titre": "Mise en ligne", "projet": {"nom": "` + projet + `"}, "dev": {"brief": "Finir le site"}, "jalons_estimes": 2,
    "envs": [{"nom": "site", "url": "https://` + projet + `.example.fr", "serveurs": ["a4"], "production": true, "politique": "accord"}],
    "approbations": [{"action": "mise_en_prod", "quand": "accord"}], "touche": {"serveurs": ["dev-4"], "dns": [], "zones": [], "fichiers": []},
    "estimation": {"jalons": 2, "minutes": 50}},
  "politique": "prod",
  "voies": [{"module": "dev", "ref": "` + projet + `", "etat": "lancee"}, {"module": "servers", "ref": "0a1b2c", "etat": "preparation"}],
  "questions": [],
  "historique": [{"t": "2026-11-10T08:00:00.000Z", "etat": "proposee"}, {"t": "2026-11-10T08:05:00.000Z", "etat": "lancee", "detail": "hub"}],
  "cree": "` + cree + `",
  "session": "0b9c2f6e-1111-4222-8333-444455556666",
  "relais": {"etape": "accord_prod", "commit": "4f2a9c1e8b7d60a3f5c2e9d14b8a7f60c3e2d1b9", "deploiement_dev": "d16", "accord": "ap1"},
  "journal": [{"id": "jalon:M1", "t": "2026-11-10T09:00:00.000Z", "cle": "Milestone delivered: {0}", "params": ["M1"], "niveau": "info"}]
}
`
}

// TestAnciennesMissionsLisibles (M302): the missions Athanor 3.0.0 left running, paused and finished are listed
// and read, never rewritten nor followed; their project keeps its pilot and its deployment, and a paused one no
// longer holds it; a mission is deleted on request whatever its state, its project's folder only when the mission
// created it and the path is safe, never while its pilot runs.
func TestAnciennesMissionsLisibles(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.session = "session-test"
	s.mu.Unlock()
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{dossierMissions(), fichierRelais(), dossierJournalAudit()} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	base := t.TempDir()
	atelier := Projet{Nom: "atelier", Dossier: filepath.Join(base, "atelier")}
	ecrireFichier(t, atelier.Plan(), "# Plan\n\n- [x] M1 : Le site\n- [ ] M2 : La suite\n", 0o644)
	neuf := Projet{Nom: "neuf", Dossier: filepath.Join(base, "neuf")}
	ecrireFichier(t, filepath.Join(neuf.Dossier, "index.html"), "<p>neuf</p>", 0o644)
	s.config.Projets = append(s.config.Projets, atelier, neuf)
	pid := filepath.Join(atelier.Atelier(), "pilot.pid")
	ecrireFichier(t, pid, "1", 0o644) // atelier's pilot works
	site := Environnement{URL: "https://atelier.example.fr", Serveurs: []string{"a4"}, Protection: "aucune", Politique: "accord", Production: true, Moment: "livraison"}
	if err := s.acces.definirAncien(atelier.Nom, "site", site); err != nil {
		t.Fatal(err)
	}
	ecrireRelais(atelier.Nom, RelaisJalon{Jalon: "M1 : Le site", Etat: "attente", Commit: strings.Repeat("a", 40)})

	var corbeille, arrets []string
	ancienArret, ancienneCorbeille := arreterPiloteProjet, corbeilleProjet
	t.Cleanup(func() { arreterPiloteProjet, corbeilleProjet = ancienArret, ancienneCorbeille })
	arreterPiloteProjet = func(_ *Serveur, p Projet) error { arrets = append(arrets, p.Nom); return nil }
	corbeilleProjet = func(d string) error { corbeille = append(corbeille, d); return os.RemoveAll(d) }

	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, chemin, corps string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	existe := func(id string) bool { _, err := lireMission(id); return err == nil }
	stockee := func(t *testing.T, etat string, voies []VoieMission, h ...EvenementMission) string {
		t.Helper()
		id := aleatoire(8)
		if err := enregistrerMission(Mission{ID: id, Phrase: "une mission", Etat: etat, Politique: "prod", Voies: voies, Questions: []QuestionMission{},
			Historique: append([]EvenementMission{evenementMission("proposee", "")}, h...), Cree: time.Now().UTC().Format(formatCree)}); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// the three missions of the 3.0.0, and what its relay still asked the owner
	anciennes := map[string]string{"encours1": "lancee", "enpause1": "en_pause", "finie001": "terminee"}
	fichiers := map[string]string{}
	for id, etat := range anciennes {
		fichiers[id] = missionAncienne(id, etat, atelier.Nom, "2026-11-10T07:5"+id[len(id)-1:]+":00.000Z")
		ecrireFichier(t, filepath.Join(dossierMissions(), id+".json"), fichiers[id], 0o600)
	}
	accord, err := s.approbations.Demander(Approbation{Projet: atelier.Nom, Env: "site", Mission: "encours1", Agent: "relais", Action: actionDeploiement,
		Raison: "commit → atelier.example.fr", Details: map[string]string{"mission": "encours1", "commit": strings.Repeat("a", 40)}})
	if err != nil {
		t.Fatal(err)
	}
	relaisAvant := lireRelais()[atelier.Nom]

	s.refuserLancementsMission() // Athanor starts
	if e := s.approbations.PourAgent(accord.ID); e != "refusee" {
		t.Errorf("the old mission's accord to go online is %q, want refusee", e)
	}
	c, b := appel("GET", "/api/missions", "")
	if c != 200 || strings.Count(b, `"phrase"`) != 3 {
		t.Fatalf("list: %d %s", c, b)
	}
	for id, etat := range anciennes {
		c, b := appel("GET", "/api/missions/"+id, "")
		for _, attendu := range []string{`"etat":"` + etat + `"`, `"Mise en ligne"`, `"accord_prod"`, `"Milestone delivered: {0}"`, `"actuel":"travaille"`, `"fait":1`, `"total":2`, `"projet":"atelier"`, `"dossier_ok":false`} {
			if c != 200 || !strings.Contains(b, attendu) {
				t.Errorf("%s: %d without %s: %s", id, c, attendu, b)
			}
		}
	}
	for _, x := range [][2]string{{"POST", "/api/missions"}, {"POST", "/api/missions/encours1/pause"}, {"POST", "/api/missions/enpause1/reprendre"}, {"POST", "/api/missions/encours1/arreter"},
		{"POST", "/api/missions/encours1/politique"}, {"POST", "/api/missions/encours1/redemander"}, {"POST", "/api/missions/finie001/supprimer"}, {"POST", "/api/missions/vider"}} {
		if c, _ := appel(x[0], x[1], `{}`); c != http.StatusNotFound && c != http.StatusMethodNotAllowed {
			t.Errorf("%s %s still answers: %d", x[0], x[1], c)
		}
	}
	for id, avant := range fichiers {
		if f, _ := os.ReadFile(filepath.Join(dossierMissions(), id+".json")); string(f) != avant {
			t.Errorf("mission %s was rewritten", id)
		}
	}
	// the project is the owner's alone: a paused mission no longer holds it, nothing stopped, its deployment as it was
	if projetBloque("dev", atelier.Nom) != "" || len(arrets) != 0 || !atelier.PiloteActif() {
		t.Errorf("the old missions still act on the project: blocked %q, stops %v", projetBloque("dev", atelier.Nom), arrets)
	}
	if e := s.acces.Environnements(atelier.Nom)["site"]; e.URL != site.URL || !e.Production || e.Moment != "livraison" || lireRelais()[atelier.Nom] != relaisAvant {
		t.Errorf("the project's deployment changed: %+v, relay %+v", e, lireRelais()[atelier.Nom])
	}

	t.Run("supprimees_sur_demande", func(t *testing.T) {
		ecrireFichier(t, filepath.Join(dossierSessionMission("encours1"), "notes.md"), "suivi", 0o600)
		if c, _ := appel("DELETE", "/api/missions/encours1", `{"projet":true}`); c != http.StatusConflict || !existe("encours1") {
			t.Fatalf("a project that existed before the mission removed with it: %d", c)
		}
		for id := range anciennes {
			if c, b := appel("DELETE", "/api/missions/"+id, ""); c != 200 || !strings.Contains(b, `"supprimee":true`) || existe(id) {
				t.Fatalf("delete %s: %d %s", id, c, b)
			}
		}
		if _, err := os.Stat(dossierSessionMission("encours1")); err == nil {
			t.Error("the mission's session folder left behind")
		}
		if c, _ := appel("DELETE", "/api/missions/encours1", ""); c != http.StatusNotFound {
			t.Errorf("deleted twice: %d", c)
		}
		if _, ok := s.projetParNom(atelier.Nom); !ok || len(arrets) != 0 || !atelier.PiloteActif() || len(corbeille) != 0 || lireRelais()[atelier.Nom] != relaisAvant ||
			s.acces.Environnements(atelier.Nom)["site"].URL != site.URL {
			t.Errorf("deleting the missions touched the project: stops %v, recycle bin %v", arrets, corbeille)
		}
		if _, err := os.Stat(atelier.Plan()); err != nil {
			t.Error("the project's files went with the mission")
		}
		p, err := (Audit{Dossier: dossierJournalAudit()}).Lire(FiltreAudit{Q: "mission supprimée"})
		if err != nil || !slices.ContainsFunc(p.Lignes, func(l LigneAudit) bool {
			return l.Mission == "encours1" && strings.Contains(l.Resultat, "mission encours1")
		}) {
			t.Errorf("not audited: %v %+v", err, p.Lignes)
		}
	})

	creee := func(t *testing.T) string {
		return stockee(t, "lancee", []VoieMission{{Module: "dev", Ref: neuf.Nom, Etat: "lancee"}}, evenementMission("lancee", "project created: "+neuf.Dossier))
	}
	t.Run("sans_dossier", func(t *testing.T) {
		id := creee(t)
		if c, _ := appel("DELETE", "/api/missions/"+id, `{}`); c != 200 || existe(id) {
			t.Fatalf("delete: %d", c)
		}
		if _, ok := s.projetParNom(neuf.Nom); !ok || len(corbeille) != 0 {
			t.Fatalf("the project went without being ticked: %v", corbeille)
		}
	})

	t.Run("avec_dossier", func(t *testing.T) {
		id, autre := creee(t), creee(t)
		if _, b := appel("GET", "/api/missions/"+id, ""); !strings.Contains(b, "Another mission uses this project") {
			t.Fatalf("a project shared by two missions offered: %s", b)
		}
		if c, _ := appel("DELETE", "/api/missions/"+autre, ""); c != 200 {
			t.Fatal(c)
		}
		// its pilot works: the old mission stops nothing, the owner stops it first
		pidNeuf := filepath.Join(neuf.Atelier(), "pilot.pid")
		ecrireFichier(t, pidNeuf, "1", 0o644)
		if c, b := appel("DELETE", "/api/missions/"+id, `{"projet":true}`); c != http.StatusConflict || !strings.Contains(b, "Stop its pilot first") || !existe(id) || len(arrets) != 0 {
			t.Fatalf("a project removed while its pilot runs: %d %s, stops %v", c, b, arrets)
		}
		_ = os.Remove(pidNeuf)
		if _, b := appel("GET", "/api/missions/"+id, ""); !strings.Contains(b, `"dossier_ok":true`) || !strings.Contains(b, `"projet":"neuf"`) {
			t.Fatalf("preview: %s", b)
		}
		// the folder still held by the pilot just stopped: refused twice, then it goes
		refus, normale := 2, corbeilleProjet
		pauseCorbeille = time.Millisecond
		corbeilleProjet = func(d string) error {
			if refus--; refus >= 0 {
				return errors.New("Windows refused (a file in use?)")
			}
			return normale(d)
		}
		defer func() { corbeilleProjet, pauseCorbeille = normale, 500*time.Millisecond }()
		if c, b := appel("DELETE", "/api/missions/"+id, `{"projet":true}`); c != 200 || !strings.Contains(b, "folder "+strings.ReplaceAll(neuf.Dossier, `\`, `\\`)) {
			t.Fatalf("delete with the folder: %d %s", c, b)
		}
		if _, ok := s.projetParNom(neuf.Nom); ok || !slices.Equal(corbeille, []string{neuf.Dossier}) {
			t.Fatalf("project still listed or folder not in the recycle bin: %v", corbeille)
		}
		if _, err := os.Stat(atelier.Dossier); err != nil {
			t.Fatal("another folder went")
		}
	})

	t.Run("chemin_dangereux", func(t *testing.T) {
		home, _ := os.UserHomeDir()
		racine := filepath.VolumeName(base) + string(filepath.Separator)
		for _, d := range []string{home, racine, filepath.Dir(home), "relatif", s.kitCourant().Racine} {
			if d != "" && controlerDossierSupprime(d, s.kitCourant().Racine) == "" {
				t.Errorf("%q accepted", d)
			}
		}
		lien, cible := filepath.Join(base, "lien"), filepath.Join(base, "dehors")
		ecrireFichier(t, filepath.Join(cible, "a.txt"), "a", 0o644)
		if os.Symlink(cible, lien) == nil {
			if controlerDossierSupprime(lien, "") == "" {
				t.Error("a link accepted")
			}
		}
		perso := Projet{Nom: "perso", Dossier: home}
		s.config.Projets = append(s.config.Projets, perso)
		id := stockee(t, "terminee", []VoieMission{{Module: "dev", Ref: perso.Nom}}, evenementMission("lancee", "project created: "+home))
		if c, _ := appel("DELETE", "/api/missions/"+id, `{"projet":true}`); c != http.StatusConflict || !existe(id) || len(corbeille) != 1 {
			t.Fatalf("the personal folder accepted: %d %v", c, corbeille)
		}
		_, _ = appel("DELETE", "/api/missions/"+id, "")
		s.config.Projets = slices.DeleteFunc(s.config.Projets, func(p Projet) bool { return p.Nom == perso.Nom })
	})

	// M300: the module reads the mission files at each hub request; Windows refuses to remove a file being read
	t.Run("lue pendant la suppression", func(t *testing.T) {
		for range 30 {
			id, fin := stockee(t, "terminee", nil), make(chan struct{})
			go func() {
				for {
					select {
					case <-fin:
						return
					default:
						_, _ = lireMission(id)
					}
				}
			}()
			_, err := s.supprimerMission(id, "test", OptionsSuppression{})
			close(fin)
			if err != nil {
				t.Fatalf("deleted while read: %v", err)
			}
		}
	})
}
