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
esac
[ "$1" = api ] || exit 2
shift
case "$*" in
  user) echo '{"login":"moi","id":42,"name":"Moi Même"}' ;;
  user/emails) echo '[{"email":"m@x.org","verified":true},{"email":"no@x.org","verified":false},{"email":"pas un email","verified":true}]' ;;
  *"POST user/repos -f name=existe"*) echo '{"message":"Repository creation failed."}'; echo "name already exists on this account" >&2; exit 1 ;;
  *"POST user/repos -f name="*) n=${3#*name=}; n=$(printf '%s' "$*" | sed 's/.*name=\([^ ]*\).*/\1/'); echo "{\"full_name\":\"moi/$n\"}" ;;
  repos/moi/vieux) echo '{"full_name":"moi/vieux","permissions":{"admin":true},"archived":false,"default_branch":"dev"}' ;;
  repos/moi/pasadmin) echo '{"full_name":"moi/pasadmin","permissions":{"admin":false},"archived":false}' ;;
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
	t.Setenv("ATHANOR_GITHUB_SSH", "127.0.0.1:1") // nothing answers: the first push only fails
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
	out, code := lier("", projet, "creer", "site")
	if code != 0 {
		t.Fatalf("creer: %d\n%s", code, out)
	}
	for _, m := range []string{"Signed in as moi.", "Private repository created: moi/site", "Linked: moi/site.", "First push...", "No commit to push yet."} {
		if !strings.Contains(out, m) {
			t.Errorf("creer: %q missing in:\n%s", m, out)
		}
	}
	reg, _ := os.ReadFile(filepath.Join(d, "reglages"))
	r, lie := lireReglagesGH(d)
	if !lie || r.Depot != "moi/site" || r.Branche != "main" || !r.Auto || r.Compte != "moi" || r.CleID != "777" || r.LieLe == "" {
		t.Fatalf("reglages: %+v %v\n%s", r, lie, reg)
	}
	if st, err := os.Stat(filepath.Join(d, "cle")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("the key: %v %v", st, err)
	}
	kh, _ := os.ReadFile(filepath.Join(d, "known_hosts"))
	if string(kh) != "[127.0.0.1]:1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n" {
		t.Errorf("known_hosts: %q", kh)
	}
	compte, _ := os.ReadFile(filepath.Join(d, "compte"))
	if string(compte) != "login=moi\nid=42\nnom=Moi Même\nemail=m@x.org\n" {
		t.Errorf("compte: %q", compte)
	}
	if p, _ := os.ReadFile(filepath.Join(d, "projet")); string(p) != projet+"\n" {
		t.Errorf("projet: %q", p)
	}
	tr, _ := os.ReadFile(trace)
	ts := string(tr)
	if !strings.Contains(ts, "token=none") || !strings.Contains(ts, "auth login --hostname github.com --git-protocol ssh --skip-ssh-key --web --scopes user:email --insecure-storage") ||
		!strings.Contains(ts, "auth logout --hostname github.com") || !strings.Contains(ts, "title=Athanor (mon site)") || !strings.Contains(ts, "-F read_only=false") {
		t.Errorf("gh calls:\n%s", ts)
	}
	// the sign-in's folder is gone
	if l, _ := filepath.Glob(filepath.Join(donnees, ".connexion.*")); len(l) != 0 {
		t.Errorf("sign-in kept: %v", l)
	}
	// the key's comment: athanor-ACCOUNT-FOLDER, other characters as dashes
	if pub, _ := os.ReadFile(filepath.Join(d, "cle.pub")); !strings.HasSuffix(strings.TrimSpace(string(pub)), " athanor-moi-mon-site") {
		t.Errorf("cle.pub: %q", pub)
	}

	// a name taken; not an administrator
	if out, code := lier("", projet, "creer", "existe"); code != 1 || !strings.Contains(out, `You already have a repository named existe: choose "Use an existing repository" (moi/existe), or another name.`) {
		t.Errorf("existe: %d %q", code, out)
	}
	if out, code := lier("", projet, "existant", "moi/pasadmin"); code != 1 || !strings.Contains(out, "You are not an administrator of moi/pasadmin") {
		t.Errorf("pasadmin: %d %q", code, out)
	}
	// an existing repository with workflows and secrets: the user decides
	if out, code := lier("no\n", projet, "existant", "moi/vieux"); code != 1 || !strings.Contains(out, "WARNING: moi/vieux runs GitHub Actions workflows (1) and has Actions secrets (2 of its own, 0 from its organization).") || !strings.Contains(out, "Not linked.") {
		t.Errorf("vieux, no: %d %q", code, out)
	}
	_ = os.MkdirAll(filepath.Join(d, "miroir.git"), 0o755)
	_ = os.WriteFile(filepath.Join(d, "dernier.json"), []byte("{}"), 0o644)
	_ = os.Truncate(trace, 0)
	out, code = lier("yes\n", projet, "existant", "moi/vieux", "0")
	if code != 0 || !strings.Contains(out, "Repository: moi/vieux") || !strings.Contains(out, "Previous key removed from moi/site.") {
		t.Fatalf("vieux, yes: %d\n%s", code, out)
	}
	r, _ = lireReglagesGH(d)
	if r.Depot != "moi/vieux" || r.Branche != "dev" || r.Auto {
		t.Errorf("vieux: %+v", r)
	}
	if tr, _ := os.ReadFile(trace); !strings.Contains(string(tr), "-X DELETE repos/moi/site/keys/777") {
		t.Errorf("previous key not removed:\n%s", tr)
	}
	if existeGH(filepath.Join(d, "miroir.git")) {
		t.Errorf("another repository: the push folder of the previous one must go")
	}
}
