package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMissionCarteMartin (M160): the sentence of VISION §1 with @vps → a scripted fake Claude reads modules_etat
// then calls proposer_mission through the real /api/mcp route → the card: Dev and Servers lanes, dev behind
// auth_proxy on dev.martin.fr, prod on martin.fr, both on the mentioned VPS, the accesses ticked, the go-live
// approval planned, what is touched (the server, 2 DNS names, the Caddy files), the estimate; Modifier (one access
// unticked, policy tout) computes it again without the agent; Lancer starts both lanes (fake pilot, fake chef ops).
// No vault value in any answer, consigne or file.
func TestMissionCarteMartin(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil { // see TestMissionLancer: dossierAthanor() must not move mid-test
		t.Fatal(err)
	}
	temoin := "TEMOIN-martin-" + aleatoire(10)
	cf, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare"}, temoin+"-cf")
	if err != nil {
		t.Fatal(err)
	}
	maps, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Clé Google Maps", Famille: "secret", Type: "jeton"}, temoin+"-maps")
	if err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { // the owner's one VPS
		return append(l, ServeurInventaire{ID: "v1", Nom: "vps", Hote: "203.0.113.20", Roles: []string{"dev", "prod"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "ancien-site", Dossier: filepath.Join(base, "ancien-site")})
	s.session = aleatoire(8)
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })

	var reponses strings.Builder
	var mu sync.Mutex
	appel := func(methode, chemin, corps string, code int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		mu.Lock()
		reponses.WriteString(w.Body.String())
		mu.Unlock()
		if w.Code != code {
			t.Fatalf("%s %s: %d %s, expected %d", methode, chemin, w.Code, w.Body.String(), code)
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return v
	}
	carteDe := func(id string) (Mission, CarteMission) {
		t.Helper()
		var v struct{ Mission Mission }
		r := httptest.NewRequest("GET", "/api/missions/"+id, nil)
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		var c CarteMission
		_ = json.Unmarshal(v.Mission.Carte, &c)
		return v.Mission, c
	}

	// the fake orchestrator: reads the hub's state, then proposes the card V§1 calls for
	var consignes []string
	ancien, ancienPilote, ancienneSession := sessionOrchestrateur, demarrerPiloteMission, sessionTacheServeurs
	t.Cleanup(func() {
		sessionOrchestrateur, demarrerPiloteMission, sessionTacheServeurs = ancien, ancienPilote, ancienneSession
	})
	sessionOrchestrateur = func(_ *Serveur, _ context.Context, dossier string) error {
		c, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		consignes = append(consignes, string(c))
		jeton, _ := os.ReadFile(filepath.Join(dossier, "hub-jeton"))
		outil := func(nom, args string) string {
			rep := appelMCP(t, mux, "Bearer "+strings.TrimSpace(string(jeton)), appelOutil(nom, args))
			if rep.Error != nil || rep.Result.IsError {
				t.Errorf("%s: %+v", nom, rep)
				return ""
			}
			mu.Lock()
			reponses.WriteString(rep.Result.Content[0].Text)
			mu.Unlock()
			return rep.Result.Content[0].Text
		}
		etat := outil("modules_etat", "{}")
		for _, v := range []string{`"vps"`, `"Jeton Cloudflare"`, `"Clé Google Maps"`, `"ancien-site"`} {
			if !strings.Contains(etat, v) {
				t.Errorf("modules_etat without %s: %s", v, etat)
			}
		}
		outil("proposer_mission", `{"carte":{"titre":"Site vitrine de la boulangerie Martin","projet":{"nom":"boulangerie-martin","nouveau":true},`+
			`"dev":{"brief":"Un site vitrine : accueil, produits, horaires, plan d'accès, contact"},`+
			`"envs":[{"nom":"dev","url":"https://dev.martin.fr","protection":"auth_proxy","serveurs":["v1"]},`+
			`{"nom":"prod","url":"https://martin.fr","protection":"aucune","serveurs":["v1"]}],`+
			`"serveurs":{"brief":"DNS, certificats, Caddy en reverse proxy, mot de passe devant la dev, pare-feu"},`+
			`"acces":[{"entree":"Jeton Cloudflare","pour":"v1"},{"entree":"Clé Google Maps","pour":"boulangerie-martin"}],"jalons_estimes":6,`+
			`"approbations":[],"touche":{"serveurs":["autre"]}}}`)
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(`{"type":"system","subtype":"init","session_id":"sess-m"}`+"\n"+
			`{"type":"result","is_error":false,"result":"ok"}`+"\n"), 0o600)
	}
	var pilotes, taches []string
	demarrerPiloteMission = func(_ *Serveur, p Projet) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		pilotes = append(pilotes, p.Nom)
		return "Pilot started", nil
	}
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error { // the fake chef ops
		b, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		mu.Lock()
		taches = append(taches, string(b))
		mu.Unlock()
		j := `{"type":"system","subtype":"init","session_id":"sess-ops"}` + "\n" + `{"type":"result","is_error":false,"result":"done"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}

	// 1. the sentence of V§1, @vps and "New project": the card in a few seconds
	phrase := "Développe un site vitrine pour la boulangerie Martin. Mets une version de dev protégée par mot de passe sur dev.martin.fr " +
		"et la prod sur martin.fr, sur mon @vps, de la façon la plus propre et la plus sûre possible."
	v := appel("POST", "/api/missions", `{"phrase":"`+phrase+`","mentions":[{"type":"serveur","ref":"v1"},{"type":"nouveau"}]}`, http.StatusOK)
	id, _ := v["mission"].(map[string]any)["id"].(string)
	for i := 0; i < 200 && orchestrateurTravaille(id); i++ {
		time.Sleep(25 * time.Millisecond)
	}
	m, c := carteDe(id)
	if m.Etat != "proposee" || m.Phrase != phrase || !slices.Equal(m.Mentions.Serveurs, []string{"v1"}) || !m.Mentions.Nouveau || len(consignes) != 1 ||
		!strings.Contains(consignes[0], "boulangerie Martin") || !strings.Contains(consignes[0], "server vps") {
		t.Fatalf("mission %+v, consignes %q", m, consignes)
	}
	// the lanes: Dev builds a new project, Servers prepares in parallel
	if c.Projet != (ProjetCarte{"boulangerie-martin", true}) || c.Dev.Brief == "" || !strings.Contains(c.Serveurs.Brief, "Caddy") || len(c.Envs) != 2 {
		t.Fatalf("lanes: %+v", c)
	}
	if e := c.Envs[0]; e.Nom != "dev" || e.URL != "https://dev.martin.fr" || e.Protection != "auth_proxy" || !slices.Equal(e.Serveurs, []string{"v1"}) {
		t.Fatalf("dev: %+v", e)
	}
	if e := c.Envs[1]; e.Nom != "prod" || e.URL != "https://martin.fr" || !slices.Equal(e.Serveurs, []string{"v1"}) {
		t.Fatalf("prod: %+v", e)
	}
	// the accesses, resolved by the vault and ticked; never a value
	if len(c.Acces) != 2 || c.Acces[0].ID != cf.ID || c.Acces[0].Portee != "infra" || c.Acces[1].ID != maps.ID ||
		slices.ContainsFunc(c.Acces, func(a AccesCarte) bool { return a.Decoche || a.ARemplir }) {
		t.Fatalf("accesses: %+v", c.Acces)
	}
	// worked out by the Go (the agent's empty list and its "autre" server dropped)
	if !slices.Contains(c.Approbations, ApprobationPrevue{"mise_en_prod", "https://martin.fr"}) || !slices.Contains(c.Approbations, ApprobationPrevue{"plan_serveurs", "prod"}) ||
		!slices.Contains(c.Approbations, ApprobationPrevue{"durcissement", "vps"}) || slices.Contains(c.Approbations, ApprobationPrevue{"deploiement", "dev"}) {
		t.Fatalf("planned approvals: %+v", c.Approbations)
	}
	if k := c.Touche; !slices.Equal(k.Serveurs, []string{"vps"}) || !slices.Equal(k.DNS, []string{"dev.martin.fr", "martin.fr"}) || !slices.Equal(k.Zones, []string{"martin.fr"}) ||
		!slices.Contains(k.Fichiers, "/etc/caddy/sites/dev.martin.fr.caddy") || !slices.Contains(k.Fichiers, "/etc/caddy/sites/martin.fr.caddy") ||
		k.Dossier != filepath.Join(base, "boulangerie-martin") {
		t.Fatalf("touched: %+v", k)
	}
	if e := c.Estimation; e.Jalons != 6 || e.MinutesMin <= 0 || e.MinutesMax <= e.MinutesMin {
		t.Fatalf("estimate: %+v", e)
	}

	// 2. Modifier: Maps unticked, policy tout — computed again by the Go, no new session
	appel("POST", "/api/missions/"+id+"/modifier", `{"acces_decoches":[{"entree":"Clé Google Maps","pour":"boulangerie-martin"}],"politique":"tout"}`, http.StatusOK)
	m, c = carteDe(id)
	if m.Politique != "tout" || c.Acces[0].Decoche || !c.Acces[1].Decoche || len(consignes) != 1 ||
		!slices.Contains(c.Approbations, ApprobationPrevue{"deploiement", "dev"}) || !slices.Contains(c.Approbations, ApprobationPrevue{"plan_serveurs", "dev"}) ||
		!slices.Contains(c.Approbations, ApprobationPrevue{"mise_en_prod", "https://martin.fr"}) {
		t.Fatalf("changed card: %s %+v", m.Politique, c)
	}

	// 3. Lancer: the project created and its pilot started, the ticked access given, both Servers tasks run
	v = appel("POST", "/api/missions/"+id+"/lancer", "", http.StatusOK)
	for i := 0; i < 400; i++ {
		tachesEnCours.Lock()
		n := len(tachesEnCours.m)
		tachesEnCours.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	m, _ = lireMission(id)
	if m.Etat != "lancee" || len(m.Voies) != 3 || m.Voies[0] != (VoieMission{Module: "dev", Ref: "boulangerie-martin", Etat: "lancee"}) ||
		m.Voies[1].Module != "servers" || m.Voies[2].Module != "servers" || !slices.Equal(pilotes, []string{"boulangerie-martin"}) {
		t.Fatalf("launched: %+v, pilots %v", m, pilotes)
	}
	mu.Lock()
	if len(taches) != 2 || !slices.ContainsFunc(taches, func(x string) bool { return strings.Contains(x, "https://dev.martin.fr") }) ||
		!slices.ContainsFunc(taches, func(x string) bool { return strings.Contains(x, "environment prod: https://martin.fr") }) {
		t.Fatalf("chef ops tasks: %q", taches)
	}
	mu.Unlock()
	p, ok := s.projetNomme("boulangerie-martin")
	if b, _ := os.ReadFile(p.Demande()); !ok || !strings.Contains(string(b), "horaires") {
		t.Fatalf("project %+v, request %q", p, b)
	}
	for _, a := range s.acces.Projet(p.Nom).Acces {
		if a.Entree == maps.ID {
			t.Fatalf("the unticked access was given: %+v", a)
		}
	}
	if !slices.ContainsFunc(s.acces.Projet(p.Nom).Acces, func(a AccesProjet) bool { return a.Entree == cf.ID }) {
		t.Fatal("the ticked access was not given")
	}
	envs := s.acces.Environnements(p.Nom)
	if d := envs["dev"]; d.URL != "https://dev.martin.fr" || d.Protection != "auth_proxy" || !d.DeployerAuJalon || !slices.Equal(d.Serveurs, []string{"v1"}) {
		t.Fatalf("dev: %+v", d)
	}
	if pr := envs["prod"]; pr.URL != "https://martin.fr" || pr.Politique != "accord" || !slices.Equal(pr.Serveurs, []string{"v1"}) {
		t.Fatalf("prod: %+v", pr)
	}

	// no vault value in any answer, consigne, task or file of Athanor and of the new project
	tout := reponses.String() + strings.Join(consignes, "") + strings.Join(taches, "")
	if strings.Contains(tout, temoin) {
		t.Fatal("a vault value in an answer or a consigne")
	}
	for _, d := range []string{dossierAthanor(), base} {
		_ = filepath.WalkDir(d, func(chemin string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				if b, _ := os.ReadFile(chemin); strings.Contains(string(b), temoin) {
					t.Errorf("a vault value in %s", chemin)
				}
			}
			return nil
		})
	}
}
