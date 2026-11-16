package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// a gh that answers the deployment-key calls as GitHub does: this test program itself, copied under gh's
// name (the same on every system); each call traced on one line
func init() {
	if os.Getenv("ATHANOR_FAUX_GH_CLE") != "1" {
		return
	}
	args := os.Args[1:]
	if o, err := os.OpenFile(os.Getenv("ATHANOR_FAUX_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintln(o, strings.Join(args, " "))
		o.Close()
	}
	a := strings.Join(args, " ")
	switch {
	case os.Getenv("ATHANOR_FAUX_GH_REFUS") == "1":
		fmt.Fprintln(os.Stderr, "gh: key is already in use (HTTP 422)")
		os.Exit(1)
	case strings.HasPrefix(a, "api -X POST repos/") && strings.Contains(a, "/keys "):
		fmt.Printf(`{"id":%d,"key":"ssh-ed25519 AAAA","read_only":true}`+"\n", 700+len(a)%100)
		os.Exit(0)
	case strings.HasPrefix(a, "api -X DELETE repos/"):
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "gh: unexpected call")
	os.Exit(1)
}

// M119: Create the deployment key: the private half in the vault (infra, ssh, deploiement:<project>), the
// public half read-only on the repository (gh's exact arguments), once; a project without GitHub, a
// refusal: a clear message and nothing kept; the private key in no answer and no audit line; unlinking
// and deleting the project take it off the repository and out of the vault.
func TestCleDeploiement(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s) // infra entries need the vault's password
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	d := t.TempDir()
	demo, autre := Projet{Nom: "demo", Dossier: filepath.Join(d, "demo")}, Projet{Nom: "autre", Dossier: filepath.Join(d, "autre")}
	for _, p := range []Projet{demo, autre} {
		_ = os.MkdirAll(filepath.Join(p.Dossier, ".athanor"), 0o755)
	}
	s.mu.Lock()
	s.config.Projets = []Projet{demo, autre}
	s.mu.Unlock()
	// the fake gh first in PATH, the GitHub link of demo only
	bin, trace := t.TempDir(), filepath.Join(t.TempDir(), "trace")
	moi, _ := os.Executable()
	b, err := os.ReadFile(moi)
	nomGH := "gh"
	if runtime.GOOS == "windows" {
		nomGH = "gh.exe"
	}
	if err != nil || os.WriteFile(filepath.Join(bin, nomGH), b, 0o755) != nil {
		t.Fatal("fake gh:", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ATHANOR_FAUX_GH_CLE", "1")
	t.Setenv("ATHANOR_FAUX_TRACE", trace)
	t.Setenv("ATHANOR_GITHUB_DIR", filepath.Join(d, "github"))
	dGH := dossierGH(demo.Dossier)
	if err := os.MkdirAll(dGH, 0o700); err != nil || ecrireReglagesGH(dGH, reglagesGH{Depot: "moi/demo", Branche: "main", Auto: true}) != nil {
		t.Fatal("link:", err)
	}
	// the pilot's side, run in this process
	ancien := executerCleDeploiement
	executerCleDeploiement = func(_ *Serveur, p Projet, args ...string) (string, error) {
		var b bytes.Buffer
		if code, _ := commandeGitHub(append([]string{"cle-deploiement", p.Dossier}, args...), &b); code != 0 {
			return b.String(), fmt.Errorf("exit %d", code)
		}
		return b.String(), nil
	}
	t.Cleanup(func() { executerCleDeploiement = ancien })
	mux := http.NewServeMux()
	s.routes(mux)
	var reponses strings.Builder
	creer := func(projet string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/projets/"+projet+"/cle-deploiement", strings.NewReader("{}"))
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
	cles := func() []EntreeCoffre {
		var l []EntreeCoffre
		for _, e := range s.coffre.Liste() {
			if strings.HasPrefix(e.Source, "deploiement:") {
				l = append(l, e)
			}
		}
		return l
	}
	lireTrace := func() string { x, _ := os.ReadFile(trace); return string(x) }

	// not linked to GitHub: a clear message, nothing kept, GitHub not called
	if code, v := creer("autre"); code != 400 || v["erreur"] != errCleDeploiementNonLie.Error() || len(cles()) != 0 || lireTrace() != "" {
		t.Fatalf("not linked: %d %v %v %q", code, v, cles(), lireTrace())
	}
	// GitHub refuses: its reason, nothing kept
	t.Setenv("ATHANOR_FAUX_GH_REFUS", "1")
	if code, v := creer("demo"); code != 502 || !strings.Contains(fmt.Sprint(v["erreur"]), "key is already in use") || len(cles()) != 0 || s.acces.CleDeploiementProjet("demo") != "" {
		t.Fatalf("refused: %d %v %v", code, v, cles())
	}
	t.Setenv("ATHANOR_FAUX_GH_REFUS", "")
	_ = os.Remove(trace)
	// created: the exact call, read-only, with the public half of the key kept in the vault
	code, v := creer("demo")
	l := cles()
	if code != 200 || v["depot"] != "moi/demo" || len(l) != 1 || l[0].Type != "ssh" || l[0].Portee != "infra" || l[0].Source != "deploiement:demo" {
		t.Fatalf("created: %d %v %+v", code, v, l)
	}
	prive, err := s.coffre.Valeur(l[0].ID)
	signataire, err2 := ssh.ParsePrivateKey([]byte(prive))
	if err != nil || err2 != nil {
		t.Fatal("private key:", err, err2)
	}
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signataire.PublicKey())))
	attendu := "api -X POST repos/moi/demo/keys -f title=Athanor deploy key (read-only) -f key=" + pub + " -F read_only=true\n"
	if lireTrace() != attendu {
		t.Fatalf("gh calls:\n%s\nexpected:\n%s", lireTrace(), attendu)
	}
	id := lireIDCleDeploiement(dGH)
	envs := s.acces.Environnements("demo")
	if id == "" || s.acces.CleDeploiementProjet("demo") != l[0].ID || len(envs) != 0 || s.acces.CleDeploiementProjet("autre") != "" {
		t.Fatalf("recorded: %q %+v", id, envs)
	}
	// saving an environment keeps it; already there: nothing, GitHub not called again
	if err := s.acces.DefinirEnvironnement("demo", "dev", envDefaut()); err != nil || s.acces.Environnements("demo")["dev"].CleDeploiement != l[0].ID ||
		vueEnvironnement(s.acces.Environnements("demo")["dev"], nil)["cle_deploiement_prete"] != true {
		t.Fatal("kept on save:", err)
	}
	if code, v := creer("demo"); code != 200 || v["deja"] != true || len(cles()) != 1 || lireTrace() != attendu {
		t.Fatalf("again: %d %v %q", code, v, lireTrace())
	}
	// the private key in no answer and no audit line
	audit := ""
	_ = filepath.WalkDir(dossierJournalAudit(), func(p string, e os.DirEntry, _ error) error {
		if e != nil && !e.IsDir() {
			x, _ := os.ReadFile(p)
			audit += string(x)
		}
		return nil
	})
	coeur := strings.Split(strings.TrimSpace(prive), "\n")[2]
	if strings.Contains(reponses.String(), coeur) || strings.Contains(reponses.String(), "PRIVATE KEY") || strings.Contains(audit, coeur) || !strings.Contains(audit, "clé de déploiement créée") {
		t.Fatalf("private key leaked, or no audit line: %s\n%s", reponses.String(), audit)
	}
	// unlinked: off the repository (delierGH), out of the vault (the hub)
	var sortie bytes.Buffer
	if code, _ := commandeGitHub([]string{"delier", demo.Dossier}, &sortie); code != 0 || !strings.Contains(sortie.String(), "Deployment key removed from moi/demo.") ||
		!strings.HasSuffix(lireTrace(), "api -X DELETE repos/moi/demo/keys/"+id+"\n") {
		t.Fatalf("unlink: %s\n%s", sortie.String(), lireTrace())
	}
	s.oublierCleDeploiement("demo")
	if len(cles()) != 0 || s.acces.CleDeploiementProjet("demo") != "" {
		t.Fatalf("still kept after unlinking: %+v", cles())
	}
	// linked again, created again, then the project deleted: out of the vault, the access entry gone
	if ecrireReglagesGH(dGH, reglagesGH{Depot: "moi/demo", Branche: "main", Auto: true}) != nil {
		t.Fatal("link again")
	}
	// B72: six clicks at once: one key in the vault, one registration on GitHub
	_ = os.Remove(trace)
	var wg sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/api/projets/demo/cle-deploiement", strings.NewReader("{}"))
			r.Header.Set("X-Athanor", "jeton")
			r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
			r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			codes[i] = w.Code
		}()
	}
	wg.Wait()
	if n := strings.Count(lireTrace(), "-X POST repos/moi/demo/keys"); len(cles()) != 1 || n != 1 || slices.ContainsFunc(codes, func(c int) bool { return c != 200 }) {
		t.Fatalf("six clicks at once: %d keys, %d registrations, %v", len(cles()), n, codes)
	}
	if code, _ := creer("demo"); code != 200 || len(cles()) != 1 {
		t.Fatal("create again:", code)
	}
	if err := s.retirerProjet(demo); err != nil || len(cles()) != 0 || s.acces.CleDeploiementProjet("demo") != "" {
		t.Fatalf("deleted: %v %+v", err, cles())
	}
}
