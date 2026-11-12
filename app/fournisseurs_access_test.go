package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestClasserAccess: the e-mail addresses made canonical (lower case, sorted, once each), anything else refused.
func TestClasserAccess(t *testing.T) {
	c, _, a, err := classerCloudflare("access_proteger", []byte(`{"nom":"App.Exemple.fr","emails":[" Bob@Exemple.fr","alice@exemple.fr","bob@exemple.fr"]}`))
	if err != nil || c != ClasseChangement || a.Nom != "app.exemple.fr" || !slices.Equal(a.Emails, []string{"alice@exemple.fr", "bob@exemple.fr"}) {
		t.Errorf("canonical: %v %v %+v", c, err, a)
	}
	if c, _, a, err := classerCloudflare("access_retirer", []byte(`{"nom":"app.exemple.fr"}`)); err != nil || c != ClasseDestructrice || a.Emails != nil {
		t.Errorf("access_retirer: %v %v %+v", c, err, a)
	}
	trop := make([]string, maxEmailsAccess+1)
	for i := range trop {
		trop[i] = fmt.Sprintf("p%d@exemple.fr", i)
	}
	b, _ := json.Marshal(trop)
	for _, args := range []string{`{"nom":"app.exemple.fr","emails":[]}`, `{"nom":"app.exemple.fr"}`, `{"nom":"app.exemple.fr","emails":["pas une adresse"]}`,
		`{"nom":"app.exemple.fr","emails":["a\n@b.fr"]}`, `{"nom":"app.exemple.fr","emails":` + string(b) + `}`, `{"emails":["a@exemple.fr"]}`} {
		if _, _, _, err := classerCloudflare("access_proteger", []byte(args)); err == nil {
			t.Errorf("accepted: %s", args)
		}
	}
}

// fauxAccess: Cloudflare Access of account acc1 for one token, every call kept with its body.
type fauxAccess struct {
	mu     sync.Mutex
	appels []string
	apps   map[string]string   // id → domain
	pols   map[string][]string // application id → policy ids
	n      int
}

func lancerFauxAccess(t *testing.T, jeton string) *fauxAccess {
	t.Helper()
	f := &fauxAccess{apps: map[string]string{}, pols: map[string][]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corps, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		chemin := strings.TrimPrefix(r.URL.Path, "/client/v4/accounts/acc1/access/apps")
		f.appels = append(f.appels, strings.TrimSpace(r.Method+" "+chemin+" "+string(corps)))
		repondre := func(v any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": v})
		}
		parts := strings.Split(strings.Trim(chemin, "/"), "/")
		switch {
		case r.Header.Get("Authorization") != "Bearer "+jeton || !strings.HasPrefix(r.URL.Path, "/client/v4/accounts/acc1/access/apps"):
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"Authentication error"}]}`))
		case r.Method == "GET" && chemin == "":
			l := []map[string]string{{"id": "autre", "domain": "autre.exemple.fr"}}
			for id, d := range f.apps {
				l = append(l, map[string]string{"id": id, "domain": d})
			}
			repondre(l)
		case r.Method == "POST" && chemin == "":
			var a struct{ Domain string }
			_ = json.Unmarshal(corps, &a)
			f.n++
			id := fmt.Sprintf("app%d", f.n)
			f.apps[id] = a.Domain
			repondre(map[string]string{"id": id, "domain": a.Domain})
		case r.Method == "DELETE" && len(parts) == 1:
			delete(f.apps, parts[0])
			repondre(map[string]string{"id": parts[0]})
		case r.Method == "GET" && len(parts) == 2 && parts[1] == "policies":
			l := []map[string]string{}
			for _, p := range f.pols[parts[0]] {
				l = append(l, map[string]string{"id": p})
			}
			repondre(l)
		case r.Method == "POST" && len(parts) == 2 && parts[1] == "policies":
			f.pols[parts[0]] = append(f.pols[parts[0]], "pol-"+parts[0])
			repondre(map[string]string{"id": "pol-" + parts[0]})
		case r.Method == "PUT" && len(parts) == 3:
			repondre(map[string]string{"id": parts[2]})
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"not found"}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	ancien := urlCloudflare
	urlCloudflare = srv.URL
	t.Cleanup(func() { urlCloudflare = ancien })
	return f
}

// TestAccessProteger (M118): access_proteger and access_retirer only as steps of an accepted plan, for a host
// name of the environment; the exact calls and bodies Cloudflare receives (the application, then its allow
// policy; updated in place when it exists; removed); the token in no MCP answer nor the audit log.
func TestAccessProteger(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(string, []byte) (string, int) { return "", 0 })
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	serveursSessionTest(t, s, srv.ID)
	const jetonCF = "cf-jeton-TEMOIN-acc3ss"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, jetonCF)
	if err != nil {
		t.Fatal(err)
	}
	f := Fournisseur{ID: "f1", Type: "cloudflare", Nom: "CF", Cle: cle.ID, Etat: "ok", Compte: "acc1", Zones: []ZoneFournisseur{{ID: "z1", Nom: "exemple.fr", Droits: []string{"access"}}}}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) { return append(l, f), nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.DefinirEnvironnement("demo", "dev", Environnement{URL: "https://app.exemple.fr", Serveurs: []string{srv.ID},
		Protection: "access", Politique: "auto", Fournisseur: "f1"}); err != nil {
		t.Fatal(err)
	}
	cf := lancerFauxAccess(t, jetonCF)
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
	const proteger = `{"nom":"app.exemple.fr","emails":["Bob@exemple.fr","alice@exemple.fr"]}`
	if txt, e := appel("access_proteger", proteger); !e || !strings.Contains(txt, "plan required") {
		t.Errorf("without a plan: %v %s", e, txt)
	}
	if txt, e := appel("access_retirer", `{"nom":"autre.exemple.fr"}`); !e || txt != errNomInconnu.Error() {
		t.Errorf("outside the perimeter: %v %s", e, txt)
	}
	if txt, e := appel("proposer_plan", `{"serveur":"`+srv.ID+`","resume":"Accès réservé","etapes":[`+
		`{"outil":"access_proteger","arguments":`+proteger+`,"pourquoi":"réservé à l'équipe"},`+
		`{"outil":"access_proteger","arguments":{"nom":"app.exemple.fr","emails":["carol@exemple.fr"]},"pourquoi":"une seule personne"},`+
		`{"outil":"access_retirer","arguments":{"nom":"app.exemple.fr"},"pourquoi":"ouvert à tous","sans_sauvegarde":"rien sur le serveur"}]}`); e || !strings.Contains(txt, "destructrice") {
		t.Fatalf("proposer_plan: %v %s", e, txt)
	}
	l, _ := s.approbations.Liste()
	for _, a := range l {
		if a.Action == actionPlanServeur && a.Etat == "attente" {
			if _, _, err := s.deciderApprobation("fenetre", a.ID, "accepter", "", "", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	// the step's addresses in another order and case: the same step
	if txt, e := appel("access_proteger", `{"nom":"app.exemple.fr","emails":["alice@exemple.fr","bob@exemple.fr"]}`); e || !strings.Contains(txt, `"resultat": "protected"`) {
		t.Fatalf("access_proteger: %v %s", e, txt)
	}
	if txt, e := appel("access_proteger", `{"nom":"app.exemple.fr","emails":["carol@exemple.fr"]}`); e || !strings.Contains(txt, `"resultat": "protected"`) {
		t.Fatalf("access_proteger again: %v %s", e, txt)
	}
	if txt, e := appel("access_retirer", `{"nom":"app.exemple.fr"}`); e || !strings.Contains(txt, `"resultat": "removed"`) {
		t.Fatalf("access_retirer: %v %s", e, txt)
	}
	if txt, e := appel("access_retirer", `{"nom":"app.exemple.fr"}`); !e || !strings.Contains(txt, "already ran") {
		t.Errorf("a step ran twice: %v %s", e, txt)
	}
	politique := func(emails ...string) string {
		inc := []string{}
		for _, e := range emails {
			inc = append(inc, `{"email":{"email":"`+e+`"}}`)
		}
		return `{"decision":"allow","include":[` + strings.Join(inc, ",") + `],"name":"Athanor: allowed e-mails","precedence":1}`
	}
	attendus := []string{
		"GET", `POST  {"domain":"app.exemple.fr","name":"Athanor app.exemple.fr","session_duration":"24h","type":"self_hosted"}`,
		"GET /app1/policies", "POST /app1/policies " + politique("alice@exemple.fr", "bob@exemple.fr"),
		"GET", "GET /app1/policies", "PUT /app1/policies/pol-app1 " + politique("carol@exemple.fr"),
		"GET", "DELETE /app1",
	}
	if !slices.Equal(cf.appels, attendus) {
		t.Errorf("Cloudflare calls:\n got  %q\n want %q", cf.appels, attendus)
	}
	if len(cf.apps) != 0 {
		t.Errorf("applications left: %v", cf.apps)
	}
	audit := &strings.Builder{}
	_ = filepath.Walk(dossierJournalAudit(), func(p string, i os.FileInfo, _ error) error {
		if i != nil && !i.IsDir() {
			b, _ := os.ReadFile(p)
			audit.Write(b)
		}
		return nil
	})
	if strings.Contains(vu.String()+audit.String(), jetonCF) || !strings.Contains(audit.String(), "cloudflare DELETE /accounts/acc1/access/apps/app1") {
		t.Errorf("the token in an answer or the audit, or the call not audited")
	}
}

// TestBlocCaddy (M118): the site block for an origin certificate, ACME with and without the technical e-mail,
// plain HTTP and auth_proxy; what is refused. Then the auth_proxy hash computed from the vault (made once,
// infra, the clear password in no block), and the blocks checked by caddy validate when Docker answers.
func TestBlocCaddy(t *testing.T) {
	entetes := "\tencode zstd gzip\n\theader {\n\t\tStrict-Transport-Security \"max-age=31536000; includeSubDomains\"\n" +
		"\t\tX-Content-Type-Options nosniff\n\t\tX-Frame-Options DENY\n\t\tReferrer-Policy strict-origin-when-cross-origin\n\t\t-Server\n\t}\n"
	const h = "$2a$10$abcdefghijklmnopqrstuuJ1cVnS2q2y3w0wqQ2C8b8Lh8m1e0N1a"
	for _, c := range []struct {
		e                     Environnement
		email, hachage, voulu string
	}{
		{Environnement{URL: "https://app.exemple.fr", Certificat: "id", Protection: "access"}, "ops@exemple.fr", "",
			"app.exemple.fr {\n\ttls /etc/athanor/tls/app.exemple.fr.crt /etc/athanor/tls/app.exemple.fr.key\n" + entetes + "\treverse_proxy 127.0.0.1:8080\n}\n"},
		{Environnement{URL: "https://App.Exemple.fr/", Protection: "aucune"}, "ops@exemple.fr", h,
			"app.exemple.fr {\n\ttls ops@exemple.fr\n" + entetes + "\treverse_proxy 127.0.0.1:8080\n}\n"},
		{Environnement{URL: "https://dev.exemple.fr:8443", Protection: "auth_proxy"}, "", h,
			"dev.exemple.fr:8443 {\n" + entetes + "\tbasic_auth {\n\t\tathanor " + h + "\n\t}\n\treverse_proxy 127.0.0.1:8080\n}\n"},
		{Environnement{URL: "http://local.exemple.fr", Certificat: "id"}, "ops@exemple.fr", "",
			"http://local.exemple.fr {\n" + strings.Replace(entetes, "\t\tStrict-Transport-Security \"max-age=31536000; includeSubDomains\"\n", "", 1) + "\treverse_proxy 127.0.0.1:8080\n}\n"},
	} {
		if b, err := blocCaddy(c.e, "127.0.0.1:8080", c.email, c.hachage); err != nil || b != c.voulu {
			t.Errorf("%s: %v\n got  %q\n want %q", c.e.URL, err, b, c.voulu)
		}
	}
	for nom, c := range map[string]struct {
		e                     Environnement
		amont, email, hachage string
	}{
		"no URL":              {Environnement{}, "127.0.0.1:8080", "", ""},
		"auth_proxy, no hash": {Environnement{URL: "https://a.exemple.fr", Protection: "auth_proxy"}, "127.0.0.1:8080", "", ""},
		"hash with a brace":   {Environnement{URL: "https://a.exemple.fr", Protection: "auth_proxy"}, "127.0.0.1:8080", "", "$2a$10$x}\n"},
		"bad upstream":        {Environnement{URL: "https://a.exemple.fr"}, "127.0.0.1:8080 {\n}", "", ""},
		"bad e-mail":          {Environnement{URL: "https://a.exemple.fr"}, "127.0.0.1:8080", "ops@exemple.fr\nroot * /", ""},
	} {
		if b, err := blocCaddy(c.e, c.amont, c.email, c.hachage); err == nil {
			t.Errorf("%s accepted: %q", nom, b)
		}
	}

	// the hash from the vault
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: varEmailACME, Famille: "variable", Type: "autre"}, "ops@exemple.fr"); err != nil {
		t.Fatal(err)
	}
	for env, e := range map[string]Environnement{"dev": {URL: "https://dev.exemple.fr", Protection: "auth_proxy", Politique: "auto"},
		"prod": {URL: "https://exemple.fr", Protection: "access", Politique: "accord"}} {
		if err := s.acces.DefinirEnvironnement("demo", env, e); err != nil {
			t.Fatal(err)
		}
	}
	dev, err := s.blocCaddyEnv("demo", "dev", "127.0.0.1:3000")
	if err != nil {
		t.Fatal(err)
	}
	dev2, _ := s.blocCaddyEnv("demo", "dev", "127.0.0.1:3000")
	mdps := slices.DeleteFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.Type != "mot_de_passe" })
	if len(mdps) != 1 || mdps[0].Portee != "infra" || mdps[0].Famille != "secret" || mdps[0].Source != "proxy:demo/dev" {
		t.Fatalf("vault entries: %+v", mdps)
	}
	mdp, _ := s.coffre.Valeur(mdps[0].ID)
	hache := func(b string) string {
		i := strings.Index(b, "athanor $2")
		if i < 0 {
			return ""
		}
		return strings.Fields(b[i:])[1]
	}
	if len(mdp) < 32 || strings.Contains(dev+dev2, mdp) || bcrypt.CompareHashAndPassword([]byte(hache(dev)), []byte(mdp)) != nil ||
		bcrypt.CompareHashAndPassword([]byte(hache(dev2)), []byte(mdp)) != nil || !strings.Contains(dev, "\ttls ops@exemple.fr\n") {
		t.Errorf("auth_proxy block:\n%s", dev)
	}
	prod, err := s.blocCaddyEnv("demo", "prod", "127.0.0.1:3000")
	if err != nil || strings.Contains(prod, "basic_auth") {
		t.Errorf("prod block: %v\n%s", err, prod)
	}
	if _, err := s.blocCaddyEnv("demo", "staging", "127.0.0.1:3000"); err == nil {
		t.Error("an unknown environment gave a block")
	}

	// caddy validate, in a caddy:2 container, when Docker answers
	ctx, fin := context.WithTimeout(context.Background(), 10*time.Second)
	defer fin()
	if exec.CommandContext(ctx, "docker", "info").Run() != nil {
		t.Skip("Docker does not answer: caddy validate skipped")
	}
	d := t.TempDir()
	tls := filepath.Join(d, "tls")
	_ = os.Mkdir(tls, 0o755)
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "app.exemple.fr"},
		DNSNames: []string{"app.exemple.fr"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(1)}, k.Public(), k)
	kb, _ := x509.MarshalECPrivateKey(k)
	_ = os.WriteFile(filepath.Join(tls, "app.exemple.fr.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	_ = os.WriteFile(filepath.Join(tls, "app.exemple.fr.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o644)
	avecCert, _ := blocCaddy(Environnement{URL: "https://app.exemple.fr", Certificat: "id", Protection: "access"}, "127.0.0.1:8080", "", "")
	_ = os.WriteFile(filepath.Join(d, "Caddyfile"), []byte(avecCert+"\n"+dev+"\n"+prod), 0o644)
	ctx2, fin2 := context.WithTimeout(context.Background(), 4*time.Minute)
	defer fin2()
	sortie, err := exec.CommandContext(ctx2, "docker", "run", "--rm", "-v", d+":/etc/caddy:ro", "-v", tls+":"+dossierTLS+":ro",
		"caddy:2", "caddy", "validate", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile").CombinedOutput()
	if err != nil || !strings.Contains(string(sortie), "Valid configuration") {
		t.Errorf("caddy validate: %v\n%s", err, couper(string(sortie), 2000))
	}
}
