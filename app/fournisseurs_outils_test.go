package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

// TestClasserCloudflare: the Cloudflare tools' arguments checked, canonical, and their classes.
func TestClasserCloudflare(t *testing.T) {
	for _, c := range []struct {
		outil, args, canon string
		classe             Classe
	}{
		{"dns_lister", `{}`, `{}`, ClasseLecture},
		{"ssl_lire", `{"nom":"App.Exemple.fr."}`, `{"nom":"app.exemple.fr"}`, ClasseLecture},
		{"dns_ecrire", `{"nom":"a.fr","type":"A","contenu":" 203.0.113.5 "}`, `{"nom":"a.fr","type":"A","contenu":"203.0.113.5","proxied":false,"ttl":1}`, ClasseChangement},
		{"dns_ecrire", `{"nom":"a.fr","type":"AAAA","contenu":"2001:DB8::1","proxied":true,"ttl":300}`, `{"nom":"a.fr","type":"AAAA","contenu":"2001:db8::1","proxied":true,"ttl":300}`, ClasseChangement},
		{"dns_ecrire", `{"nom":"a.fr","type":"CNAME","contenu":"Cible.Exemple.net."}`, `{"nom":"a.fr","type":"CNAME","contenu":"cible.exemple.net","proxied":false,"ttl":1}`, ClasseChangement},
		{"dns_ecrire", `{"nom":"a.fr","type":"TXT","contenu":"v=spf1 -all","proxied":false}`, `{"nom":"a.fr","type":"TXT","contenu":"v=spf1 -all","ttl":1}`, ClasseChangement},
		{"dns_supprimer", `{"nom":"a.fr","type":"TXT"}`, `{"nom":"a.fr","type":"TXT"}`, ClasseDestructrice},
		{"ssl_regler", `{"nom":"a.fr","mode":"full"}`, `{"nom":"a.fr","mode":"full"}`, ClasseChangement},
		{"ssl_regler", `{"nom":"a.fr","mode":"off"}`, `{"nom":"a.fr","mode":"off"}`, ClasseDestructrice},
	} {
		classe, _, a, err := classerCloudflare(c.outil, []byte(c.args))
		b, _ := json.Marshal(a)
		if err != nil || classe != c.classe || string(b) != c.canon {
			t.Errorf("%s %s: %v %v %s", c.outil, c.args, err, classe, b)
		}
	}
	for _, c := range [][2]string{{"dns_ecrire", `{"nom":"a.fr","type":"A","contenu":"2001:db8::1"}`},
		{"dns_ecrire", `{"nom":"a.fr","type":"AAAA","contenu":"::ffff:1.2.3.4"}`}, {"dns_ecrire", `{"nom":"a.fr","type":"MX","contenu":"x"}`},
		{"dns_ecrire", `{"nom":"a.fr","type":"CNAME","contenu":"pas un nom"}`}, {"dns_ecrire", `{"nom":"a.fr","type":"TXT","contenu":"a\nb"}`},
		{"dns_ecrire", `{"nom":"a.fr","type":"TXT","contenu":"x","proxied":true}`}, {"dns_ecrire", `{"nom":"a.fr","type":"A","contenu":"1.2.3.4","ttl":5}`},
		{"dns_ecrire", `{"nom":"a b","type":"A","contenu":"1.2.3.4"}`}, {"ssl_lire", `{}`}, {"ssl_regler", `{"nom":"a.fr","mode":"total"}`},
		{"dns_lister", `{"zone":"a.fr"}`}} {
		if _, _, _, err := classerCloudflare(c[0], []byte(c[1])); err == nil {
			t.Errorf("%s %s accepted", c[0], c[1])
		}
	}
}

// fauxCFDNS: a fake Cloudflare API holding the DNS records and SSL mode of zone z1 (exemple.fr); it records every
// call received as "METHOD path?query body" and refuses any other token.
type fauxCFDNS struct {
	mu     sync.Mutex
	appels []string
	recs   []enregistrementDNS
	ssl    string
	n      int
}

func lancerFauxCFDNS(t *testing.T, jeton string) *fauxCFDNS {
	t.Helper()
	f := &fauxCFDNS{ssl: "full", recs: []enregistrementDNS{{ID: "r1", Type: "A", Name: "app.exemple.fr", Content: "203.0.113.5", TTL: 1},
		{ID: "r2", Type: "TXT", Name: "app.exemple.fr", Content: "v=1", TTL: 1}, {ID: "r9", Type: "A", Name: "mail.exemple.fr", Content: "203.0.113.9", TTL: 1}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corps, _ := io.ReadAll(r.Body)
		appel := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/client/v4")
		if r.URL.RawQuery != "" {
			appel += "?" + r.URL.RawQuery
		}
		if len(corps) > 0 {
			appel += " " + string(corps)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.appels = append(f.appels, appel)
		repondre := func(code int, v any) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": code == 200, "errors": []any{}, "result": v})
		}
		if r.Header.Get("Authorization") != "Bearer "+jeton {
			repondre(401, nil)
			return
		}
		var rec enregistrementDNS
		_ = json.Unmarshal(corps, &rec)
		chemin := strings.TrimPrefix(r.URL.Path, "/client/v4/zones/z1")
		id := strings.TrimPrefix(chemin, "/dns_records/")
		i := slices.IndexFunc(f.recs, func(x enregistrementDNS) bool { return x.ID == id })
		switch {
		case r.Method == "GET" && chemin == "/dns_records":
			q := r.URL.Query()
			l := []enregistrementDNS{}
			for _, x := range f.recs {
				if x.Name == q.Get("name") && (q.Get("type") == "" || x.Type == q.Get("type")) {
					l = append(l, x)
				}
			}
			repondre(200, l)
		case r.Method == "POST" && chemin == "/dns_records":
			f.n++
			rec.ID = "n" + string(rune('0'+f.n))
			f.recs = append(f.recs, rec)
			repondre(200, rec)
		case r.Method == "PUT" && i >= 0:
			rec.ID = id
			f.recs[i] = rec
			repondre(200, rec)
		case r.Method == "DELETE" && i >= 0:
			f.recs = slices.Delete(f.recs, i, i+1)
			repondre(200, map[string]string{"id": id})
		case chemin == "/settings/ssl" && r.Method == "GET":
			repondre(200, map[string]string{"id": "ssl", "value": f.ssl})
		case chemin == "/settings/ssl" && r.Method == "PATCH":
			var v struct{ Value string }
			_ = json.Unmarshal(corps, &v)
			f.ssl = v.Value
			repondre(200, map[string]string{"id": "ssl", "value": f.ssl})
		default:
			repondre(404, nil)
		}
	}))
	t.Cleanup(srv.Close)
	ancien := urlCloudflare
	urlCloudflare = srv.URL
	t.Cleanup(func() { urlCloudflare = ancien })
	return f
}

func (f *fauxCFDNS) relever() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.appels
	f.appels = nil
	return a
}

// TestOutilsCloudflare (M116): a Servers task launched for an environment, played by a scripted fake Claude
// through the hub's MCP route against a fake Cloudflare: its token carries the environment's host name only;
// reads run, any other name or zone gets the unknown name's answer, a change without an accepted plan is
// refused, the accepted steps run once each; the exact calls Cloudflare received (method, path, body); a prod
// environment's plan always asks the owner; the Cloudflare token in nothing the agent sees, nor the audit log.
func TestOutilsCloudflare(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, _ []byte) (string, int) { return "ok", 0 })
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	_, appelSansEnv, _ := serveursSessionTest(t, s, srv.ID) // project demo, a token of a task without environment
	const jetonCF = "cf-jeton-TEMOIN-7f3a9c"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, jetonCF)
	if err != nil {
		t.Fatal(err)
	}
	f := Fournisseur{ID: "f1", Type: "cloudflare", Nom: "Mon Cloudflare", Cle: cle.ID, Etat: "ok", Droits: []string{"dns:lire", "dns:écrire", "ssl"},
		Zones: []ZoneFournisseur{{ID: "z1", Nom: "exemple.fr", Droits: []string{"dns:lire", "dns:écrire", "ssl"}}, {ID: "z2", Nom: "autre.org", Droits: []string{"dns:lire"}}}}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) { return append(l, f), nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.DefinirEnvironnement("demo", "dev", Environnement{URL: "https://App.exemple.fr/", Serveurs: []string{srv.ID},
		Protection: "aucune", Politique: "auto", Fournisseur: "f1"}); err != nil {
		t.Fatal(err)
	}
	cf := lancerFauxCFDNS(t, jetonCF)
	mux := http.NewServeMux()
	s.routes(mux)
	vu := &strings.Builder{} // everything the agent received
	appel := func(jeton, nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args))
		if rep.Error != nil {
			vu.WriteString(rep.Error.Message)
			return rep.Error.Message, true
		}
		vu.WriteString(rep.Result.Content[0].Text)
		return rep.Result.Content[0].Text, rep.Result.IsError
	}

	// a task without environment: no host name is its own
	if txt, e := appelSansEnv("dns_lister", `{"nom":"app.exemple.fr"}`); !e || txt != errNomInconnu.Error() {
		t.Errorf("without environment: %v %s", e, txt)
	}
	// an environment the server is not in
	if _, err := s.lancerTache("x", []string{srv.ID}, false, "demo/prod"); err == nil {
		t.Error("task for an environment without this server accepted")
	}

	ecrire := `{"nom":"app.exemple.fr","type":"A","contenu":"198.51.100.7","proxied":true}`
	ssl := `{"nom":"app.exemple.fr","mode":"strict"}`
	suppr := `{"nom":"app.exemple.fr","type":"TXT","contenu":"v=1"}`
	n := 0
	ancien := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancien })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		lire := func(nom string) string {
			b, _ := os.ReadFile(filepath.Join(dossier, nom))
			return strings.TrimSpace(string(b))
		}
		consigne, jeton := lire("consigne.txt"), lire("hub-jeton")
		id, res := "sess-"+string(rune('0'+n)), "ok"
		switch {
		case strings.Contains(consigne, "security critic"):
			id, res = "sess-critique", "VERDICT: ok"
		case strings.Contains(consigne, "external verifier"):
			id, res = "sess-verif", "VERIFICATION: ok"
		case n == 0:
			n++
			if !strings.Contains(consigne, "environment demo/dev (host names: app.exemple.fr)") {
				t.Errorf("chef ops instructions: %q", consigne)
			}
			if txt, e := appel(jeton, "fournisseurs", `{}`); e || !strings.Contains(txt, `"exemple.fr"`) || !strings.Contains(txt, "dns:écrire") || strings.Contains(txt, "autre.org") || strings.Contains(txt, cle.ID) {
				t.Errorf("fournisseurs: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "dns_lister", `{}`); e || !strings.Contains(txt, "203.0.113.5") || strings.Contains(txt, "mail.exemple.fr") {
				t.Errorf("dns_lister: %v %s", e, txt)
			}
			for _, c := range [][2]string{{"dns_lister", `{"nom":"mail.exemple.fr"}`}, {"dns_lister", `{"nom":"autre.org"}`},
				{"ssl_lire", `{"nom":"exemple.fr"}`}, {"dns_ecrire", `{"nom":"inconnu.net","type":"A","contenu":"1.2.3.4"}`}} {
				if txt, e := appel(jeton, c[0], c[1]); !e || txt != errNomInconnu.Error() {
					t.Errorf("%s %s outside the perimeter: %v %s", c[0], c[1], e, txt)
				}
			}
			if txt, e := appel(jeton, "dns_ecrire", ecrire); !e || !strings.Contains(txt, "plan required") {
				t.Errorf("change without a plan: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "proposer_plan", `{"serveur":"ab12","resume":"r","etapes":[{"outil":"dns_ecrire","arguments":{"nom":"autre.org","type":"A","contenu":"1.2.3.4"},"pourquoi":"p"}]}`); !e || !strings.Contains(txt, errNomInconnu.Error()) {
				t.Errorf("plan outside the perimeter: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "proposer_plan", `{"serveur":"ab12","resume":"Pointer app vers le nouveau serveur","etapes":[`+
				`{"outil":"dns_ecrire","arguments":`+ecrire+`,"pourquoi":"nouvelle adresse"},`+
				`{"outil":"ssl_regler","arguments":`+ssl+`,"pourquoi":"certificat vérifié"},`+
				`{"outil":"dns_supprimer","arguments":`+suppr+`,"pourquoi":"ancienne vérification","sans_sauvegarde":"le résultat liste l'enregistrement retiré"}]}`); e {
				t.Errorf("proposer_plan: %s", txt)
			}
			if got := cf.relever(); !slices.Equal(got, []string{"GET /zones/z1/dns_records?name=app.exemple.fr&per_page=100"}) {
				t.Errorf("Cloudflare calls of the reads: %q", got)
			}
			return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(`{"type":"system","subtype":"init","session_id":"sess-1"}`+"\n"+`{"type":"result","is_error":false,"result":"Plan proposé."}`+"\n"), 0o600)
		default: // the resumed session: the accepted steps, once each
			n++
			for _, c := range [][2]string{{"dns_ecrire", ecrire}, {"ssl_regler", ssl}, {"dns_supprimer", suppr}} {
				if txt, e := appel(jeton, c[0], c[1]); e {
					t.Errorf("accepted step %s refused: %s", c[0], txt)
				}
			}
			if txt, e := appel(jeton, "dns_ecrire", ecrire); !e || !strings.Contains(txt, "already ran") {
				t.Errorf("a step ran twice: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "dns_ecrire", strings.Replace(ecrire, "198.51.100.7", "198.51.100.8", 1)); !e || !strings.Contains(txt, "plan required") {
				t.Errorf("a step other than accepted ran: %v %s", e, txt)
			}
		}
		j := `{"type":"system","subtype":"init","session_id":"` + id + `"}` + "\n" + `{"type":"result","is_error":false,"result":"` + res + `"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	// launched from the server's page for its environment
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/serveurs/ab12/taches", strings.NewReader(`{"texte":"Pointe app vers 198.51.100.7","env":"demo/dev"}`))
	r.SetPathValue("id", srv.ID)
	s.apiLancerTacheServeur(w, r)
	var lance struct{ Tache TacheServeurs }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &lance) != nil || lance.Tache.Env != "demo/dev" {
		t.Fatalf("launch: %d %s", w.Code, w.Body)
	}
	attendre := func(etats ...string) TacheServeurs {
		t.Helper()
		for i := 0; i < 100; i++ {
			for _, x := range tachesServeur(srv.ID) {
				if x.ID == lance.Tache.ID && slices.Contains(etats, x.Etat) {
					return x
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("task never reached %v: %+v", etats, tachesServeur(srv.ID))
		return TacheServeurs{}
	}
	att := attendre("attente_accord", "finie", "echec")
	if att.Etat != "attente_accord" || len(att.Plans) != 1 {
		t.Fatalf("waiting task: %+v", att)
	}
	if _, _, err := s.deciderApprobation("fenetre", att.Plans[0], "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if fin := attendre("finie", "echec", "arretee"); fin.Etat != "finie" {
		t.Fatalf("task: %+v", fin)
	}
	attendu := []string{
		"GET /zones/z1/dns_records?name=app.exemple.fr&per_page=100&type=A",
		`PUT /zones/z1/dns_records/r1 {"content":"198.51.100.7","name":"app.exemple.fr","proxied":true,"ttl":1,"type":"A"}`,
		`PATCH /zones/z1/settings/ssl {"value":"strict"}`,
		"GET /zones/z1/dns_records?name=app.exemple.fr&per_page=100&type=TXT",
		"DELETE /zones/z1/dns_records/r2",
	}
	if got := cf.relever(); !slices.Equal(got, attendu) {
		t.Errorf("Cloudflare calls of the plan:\n got  %q\n want %q", got, attendu)
	}
	if p, err := s.lirePlan(att.Plans[0]); err != nil || slices.ContainsFunc(p.Etapes, func(e EtapePlan) bool { return e.Etat != "ok" }) {
		t.Errorf("plan steps: %v %+v", err, p.Etapes)
	}

	// a prod environment's task: its plan asks the owner, never by a rule
	jp, err := s.jetons.CreerSession(SessionHub{Projet: "demo", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{srv.ID}, Env: "demo/prod", Hotes: []string{"app.exemple.fr"}})
	if err != nil {
		t.Fatal(err)
	}
	if txt, e := appel(jp, "proposer_plan", `{"serveur":"ab12","resume":"r","etapes":[{"outil":"dns_ecrire","arguments":`+ecrire+`,"pourquoi":"p"}]}`); e {
		t.Fatalf("prod plan: %s", txt)
	}
	l, _ := s.approbations.Liste()
	if i := slices.IndexFunc(l, func(a Approbation) bool { return a.Projet == "demo" && a.Action == actionPlanServeur }); i < 0 || l[i].Env != "prod" || l[i].Details["regle"] != "non" || l[i].Etat != "attente" {
		t.Errorf("prod plan's approval: %+v", l)
	}

	// C56: another project's prod lives in the same zone; a dev task holding "do not ask again" for its server
	// still cannot set the zone's SSL without the owner, and the card names the zone and that prod
	if err := s.acces.DefinirEnvironnement("boutique", "prod", Environnement{URL: "https://www.exemple.fr", Protection: "aucune", Politique: "accord"}); err != nil {
		t.Fatal(err)
	}
	jd, err := s.jetons.CreerSession(SessionHub{Projet: "demo", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{srv.ID}, Env: "demo/dev", Hotes: []string{"app.exemple.fr"}})
	if err != nil {
		t.Fatal(err)
	}
	planDev := func(etape string) Approbation {
		t.Helper()
		txt, e := appel(jd, "proposer_plan", `{"serveur":"ab12","resume":"r","etapes":[`+etape+`]}`)
		var r struct{ Plan string }
		if e || json.Unmarshal([]byte(txt), &r) != nil {
			t.Fatalf("dev plan: %s", txt)
		}
		a, _ := s.approbations.Lire(r.Plan)
		return a
	}
	premier := planDev(`{"outil":"dns_ecrire","arguments":` + ecrire + `,"pourquoi":"p"}`)
	if premier.Env != "dev" || premier.Details["regle"] != "oui" {
		t.Fatalf("dev plan: %+v", premier)
	}
	if premier.Etat == "attente" {
		if _, err := s.approbations.Decider(premier.ID, "toujours", "owner"); err != nil {
			t.Fatal(err)
		}
	}
	cf.relever()
	zone := planDev(`{"outil":"ssl_regler","arguments":{"nom":"app.exemple.fr","mode":"strict"},"pourquoi":"p"}`)
	if zone.Env != "prod" || zone.Details["regle"] != "non" || zone.Etat != "attente" {
		t.Errorf("zone-wide SSL from a dev task: %+v", zone)
	}
	if p, err := s.lirePlan(zone.ID); err == nil && (len(p.Etapes) != 1 || !strings.Contains(p.Etapes[0].Raison, "the whole zone exemple.fr") ||
		!strings.Contains(p.Etapes[0].Raison, "boutique/prod (www.exemple.fr)")) {
		t.Errorf("the card does not name the zone and the prod: %+v", p.Etapes)
	}
	if l := cf.relever(); len(l) != 0 {
		t.Errorf("Cloudflare called before the owner's approval: %q", l)
	}
	// a record of the dev name stays a dev step, let through by the rule
	if d := planDev(`{"outil":"dns_ecrire","arguments":` + ecrire + `,"pourquoi":"p"}`); d.Env != "dev" || d.Etat == "attente" {
		t.Errorf("dev record after the rule: %+v", d)
	}
	// a dev task still holding prod's name (recorded before C57): its step on that name is a prod step
	jv, err := s.jetons.CreerSession(SessionHub{Projet: "demo", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{srv.ID}, Env: "demo/dev", Hotes: []string{"www.exemple.fr"}})
	if err != nil {
		t.Fatal(err)
	}
	txt, e := appel(jv, "proposer_plan", `{"serveur":"ab12","resume":"r","etapes":[{"outil":"dns_ecrire","arguments":{"nom":"www.exemple.fr","type":"A","contenu":"198.51.100.7","proxied":true},"pourquoi":"p"}]}`)
	var rv struct{ Plan string }
	if e || json.Unmarshal([]byte(txt), &rv) != nil {
		t.Fatalf("plan on prod's name: %s", txt)
	}
	if a, _ := s.approbations.Lire(rv.Plan); a.Env != "prod" || a.Details["regle"] != "non" || a.Etat != "attente" {
		t.Errorf("a step on prod's name from a dev task: %+v", a)
	}
	if l := cf.relever(); len(l) != 0 {
		t.Errorf("Cloudflare called before the owner's approval: %q", l)
	}
	// C57: no environment, in this project or another, takes a host name another one has
	for _, c := range [][2]string{{"demo", "staging"}, {"autre", "dev"}} {
		err := s.acces.DefinirEnvironnement(c[0], c[1], Environnement{URL: "https://WWW.exemple.fr/x", Protection: "aucune", Politique: "accord"})
		var ea erreurEntreeArgs
		if !errors.As(err, &ea) || ea.cle != errEnvHotePris || ea.args[1] != "boutique/prod" {
			t.Errorf("%s/%s with prod's host: %v", c[0], c[1], err)
		}
	}
	if err := s.acces.DefinirEnvironnement("boutique", "prod", Environnement{URL: "https://www.exemple.fr/v2", Protection: "aucune", Politique: "accord"}); err != nil {
		t.Errorf("an environment keeps its own host: %v", err)
	}

	// the Cloudflare token: in nothing the agent saw, nor in the audit log (where every call is)
	if strings.Contains(vu.String(), jetonCF) {
		t.Error("the Cloudflare token reached the agent")
	}
	audit := &strings.Builder{}
	_ = filepath.Walk(dossierJournalAudit(), func(p string, i os.FileInfo, _ error) error {
		if i != nil && !i.IsDir() {
			b, _ := os.ReadFile(p)
			audit.Write(b)
		}
		return nil
	})
	if strings.Contains(audit.String(), jetonCF) || !strings.Contains(audit.String(), "cloudflare PATCH /zones/z1/settings/ssl") ||
		!strings.Contains(audit.String(), "nom hors du périmètre") {
		t.Errorf("audit log: token present, or a call or a refusal missing")
	}
}

// TestCloudflareGardes (M125): on an Access environment dns_ecrire always writes through the proxy (C58); it
// never picks one of several A records (C67); dns_lister of an environment without URL says so (C66).
func TestCloudflareGardes(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	const jetonCF = "cf-jeton-gardes"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, jetonCF)
	if err != nil {
		t.Fatal(err)
	}
	f := Fournisseur{ID: "f1", Type: "cloudflare", Nom: "CF", Cle: cle.ID, Etat: "ok", Zones: []ZoneFournisseur{{ID: "z1", Nom: "exemple.fr"}}}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) { return append(l, f), nil }); err != nil {
		t.Fatal(err)
	}
	for env, e := range map[string]Environnement{"dev": {URL: "https://app.exemple.fr", Protection: "access", Politique: "auto", Fournisseur: "f1"},
		"staging": {Protection: "aucune", Politique: "auto", Fournisseur: "f1"}} {
		if err := s.acces.DefinirEnvironnement("demo", env, e); err != nil {
			t.Fatal(err)
		}
	}
	cf := lancerFauxCFDNS(t, jetonCF)
	dev := SessionHub{Projet: "demo", Env: "demo/dev", Hotes: []string{"app.exemple.fr"}}
	ecrire := func(x SessionHub, args string) (any, error) {
		t.Helper()
		_, _, a, err := classerCloudflare("dns_ecrire", []byte(args))
		if err != nil {
			t.Fatal(err)
		}
		return s.executerCloudflare(x, "dns_ecrire", a)
	}
	r, err := ecrire(dev, `{"nom":"app.exemple.fr","type":"A","contenu":"198.51.100.7","proxied":false}`)
	if err != nil || !strings.Contains(enJSON(r), "forced to true") {
		t.Errorf("forced proxy: %v %v", err, r)
	}
	attendu := []string{"GET /zones/z1/dns_records?name=app.exemple.fr&per_page=100&type=A",
		`PUT /zones/z1/dns_records/r1 {"content":"198.51.100.7","name":"app.exemple.fr","proxied":true,"ttl":1,"type":"A"}`}
	if got := cf.relever(); !slices.Equal(got, attendu) {
		t.Errorf("calls:\n got  %q\n want %q", got, attendu)
	}
	// two A records: which one to replace is not guessed, nothing written
	cf.mu.Lock()
	cf.recs = append(cf.recs, enregistrementDNS{ID: "r4", Type: "A", Name: "app.exemple.fr", Content: "198.51.100.7", Proxied: true, TTL: 1})
	cf.mu.Unlock()
	if _, err := ecrire(dev, `{"nom":"app.exemple.fr","type":"A","contenu":"198.51.100.7","proxied":true}`); err == nil || !strings.Contains(err.Error(), "2 A records hold app.exemple.fr") {
		t.Errorf("several A records: %v", err)
	}
	if got := cf.relever(); len(got) != 1 || !strings.HasPrefix(got[0], "GET ") {
		t.Errorf("a write despite several records: %q", got)
	}
	// an environment without URL: no host name, no panic
	sansURL := SessionHub{Projet: "demo", Env: "demo/staging"}
	if _, err := s.executerCloudflare(sansURL, "dns_lister", argsCloudflare{}); err == nil || !strings.Contains(err.Error(), "has no URL") {
		t.Errorf("dns_lister without URL: %v", err)
	}
}
