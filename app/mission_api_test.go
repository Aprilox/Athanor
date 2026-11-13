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

// TestMissionAPI (M155): create (hostile sentence and mentions refused, the sentence masked, the orchestrator
// started), answer its question (unknown question 404, unknown choice 400, answered 409; the session resumed with
// the answer), change the card without the agent (boxes, policy, environments: computed again, no session) and
// with a consigne (session resumed with the card), cancel (session stopped, token revoked, launch request refused),
// and every route of a mission in another state answers 409. No vault value in any answer.
func TestMissionAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	temoin := "TEMOIN-" + aleatoire(10)
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, temoin); err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5", Roles: []string{"prod"}},
			ServeurInventaire{ID: "a2", Nom: "dev-1", Hote: "203.0.113.6", Roles: []string{"dev"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	s.config.Projets = append(s.config.Projets, Projet{Nom: "boutique", Dossier: filepath.Join(t.TempDir(), "boutique")})
	s.session = aleatoire(8) // Athanor's password is set: the window is unlocked
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })

	var reponses strings.Builder
	appel := func(chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	attendu := func(chemin, corps string, code int) map[string]any {
		t.Helper()
		c, v := appel(chemin, corps)
		if c != code {
			t.Fatalf("%s %s: %d %v, expected %d", chemin, corps, c, v, code)
		}
		return v
	}
	finie := func(id string) Mission {
		t.Helper()
		for range 200 {
			if !orchestrateurTravaille(id) {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		m, err := lireMission(id)
		if err != nil || orchestrateurTravaille(id) {
			t.Fatalf("the orchestrator of %s did not end: %v", id, err)
		}
		return m
	}

	carte := `{"titre":"Boutique en ligne","projet":{"nom":"boutique"},"dev":{"brief":"Finir la boutique"},` +
		`"envs":[{"nom":"prod","url":"https://boutique.example.org","protection":"aucune","serveurs":["a1"]}],` +
		`"serveurs":{"brief":"DNS, certificat, proxy"},"acces":[{"entree":"Jeton Cloudflare","pour":"a1"}],"jalons_estimes":4}`
	var consignes []string
	scenario := "question"
	ancien := sessionOrchestrateur
	t.Cleanup(func() { sessionOrchestrateur = ancien })
	sessionOrchestrateur = func(_ *Serveur, ctx context.Context, dossier string) error {
		c, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		consignes = append(consignes, string(c))
		jeton, _ := os.ReadFile(filepath.Join(dossier, "hub-jeton"))
		outil := func(nom, args string) string {
			rep := appelMCP(t, mux, "Bearer "+strings.TrimSpace(string(jeton)), appelOutil(nom, args))
			if rep.Error != nil || rep.Result.IsError {
				t.Errorf("%s: %+v", nom, rep)
				return ""
			}
			return rep.Result.Content[0].Text
		}
		var err error
		switch scenario {
		case "question":
			outil("question_proprietaire", `{"texte":"Quel domaine ?","choix":["boutique.example.org","autre"]}`)
		case "carte":
			outil("proposer_mission", `{"carte":`+carte+`}`)
		case "bloque": // asks to launch, then works until it is stopped
			outil("lancer_mission", `{"id":"`+filepath.Base(strings.TrimSuffix(dossier, "-session"))+`"}`)
			<-ctx.Done()
			err = ctx.Err()
		}
		_ = os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(`{"type":"system","subtype":"init","session_id":"sess-1"}`+"\n"+
			`{"type":"result","is_error":false,"result":"ok"}`+"\n"), 0o600)
		return err
	}

	// creating: hostile input refused, nothing written
	for _, corps := range []string{`{`, `{"phrase":"  "}`, `{"phrase":"` + strings.Repeat("é", maxPhraseMission+1) + `"}`,
		`{"phrase":"x","mentions":[{"type":"serveur","ref":"zzz"}]}`, `{"phrase":"x","mentions":[{"type":"projet","ref":"../boutique"}]}`,
		`{"phrase":"x","mentions":[{"type":"cle","ref":"Jeton Cloudflare"}]}`} {
		attendu("/api/missions", corps, http.StatusBadRequest)
	}
	if l := lireMissions(); len(l) != 0 {
		t.Fatalf("a refused mission was written: %v", l)
	}
	v := attendu("/api/missions", `{"phrase":"Mettre la boutique en ligne avec `+temoin+`","mentions":[{"type":"projet","ref":"boutique"},{"type":"serveur","ref":"a1"},{"type":"nouveau"}]}`, http.StatusOK)
	id, _ := v["mission"].(map[string]any)["id"].(string)
	m := finie(id)
	if m.Etat != "preparation" || len(m.Questions) != 1 || m.Politique != "prod" || strings.Contains(m.Phrase, temoin) ||
		!slices.Equal(m.Mentions.Projets, []string{"boutique"}) || !slices.Equal(m.Mentions.Serveurs, []string{"a1"}) || !m.Mentions.Nouveau ||
		len(consignes) != 1 || !strings.Contains(consignes[0], "server web-1") {
		t.Fatalf("created mission: %+v, consignes %d", m, len(consignes))
	}
	q := m.Questions[0]
	base := "/api/missions/" + id

	// a card is not there yet: no change; the question: unknown, bad choice, empty answer
	attendu(base+"/modifier", `{"politique":"tout"}`, http.StatusConflict)
	attendu(base+"/repondre", `{"question":"zz","choix":"autre"}`, http.StatusNotFound)
	attendu(base+"/repondre", `{"question":"`+q.ID+`","choix":"inventé"}`, http.StatusBadRequest)
	attendu(base+"/repondre", `{"question":"`+q.ID+`","texte":"   "}`, http.StatusBadRequest)
	attendu(base+"/repondre", `{`, http.StatusBadRequest)
	scenario = "carte"
	attendu(base+"/repondre", `{"question":"`+q.ID+`","choix":"boutique.example.org"}`, http.StatusOK)
	m = finie(id)
	if m.Etat != "proposee" || m.Questions[0].Reponse != "boutique.example.org" || len(consignes) != 2 ||
		!strings.Contains(consignes[1], "The owner answers your question") || !strings.Contains(consignes[1], "boutique.example.org") {
		t.Fatalf("after the answer: %+v, consigne %q", m, consignes[len(consignes)-1])
	}
	attendu(base+"/repondre", `{"question":"`+q.ID+`","texte":"encore"}`, http.StatusConflict)

	// changing without the agent: hostile input, then boxes, policy and environments computed again
	for _, corps := range []string{`{"politique":"jamais"}`, `{"acces_decoches":[{"entree":"Inconnu","pour":"a1"}]}`,
		`{"envs":[{"nom":"prod","url":"javascript:alert(1)","protection":"aucune","serveurs":["a1"]}]}`,
		`{"projet":{"nom":"../x","nouveau":true}}`, `{"consigne":"` + strings.Repeat("a", maxConsigneMission+1) + `"}`, `[`} {
		attendu(base+"/modifier", corps, http.StatusBadRequest)
	}
	v = attendu(base+"/modifier", `{"acces_decoches":[{"entree":"jeton cloudflare","pour":"a1"}],"politique":"tout",`+
		`"envs":[{"nom":"prod","url":"https://boutique.example.org","protection":"aucune","serveurs":["a1"]},`+
		`{"nom":"dev","url":"https://dev.boutique.example.org","protection":"auth_proxy","serveurs":["a2"]}]}`, http.StatusOK)
	m, _ = lireMission(id)
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	if m.Politique != "tout" || len(c.Envs) != 2 || !c.Acces[0].Decoche || len(consignes) != 2 ||
		!slices.Contains(c.Approbations, ApprobationPrevue{"deploiement", "dev"}) || !slices.Contains(c.Approbations, ApprobationPrevue{"plan_serveurs", "dev"}) ||
		slices.Contains(c.Approbations, ApprobationPrevue{Type: "deverrouiller_coffre"}) || !slices.Contains(c.Touche.DNS, "dev.boutique.example.org") {
		t.Fatalf("changed card: politique %s, %+v, sessions %d", m.Politique, c, len(consignes))
	}
	attendu(base+"/modifier", `{"acces_decoches":[]}`, http.StatusOK) // every box ticked again
	m, _ = lireMission(id)
	c = CarteMission{} // decoche is omitted when false: never decode over the old card
	_ = json.Unmarshal(m.Carte, &c)
	if c.Acces[0].Decoche {
		t.Fatal("the box was not ticked again")
	}

	// a change asked in words: the orchestrator resumed with the card; while it works, nothing else changes it
	scenario = "bloque"
	attendu(base+"/modifier", `{"consigne":"Ajoute un environnement staging"}`, http.StatusOK)
	if !orchestrateurTravaille(id) {
		t.Fatal("the consigne did not resume the orchestrator")
	}
	if b, _ := os.ReadFile(filepath.Join(dossierSessionMission(id), "consigne.txt")); !strings.Contains(string(b), "Ajoute un environnement staging") || !strings.Contains(string(b), "dev.boutique.example.org") {
		t.Fatalf("the resumed session's consigne: %q", b)
	}
	attendu(base+"/modifier", `{"politique":"prod"}`, http.StatusConflict)
	var approbation string
	for range 100 {
		l, _ := s.approbations.Liste()
		if i := slices.IndexFunc(l, func(d Approbation) bool { return d.Mission == id && d.Action == actionMission }); i >= 0 {
			approbation = l[i].ID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if approbation == "" {
		t.Fatal("no launch request from lancer_mission")
	}

	// cancelling: the session stopped, its token revoked, the launch request refused; then everything is 409
	v = attendu(base+"/annuler", `{}`, http.StatusOK)
	m = finie(id)
	if m.Etat != "annulee" || slices.ContainsFunc(m.Historique, func(e EvenementMission) bool { return e.Etat == "echec" }) {
		t.Fatalf("cancelled mission: %+v", m)
	}
	if slices.ContainsFunc(s.jetons.Liste(), func(x SessionHub) bool { return x.Projet == prefixeMission+id }) {
		t.Fatal("the orchestrator's token is still valid")
	}
	if d, _ := s.approbations.Lire(approbation); d.Etat != "refusee" {
		t.Fatalf("the launch request: %+v", d)
	}
	attendu(base+"/annuler", `{}`, http.StatusConflict)
	attendu(base+"/modifier", `{"politique":"prod"}`, http.StatusConflict)
	attendu(base+"/repondre", `{"question":"`+q.ID+`","texte":"x"}`, http.StatusConflict)
	for _, chemin := range []string{"/api/missions/zzzzzz/annuler", "/api/missions/ZZ..zz/modifier", "/api/missions/abc/repondre"} {
		attendu(chemin, `{"politique":"prod","question":"x","texte":"y"}`, http.StatusNotFound)
	}
	if strings.Contains(reponses.String(), temoin) {
		t.Fatal("a vault value in an answer")
	}
	b, _ := os.ReadFile(filepath.Join(dossierMissions(), id+".json"))
	if strings.Contains(string(b), temoin) {
		t.Fatal("a vault value in the mission's file")
	}
}
