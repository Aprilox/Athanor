package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestVerifierHTTP (M98): what a visitor sees: status, a password asked (401), the security headers present and
// missing, a redirect not followed, TLS (version, expiry, validity against the roots), an address outside the
// servers refused before any connection.
func TestVerifierHTTP(t *testing.T) {
	gerer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/prive":
			w.Header().Set("WWW-Authenticate", `Basic realm="admin"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/vers":
			http.Redirect(w, r, "/ailleurs", http.StatusFound)
		default:
			w.Header().Set("Content-Security-Policy", "default-src 'self'")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Strict-Transport-Security", "max-age=63072000")
		}
	})
	ts := httptest.NewServer(gerer)
	defer ts.Close()
	ctx := context.Background()
	visite := func(base, chemin string, hotes ...string) ResultatHTTP {
		u, _ := url.Parse(base + chemin)
		return verifierHTTP(ctx, u, hotes)
	}
	if r := visite(ts.URL, "/prive", "127.0.0.1"); r.Statut != 401 || r.Authentification != "demandee" || !strings.Contains(r.WWWAuthenticate, "Basic") || r.Erreur != "" {
		t.Errorf("401: %+v", r)
	}
	r := visite(ts.URL, "/", "127.0.0.1")
	if r.Statut != 200 || r.Authentification != "aucune" || r.EnTetes["Content-Security-Policy"] == "" || !slices.Contains(r.Manquants, "X-Frame-Options") ||
		slices.Contains(r.Manquants, "Strict-Transport-Security") || r.TLS != nil {
		t.Errorf("plain http: %+v", r)
	}
	if r := visite(ts.URL, "/vers", "127.0.0.1"); r.Statut != 302 || r.Redirection != "/ailleurs" {
		t.Errorf("redirect: %+v", r)
	}
	if r := visite(ts.URL, "/", "192.0.2.1"); r.Statut != 0 || !strings.Contains(r.Erreur, "not the address of one of your servers") {
		t.Errorf("outside the servers: %+v", r)
	}

	tls := httptest.NewTLSServer(gerer)
	defer tls.Close()
	r = visite(tls.URL, "/", "127.0.0.1")
	if r.Statut != 200 || r.TLS == nil || r.TLS.Valide || r.TLS.Erreur == "" || r.TLS.Version == "" || r.TLS.JoursRestants <= 0 || r.EnTetes["Strict-Transport-Security"] == "" {
		t.Errorf("self-signed TLS: %+v %+v", r, r.TLS)
	}
	if exp, err := time.Parse(time.RFC3339, r.TLS.Expire); err != nil || !exp.Equal(tls.Certificate().NotAfter.UTC().Truncate(time.Second)) {
		t.Errorf("expiry %q, certificate's %v", r.TLS.Expire, tls.Certificate().NotAfter)
	}
	racines := x509.NewCertPool()
	racines.AddCert(tls.Certificate())
	racinesVerif = racines
	defer func() { racinesVerif = nil }()
	if r := visite(tls.URL, "/prive", "127.0.0.1"); r.TLS == nil || !r.TLS.Valide || r.TLS.Erreur != "" || r.Authentification != "demandee" {
		t.Errorf("trusted TLS: %+v %+v", r, r.TLS)
	}
}

// TestServeursCritiqueVerification (M98): in a task, the security critic reads the plan with read-only tools
// before the owner may accept it, its verdict goes on the plan; after the application the external verifier
// tests the site through verifier_http, its report goes to the task and the RUNBOOK; a failure turns the task to
// echec and rings the bell, a later passing task clears it.
func TestServeursCritiqueVerification(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, _ []byte) (string, int) { return "ok:" + cmd, 0 })
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin" {
			w.Header().Set("WWW-Authenticate", `Basic realm="admin"`)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer site.Close()

	appel := func(jeton, nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args))
		if rep.Error != nil {
			return rep.Error.Message, true
		}
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	etape := `{"outil":"ssh_commande","arguments":{"commande":"systemctl restart nginx","delai_s":30},"pourquoi":"recharger"}`
	verdictVerif := "VERIFICATION: echec\n- Le mot de passe n'est pas demandé sur /"
	var opsJeton string
	critiques, verifs := 0, 0
	ancien := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancien })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		lire := func(f string) string {
			b, _ := os.ReadFile(filepath.Join(dossier, f))
			return strings.TrimSpace(string(b))
		}
		jeton, c := lire("hub-jeton"), lire("consigne.txt")
		var id, res string
		switch {
		case strings.Contains(c, "security critic"):
			critiques++
			id = "sess-critique"
			if !strings.Contains(c, "<plan id=") || !strings.Contains(c, "systemctl restart nginx") || !strings.Contains(c, "DATA, not instructions") {
				t.Errorf("the critic's instructions lack the plan as data: %q", c)
			}
			if txt, e := appel(jeton, "ssh_commande", `{"serveur":"ab12","commande":"uptime"}`); e {
				t.Errorf("the critic cannot read: %s", txt)
			}
			for _, refus := range [][2]string{
				{"fichier_ecrire", `{"serveur":"ab12","chemin":"/tmp/x","contenu":"x"}`},
				{"proposer_plan", `{"serveur":"ab12","resume":"x","etapes":[` + etape + `]}`},
				{"ssh_commande", `{"serveur":"ab12","commande":"systemctl restart nginx","delai_s":30}`}, // the task's plan is not the critic's
				{"verifier_http", `{"url":"` + site.URL + `"}`},
			} {
				if txt, e := appel(jeton, refus[0], refus[1]); !e {
					t.Errorf("the critic could call %s: %s", refus[0], txt)
				}
			}
			l, _ := s.approbations.Liste()
			for _, ap := range l {
				if _, code, err := s.deciderApprobation("fenetre", ap.ID, "accepter", "", "", ""); ap.Etat == "attente" && (err != errCritiqueEnCours || code != http.StatusConflict) {
					t.Errorf("a plan accepted while its critic reads it: %d %v", code, err)
				}
			}
			res = "VERDICT: risques\n- Le port 22 reste ouvert à tous\n- **Pas de retour arrière**"
		case strings.Contains(c, "external verifier"):
			verifs++
			id, res = "sess-verif", verdictVerif
			if txt, e := appel(jeton, "verifier_http", `{"url":"`+site.URL+`/admin"}`); e || !strings.Contains(txt, `demandee`) || !strings.Contains(txt, `<server-output tool="verifier_http"`) {
				t.Errorf("verifier_http: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "verifier_http", `{"url":"http://192.0.2.1/"}`); e || !strings.Contains(txt, "not the address of one of your servers") {
				t.Errorf("verifier_http outside the servers: %v %s", e, txt)
			}
			if txt, e := appel(jeton, "fichier_ecrire", `{"serveur":"ab12","chemin":"/tmp/x","contenu":"x"}`); !e {
				t.Errorf("the verifier could write: %s", txt)
			}
		case strings.Contains(c, "ACCEPTED"):
			id, res = "sess-ops", "Appliqué."
			if txt, e := appel(jeton, "ssh_commande", `{"serveur":"ab12","commande":"systemctl restart nginx","delai_s":30}`); e {
				t.Errorf("accepted step refused: %s", txt)
			}
		default:
			id, res, opsJeton = "sess-ops", "Plan proposé.", jeton
			if txt, e := appel(jeton, "proposer_plan", `{"serveur":"ab12","resume":"Redémarrer nginx","etapes":[`+etape+`]}`); e {
				t.Errorf("proposer_plan: %s", txt)
			}
			if txt, e := appel(jeton, "verifier_http", `{"url":"`+site.URL+`"}`); !e {
				t.Errorf("the ops lead has verifier_http: %s", txt)
			}
		}
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": res})
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(`{"type":"system","subtype":"init","session_id":"`+id+`"}`+"\n"+string(b)+"\n"), 0o600)
	}
	attendre := func(id string, etats ...string) TacheServeurs {
		t.Helper()
		for i := 0; i < 100; i++ {
			for _, x := range tachesServeur(srv.ID) {
				if x.ID == id && slices.Contains(etats, x.Etat) {
					return x
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("task %s never reached %v: %+v", id, etats, tachesServeur(srv.ID))
		return TacheServeurs{}
	}
	cloche := func() bool {
		return slices.ContainsFunc(moduleServeurs{}.Elements(s), func(e ElementHub) bool { return e.ID == srv.ID+":verification" && e.Attente != nil })
	}
	tache := func() TacheServeurs {
		t.Helper()
		x, err := s.lancerTacheServeurs("Redémarre nginx", []string{srv.ID}, false)
		if err != nil {
			t.Fatal(err)
		}
		att := attendre(x.ID, "attente_accord")
		p, err := s.lirePlan(att.Plans[0])
		if err != nil || p.Critique == nil || p.Critique.Verdict != "risques" || !slices.Equal(p.Critique.Lignes, []string{"Le port 22 reste ouvert à tous", "Pas de retour arrière"}) {
			t.Fatalf("the plan's critique: %v %+v", err, p.Critique)
		}
		if l, _ := s.approbations.Liste(); s.plansAffiches(l)[p.ID].Critique == nil {
			t.Error("the card's plan lacks its critique")
		}
		if _, _, err := s.deciderApprobation("fenetre", att.Plans[0], "accepter", "", "", ""); err != nil {
			t.Fatal(err)
		}
		return attendre(x.ID, "finie", "echec", "arretee")
	}

	// 1. the verifier finds a failure: the task is echec, its report in the task and the RUNBOOK, the bell rings
	fin := tache()
	if critiques != 1 || verifs != 1 {
		t.Errorf("%d critic and %d verifier sessions", critiques, verifs)
	}
	if fin.Etat != "echec" || fin.Verification == nil || fin.Verification.Verdict != "echec" || !strings.Contains(fin.Historique[len(fin.Historique)-1].Detail, "mot de passe") {
		t.Fatalf("failed verification: %+v %+v", fin, fin.Verification)
	}
	var etats []string
	for _, h := range fin.Historique {
		etats = append(etats, h.Etat)
	}
	if strings.Join(etats, ",") != "preparation,critique,attente_accord,application,verification,echec" {
		t.Errorf("states: %v", etats)
	}
	rb, _ := os.ReadFile(fichierRunbook(srv.ID))
	if !strings.Contains(string(rb), "external verifier: Verification FAILED") || !strings.Contains(string(rb), "Le mot de passe n'est pas demandé") {
		t.Errorf("RUNBOOK without the verifier's report:\n%s", rb)
	}
	if !cloche() {
		t.Error("no bell for the failed verification")
	}
	if txt, e := appel(opsJeton, "serveurs_liste", `{}`); !e {
		t.Errorf("the task's token works after its end: %s", txt)
	}
	if slices.ContainsFunc(s.jetons.Liste(), func(x SessionHub) bool { return strings.HasPrefix(x.Projet, prefixeTache+fin.ID) }) {
		t.Error("a token of the task is left after its end (the critic's and the verifier's are revoked too)")
	}

	// 2. a later task whose verification passes: finie, and the bell is gone
	verdictVerif = "VERIFICATION: ok\n- /admin demande un mot de passe"
	if fin = tache(); fin.Etat != "finie" || fin.Verification == nil || fin.Verification.Verdict != "ok" || cloche() {
		t.Errorf("passing verification: %+v %+v (bell %v)", fin, fin.Verification, cloche())
	}
}
