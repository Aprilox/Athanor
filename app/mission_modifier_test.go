package main

import (
	"context"
	"encoding/json"
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

// TestMissionModifiable (M266, note of 13h03 §B): a mission is changed after its launch. Failed: a key added goes
// to the project at once, the reason of the failure stays, the launch resumes without a second grant. Running: a
// server added gets its environment and its Servers lane, launched by the follower's next reading; a key added is
// written to the project's secrets. A server whose Servers task runs cannot be removed (said with the mission);
// once the task is over it can. Grants given and taken back are audited, never with a value; the project, the
// orchestrator's change and the policy stay frozen once launched.
func TestMissionModifiable(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	temoin := "TEMOIN-" + aleatoire(10)
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a2", Nom: "web-1", Hote: "203.0.113.6"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	cf, err := s.coffre.Ajouter(EntreeCoffre{Nom: "CloudFlare - exemple.fr", Famille: "secret", Type: "cloudflare", Portee: "code"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	smtp, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Mot de passe SMTP", Famille: "secret", Type: "autre", Portee: "code"}, temoin+"-2")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	ancienPilote, ancienneSession := demarrerPiloteMission, sessionTacheServeurs
	t.Cleanup(func() { demarrerPiloteMission, sessionTacheServeurs = ancienPilote, ancienneSession })
	piloteKO := true
	demarrerPiloteMission = func(_ *Serveur, p Projet) (string, error) {
		if piloteKO {
			return "", errors.New("the pilot script is missing")
		}
		return "Pilot started", nil
	}
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"nothing to do"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	s.session = aleatoire(8)
	mux := http.NewServeMux()
	s.routes(mux)
	var reponses strings.Builder
	appel := func(chemin, corps string) (int, Mission) {
		t.Helper()
		methode := "POST"
		if corps == "" {
			methode = "GET"
		}
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var d struct {
			Mission Mission `json:"mission"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &d)
		return w.Code, d.Mission
	}
	attendre := func() {
		for range 400 {
			tachesEnCours.Lock()
			n := len(tachesEnCours.m)
			tachesEnCours.Unlock()
			if n == 0 {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("a Servers task did not end")
	}
	// lancee: an existing project framed already, its card proposed then launched
	lancee := func(nom string) (string, Mission) {
		t.Helper()
		p := Projet{Nom: nom, Dossier: filepath.Join(base, nom)}
		ecrireFichier(t, p.Demande(), "# "+nom+"\n\nUn projet.\n", 0o644)
		ecrireFichier(t, p.Plan(), "# Plan\n\n- [ ] M1 : Le site\n", 0o644)
		s.config.Projets = append(s.config.Projets, p)
		c := CarteMission{Titre: "Site " + nom, Projet: ProjetCarte{Nom: nom}, Dev: BriefCarte{Brief: "Faire le site"}, JalonsEstimes: 1}
		x := s.contexteCarte()
		if err := validerCarte(&c, x); err != nil {
			t.Fatal(err)
		}
		id := aleatoire(8)
		b, _ := json.Marshal(calculerCarte(c, "prod", x))
		if err := enregistrerMission(Mission{ID: id, Phrase: "une mission", Etat: "proposee", Politique: "prod", Carte: b, Voies: []VoieMission{},
			Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}); err != nil {
			t.Fatal(err)
		}
		m, _ := s.lancerMission(id, "proprietaire", "", "")
		return id, m
	}
	octroi := func(projet, entree string) []AccesProjet {
		var l []AccesProjet
		for _, g := range s.acces.Projet(projet).Acces {
			if g.Entree == entree {
				l = append(l, g)
			}
		}
		return l
	}
	audit := func(projet string) map[string]int {
		a := map[string]int{}
		for _, l := range lignesAudit(t, dossierJournalAudit()) {
			if l.Projet == projet {
				a[l.Action]++
				if strings.Contains(l.Resultat, temoin) {
					t.Errorf("a vault value in the audit: %+v", l)
				}
			}
		}
		return a
	}
	evenement := func(m Mission, etat, debut string) bool {
		return slices.ContainsFunc(m.Historique, func(e EvenementMission) bool { return e.Etat == etat && strings.HasPrefix(e.Detail, debut) })
	}

	t.Run("echec_modifiee_avec_une_cle_puis_relancee", func(t *testing.T) {
		id, m := lancee("p-echec")
		if m.Etat != "echec" || !echecLancement(m) {
			t.Fatalf("launch: %s %+v", m.Etat, m.Historique)
		}
		code, m := appel("/api/missions/"+id+"/ressources", `{"type":"cle","ref":"`+cf.ID+`"}`)
		g := octroi("p-echec", cf.ID)
		if code != http.StatusOK || len(g) != 1 || g[0].Auto != "mission" || g[0].Env != "dev" || audit("p-echec")["acces_accorde"] != 1 {
			t.Fatalf("key added to a failed mission: %d grants %+v audit %v", code, g, audit("p-echec"))
		}
		if !evenement(m, "lancee", "access given: CloudFlare") || !evenement(m, "modifiee", "resource added: cle") || !echecLancement(m) ||
			!strings.Contains(raisonEchec(m).Detail, "pilot script is missing") {
			t.Fatalf("history after the change: %+v", m.Historique)
		}
		code, m = appel("/api/missions/"+id, "")
		if code != http.StatusOK || m.Figes["carte"] != "" || m.Figes["projet"] != figeProjet || m.Figes["consigne"] == "" {
			t.Fatalf("frozen fields: %d %v", code, m.Figes)
		}
		for _, corps := range []string{`{"consigne":"Ajoute un blog"}`, `{"politique":"autonome"}`} {
			if code, _ = appel("/api/missions/"+id+"/modifier", corps); code != http.StatusConflict {
				t.Fatalf("%s on a launched mission: %d", corps, code)
			}
		}
		piloteKO = false
		m, err := s.lancerMission(id, "proprietaire", "", "")
		if err != nil || m.Etat != "lancee" || len(octroi("p-echec", cf.ID)) != 1 || !slices.ContainsFunc(m.Voies, func(v VoieMission) bool { return v.Module == "dev" }) {
			t.Fatalf("resumed: %v %s %+v %+v", err, m.Etat, octroi("p-echec", cf.ID), m.Voies)
		}
	})

	t.Run("ressources_ajoutees_pendant_l_execution", func(t *testing.T) {
		piloteKO = false
		id, m := lancee("p-cours")
		if m.Etat != "lancee" {
			t.Fatalf("launch: %s %+v", m.Etat, m.Historique)
		}
		code, m := appel("/api/missions/"+id+"/ressources", `{"type":"serveur","ref":"a2"}`)
		env, ok := s.acces.Environnements("p-cours")["site"]
		i := slices.IndexFunc(m.Voies, func(v VoieMission) bool { return v.Module == "servers" && v.Env == "p-cours/site" })
		if code != http.StatusOK || !ok || !slices.Equal(env.Serveurs, []string{"a2"}) || i < 0 || m.Voies[i].Etat != "attente_plan" {
			t.Fatalf("server added while running: %d env %+v lanes %+v", code, env, m.Voies)
		}
		m = s.preparerServeursMission(m) // the follower's next reading
		attendre()
		if i = slices.IndexFunc(m.Voies, func(v VoieMission) bool { return v.Module == "servers" && v.Env == "p-cours/site" }); i < 0 || m.Voies[i].Ref == "" || audit("p-cours")["préparation des serveurs"] != 1 {
			t.Fatalf("the Servers lane not launched by the follower: %+v %v", m.Voies, audit("p-cours"))
		}
		code, _ = appel("/api/missions/"+id+"/ressources", `{"type":"cle","ref":"`+smtp.ID+`"}`)
		g := octroi("p-cours", smtp.ID)
		secrets, _ := lireAtelier(Projet{Nom: "p-cours", Dossier: filepath.Join(base, "p-cours")}, "secrets.env", 1<<20)
		if code != http.StatusOK || len(g) != 1 || !strings.Contains(string(secrets), g[0].Variable+"=") {
			t.Fatalf("key added while running: %d %+v, in secrets.env: %v", code, g, len(g) == 1 && strings.Contains(string(secrets), g[0].Variable))
		}

		// the Servers task of site runs: its server is frozen, the dev key is not
		ref := m.Voies[i].Ref
		tachesEnCours.Lock()
		tachesEnCours.m[ref] = &tacheEnCours{}
		tachesEnCours.Unlock()
		code, vue := appel("/api/missions/"+id, "")
		if code != http.StatusOK || vue.Figes["env:site"] != figeEnv {
			t.Fatalf("frozen environment not said: %v", vue.Figes)
		}
		if code, _ = appel("/api/missions/"+id+"/ressources", `{"type":"serveur","ref":"a2","retirer":true}`); code != http.StatusConflict {
			t.Fatalf("server of a running task removed: %d", code)
		}
		if env = s.acces.Environnements("p-cours")["site"]; !slices.Equal(env.Serveurs, []string{"a2"}) {
			t.Fatalf("refused, yet the environment changed: %+v", env)
		}
		code, m = appel("/api/missions/"+id+"/ressources", `{"type":"cle","ref":"`+smtp.ID+`","retirer":true}`)
		if code != http.StatusOK || len(octroi("p-cours", smtp.ID)) != 0 || audit("p-cours")["acces_retire"] != 1 || !evenement(m, "modifiee", "access taken back: Mot de passe SMTP") {
			t.Fatalf("dev key taken back while running: %d %+v %v", code, octroi("p-cours", smtp.ID), audit("p-cours"))
		}
		tachesEnCours.Lock()
		delete(tachesEnCours.m, ref)
		tachesEnCours.Unlock()
		if code, m = appel("/api/missions/"+id+"/ressources", `{"type":"serveur","ref":"a2","retirer":true}`); code != http.StatusOK || len(s.acces.Environnements("p-cours")["site"].Serveurs) != 0 {
			t.Fatalf("server removed once the task is over: %d %+v", code, s.acces.Environnements("p-cours")["site"])
		}
		if slices.ContainsFunc(m.Voies, func(v VoieMission) bool { return v.Etat == "attente_plan" }) || m.Etat != "lancee" {
			t.Fatalf("lanes after the removal: %s %+v", m.Etat, m.Voies)
		}
	})

	// a grant the owner gave by hand is never taken back by a mission's change
	t.Run("octroi_du_proprietaire_garde", func(t *testing.T) {
		id, m := lancee("p-main")
		if m.Etat != "lancee" {
			t.Fatalf("launch: %s", m.Etat)
		}
		if _, err := s.acces.Ajouter("p-main", s.entreeCoffre(cf.ID), "dev", "", ""); err != nil {
			t.Fatal(err)
		}
		for _, r := range []bool{false, true} {
			corps, _ := json.Marshal(map[string]any{"type": "cle", "ref": cf.ID, "retirer": r})
			if code, _ := appel("/api/missions/"+id+"/ressources", string(corps)); code != http.StatusOK {
				t.Fatalf("retirer=%v: %d", r, code)
			}
		}
		if g := octroi("p-main", cf.ID); len(g) != 1 || g[0].Auto != "" {
			t.Fatalf("the owner's grant: %+v", g)
		}
	})
	if strings.Contains(reponses.String(), temoin) {
		t.Fatal("a vault value in an answer")
	}
}
