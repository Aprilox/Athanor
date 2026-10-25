//go:build !windows

package main

// athanor github lier with a fake gh: the sign-in in a temporary folder deleted after, the repository
// created or checked, the deploy key, GitHub's SSH keys, the settings, the previous key removed.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const faux_gh = `#!/bin/sh
echo "$*" >> "$TRACE_GH"
case "$1 $2" in
  "auth login")
    case "$*" in *--help*) echo "  --skip-ssh-key  --insecure-storage"; exit 0 ;; esac
    echo "config=$GH_CONFIG_DIR token=${GH_TOKEN:-none}" >> "$TRACE_GH"
    : > "$GH_CONFIG_DIR/hosts.yml"; exit 0 ;;
  "auth logout") cat > /dev/null; exit 0 ;;
  "auth refresh") exit 0 ;;
esac
[ "$1" = api ] || exit 2
shift
case "$*" in
  user) echo '{"login":"moi","id":42,"name":"Moi Même"}' ;;
  user/emails) echo '[{"email":"m@x.org","verified":true},{"email":"no@x.org","verified":false},{"email":"pas un email","verified":true}]' ;;
  *"POST user/repos -f name=existe"*) echo '{"message":"Repository creation failed."}'; echo "name already exists on this account" >&2; exit 1 ;;
  *"POST user/repos -f name="*) n=${3#*name=}; n=$(printf '%s' "$*" | sed 's/.*name=\([^ ]*\).*/\1/'); echo "{\"full_name\":\"moi/$n\"}" ;;
  repos/moi/vieux) echo '{"full_name":"moi/vieux","permissions":{"admin":true},"archived":false,"default_branch":"dev"}' ;;
  repos/moi/pasadmin) echo '{"full_name":"moi/pasadmin","permissions":{"admin":false,"push":false},"archived":false}' ;;
  repos/ami/projet) echo '{"full_name":"ami/projet","permissions":{"admin":false,"push":true},"archived":false,"default_branch":"main"}' ;;
  repos/ami/projet/actions/workflows) echo '{"total_count":0}' ;;
  repos/ami/projet/actions/secrets) echo '{"total_count":0}' ;;
  repos/ami/projet/actions/organization-secrets) echo '{"total_count":0}' ;;
  *"POST user/keys"*) echo '{"id":888}' ;;
  *"DELETE user/keys/"*) exit 0 ;;
  repos/moi/vieux/actions/workflows) echo '{"total_count":1}' ;;
  repos/moi/vieux/actions/secrets) echo '{"total_count":2}' ;;
  repos/moi/vieux/actions/organization-secrets) echo '{"total_count":0}' ;;
  *"POST repos/"*"/keys"*) echo '{"id":777}' ;;
  *"DELETE repos/"*) exit 0 ;;
  meta) echo '{"ssh_keys":["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl","pas une cle"]}' ;;
  *) echo "unknown: $*" >&2; exit 1 ;;
esac
`

func TestLierGitHub(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	bin := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(faux_gh), 0o755); err != nil {
		t.Fatal(err)
	}
	donnees := filepath.Join(t.TempDir(), "github")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TRACE_GH", trace)
	t.Setenv("ATHANOR_GITHUB_DIR", donnees)
	t.Setenv("ATHANOR_GITHUB_SSH", "127.0.0.1:1") // nothing answers: reaching GitHub only fails
	t.Setenv("ATHANOR_GITHUB_SSH_SECOURS", "")
	t.Setenv("GH_TOKEN", "jeton-de-l-utilisateur")
	pauseLierGH = 0
	projet := filepath.Join(t.TempDir(), "mon site")
	_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
	stdinAvant := os.Stdin
	defer func() { os.Stdin = stdinAvant }()
	lier := func(entree string, args ...string) (string, int) {
		f := filepath.Join(t.TempDir(), "stdin")
		_ = os.WriteFile(f, []byte(entree), 0o644)
		in, _ := os.Open(f)
		defer in.Close()
		os.Stdin = in
		var b bytes.Buffer
		code := lierGH(args, &b)
		return b.String(), code
	}
	d := dossierGH(projet)
	if out, code := lier("", projet, "creer", "a b"); code != 2 || !strings.Contains(out, "Invalid repository name") {
		t.Fatalf("invalid name: %d %q", code, out)
	}
	if out, code := lier("", projet, "creer", "site", "1", "a..b"); code != 2 || !strings.Contains(out, "Invalid branch name: a..b") {
		t.Fatalf("invalid branch: %d %q", code, out)
	}
	// no project is linked without this computer's connection (never a key for one repository)
	for _, a := range [][]string{{projet, "creer", "site"}, {projet, "existant", "moi/vieux"}} {
		if out, code := lier("", a...); code != 1 || !strings.Contains(out, "not connected to GitHub") {
			t.Fatalf("not connected %v: %d %q", a, code, out)
		}
	}
	if tr, _ := os.ReadFile(trace); len(tr) != 0 {
		t.Fatalf("GitHub reached while not connected:\n%s", tr)
	}
	var b bytes.Buffer
	if code := connecterGH(nil, &b); code != 0 {
		t.Fatal(b.String())
	}
	_ = os.Truncate(trace, 0)
	// a new repository: one sign-in for GitHub's API, then the connection pushes to it
	out, code := lier("", projet, "creer", "site")
	if code != 0 {
		t.Fatalf("creer: %d\n%s", code, out)
	}
	for _, m := range []string{"Signed in as moi.", "Private repository created: moi/site", "Linked: moi/site, with this computer's connection to GitHub (moi).", "First push...", "No commit to push yet."} {
		if !strings.Contains(out, m) {
			t.Errorf("creer: %q missing in:\n%s", m, out)
		}
	}
	r, lie := lireReglagesGH(d)
	if !lie || r.Depot != "moi/site" || r.Branche != "main" || !r.Auto || r.Compte != "moi" || !r.Connexion || r.CleID != "" || existeGH(filepath.Join(d, "cle")) {
		t.Fatalf("reglages: %+v %v", r, lie)
	}
	if c, _ := os.ReadFile(filepath.Join(d, "compte")); !strings.Contains(string(c), "login=moi") {
		t.Errorf("compte: %q", c)
	}
	if p, _ := os.ReadFile(filepath.Join(d, "projet")); string(p) != projet+"\n" {
		t.Errorf("projet: %q", p)
	}
	tr, _ := os.ReadFile(trace)
	ts := string(tr)
	if !strings.Contains(ts, "token=none") || !strings.Contains(ts, "auth login --hostname github.com --git-protocol ssh --skip-ssh-key --web --scopes user:email --insecure-storage") ||
		!strings.Contains(ts, "auth logout --hostname github.com") || strings.Contains(ts, "/keys") {
		t.Errorf("gh calls:\n%s", ts)
	}
	if l, _ := filepath.Glob(filepath.Join(donnees, ".connexion.*")); len(l) != 0 {
		t.Errorf("sign-in kept: %v", l)
	}
	// a name taken
	if out, code := lier("", projet, "creer", "existe"); code != 1 || !strings.Contains(out, `You already have a repository named existe: choose "Use one of my repositories" (moi/existe), or another name.`) {
		t.Errorf("existe: %d %q", code, out)
	}
	// an existing repository: no sign-in at all (lier-compte); GitHub unreachable here, said so
	_ = os.Truncate(trace, 0)
	if out, code := lier("", projet, "existant", "moi/vieux"); code != 1 || !strings.Contains(out, "GitHub cannot be reached") {
		t.Errorf("existant: %d %q", code, out)
	}
	if tr, _ := os.ReadFile(trace); strings.Contains(string(tr), "auth login") {
		t.Errorf("a sign-in for an existing repository:\n%s", tr)
	}
	if r, _ := lireReglagesGH(d); r.Depot != "moi/site" {
		t.Errorf("the settings of before not kept: %+v", r)
	}
	b.Reset()
	delierGH([]string{projet}, &b)
	if !strings.Contains(b.String(), "this computer's connection to GitHub stays") || !lireConnexionGH().Connecte {
		t.Errorf("unlink of a project using the connection: %q", b.String())
	}
}

// TestConnecterGitHub: the computer's connection: one sign-in asking for the account's keys, a key
// "Athanor (PC)" on the account, kept; the sign-in itself deleted.
func TestConnecterGitHub(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	bin := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(faux_gh), 0o755); err != nil {
		t.Fatal(err)
	}
	donnees := filepath.Join(t.TempDir(), "github")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TRACE_GH", trace)
	t.Setenv("ATHANOR_GITHUB_DIR", donnees)
	t.Setenv("ATHANOR_GITHUB_SSH_SECOURS", "")
	var b bytes.Buffer
	if code := connecterGH(nil, &b); code != 0 || !strings.Contains(b.String(), "Connected as moi.") {
		t.Fatalf("connecter: %d %s", code, b.String())
	}
	c := lireConnexionGH()
	if !c.Connecte || c.Login != "moi" || c.Nom != "Moi Même" || c.CleID != "888" {
		t.Fatalf("connection: %+v", c)
	}
	if st, err := os.Stat(filepath.Join(dossierConnexionGH(), "cle")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the key: %v %v", st, err)
	}
	tr, _ := os.ReadFile(trace)
	if !strings.Contains(string(tr), "--scopes user:email,admin:public_key") || !strings.Contains(string(tr), "-X POST user/keys -f title=Athanor (") ||
		!strings.Contains(string(tr), "auth logout") {
		t.Fatalf("gh calls:\n%s", tr)
	}
	if l, _ := filepath.Glob(filepath.Join(donnees, ".connexion.*")); len(l) != 0 {
		t.Fatalf("sign-in kept: %v", l)
	}
	b.Reset()
	connexionGHCommande(nil, &b)
	if !strings.Contains(b.String(), `"connecte":true`) || !strings.Contains(b.String(), `"login":"moi"`) || strings.Contains(b.String(), "888") {
		t.Fatalf("state: %s", b.String())
	}
}
