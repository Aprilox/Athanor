package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// The security invariants of VISION §5, one named test each (I1, I2, I3, I8 here; the others come with
// the modules they guard). No system tool, no network, no real value: they run on the 3 OS of the CI.

// montageInvariants: a realistic Athanor. A vault with a password and a (fake) system envelope, two
// projects, code entries granted to "demo" on dev and on prod, a code entry granted to nobody, an infra
// entry linked to "demo" on dev. Witness values are built at run time.
type montageInvariants struct {
	s                         *Serveur
	mux                       *http.ServeMux
	racine                    string // the projects' parent folder
	demo, autre               Projet
	dev, prod, non, infra     EntreeCoffre
	tDev, tProd, tNon, tInfra string
	mdp                       string
	reponses                  strings.Builder // every answer of appel
}

func montage(t *testing.T) *montageInvariants {
	t.Helper()
	m := &montageInvariants{s: serveurCoffreTest(t), racine: t.TempDir(), mdp: "temoin-mdp-" + aleatoire(8)}
	s := m.s
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin); _ = os.Remove(s.journalAccesNatif()) })
	m.demo = Projet{Nom: "demo", Dossier: filepath.Join(m.racine, "demo")}
	m.autre = Projet{Nom: "autre", Dossier: filepath.Join(m.racine, "autre")}
	for _, p := range []Projet{m.demo, m.autre} {
		if err := os.MkdirAll(filepath.Join(p.Atelier(), "logs"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	s.config.Projets = []Projet{m.demo, m.autre}
	s.mu.Unlock()
	m.mux = http.NewServeMux()
	s.routes(m.mux)
	if err := s.coffre.ChangerMotDePasse("", m.mdp); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller(m.mdp); err != nil {
		t.Fatal(err)
	}
	sfx := aleatoire(8)
	m.tDev, m.tProd, m.tNon, m.tInfra = "temoin-dev-"+sfx, "temoin-prod-"+sfx, "temoin-non-"+sfx, "temoin-infra-"+sfx
	nouvelle := func(nom, typ, valeur string) EntreeCoffre {
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nom, Famille: "secret", Type: typ}, valeur)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	m.dev, m.prod, m.non = nouvelle("Stripe test", "stripe", m.tDev), nouvelle("Stripe live", "stripe", m.tProd), nouvelle("OpenAI", "openai", m.tNon)
	// the infra entry goes in through the API, as the page sends it
	code, v := m.appel(t, "POST", "/api/coffre", `{"nom":"Cloudflare – martin.fr","famille":"secret","type":"cloudflare","portee":"infra","valeur":"`+m.tInfra+`"}`)
	if code != 200 {
		t.Fatalf("infra entry: %d %v", code, v)
	}
	for _, e := range s.coffre.Liste() {
		if e.Portee == "infra" {
			m.infra = e
		}
	}
	for _, a := range []struct {
		e   EntreeCoffre
		env string
	}{{m.dev, "dev"}, {m.prod, "prod"}, {m.infra, "dev"}} {
		if _, err := s.acces.Ajouter("demo", a.e, a.env, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func (m *montageInvariants) appel(t *testing.T, methode, chemin, corps string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
	r.Header.Set("X-Athanor", "jeton")
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	w := httptest.NewRecorder()
	m.mux.ServeHTTP(w, r)
	m.reponses.WriteString(w.Body.String())
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return w.Code, v
}

// lignesSecrets: the NAME=value lines of a project's secrets.env (shell quotes removed).
func lignesSecrets(t *testing.T, p Projet) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.Atelier(), "secrets.env"))
	if os.IsNotExist(err) {
		return map[string]string{}
	} else if err != nil {
		t.Fatal(err)
	}
	vus := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if n, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok && !strings.HasPrefix(n, "#") {
			vus[n] = strings.Trim(v, "'\"")
		}
	}
	return vus
}

// fichiersAvec: the files under racine (except those named in sauf) holding one of the values.
func fichiersAvec(t *testing.T, racine string, valeurs []string, sauf ...string) []string {
	t.Helper()
	var trouves []string
	_ = filepath.WalkDir(racine, func(c string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || slices.Contains(sauf, e.Name()) {
			return nil
		}
		b, _ := os.ReadFile(c)
		for _, v := range valeurs {
			if bytes.Contains(b, []byte(v)) {
				trouves = append(trouves, c+" ("+v+")")
			}
		}
		return nil
	})
	return trouves
}

// TestInvariantI1: a Dev agent receives only the code entries granted to its project for the current
// environment. Its injected file holds exactly those, nothing reaches envClaude or the project's files.
func TestInvariantI1(t *testing.T) {
	m := montage(t)
	attendu := func(env, valeur string) {
		t.Helper()
		if got := lignesSecrets(t, m.demo); len(got) != 1 || got["STRIPE_KEY"] != valeur {
			t.Fatalf("%s: the injected file must hold exactly STRIPE_KEY, got %d lines", env, len(got))
		}
		if got := lignesSecrets(t, m.autre); len(got) != 0 {
			t.Fatalf("%s: nothing is granted to the other project, got %d lines", env, len(got))
		}
	}
	// Start injects (it fails afterwards here: no kit)
	_, _ = m.s.demarrerPilote(m.demo)
	attendu("dev", m.tDev)
	if code, _ := m.appel(t, "POST", "/api/acces", `{"projet":"demo","action":"env","env":"prod"}`); code != 200 {
		t.Fatal("env prod:", code)
	}
	attendu("prod", m.tProd)
	if code, _ := m.appel(t, "POST", "/api/acces", `{"projet":"demo","action":"env","env":"staging"}`); code != 200 {
		t.Fatal("env staging:", code)
	}
	if got := lignesSecrets(t, m.demo); len(got) != 0 {
		t.Fatalf("staging: nothing granted, got %d lines", len(got))
	}
	m.appel(t, "POST", "/api/acces", `{"projet":"demo","action":"env","env":"dev"}`)
	attendu("dev again", m.tDev)

	// a grant taken back: the value leaves the file at once
	if code, _ := m.appel(t, "POST", "/api/acces", `{"projet":"demo","action":"retirer","entree":"`+m.dev.ID+`","env":"dev"}`); code != 200 {
		t.Fatal("remove:", code)
	}
	if got := lignesSecrets(t, m.demo); len(got) != 0 {
		t.Fatalf("removed, yet %d lines left", len(got))
	}

	tous := []string{m.tDev, m.tProd, m.tNon, m.tInfra}
	env := strings.Join((&pilote{}).envClaude(), "\n")
	for _, v := range tous {
		if strings.Contains(env, v) {
			t.Fatalf("value %s in the agent's environment", v)
		}
	}
	if f := fichiersAvec(t, m.racine, tous, "secrets.env"); len(f) > 0 {
		t.Fatalf("values in the project files: %v", f)
	}
}

// TestInvariantI2: an infra secret never leaves Athanor's process: not in the agent's environment or
// file, the project's logs/, pilot.log, STATE.json, Athanor's hub/ files, nor any answer of GET /api/*.
func TestInvariantI2(t *testing.T) {
	m := montage(t)
	// the forced native mode writes its own log line: still without the value
	avant := modeNatif.Load()
	t.Cleanup(func() { modeNatif.Store(avant) })
	modeNatif.Store(true)
	if code, v := m.appel(t, "POST", "/api/acces", `{"projet":"autre","action":"forcer","forcer":true,"confirme":true}`); code != 200 {
		t.Fatalf("force: %d %v", code, v)
	}
	if code, v := m.appel(t, "POST", "/api/acces", `{"projet":"autre","action":"ajouter","entree":"`+m.infra.ID+`","env":"dev"}`); code != 200 {
		t.Fatalf("infra forced: %d %v", code, v)
	}
	modeNatif.Store(avant)
	for _, p := range []Projet{m.demo, m.autre} {
		_, _ = m.s.demarrerPilote(p)
		// what the pilot writes in the workshop, so that the walk below reads real files
		ecrireFichier(t, p.PiloteLog(), "cycle 1\n", 0o644)
		ecrireFichier(t, p.EtatJSON(), `{"etat":"ok"}`, 0o644)
	}

	// every GET /api/* route of serveur.go, with the usual query parameters
	src, err := os.ReadFile("serveur.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := regexp.MustCompile(`mux\.HandleFunc\("GET (/api/[^"]*)"`).FindAllStringSubmatch(string(src), -1)
	if len(routes) < 30 {
		t.Fatalf("only %d GET routes read in serveur.go", len(routes))
	}
	joker := regexp.MustCompile(`\{[^}]*\}`)
	for _, r := range routes {
		chemin := joker.ReplaceAllString(r[1], "x") + "?p=demo&projet=demo&nom=demo&id=" + m.infra.ID
		fini := make(chan struct{})
		go func() { defer close(fini); m.appel(t, "GET", chemin, "") }()
		select {
		case <-fini:
		case <-time.After(60 * time.Second):
			t.Fatalf("GET %s did not answer in 60 s", chemin)
		}
	}
	if !strings.Contains(m.reponses.String(), "Cloudflare – martin.fr") {
		t.Fatal("the infra entry is not listed: the walk proves nothing")
	}

	if env := strings.Join((&pilote{}).envClaude(), "\n"); strings.Contains(env, m.tInfra) {
		t.Fatal("infra value in the agent's environment")
	}
	if strings.Contains(m.reponses.String(), m.tInfra) {
		t.Fatal("infra value in an API answer")
	}
	// everywhere on disk, secrets.env and the encrypted vault included
	for _, racine := range []string{m.racine, filepath.Dir(m.s.coffre.chemin), filepath.Dir(m.s.acces.chemin)} {
		if f := fichiersAvec(t, racine, []string{m.tInfra}); len(f) > 0 {
			t.Fatalf("infra value on disk: %v", f)
		}
	}
	if b, _ := os.ReadFile(m.s.journalAccesNatif()); !bytes.Contains(b, []byte("autre")) {
		t.Fatalf("the forced link is not logged: %q", b)
	}
}

// TestInvariantI3: a copy of Athanor's folder alone gives nothing away; a locked vault refuses every
// action that needs an infra secret.
func TestInvariantI3(t *testing.T) {
	m := montage(t)
	_, _ = m.s.demarrerPilote(m.demo)
	// the copy: hub/ (vault, accesses) and the config, on another PC (no system envelope, or another one)
	copie := t.TempDir()
	hub := filepath.Dir(m.s.coffre.chemin)
	err := filepath.WalkDir(hub, func(c string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(c)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(hub, c)
		cible := filepath.Join(copie, "hub", rel)
		if err := os.MkdirAll(filepath.Dir(cible), 0o700); err != nil {
			return err
		}
		return os.WriteFile(cible, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(cheminConfig()); err == nil {
		ecrireFichier(t, filepath.Join(copie, "config.json"), string(b), 0o600)
	}
	tous := []string{m.tDev, m.tProd, m.tNon, m.tInfra, m.mdp}
	var formes []string
	for _, v := range tous {
		formes = append(formes, v, base64.StdEncoding.EncodeToString([]byte(v)), base64.RawURLEncoding.EncodeToString([]byte(v)), hex.EncodeToString([]byte(v)))
	}
	if f := fichiersAvec(t, copie, formes); len(f) > 0 {
		t.Fatalf("the copy gives values away: %v", f)
	}
	for _, sys := range []enveloppeSysteme{nil, nouveauFauxSysteme()} {
		c, err := ouvrirCoffre(filepath.Join(copie, "hub", "coffre.json"), sys)
		if err != nil {
			t.Fatal("the copy no longer opens as a vault:", err)
		}
		if n := len(c.Liste()); n != 4 {
			t.Fatalf("copy: %d entries listed, want 4", n)
		}
		for _, e := range c.Liste() {
			if v, err := c.Valeur(e.ID); err == nil || v != "" {
				t.Fatalf("copy (system envelope %v): %s readable", sys != nil, e.Nom)
			}
		}
	}

	// locked: the infra value cannot be read, tested, changed nor injected; the code keys still reopen
	m.s.coffre.Verrouiller()
	if _, err := m.s.coffre.Valeur(m.infra.ID); err == nil {
		t.Fatal("locked vault, infra value read")
	}
	if v, err := m.s.coffre.Valeur(m.dev.ID); err != nil || v != m.tDev {
		t.Fatalf("locked vault: the code key must reopen through the system envelope (%v)", err)
	}
	if code, v := m.appel(t, "POST", "/api/coffre/tester", `{"id":"`+m.infra.ID+`"}`); code != http.StatusLocked {
		t.Fatalf("test of the infra entry while locked: %d %v", code, v)
	}
	if code, v := m.appel(t, "POST", "/api/coffre/"+m.infra.ID, `{"valeur":"temoin-autre-`+aleatoire(6)+`"}`); code != http.StatusLocked {
		t.Fatalf("infra value changed while locked: %d %v", code, v)
	}
	if _, err := m.s.injecterAcces(m.demo, nil); err != nil {
		t.Fatal(err)
	}
	if got := lignesSecrets(t, m.demo); len(got) != 1 || got["STRIPE_KEY"] != m.tDev {
		t.Fatalf("locked: the injection must still give the code entry alone, got %d lines", len(got))
	}
}

// TestInvariantI8: no real secret in the repository, the tests or the CI. Real token shapes appear
// nowhere; the gitleaks exemption stays limited to the run-time witness values; the CI uses no secret
// but its own GITHUB_TOKEN.
func TestInvariantI8(t *testing.T) {
	formes := regexp.MustCompile(strings.Join([]string{
		`sk-ant-[A-Za-z0-9_-]{30,}`,                                 // Anthropic
		`sk-(proj-)?[A-Za-z0-9]{40,}`,                               // OpenAI
		`[sr]k_live_[0-9A-Za-z]{16,}`,                               // Stripe live
		`gh[pousr]_[A-Za-z0-9]{36}`,                                 // GitHub
		`github_pat_[A-Za-z0-9_]{50,}`,                              // GitHub fine-grained
		`AKIA[0-9A-Z]{16}`,                                          // AWS
		`AIza[0-9A-Za-z_-]{35}`,                                     // Google
		`xox[abprs]-[0-9A-Za-z-]{20,}`,                              // Slack
		`-----BEGIN [A-Z ]*PRIVATE KEY-----\r?\n[A-Za-z0-9+/=]{40}`, // a real private key body
	}, "|"))
	racine := ".."
	ignores := []string{".git", ".athanor", ".claude", "node_modules", "third_party", "deliverable", "dist"}
	lus := 0
	err := filepath.WalkDir(racine, func(c string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if slices.Contains(ignores, e.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if i, err := e.Info(); err != nil || i.Size() > 2<<20 || strings.HasSuffix(c, ".exe") {
			return nil
		}
		b, err := os.ReadFile(c)
		if err != nil || bytes.IndexByte(b, 0) >= 0 { // binary
			return nil
		}
		lus++
		if f := formes.Find(b); f != nil {
			t.Errorf("%s: a real secret shape (%d bytes): tests build their fake values at run time", c, len(f))
		}
		return nil
	})
	if err != nil || lus < 100 {
		t.Fatalf("repository walk: %v, %d files read", err, lus)
	}

	// the gitleaks exemption: the witness values alone, and the 5 fingerprints already on main
	toml, err := os.ReadFile("../.gitleaks.toml")
	if err != nil {
		t.Fatal(err)
	}
	if r := regexp.MustCompile(`'''([^']*)'''`).FindAllStringSubmatch(string(toml), -1); len(r) != 1 || r[0][1] != `(?i)^t[eé]moin` {
		t.Fatalf(".gitleaks.toml exempts more than the witness values: %v", r)
	}
	ign, err := os.ReadFile("../.gitleaksignore")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range strings.Split(string(ign), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			n++
		}
	}
	if n != 5 {
		t.Fatalf(".gitleaksignore: %d fingerprints, want the 5 already on main (a new fake value starts with \"temoin\")", n)
	}

	// the CI: the gitleaks job runs, and no secret of the repository is used (no real account)
	ci, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`gitleaks"?\s+git\s`).Match(ci) {
		t.Fatal("ci.yml no longer runs gitleaks")
	}
	for _, f := range regexp.MustCompile(`secrets\.([A-Za-z0-9_]+)`).FindAllSubmatch(ci, -1) {
		if string(f[1]) != "GITHUB_TOKEN" {
			t.Errorf("ci.yml uses the secret %s: the CI runs without real accounts", f[1])
		}
	}
}
