package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestJugerSante (M137): what a check makes of the site, the certificate, the disk and the memory; red twice in a row is an incident.
func TestJugerSante(t *testing.T) {
	dix, deux := 10, 2
	l := jugerSante(&VerifDeploiement{OK: false, Problemes: []string{"HTTP 502"}}, &dix,
		map[string]RessourcesFiche{"vps": {DisqueMo: 100, DisqueLibreMo: 5, MemoireMo: 100, MemoireLibreMo: 60}})
	if got := enJSON(l); len(l) != 3 || !l[0].Rouge || l[0].Code != "verif" || l[1].Code != "certificat" || l[1].Rouge || l[2].Code != "disque" {
		t.Fatalf("problems: %s", got)
	}
	if l := jugerSante(&VerifDeploiement{OK: true}, &deux, nil); len(l) != 1 || !l[0].Rouge {
		t.Errorf("certificate under 3 days: %s", enJSON(l))
	}
	s := &Serveur{}
	if txt := s.texteProblemes(l); txt != "the site does not pass the check made from this PC; its certificate expires in 10 days; the disk of vps is 95% full" {
		t.Errorf("plain words: %q", txt)
	}
	s.config.Langue = "fr"
	if txt := s.texteProblemes(l[2:]); txt != "le disque de vps est plein à 95 %" {
		t.Errorf("in French: %q", txt)
	}
	f := fichierSante{Envs: map[string]SanteEnv{}}
	rouge := SanteEnv{Problemes: []ProblemeSante{{Code: "verif", Rouge: true}}, T: "2026-11-12T10:00:00Z"}
	noterControle(&f, "p/dev", rouge)
	if e := f.Envs["p/dev"]; e.Etat != "alerte" || e.Rouges != 1 {
		t.Errorf("red once: %+v", e)
	}
	rouge.T = "2026-11-12T10:15:00Z"
	noterControle(&f, "p/dev", rouge)
	if e := f.Envs["p/dev"]; e.Etat != "incident" || e.Rouges != 2 || e.RougeDepuis != "2026-11-12T10:00:00Z" {
		t.Errorf("red twice: %+v", e)
	}
	noterControle(&f, "p/dev", SanteEnv{T: "2026-11-12T10:30:00Z"})
	if e := f.Envs["p/dev"]; e.Etat != "ok" || e.Rouges != 0 || e.RougeDepuis != "" {
		t.Errorf("green again: %+v", e)
	}
}

// TestSanteIncidents (M137): the periodic check and its setting, the incidents (a failed deployment, red twice),
// the bell's three actions (prod's Retry and Go back only with the password), the phone told once.
func TestSanteIncidents(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	for _, d := range []string{dossierModule("servers"), fichierSanteEnvs()} {
		_ = os.RemoveAll(d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	const projet = "m137"
	depot, _, servi := depotSiteExemple(t)
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: projet, Dossier: depot}}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}

	// the periodic check on a site that answers, then fails twice
	var panne atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if panne.Load() {
			w.WriteHeader(http.StatusBadGateway)
		}
		fmt.Fprint(w, "site m137")
	}))
	defer site.Close()
	local := aleatoire(8)
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: local, Nom: "vps-local", Hote: "127.0.0.1", Port: 22, Roles: []string{"staging"}, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	for env, e := range map[string]Environnement{"dev": {URL: "http://m137.test", Serveurs: []string{"absent"}, Protection: "aucune", Politique: "accord", DeployerAuJalon: true},
		"staging": {URL: site.URL, Serveurs: []string{local}, Protection: "aucune", Politique: "accord"}, "prod": {URL: "https://m137p.test", Protection: "aucune", Politique: "accord"}} {
		if err := s.acces.DefinirEnvironnement(projet, env, e); err != nil {
			t.Fatalf("%s: %v", env, err)
		}
	}
	if err := s.acces.NoterDeploiement(projet, "staging", ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}); err != nil {
		t.Fatal(err)
	}
	if err := noterHistorique(Deploiement{ID: "st1", Projet: projet, Env: "staging", Commit: servi, Methode: "statique", Etat: "ok"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s.controlerSante(ctx)
	if e := lireSante().Envs[projet+"/staging"]; e.Etat != "ok" || e.Verification == nil || !e.Verification.OK {
		t.Fatalf("healthy site: %s", enJSON(e))
	}
	if _, ok := lireSante().Envs[projet+"/dev"]; ok {
		t.Error("dev checked without a deployment method")
	}
	panne.Store(true)
	s.controlerSante(ctx)
	if i := s.incidentEnv(lireSante(), projet, "staging"); i != nil || lireSante().Envs[projet+"/staging"].Etat != "alerte" {
		t.Fatalf("red once is not an incident: %+v", i)
	}
	s.controlerSante(ctx)
	i := s.incidentEnv(lireSante(), projet, "staging")
	if i == nil || i.Source != "sante" || i.Commit != servi || !strings.Contains(i.Echec, "does not pass the check") || !strings.Contains(i.Tente, "2 checks in a row") {
		t.Fatalf("red twice: %s", enJSON(i))
	}
	if v := s.vueSante(projet)["staging"].(map[string]any); v["etat"] != "incident" {
		t.Errorf("card: %s", enJSON(v))
	}
	if v := s.santeServeurs(); v["envs"].(map[string]string)[projet+"/staging"] != "incident" || v["minutes"] != minutesSanteDefaut {
		t.Errorf("Servers page: %s", enJSON(v))
	}

	// a failed deployment is an incident too
	if err := noterHistorique(Deploiement{ID: "dv1", Projet: projet, Env: "dev", Commit: "abc1234def", Methode: "statique", Etat: "echec", Bascule: true, Erreur: "the site does not answer."}); err != nil {
		t.Fatal(err)
	}
	if err := noterHistorique(Deploiement{ID: "pr1", Projet: projet, Env: "prod", Commit: "abc1234def", Methode: "statique", Etat: "echec", Bascule: true, Erreur: "upload refused"}); err != nil {
		t.Fatal(err)
	}
	dev := s.incidentEnv(lireSante(), projet, "dev")
	if dev == nil || dev.Source != "deploiement" || dev.Echec != "the site does not answer" || !strings.Contains(dev.Tente, "commit abc1234") {
		t.Fatalf("failed deployment: %s", enJSON(dev))
	}
	al := s.alertesIncidents()
	accords := map[string]string{}
	for _, a := range al {
		if len(a.Actions) != 3 || a.Actions[0].ID != "reessayer" || a.Actions[1].ID != "revenir" || a.Actions[2].ID != "main" {
			t.Fatalf("actions: %s", enJSON(a))
		}
		accords[a.Element] = fmt.Sprint(a.Actions[0].Accord, a.Actions[1].Accord, a.Actions[2].Accord)
	}
	if len(al) != 3 || accords[projet+"/prod"] != "true true false" || accords[projet+"/dev"] != "false false false" {
		t.Fatalf("bell: %v %s", accords, enJSON(al))
	}
	if actives, _ := s.alertesHub(nil); !strings.Contains(enJSON(actives), al[0].Empreinte) {
		t.Error("not in the bell")
	}

	// the phone is told once per incident
	s.notifierIncidents()
	premier := lireSante().Notifies
	s.notifierIncidents()
	if len(premier) != 3 || enJSON(lireSante().Notifies) != enJSON(premier) {
		t.Errorf("told: %v then %v", premier, lireSante().Notifies)
	}

	// the actions: prod asks for the password; a stale incident is refused; Retry and Go back run the deployment
	for _, c := range []struct {
		corps string
		code  int
		texte string
	}{
		{`{"env":"prod","action":"reessayer","cle":"deploi-pr1"}`, 401, "Wrong password"},
		{`{"env":"prod","action":"revenir","cle":"deploi-pr1","mot_de_passe":"faux"}`, 401, "Wrong password"},
		{`{"env":"prod","action":"reessayer","cle":"deploi-pr1","mot_de_passe":"` + mdpPreuveTest + `"}`, 409, "no deployment method"},
		{`{"env":"dev","action":"reessayer","cle":"deploi-ancien"}`, 409, "This incident is over."},
		{`{"env":"dev","action":"reessayer","cle":"deploi-dv1"}`, 409, "no deployment method"},
		{`{"env":"dev","action":"revenir","cle":"deploi-dv1"}`, 409, "nothing was deployed"},
		{`{"env":"staging","action":"reessayer"}`, 200, ""},
		{`{"env":"dev","action":"autre"}`, 400, "Invalid request"},
	} {
		code, v := appel("/api/projets/"+projet+"/incident", c.corps)
		if code != c.code || !strings.Contains(fmt.Sprint(v["erreur"]), c.texte) {
			t.Errorf("%s: %d %v", c.corps, code, v)
		}
	}
	if h := historiqueDeploiements(projet, "staging"); len(h) != 2 || h[1].Commit != servi || h[1].Auteur != "proprietaire" {
		t.Errorf("Retry deployed again the version served: %s", enJSON(h))
	}

	// I take over: the relay of dev stops, the server's sheet opens, the bell is quiet for it
	code, v := appel("/api/projets/"+projet+"/incident", `{"env":"dev","action":"main","cle":"deploi-dv1"}`)
	if code != 200 || v["page"] != "#serveur/absent" || s.acces.Environnements(projet)["dev"].DeployerAuJalon {
		t.Fatalf("take over: %d %v", code, v)
	}
	for _, a := range s.alertesIncidents() {
		if a.Element == projet+"/dev" {
			t.Error("still in the bell after taking over")
		}
	}
	if i := s.vueSante(projet)["dev"].(map[string]any)["incident"].(*Incident); i.PrisEnMain == "" {
		t.Error("the card does not say it was taken over")
	}

	// the setting: a fixed list of intervals, or off (no state shown then)
	for corps, code := range map[string]int{`{"minutes":7}`: 400, `{"minutes":30}`: 200} {
		if c, v := appel("/api/serveurs/sante", corps); c != code {
			t.Errorf("setting %s: %d %v", corps, c, v)
		}
	}
	if code, v := appel("/api/serveurs/sante", `{"arret":true}`); code != 200 || v["arret"] != true || v["minutes"] != float64(30) {
		t.Errorf("off: %d %v", code, v)
	}
	if e := lireSante(); !e.Arret || s.vueSante(projet)["staging"].(map[string]any)["verification"].(*VerifDeploiement) != nil {
		t.Errorf("off still shows the health: %s", enJSON(s.santeServeurs()))
	}
}
