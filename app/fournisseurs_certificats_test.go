package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
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

// fauxOrigineCA: Cloudflare's origin CA for one token: POST /certificates checked (the CSR signed by its key,
// for the names asked, the request's fields), the certificate signed by a test CA; autreCle: one for another key.
type fauxOrigineCA struct {
	mu       sync.Mutex
	appels   []string
	autreCle bool
}

func lancerFauxOrigineCA(t *testing.T, jeton string) *fauxOrigineCA {
	t.Helper()
	f := &fauxOrigineCA{}
	ca, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	modele := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Faux Origin CA"}, IsCA: true, BasicConstraintsValid: true,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(20, 0, 0), KeyUsage: x509.KeyUsageCertSign}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corps, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.appels = append(f.appels, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/client/v4"))
		var d struct {
			CSR       string   `json:"csr"`
			Hostnames []string `json:"hostnames"`
			Type      string   `json:"request_type"`
			Validite  int      `json:"requested_validity"`
		}
		_ = json.Unmarshal(corps, &d)
		var csr *x509.CertificateRequest
		if b, _ := pem.Decode([]byte(d.CSR)); b != nil && b.Type == "CERTIFICATE REQUEST" {
			csr, _ = x509.ParseCertificateRequest(b.Bytes)
		}
		if r.Header.Get("Authorization") != "Bearer "+jeton || r.Method != "POST" || r.URL.Path != "/client/v4/certificates" || csr == nil ||
			csr.CheckSignature() != nil || !slices.Equal(csr.DNSNames, d.Hostnames) || len(d.Hostnames) != 1 || d.Type != "origin-ecc" || d.Validite != 5475 {
			t.Errorf("origin CA request: %s %s %s", r.Method, r.URL.Path, corps)
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"bad request"}]}`))
			return
		}
		pub := csr.PublicKey
		if f.autreCle {
			k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			pub = k.Public()
		}
		der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: d.Hostnames[0]},
			DNSNames: d.Hostnames, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(0, 0, d.Validite)}, modele, pub, ca)
		if err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": map[string]any{"id": "c1",
			"certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "hostnames": d.Hostnames, "request_type": d.Type}})
	}))
	t.Cleanup(srv.Close)
	ancien := urlCloudflare
	urlCloudflare = srv.URL
	t.Cleanup(func() { urlCloudflare = ancien })
	return f
}

// TestCertificatOrigine (M117): certificat_origine only as a step of an accepted plan, for a host name of the
// environment; the CSR sent to the origin CA, the certificate and its key in the infra vault (one entry, kept
// when renewed), the expiry in the environment, the key then the certificate written as root on the plan's
// server (0600 / 0644); an answer for another key installs nothing; the private key in no MCP answer, nor the
// audit log, nor the RUNBOOK.
func TestCertificatOrigine(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	var mu sync.Mutex
	var cmds, entrees []string
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, e []byte) (string, int) {
		mu.Lock()
		defer mu.Unlock()
		cmds, entrees = append(cmds, cmd), append(entrees, string(e))
		return "", 0
	})
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	serveursSessionTest(t, s, srv.ID)
	const jetonCF = "cf-jeton-TEMOIN-c3rt"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, jetonCF)
	if err != nil {
		t.Fatal(err)
	}
	f := Fournisseur{ID: "f1", Type: "cloudflare", Nom: "CF", Cle: cle.ID, Etat: "ok", Zones: []ZoneFournisseur{{ID: "z1", Nom: "exemple.fr", Droits: []string{"certificats"}}}}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) { return append(l, f), nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.definirAncien("demo", "dev", Environnement{URL: "https://app.exemple.fr", Serveurs: []string{srv.ID},
		Protection: "aucune", Politique: "auto", Fournisseur: "f1"}); err != nil {
		t.Fatal(err)
	}
	ca := lancerFauxOrigineCA(t, jetonCF)
	jeton, err := s.jetons.CreerSession(SessionHub{Projet: "demo", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{srv.ID}, Env: "demo/dev", Hotes: []string{"app.exemple.fr"}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	vu := &strings.Builder{}
	appel := func(nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args))
		if rep.Error != nil {
			vu.WriteString(rep.Error.Message)
			return rep.Error.Message, true
		}
		vu.WriteString(rep.Result.Content[0].Text)
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	const args = `{"nom":"app.exemple.fr"}`
	if txt, e := appel("certificat_origine", args); !e || !strings.Contains(txt, "plan required") {
		t.Errorf("without a plan: %v %s", e, txt)
	}
	if txt, e := appel("certificat_origine", `{"nom":"autre.exemple.fr"}`); !e || txt != errNomInconnu.Error() {
		t.Errorf("outside the perimeter: %v %s", e, txt)
	}
	poser := func() (string, bool) { // a plan proposed, accepted, then its step
		t.Helper()
		if txt, e := appel("proposer_plan", `{"serveur":"`+srv.ID+`","resume":"HTTPS d'origine","etapes":[{"outil":"certificat_origine","arguments":`+args+`,"pourquoi":"mode strict"}]}`); e {
			t.Fatalf("proposer_plan: %s", txt)
		}
		l, _ := s.approbations.Liste()
		for _, a := range l {
			if a.Action == actionPlanServeur && a.Etat == "attente" {
				if _, _, err := s.deciderApprobation("fenetre", a.ID, "accepter", "", "", ""); err != nil {
					t.Fatal(err)
				}
			}
		}
		return appel("certificat_origine", args)
	}
	txt, e := poser()
	if e || !strings.Contains(txt, `"resultat": "installed"`) || !strings.Contains(txt, dossierTLS+"/app.exemple.fr.key") {
		t.Fatalf("certificat_origine: %v %s", e, txt)
	}
	if txt, e := appel("certificat_origine", args); !e || !strings.Contains(txt, "already ran") {
		t.Errorf("a step ran twice: %v %s", e, txt)
	}
	mu.Lock()
	if len(cmds) != 2 || !strings.HasPrefix(cmds[0], "sudo -n sh -c ") || !strings.HasSuffix(cmds[0], " 'app.exemple.fr.key' '640'") ||
		!strings.HasSuffix(cmds[1], " 'app.exemple.fr.crt' '644'") || !strings.Contains(cmds[0], `g=caddy`) {
		t.Fatalf("commands on the server: %q", cmds)
	}
	crt, clePrivee := entrees[1], entrees[0]
	mu.Unlock()
	if _, err := tls.X509KeyPair([]byte(crt), []byte(clePrivee)); err != nil || !strings.Contains(clePrivee, "EC PRIVATE KEY") {
		t.Fatalf("installed pair: %v", err)
	}
	env := s.acces.Environnements("demo")["dev"]
	exp, err := time.Parse(time.RFC3339, env.CertificatExpire)
	if err != nil || exp.Before(time.Now().AddDate(14, 0, 0)) || env.Fournisseur != "f1" || !slices.Equal(env.CertificatServeurs, []string{srv.ID}) ||
		!strings.Contains(txt, `"`+srv.ID+`": "installed, key root:caddy 0640"`) {
		t.Errorf("environment: %+v", env)
	}
	if v, err := s.coffre.Valeur(env.Certificat); err != nil || v != crt+clePrivee {
		t.Errorf("vault value: %v", err)
	}
	certs := slices.DeleteFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.Type != "certificat" })
	if len(certs) != 1 || certs[0].Portee != "infra" || certs[0].Source != "env:demo/dev" || certs[0].ID != env.Certificat {
		t.Errorf("vault entries: %+v", certs)
	}
	// a request saved from the panel keeps what Athanor recorded
	if err := s.acces.definirAncien("demo", "dev", Environnement{URL: "https://app.exemple.fr", Serveurs: []string{srv.ID}, Protection: "aucune",
		Politique: "auto", Fournisseur: "f1", Certificat: "pirate", CertificatServeurs: []string{"pirate"}}); err != nil || s.acces.Environnements("demo")["dev"].Certificat != env.Certificat ||
		!slices.Equal(s.acces.Environnements("demo")["dev"].CertificatServeurs, env.CertificatServeurs) {
		t.Errorf("certificate after a panel save: %v %+v", err, s.acces.Environnements("demo")["dev"])
	}

	// renewed: the same vault entry, a new key
	if txt, e := poser(); e {
		t.Fatalf("renewal: %s", txt)
	}
	mu.Lock()
	cle2 := entrees[2]
	mu.Unlock()
	certs = slices.DeleteFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.Type != "certificat" })
	if v, _ := s.coffre.Valeur(env.Certificat); len(certs) != 1 || cle2 == clePrivee || !strings.HasSuffix(v, cle2) {
		t.Errorf("renewal: %d entries, same key %v", len(certs), cle2 == clePrivee)
	}
	// a certificate for another key: refused, nothing installed
	ca.autreCle = true
	if txt, e := poser(); !e || !strings.Contains(txt, "not a certificate for this name and this key") {
		t.Errorf("another key: %v %s", e, txt)
	}
	mu.Lock()
	if len(cmds) != 4 {
		t.Errorf("installed despite a wrong certificate: %d commands", len(cmds))
	}
	mu.Unlock()
	if !slices.Equal(ca.appels, []string{"POST /certificates", "POST /certificates", "POST /certificates"}) {
		t.Errorf("origin CA calls: %q", ca.appels)
	}

	// the private keys: in nothing the agent saw, nor the audit log, nor the RUNBOOK
	audit := &strings.Builder{}
	_ = filepath.Walk(dossierJournalAudit(), func(p string, i os.FileInfo, _ error) error {
		if i != nil && !i.IsDir() {
			b, _ := os.ReadFile(p)
			audit.Write(b)
		}
		return nil
	})
	runbook, _ := os.ReadFile(fichierRunbook(srv.ID))
	for nom, txt := range map[string]string{"MCP": vu.String(), "audit": audit.String(), "RUNBOOK": string(runbook)} {
		if strings.Contains(txt, "PRIVATE KEY") || strings.Contains(txt, jetonCF) {
			t.Errorf("a private key or the token in the %s", nom)
		}
		for _, k := range []string{clePrivee, cle2} {
			if b, _ := pem.Decode([]byte(k)); b != nil && strings.Contains(txt, string(pem.EncodeToMemory(b))[31:95]) {
				t.Errorf("the private key in the %s", nom)
			}
		}
	}
	if !strings.Contains(string(runbook), "certificat_origine") || !strings.Contains(audit.String(), "cloudflare POST /certificates") {
		t.Errorf("RUNBOOK or audit without the step")
	}
}

// TestCertificatOrigineConteneur (M117): on the test container's real sshd, the pair written as root:
// /etc/athanor/tls 0755, the key unreadable by the login user, the certificate 0644, no temporary file; the key
// 0600 root:root without a caddy group, root:caddy 0640 and readable by user caddy with it (M129, C59).
func TestCertificatOrigineConteneur(t *testing.T) {
	s, _, id, _, appel := serveurConteneurTest(t)
	const nom = "app.exemple.fr"
	crt, cle := "-----BEGIN CERTIFICATE-----\nfaux\n-----END CERTIFICATE-----\n", "-----BEGIN EC PRIVATE KEY-----\n"+temoinOutils+"\n-----END EC PRIVATE KEY-----\n"
	d := dossierTLS + "/" + nom
	verifier := func(cas, cleVoulue string, lisibleVoulu bool) {
		t.Helper()
		// twice: replaced in place
		for range 2 {
			if lisible, err := s.poserCertificat(id, nom, crt, cle); err != nil || lisible != lisibleVoulu {
				t.Fatalf("%s: %v, readable by Caddy %v", cas, err, lisible)
			}
		}
		r := appel("ssh_commande", map[string]any{"commande": "stat -c '%a %U' " + dossierTLS + "; sudo -n stat -c '%a %U %G' " + d + ".key " + d + ".crt; cat " + d + ".key >/dev/null 2>&1 || echo refused; cat " + d + ".crt; sudo -n ls -A " + dossierTLS})
		if want := "755 root\n" + cleVoulue + "\n644 root root\nrefused\n" + crt + nom + ".crt\n" + nom + ".key\n"; r.Sortie != want {
			t.Errorf("%s, on the server:\n got  %q\n want %q", cas, r.Sortie, want)
		}
	}
	creeCaddy := strings.TrimSpace(appel("ssh_commande", map[string]any{"commande": "getent group caddy >/dev/null || echo absent"}).Sortie) == "absent"
	if creeCaddy {
		verifier("no caddy group", "600 root root", false)
		r := appel("ssh_commande", map[string]any{"commande": "sudo -n useradd --system --user-group --no-create-home --shell /usr/sbin/nologin caddy && echo ok"})
		if strings.TrimSpace(r.Sortie) != "ok" {
			t.Fatalf("user caddy: %q", r.Sortie)
		}
		t.Cleanup(func() { appel("ssh_commande", map[string]any{"commande": "sudo -n userdel caddy"}) })
	}
	verifier("caddy group", "640 root caddy", true)
	r := appel("ssh_commande", map[string]any{"commande": "sudo -n -u caddy test -r " + d + ".key && echo readable; sudo -n cat " + d + ".key | grep -c " + temoinOutils[:12] + "; sudo -n rm -rf /etc/athanor/tls"})
	if r.Sortie != "readable\n1\n" {
		t.Errorf("key for Caddy, content: %q", r.Sortie)
	}
}
