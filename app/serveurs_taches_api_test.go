package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestTacheAPISeule (M286): a task on vault API with no server, played by a scripted fake Claude through the hub's
// MCP route against a fake Cloudflare: its token has no SSH, SFTP nor file tool; it reads the zone of its API's
// sheet and the names inside it, another sheet's zone gets the unknown name's answer; the mode its launcher gave
// decides (auto: a change runs, a destructive call waits for a plan; manuel: a read too is a plan step); without
// a mode, Settings › Permissions' assistant mode; every call in the audit log, the token's value nowhere.
func TestTacheAPISeule(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	_ = os.RemoveAll(dossierJournalAudit())
	t.Cleanup(func() { _ = os.RemoveAll(dossierJournalAudit()) })
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	const jetonCF = "cf-jeton-m286-4b1d9e"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, jetonCF)
	if err != nil {
		t.Fatal(err)
	}
	autre, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare bis", Famille: "secret", Type: "cloudflare", Portee: "infra"}, "cf-jeton-m286-bis")
	if err != nil {
		t.Fatal(err)
	}
	droits := []string{"dns:lire", "dns:écrire", "ssl"}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		return append(l, Fournisseur{ID: "f1", Type: "cloudflare", Nom: "Mon Cloudflare", Cle: cle.ID, Etat: "ok", Droits: droits, Zones: []ZoneFournisseur{{ID: "z1", Nom: "exemple.fr", Droits: droits}}},
			Fournisseur{ID: "f2", Type: "cloudflare", Nom: "Un autre", Cle: autre.ID, Etat: "ok", Droits: droits, Zones: []ZoneFournisseur{{ID: "z2", Nom: "autre.org", Droits: droits}}}), nil
	}); err != nil {
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
	ecrire := `{"nom":"app.exemple.fr","type":"A","contenu":"198.51.100.7","proxied":true}`
	suppr := `{"nom":"app.exemple.fr","type":"TXT","contenu":"v=1"}`
	verificateur := false
	scenes := map[string]func(consigne, jeton string){
		"auto": func(consigne, jeton string) {
			if !strings.Contains(consigne, "Cloudflare zones: exemple.fr. You have no server") || strings.Contains(consigne, "autre.org") {
				t.Errorf("instructions: %q", consigne)
			}
			if txt, e := appel(jeton, "fournisseurs", `{}`); e || !strings.Contains(txt, `"exemple.fr"`) || !strings.Contains(txt, "dns:écrire") || strings.Contains(txt, "autre.org") || strings.Contains(txt, cle.ID) {
				t.Errorf("fournisseurs: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "dns_lister", `{"nom":"app.exemple.fr"}`); e || !strings.Contains(txt, "203.0.113.5") {
				t.Errorf("dns_lister of a name inside the zone: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "dns_lister", `{}`); e {
				t.Errorf("dns_lister of the zone: %s", txt)
			}
			if got, want := cf.relever(), []string{"GET /zones/z1/dns_records?name=app.exemple.fr&per_page=100", "GET /zones/z1/dns_records?per_page=100"}; !slices.Equal(got, want) {
				t.Errorf("Cloudflare calls of the reads: %q", got)
			}
			for _, c := range [][2]string{{"dns_lister", `{"nom":"autre.org"}`}, {"dns_lister", `{"nom":"www.autre.org"}`}, {"ssl_lire", `{"nom":"malexemple.fr"}`},
				{"dns_ecrire", `{"nom":"inconnu.net","type":"A","contenu":"1.2.3.4"}`}} {
				if txt, e := appel(jeton, c[0], c[1]); !e || txt != errNomInconnu.Error() {
					t.Errorf("%s %s outside the zones: %v %s", c[0], c[1], e, txt)
				}
			}
			for _, c := range [][2]string{{"ssh_commande", `{"serveur":"api","commande":"id"}`}, {"serveurs_liste", `{}`}, {"fichier_lire", `{"serveur":"api","chemin":"/etc/hostname"}`},
				{"sftp_envoyer", `{"serveur":"api","local":"a","distant":"/tmp/a"}`}, {"certificat_origine", `{"nom":"app.exemple.fr"}`}} {
				if txt, e := appel(jeton, c[0], c[1]); !e {
					t.Errorf("%s answered a task without server: %s", c[0], txt)
				}
			}
			if txt, e := appel(jeton, "dns_ecrire", ecrire); e { // auto: a change runs at once
				t.Errorf("auto, change: %s", txt)
			}
			if txt, e := appel(jeton, "dns_supprimer", suppr); !e || !strings.Contains(txt, `plan required`) || !strings.Contains(txt, `serveur = "api"`) {
				t.Errorf("auto, destructive call without a plan: %v %s", e, txt)
			}
			if got := cf.relever(); len(got) != 2 || !strings.HasPrefix(got[1], "PUT /zones/z1/dns_records/r1 ") {
				t.Errorf("Cloudflare calls of the change: %q", got)
			}
		},
		"manuel": func(consigne, jeton string) {
			if strings.Contains(consigne, "ACCEPTED") { // the resumed session: the accepted step, without RUNBOOK nor firewall
				if strings.Contains(consigne, "runbook") {
					t.Errorf("application instructions: %q", consigne)
				}
				if txt, e := appel(jeton, "dns_supprimer", suppr); e {
					t.Errorf("accepted step refused: %s", txt)
				}
				return
			}
			if txt, e := appel(jeton, "dns_lister", `{"nom":"exemple.fr"}`); !e || !strings.Contains(txt, "plan required") {
				t.Errorf("manuel, read without a plan: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "proposer_plan", `{"serveur":"api","resume":"r","etapes":[{"outil":"ssh_commande","arguments":{"serveur":"api","commande":"id"},"pourquoi":"p"}]}`); !e || !strings.Contains(txt, "unknown tool") {
				t.Errorf("plan with a server tool: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "proposer_plan", `{"serveur":"api","resume":"r","etapes":[{"outil":"dns_supprimer","arguments":{"nom":"autre.org","type":"TXT","contenu":"v=1"},"pourquoi":"p","sans_sauvegarde":"s"}]}`); !e || !strings.Contains(txt, errNomInconnu.Error()) {
				t.Errorf("plan outside the zones: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "proposer_plan", `{"serveur":"api","resume":"Retirer l'ancienne vérification","etapes":[{"outil":"dns_supprimer","arguments":`+suppr+`,"pourquoi":"p","sans_sauvegarde":"le résultat liste l'enregistrement retiré"}]}`); e || !strings.Contains(txt, `"etat": "attente"`) {
				t.Errorf("proposer_plan: %v %s", e, txt)
			}
		},
	}
	ancien := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancien })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		lire := func(nom string) string {
			b, _ := os.ReadFile(filepath.Join(dossier, nom))
			return strings.TrimSpace(string(b))
		}
		consigne, jeton, res := lire("consigne.txt"), lire("hub-jeton"), "ok"
		switch {
		case strings.Contains(consigne, "security critic"):
			res = "VERDICT: ok"
		case strings.Contains(consigne, "external verifier"):
			verificateur, res = true, "VERIFICATION: ok"
		default:
			for _, x := range tachesServeur(serveurAPI) {
				if scene := scenes[x.Mode]; scene != nil && x.Fin == "" && x.Texte != "sans scène" {
					scene(consigne, jeton)
				}
			}
		}
		j := `{"type":"system","subtype":"init","session_id":"sess-m286"}` + "\n" + `{"type":"result","is_error":false,"result":"` + res + `"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	attendre := func(id string, etats ...string) TacheServeurs {
		t.Helper()
		for i := 0; i < 100; i++ {
			for _, x := range tachesServeur(serveurAPI) {
				if x.ID == id && slices.Contains(etats, x.Etat) {
					return x
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("task never reached %v: %+v", etats, tachesServeur(serveurAPI))
		return TacheServeurs{}
	}

	// the launch route, with the API and the mode of the caller
	lancer := func(corps string) (int, TacheServeurs, string) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/serveurs/api/taches", strings.NewReader(corps))
		r.SetPathValue("id", serveurAPI)
		s.apiLancerTacheServeur(w, r)
		var rep struct{ Tache TacheServeurs }
		_ = json.Unmarshal(w.Body.Bytes(), &rep)
		return w.Code, rep.Tache, w.Body.String()
	}
	code, x, corps := lancer(`{"texte":"Pointe app vers 198.51.100.7","apis":["` + cle.ID + `"],"mode":"auto"}`)
	if code != 200 || len(x.Serveurs) != 0 || x.Mode != "auto" || !slices.Equal(x.APIs, []string{cle.ID}) || !strings.Contains(corps, `"serveurs":[]`) {
		t.Fatalf("launch: %d %s", code, corps)
	}
	if fin := attendre(x.ID, "finie", "echec", "arretee"); fin.Etat != "finie" {
		t.Fatalf("auto task: %+v", fin)
	}
	if code, _, corps := lancer(`{"texte":"x","apis":["` + cle.ID + `"],"mode":"ignorer"}`); code != http.StatusConflict {
		t.Errorf("ignorer without its confirmation: %d %s", code, corps)
	}
	if code, _, corps := lancer(`{"texte":"x","apis":["` + cle.ID + `"],"mode":"nimporte"}`); code != http.StatusBadRequest {
		t.Errorf("unknown mode: %d %s", code, corps)
	}

	// no mode given: manuel while Settings › Permissions says nothing, then the assistant's mode chosen there
	x, err = s.lancerTacheDans("Retire l'ancienne vérification", nil, false, "", "", false, "", []string{cle.ID}, "")
	if err != nil || x.Mode != "manuel" {
		t.Fatalf("task without a mode: %v %+v", err, x)
	}
	att := attendre(x.ID, "attente_accord", "finie", "echec")
	if att.Etat != "attente_accord" || len(att.Plans) != 1 {
		t.Fatalf("waiting task: %+v", att)
	}
	if ap, _ := s.approbations.Lire(att.Plans[0]); ap.Details["serveur"] != serveurAPI || ap.Details["nom"] != "exemple.fr" || ap.Details["regle"] != "non" || ap.Env != "dev" {
		t.Errorf("the plan's approval: %+v", ap)
	}
	if _, _, err := s.deciderApprobation("fenetre", att.Plans[0], "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if fin := attendre(x.ID, "finie", "echec", "arretee"); fin.Etat != "finie" || fin.Verification != nil || verificateur {
		t.Fatalf("manuel task (no external verifier without a server): %+v", fin)
	}
	if got := cf.relever(); len(got) != 2 || got[1] != "DELETE /zones/z1/dns_records/r2" {
		t.Errorf("Cloudflare calls of the plan: %q", got)
	}
	s.config.PermissionsUsage = map[string]string{"assistant": "modifications"}
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	if x, err = s.lancerTacheDans("sans scène", nil, false, "", "", false, "", []string{cle.ID}, ""); err != nil || x.Mode != "modifications" {
		t.Fatalf("task with the settings' mode: %v %+v", err, x)
	}
	attendre(x.ID, "finie", "echec", "arretee")

	// the token's value: in nothing the agent saw, in no task file, nor in the audit log (where every call is)
	traces := &strings.Builder{}
	for _, d := range []string{dossierJournalAudit(), dossierTaches(serveurAPI)} {
		_ = filepath.Walk(d, func(p string, i os.FileInfo, _ error) error {
			if i != nil && !i.IsDir() {
				b, _ := os.ReadFile(p)
				traces.Write(b)
			}
			return nil
		})
	}
	if strings.Contains(vu.String(), jetonCF) || strings.Contains(traces.String(), jetonCF) || strings.Contains(corps, jetonCF) {
		t.Error("the Cloudflare token reached the agent, a task file, an answer or the audit log")
	}
	for _, attendu := range []string{"API seule", "(mode auto)", "accordé : mode « auto » de la tâche (changement)", "nom hors du périmètre (www.autre.org)",
		"cloudflare GET /zones/z1/dns_records", "cloudflare DELETE /zones/z1/dns_records/r2", "serveurs.dns_supprimer"} {
		if !strings.Contains(traces.String(), attendu) {
			t.Errorf("audit log: %q missing", attendu)
		}
	}
}

// TestTacheSansRessource (M286): neither a server nor an API, an API the vault does not hold, an API with an
// environment: refused, and nothing is created.
func TestTacheSansRessource(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/serveurs/api/taches", strings.NewReader(`{"texte":"x"}`))
	r.SetPathValue("id", serveurAPI)
	s.apiLancerTacheServeur(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "A task needs at least one server or one API") {
		t.Errorf("neither server nor API: %d %s", w.Code, w.Body)
	}
	if dico, _ := os.ReadFile("static/i18n.js"); strings.Count(string(dico), `"A task needs at least one server or one API": "`) != 5 {
		t.Error("the refusal has no translation")
	}
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, "cf-jeton-m286-sans")
	if err != nil {
		t.Fatal(err)
	}
	for nom, f := range map[string]func() error{
		"unknown API": func() error {
			_, err := s.lancerTacheDans("x", nil, false, "", "", false, "", []string{"inconnue"}, "")
			return err
		},
		"API and environment": func() error {
			_, err := s.lancerTacheDans("x", nil, false, "demo/dev", "", false, "", []string{cle.ID}, "")
			return err
		},
		"unknown mode": func() error {
			_, err := s.lancerTacheDans("x", nil, false, "", "", false, "", []string{cle.ID}, "tout")
			return err
		},
		"terminal, no server": func() error {
			_, err := s.lancerTacheDans("x", nil, false, "", "t1", false, "", []string{cle.ID}, "")
			return err
		},
		"no server, no API": func() error { _, err := s.lancerTacheDans("x", nil, false, "", "", false, "", nil, "auto"); return err },
		"empty list, no API": func() error {
			_, err := s.lancerTacheDans("x", []string{}, false, "", "", false, "", []string{}, "")
			return err
		},
		"unknown server + API": func() error {
			_, err := s.lancerTacheDans("x", []string{"zz"}, false, "", "", false, "", []string{cle.ID}, "")
			return err
		},
	} {
		if err := f(); err == nil {
			t.Errorf("%s: accepted", nom)
		}
	}
	if l := tachesServeur(serveurAPI); len(l) != 0 || len(s.jetons.Liste()) != 0 {
		t.Errorf("a refused task left something: %+v, %d sessions", l, len(s.jetons.Liste()))
	}
}

// TestTacheAPISeuleConteneur (M286, the short trial on the test container): a task on an API alone, launched by the
// route, reads its zone on the fake Cloudflare and cannot reach the inventory's server; then a task on that server,
// by the same route without apis nor mode, runs its command there as before and has no Cloudflare zone.
func TestTacheAPISeuleConteneur(t *testing.T) {
	s, appelAPI, id, _, _ := serveurConteneurTest(t)
	const jetonCF = "cf-jeton-m286-essai"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, jetonCF)
	if err != nil {
		t.Fatal(err)
	}
	if s.approbations == nil {
		if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	droits := []string{"dns:lire"}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		return append(l, Fournisseur{ID: "f1", Type: "cloudflare", Nom: "Mon Cloudflare", Cle: cle.ID, Etat: "ok", Droits: droits, Zones: []ZoneFournisseur{{ID: "z1", Nom: "exemple.fr", Droits: droits}}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	lancerFauxCFDNS(t, jetonCF)
	mux := http.NewServeMux()
	s.routes(mux)
	ancienne := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancienne })
	var mu sync.Mutex
	sorties := map[string]string{}
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, d string) error {
		lire := func(f string) string {
			b, _ := os.ReadFile(filepath.Join(d, f))
			return strings.TrimSpace(string(b))
		}
		if c := lire("consigne.txt"); !strings.Contains(c, "security critic") && !strings.Contains(c, "external verifier") {
			qui := map[bool]string{true: "api ", false: "serveur "}[strings.Contains(c, "You have no server")]
			for _, a := range [][2]string{{"dns_lister", `{"nom":"app.exemple.fr"}`}, {"ssh_commande", `{"serveur":"` + id + `","commande":"id -un"}`}} {
				rep := appelMCP(t, mux, "Bearer "+lire("hub-jeton"), appelOutil(a[0], a[1]))
				mu.Lock()
				if rep.Error != nil {
					sorties[qui+a[0]] = "ERROR " + rep.Error.Message
				} else {
					sorties[qui+a[0]] = map[bool]string{true: "ERROR "}[rep.Result.IsError] + rep.Result.Content[0].Text
				}
				mu.Unlock()
			}
		}
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"Done."}` + "\n"
		return os.WriteFile(filepath.Join(d, "session.jsonl"), []byte(j), 0o600)
	}
	for _, c := range []struct {
		chemin string
		corps  map[string]any
	}{{"/api/serveurs/" + serveurAPI + "/taches", map[string]any{"texte": "Lis le DNS d'app", "apis": []string{cle.ID}, "mode": "auto"}},
		{"/api/serveurs/" + id + "/taches", map[string]any{"texte": "Dis qui tu es"}}} {
		if code, v := appelAPI(c.chemin, c.corps); code != 200 {
			t.Fatalf("%s: %d %v", c.chemin, code, v)
		}
		for range 2400 { // the task ends
			tachesEnCours.Lock()
			n := len(tachesEnCours.m)
			tachesEnCours.Unlock()
			if n == 0 {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if r := sorties["api dns_lister"]; !strings.Contains(r, "203.0.113.5") || strings.HasPrefix(r, "ERROR ") {
		t.Errorf("API task, read of its zone: %q", r)
	}
	if r := sorties["api ssh_commande"]; !strings.HasPrefix(r, "ERROR ") {
		t.Errorf("API task, SSH on the inventory's server: %q", r)
	}
	if r := sorties["serveur ssh_commande"]; !strings.Contains(r, "athanor") || strings.HasPrefix(r, "ERROR ") {
		t.Errorf("server task, its command: %q", r)
	}
	if r := sorties["serveur dns_lister"]; !strings.HasPrefix(r, "ERROR ") {
		t.Errorf("server task without API, a Cloudflare read: %q", r)
	}
	for _, x := range slices.Concat(tachesServeur(serveurAPI), tachesServeur(id)) {
		if x.Etat != "finie" {
			t.Errorf("task %q: %s %s", x.Texte, x.Etat, x.Resultat)
		}
	}
	if strings.Contains(fmt.Sprint(sorties), jetonCF) {
		t.Error("the Cloudflare token reached an agent")
	}
}
