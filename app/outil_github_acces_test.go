//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// the fake gh, which also gives its token and says which one each API call used
var faux_gh_acces = strings.Replace(strings.Replace(faux_gh, `  "auth refresh") exit 0 ;;`, `  "auth refresh") exit 0 ;;
  "auth token") echo "gho_jetonGardeDeMoi0123456789"; exit 0 ;;`, 1), `[ "$1" = api ] || exit 2
shift`, `[ "$1" = api ] || exit 2
shift
echo "api-token=${GH_TOKEN:-none}" >> "$TRACE_GH"
case "$GH_TOKEN" in revoque*) echo "HTTP 401: Bad credentials" >&2; exit 1 ;; esac`, 1)

// The access kept to create repositories: off by default; one sign-in, its token kept (0600) for the
// connection's account only; a creation then signs in no more; revoked on GitHub, forgotten here and the
// creation signs in again; Forget and Disconnect remove it.
func TestAccesGardeGH(t *testing.T) {
	for _, x := range []string{"ssh-keygen", "git"} {
		if _, err := exec.LookPath(x); err != nil {
			t.Skip("no " + x)
		}
	}
	bin := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(faux_gh_acces), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TRACE_GH", trace)
	t.Setenv("ATHANOR_GITHUB_DIR", filepath.Join(t.TempDir(), "github"))
	t.Setenv("ATHANOR_GITHUB_SSH", "127.0.0.1:1")
	t.Setenv("ATHANOR_GITHUB_SSH_SECOURS", "")
	t.Setenv("GH_TOKEN", "jeton-de-l-utilisateur")
	pauseLierGH = 0
	var b bytes.Buffer
	if code := garderAccesGH(nil, &b); code != 1 || !strings.Contains(b.String(), "not connected") {
		t.Fatalf("kept without a connection: %d %s", code, b.String())
	}
	b.Reset()
	if code := connecterGH(nil, &b); code != 0 {
		t.Fatal(b.String())
	}
	if lireConnexionGH().AccesGarde {
		t.Fatal("kept by default")
	}
	b.Reset()
	if code := garderAccesGH(nil, &b); code != 0 || !strings.Contains(b.String(), "creates repositories as moi without a window") {
		t.Fatalf("keep: %d %s", code, b.String())
	}
	if st, err := os.Stat(fichierAccesGH()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the kept access: %v %v", st, err)
	}
	b.Reset()
	connexionGHCommande(nil, &b)
	if !strings.Contains(b.String(), `"acces_garde":true`) || strings.Contains(b.String(), "gho_") {
		t.Fatalf("state: %s", b.String())
	}
	if l, _ := filepath.Glob(filepath.Join(os.Getenv("ATHANOR_GITHUB_DIR"), ".connexion.*")); len(l) != 0 {
		t.Fatalf("the sign-in's folder kept: %v", l)
	}
	// a creation: no sign-in, the kept token (never the user's own GH_TOKEN)
	projet := filepath.Join(t.TempDir(), "site")
	_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
	_ = os.Truncate(trace, 0)
	b.Reset()
	if code := lierGH([]string{projet, "creer", "site"}, &b); code != 0 || !strings.Contains(b.String(), "with the access kept on this computer") || !strings.Contains(b.String(), "Private repository created: moi/site") {
		t.Fatalf("create: %d %s", code, b.String())
	}
	tr, _ := os.ReadFile(trace)
	if strings.Contains(string(tr), "auth login") || !strings.Contains(string(tr), "api-token=gho_jetonGardeDeMoi0123456789") || strings.Contains(string(tr), "jeton-de-l-utilisateur") {
		t.Fatalf("gh calls:\n%s", tr)
	}
	if l, _ := filepath.Glob(filepath.Join(os.Getenv("ATHANOR_GITHUB_DIR"), ".acces.*")); len(l) != 0 {
		t.Fatalf("a temporary folder kept: %v", l)
	}
	// another account's token: not used
	a, _ := lireAccesGH()
	a.Login = "autre"
	j, _ := json.Marshal(a)
	ecrireEssai(t, fichierAccesGH(), string(j))
	if lireConnexionGH().AccesGarde || sessionGardeeGH() != nil {
		t.Fatal("another account's access used")
	}
	// revoked on GitHub: forgotten here, the creation signs in again
	a.Login, a.Jeton = "moi", "revoque_0123456789abcdefghij"
	j, _ = json.Marshal(a)
	ecrireEssai(t, fichierAccesGH(), string(j))
	_ = os.Truncate(trace, 0)
	projet2 := filepath.Join(t.TempDir(), "site2")
	_ = os.MkdirAll(filepath.Join(projet2, ".athanor"), 0o755)
	b.Reset()
	if code := lierGH([]string{projet2, "creer", "site2"}, &b); code != 0 || !strings.Contains(b.String(), "Signed in as moi.") {
		t.Fatalf("revoked: %d %s", code, b.String())
	}
	if _, err := os.Stat(fichierAccesGH()); err == nil {
		t.Fatal("a revoked access kept")
	}
	if tr, _ := os.ReadFile(trace); !strings.Contains(string(tr), "auth login") {
		t.Fatal("no sign-in after the revoked access")
	}
	// Forget, Disconnect
	b.Reset()
	garderAccesGH(nil, &b)
	b.Reset()
	if code := oublierAccesGH(nil, &b); code != 0 || lireConnexionGH().AccesGarde {
		t.Fatalf("forget: %s", b.String())
	}
	b.Reset()
	garderAccesGH(nil, &b)
	deconnecterGH(nil, &b)
	if _, err := os.Stat(fichierAccesGH()); err == nil {
		t.Fatal("the access kept after Disconnect")
	}
}
