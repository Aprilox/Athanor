package main

// athanor github etat | pousser | publie | auto | branche | delier | suivre | identite | regler-identite |
// identite-pilote | tester | enregistrer | lier: what system/github.sh did, in Go on every system (WSL,
// Linux, macOS, native Windows), the same files in the same place (~/.local/share/athanor/github, the
// user profile's in native Windows, as Git Bash named it), the same messages and exit codes.
//
// What Claude can reach stays the project's repository only: the key, the repository's name and the
// settings live in the user's home, which Claude's sandbox cannot read; pushes run outside the sandbox
// from a private git folder (no setting of the project's .git is ever used), GitHub's identity is
// checked against the keys it published, the push is never forced, and the commits are scanned for
// secrets first.

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

func donneesGitHub() string {
	if d := os.Getenv("ATHANOR_GITHUB_DIR"); d != "" {
		return cheminNatifPilote(d)
	}
	x := cheminNatifPilote(os.Getenv("XDG_DATA_HOME"))
	if x == "" {
		h := cheminNatifPilote(os.Getenv("HOME"))
		if h == "" && runtime.GOOS == "windows" {
			// Git Bash's HOME: the user profile
			h, _ = os.UserHomeDir()
		}
		x = h + "/.local/share"
	}
	return x + "/athanor/github"
}

// cheminProjetGH: the logical path the pilot uses (cd + pwd), as given when the folder is gone.
func cheminProjetGH(p string) (string, bool) {
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", false
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		if !filepath.IsAbs(p) {
			base := os.Getenv("PWD")
			if base == "" {
				base, _ = os.Getwd()
			}
			p = filepath.Join(base, p)
		}
		return filepath.Clean(p), true
	}
	return p, true
}

// dossierGH: the project's folder of settings, named after its path as github.sh saw it (in native
// Windows, Git Bash's form: /c/Users/…).
func dossierGH(projet string) string {
	h := sha1.Sum([]byte(cleProjetGH(projet)))
	return donneesGitHub() + "/" + hex.EncodeToString(h[:])[:16]
}

var (
	reDepotGH   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
	reBrancheGH = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)
	reCompteGH  = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}$`)
	reLieLeGH   = regexp.MustCompile(`^[0-9T:+-]{10,30}$`)
	reCleIDGH   = regexp.MustCompile(`^[0-9]{1,15}$`)
	reShaGH     = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	reEmailGH   = regexp.MustCompile(`^[^ \t\n\v\f\r<>@"]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)
	reReseauGH  = regexp.MustCompile(`(?i)connection timed out|connection refused|network is unreachable|could not resolve|no route to host|connection reset|operation timed out`)
)

func depotValideGH(d string) bool {
	return reDepotGH.MatchString(d) && !strings.HasSuffix(d, "/.") && !strings.HasSuffix(d, "/..")
}

func brancheValideGH(b string) bool {
	if !reBrancheGH.MatchString(b) || strings.Contains(b, "..") || strings.Contains(b, "//") || strings.HasSuffix(b, "/") ||
		strings.HasSuffix(b, ".lock") || strings.Contains(b, ".lock/") {
		return false
	}
	return commande("git", "check-ref-format", "--branch", b).Run() == nil
}

// lignesCleValeur: KEY=value lines as bash's `while IFS='=' read -r k v` reads them (a last line without
// its newline is not read; one trailing "=" of the value is dropped, two are kept).
func lignesCleValeur(f string) [][2]string {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	var out [][2]string
	lignes := strings.Split(string(b), "\n")
	for _, l := range lignes[:len(lignes)-1] {
		l = strings.ReplaceAll(l, "\x00", "")
		k, v, _ := strings.Cut(l, "=")
		if strings.HasSuffix(v, "=") && !strings.HasSuffix(v, "==") {
			v = v[:len(v)-1]
		}
		out = append(out, [2]string{k, v})
	}
	return out
}

type reglagesGH struct {
	Depot, Branche, Compte, LieLe, CleID string
	Auto                                 bool
	Atelier                              bool // the workshop shared too (outil_atelier_partage.go)
	Equipe                               bool // several computers on the repository (pcs.go)
}

func existeGH(p string) bool { _, err := os.Stat(p); return err == nil }

func lireReglagesGH(d string) (reglagesGH, bool) {
	r := reglagesGH{Auto: true}
	if !existeGH(d + "/reglages") {
		return r, false
	}
	for _, kv := range lignesCleValeur(d + "/reglages") {
		k, v := kv[0], kv[1]
		switch k {
		case "depot":
			if depotValideGH(v) {
				r.Depot = v
			}
		case "branche":
			if brancheValideGH(v) {
				r.Branche = v
			}
		case "auto":
			if v == "0" {
				r.Auto = false
			}
		case "atelier":
			r.Atelier = v == "1"
		case "equipe":
			r.Equipe = v == "1"
		case "compte":
			if reCompteGH.MatchString(v) {
				r.Compte = v
			}
		case "lie_le":
			if reLieLeGH.MatchString(v) {
				r.LieLe = v
			}
		case "cle_id":
			if reCleIDGH.MatchString(v) {
				r.CleID = v
			}
		}
	}
	st, err := os.Stat(d + "/known_hosts")
	cle, errc := os.Stat(d + "/cle")
	return r, r.Depot != "" && errc == nil && cle.Mode().IsRegular() && err == nil && st.Size() > 0
}

func ecrireReglagesGH(d string, r reglagesGH) error {
	auto := "0"
	if r.Auto {
		auto = "1"
	}
	t, err := os.CreateTemp(d, ".reglages.")
	if err != nil {
		return err
	}
	fmt.Fprintf(t, "depot=%s\nbranche=%s\nauto=%s\ncompte=%s\nlie_le=%s\ncle_id=%s\n", r.Depot, r.Branche, auto, r.Compte, r.LieLe, r.CleID)
	if r.Atelier {
		fmt.Fprintln(t, "atelier=1")
	}
	if r.Equipe {
		fmt.Fprintln(t, "equipe=1")
	}
	t.Close()
	return os.Rename(t.Name(), d+"/reglages")
}

// verrouGH: d/verrou opened and locked (as github.sh's flock); nil after attente.
func verrouGH(d string, attente time.Duration) *os.File {
	return verrouFichierGH(d+"/verrou", attente)
}

// ---------------------------------------------------------------------------- the result of the last push

type noteGitHub struct {
	args     []string
	fichiers []string // "place<TAB>kind"
	sha      string
}

func remplirGH(m string, args []string) string {
	for i, a := range args {
		m = strings.ReplaceAll(m, fmt.Sprintf("{%d}", i), a)
	}
	return m
}

// noterGH: dernier.json {quand, resultat, modele, args, message, commit, sha, fichiers}; the message.
func noterGH(d, resultat, modele, commit string, n noteGitHub) string {
	message := remplirGH(modele, n.args)
	type fichier struct {
		F     string `json:"f"`
		Genre string `json:"genre"`
	}
	fs := []fichier{}
	for _, l := range n.fichiers {
		if l == "" {
			continue
		}
		p := strings.Split(l, "\t")
		g := ""
		if len(p) > 1 {
			g = p[1]
		}
		fs = append(fs, fichier{p[0], g})
	}
	args := n.args
	if args == nil {
		args = []string{}
	}
	v := struct {
		Quand    string    `json:"quand"`
		Resultat string    `json:"resultat"`
		Modele   string    `json:"modele"`
		Args     []string  `json:"args"`
		Message  string    `json:"message"`
		Commit   string    `json:"commit"`
		Sha      string    `json:"sha"`
		Fichiers []fichier `json:"fichiers"`
	}{time.Now().Format("2006-01-02T15:04:05"), resultat, modele, args, message, commit, n.sha, fs}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	_ = e.Encode(v)
	if t, err := os.CreateTemp(d, ".dernier."); err == nil {
		t.Write(b.Bytes())
		t.Close()
		_ = os.Rename(t.Name(), d+"/dernier.json")
	}
	return message
}

// ---------------------------------------------------------------------------- git

var envGitRetire = map[string]bool{"GIT_SSH_COMMAND": true, "GIT_SSH": true, "GIT_SSH_VARIANT": true, "GIT_PROXY_COMMAND": true,
	"GIT_CONFIG_PARAMETERS": true, "GIT_CONFIG_COUNT": true, "GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_OBJECT_DIRECTORY": true,
	"GIT_COMMON_DIR": true, "GIT_WORK_TREE": true, "GIT_NAMESPACE": true, "GIT_DIR": true}

// gmGH: git on the private folder: no system or user configuration, nothing from the project's .git.
func gmGH(miroir string, extra []string, args ...string) *exec.Cmd {
	var env []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if !envGitRetire[k] {
			env = append(env, e)
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ASKPASS=/bin/false",
		"SSH_ASKPASS=/bin/false", "SSH_ASKPASS_REQUIRE=never", "GIT_DIR="+miroir)
	env = append(env, extra...)
	c := commande("git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "credential.helper=",
		"-c", "core.askPass=", "-c", "core.quotePath=false", "-c", "protocol.allow=never", "-c", "push.recurseSubmodules=no",
		"-c", "push.gpgSign=false"}, args...)...)
	c.Env = env
	return c
}

func sortieGH(c *exec.Cmd) (string, error) {
	out, err := c.Output()
	return strings.TrimRight(string(out), "\n"), err
}

// gProjetGH: git on the project's .git with its automatic commands off (safety.sh's g).
func gProjetGH(gd string, args ...string) (string, error) {
	c := commande("git", append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "diff.external=",
		"-c", "core.pager=cat", "-c", "core.editor=true", "-c", "sequence.editor=true", "-c", "commit.gpgsign=false",
		"-c", "tag.gpgsign=false", "-c", "core.sshCommand=false", "-c", "protocol.allow=never", "-c", "credential.helper="}, args...)...)
	c.Env = append(os.Environ(), "GIT_DIR="+gd, "GIT_TERMINAL_PROMPT=0")
	return sortieGH(c)
}

func estLienGH(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

func estDossierGH(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

// lireProjetGH: the project's own repository, and nothing else (a gitfile, a commondir file, links or
// alternates would make the pilot read, then push, another repository). sha, objects, or the reason:
// "absent" (no repository or no commit yet) | "detourne".
func lireProjetGH(p string) (sha, objets, raison string) {
	gd := p + "/.git"
	if !existeGH(gd) && !estLienGH(gd) {
		return "", "", "absent"
	}
	if !estDossierGH(gd) || estLienGH(gd) || existeGH(gd+"/commondir") || estLienGH(gd+"/commondir") ||
		!estDossierGH(gd+"/objects") || estLienGH(gd+"/objects") ||
		existeGH(gd+"/objects/info/alternates") || estLienGH(gd+"/objects/info/alternates") || strings.Contains(gd, "\n") {
		return "", "", "detourne"
	}
	for _, r := range []string{"HEAD", "packed-refs", "refs", "objects"} {
		lien := false
		_ = filepath.WalkDir(gd+"/"+r, func(_ string, e fs.DirEntry, err error) error {
			if err == nil && e.Type()&fs.ModeSymlink != 0 {
				lien = true
				return filepath.SkipAll
			}
			return nil
		})
		if lien {
			return "", "", "detourne"
		}
	}
	s, _ := gProjetGH(gd, "rev-parse", "--verify", "-q", "HEAD^{commit}")
	if !reShaGH.MatchString(s) {
		return "", "", "absent"
	}
	return s, gd + "/objects", ""
}

// coupeOctets: cut -c1-n (bytes, as GNU cut counts them).
func coupeOctets(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---------------------------------------------------------------------------- secrets

// analyserGH: the "place<TAB>kind" lines of the commits to push (what was already pushed is not scanned
// again); false when the scan could not run to the end.
func analyserGH(miroir, sha string, partage bool) ([]string, bool) {
	// what was pushed from here, and what GitHub already has (the other computers' commits, fetched): public
	// already, a secret there is not this push's to stop
	plage := []string{sha, "--not", "--glob=refs/athanor/pousse/*", "--glob=" + refsDistantPC + "*"}
	passer := func(mode string, args ...string) (string, bool) {
		c := gmGH(miroir, nil, args...)
		out, err := c.StdoutPipe()
		if err != nil || c.Start() != nil {
			return "", false
		}
		var b bytes.Buffer
		code := scannerSecretsAtelier(mode, out, &b, partage)
		_, _ = io.Copy(io.Discard, out)
		if c.Wait() != nil || code != 0 {
			return "", false
		}
		return strings.TrimRight(b.String(), "\n"), true
	}
	m, ok1 := passer("messages", append([]string{"log", "--format=%x01%H%n%B"}, plage...)...)
	d, ok2 := passer("diff", append([]string{"log", "-p", "--text", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames",
		"--diff-merges=first-parent", "--format=%x01%H", "-U0"}, plage...)...)
	if !ok1 || !ok2 || !strings.HasSuffix("\n"+m, "\n#FIN#") || !strings.HasSuffix("\n"+d, "\n#FIN#") {
		return nil, false
	}
	var out []string
	for _, l := range strings.Split(d+"\n"+m, "\n") {
		if l != "#FIN#" && l != "" {
			out = append(out, l)
		}
	}
	return out, true
}

func consigneSecretsGH(p, base string, secrets []string) error {
	racine := ""
	if r, err := gProjetGH(p+"/.git", "rev-list", "--max-parents=0", "HEAD"); err == nil && r != "" {
		l := strings.Split(r, "\n")
		racine = l[len(l)-1]
	}
	var b strings.Builder
	b.WriteString("# Secrets found before the GitHub push\n\n")
	b.WriteString("The pilot did not push to GitHub: the commits not pushed yet contain what looks like a secret. A secret pushed once stays readable in the history, even after the file is deleted.\n\n")
	b.WriteString("Found (the value itself is not repeated here):\n")
	for _, l := range secrets {
		f, genre, _ := strings.Cut(l, "\t") // read -r f genre, IFS a tab
		genre = strings.Trim(genre, "\t")
		if f != "" {
			fmt.Fprintf(&b, "- `%s` — %s\n", f, genre)
		}
	}
	b.WriteString("\nFix it before any other work (these commits were never pushed: rewriting them is safe):\n")
	b.WriteString("1. Take each secret out of the code: read it from an environment variable or from an ignored file (`.env`, listed in `.gitignore`), with a placeholder in `.env.example`. A file that must not be published at all (`.env`, a private key, the `.athanor` workshop) leaves git: `git rm -r --cached PATH` and add it to `.gitignore`. A secret in a commit message goes away with the rewrite below.\n")
	if base != "" {
		fmt.Fprintf(&b, "2. Rewrite the unpushed commits into one clean commit: `git reset --soft %s`, then `git commit -m \"...\"` (GitHub already has %s and what comes before it).\n", base, base)
	} else {
		if racine == "" {
			racine = "<first commit>"
		}
		fmt.Fprintf(&b, "2. Nothing was pushed yet: rewrite the whole history into one clean commit: `git reset --soft %s`, then `git commit --amend -m \"...\"`.\n", racine)
	}
	plage := ""
	if base != "" {
		plage = base + ".."
	}
	fmt.Fprintf(&b, "3. Check that `git log -p %sHEAD` no longer shows any secret, note it in `.athanor/LOG.md`, then delete this file. The pilot pushes at the end of the cycle.\n\n", plage)
	b.WriteString("If it is not a real secret (a test value, a documented example), leave the code as it is, say so in `.athanor/BLOCKERS.md` and delete this file: the user can push anyway from the application.\n")
	return fichierAtelier(p, "SECRETS_FOUND.md", false, strings.NewReader(b.String()))
}

// ---------------------------------------------------------------------------- push

func preparerMiroirGH(miroir, objets string) error {
	if !estDossierGH(miroir) {
		if commande("git", "init", "-q", "--bare", miroir).Run() != nil {
			return fmt.Errorf("init")
		}
		c := commande("git", "config", "gc.auto", "0")
		c.Env = append(os.Environ(), "GIT_DIR="+miroir)
		if c.Run() != nil {
			return fmt.Errorf("config")
		}
	}
	if err := os.MkdirAll(miroir+"/objects/info", 0o755); err != nil {
		return err
	}
	return os.WriteFile(miroir+"/objects/info/alternates", []byte(filepath.ToSlash(objets)+"\n"), 0o644)
}

func commandeSSHGH(d, port string) string {
	nul := "/dev/null"
	ssh := programmeGH("ssh")
	if runtime.GOOS == "windows" && !sshDeGitGH(ssh) {
		nul = "NUL" // Windows' own OpenSSH
	}
	return fmt.Sprintf("'%s' -F %s -p %s -i '%s' -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile='%s' -o GlobalKnownHostsFile=%s -o ConnectTimeout=20 -o ServerAliveInterval=15 -o ServerAliveCountMax=4 -o LogLevel=ERROR",
		filepath.ToSlash(ssh), nul, port, filepath.ToSlash(d+"/cle"), filepath.ToSlash(d+"/known_hosts"), nul)
}

func contientGH(s, motif string) bool { return regexp.MustCompile("(?i)" + motif).MatchString(s) }

// pousserGH: github.sh pousser; prints what it says, returns its exit code.
func pousserGH(args []string, w io.Writer) int {
	projet, auto, malgre := "", false, ""
	for _, a := range args {
		switch {
		case a == "--auto":
			auto = true
		case strings.HasPrefix(a, "--malgre="):
			malgre = strings.TrimPrefix(a, "--malgre=")
		default:
			projet = a
		}
	}
	projet, ok := cheminProjetGH(projet)
	if !ok {
		fmt.Fprintln(w, "Usage: athanor github pousser PROJECT")
		return 1
	}
	d := dossierGH(projet)
	if !existeGH(d + "/reglages") {
		fmt.Fprintln(w, "Not linked to GitHub.")
		return 3
	}
	if _, err := os.OpenFile(d+"/verrou", os.O_WRONLY|os.O_CREATE, 0o644); err != nil {
		fmt.Fprintln(w, "Not linked to GitHub.")
		return 3
	}
	v := verrouGH(d, 300*time.Second)
	if v == nil {
		fmt.Fprintln(w, "Another push is under way.")
		return 1
	}
	defer v.Close()
	r, lie := lireReglagesGH(d) // read under the lock: a link or an unlink made meanwhile is seen
	if !lie {
		fmt.Fprintln(w, "Not linked to GitHub.")
		return 3
	}
	if auto && !r.Auto {
		fmt.Fprintln(w, "Automatic push is off.")
		return 3
	}
	sha, objets, raison := lireProjetGH(projet)
	if raison == "absent" {
		noterGH(d, "attente", "No commit yet: the first push happens after the first milestone.", "", noteGitHub{})
		fmt.Fprintln(w, "No commit to push yet.")
		return 3
	} else if raison != "" {
		noterGH(d, "erreur", "The project's .git folder points elsewhere (a link, or another repository's objects): nothing was pushed. Check that folder, or ask Claude to repair the project's git repository.", "", noteGitHub{})
		fmt.Fprintln(w, "GitHub: not pushed, the project's .git folder points elsewhere.")
		return 1
	}
	branche := firstNonEmpty(r.Branche, "main")
	if malgre != "" {
		// "push anyway" pushes exactly the commit the user reviewed, never a newer one
		bloque, _ := os.ReadFile(d + "/bloque")
		if !reShaGH.MatchString(malgre) || malgre != strings.TrimRight(string(bloque), "\n") {
			fmt.Fprintln(w, "This push is no longer the one that was blocked: refresh.")
			return 1
		}
		sha = malgre
	}
	miroir := d + "/miroir.git"
	if preparerMiroirGH(miroir, objets) != nil {
		fmt.Fprintln(w, "Cannot prepare the push folder.")
		return 1
	}
	// pushed commits that no longer exist (history rewritten) are forgotten, never an error
	if refs, err := sortieGH(gmGH(miroir, nil, "for-each-ref", "--format=%(refname)", "refs/athanor/pousse/")); err == nil {
		for _, ref := range strings.Split(refs, "\n") {
			if ref == "" {
				continue
			}
			s, _ := sortieGH(gmGH(miroir, nil, "rev-parse", "-q", "--verify", ref))
			if gmGH(miroir, nil, "cat-file", "-e", s+"^{commit}").Run() != nil {
				_ = gmGH(miroir, nil, "update-ref", "-d", ref).Run()
			}
		}
	}
	resume, _ := sortieGH(gmGH(miroir, nil, "log", "-1", "--format=%h %s", sha))
	if i := strings.IndexByte(resume, '\n'); i >= 0 {
		resume = resume[:i]
	}
	resume = coupeOctets(resume, 120)
	if resume == "" {
		noterGH(d, "erreur", "The project's commits cannot be read.", "", noteGitHub{})
		fmt.Fprintln(w, "The project's commits cannot be read.")
		return 1
	}
	base, _ := sortieGH(gmGH(miroir, nil, "rev-parse", "--verify", "-q", "refs/athanor/pousse/"+branche))
	if base == sha {
		noterGH(d, "ok", "Already up to date on GitHub.", resume, noteGitHub{})
		_ = fichierAtelier(projet, "SECRETS_FOUND.md", true, nil)
		fmt.Fprintf(w, "GitHub: already up to date (%s, %s).\n", r.Depot, branche)
		return 0
	}
	if malgre == "" {
		trouves, ok := analyserGH(miroir, sha, r.Atelier)
		if !ok {
			noterGH(d, "erreur", "The secret check could not run to the end: nothing was pushed.", resume, noteGitHub{})
			fmt.Fprintln(w, "GitHub: not pushed, the secret check failed.")
			return 1
		}
		if len(trouves) > 0 {
			n := noteGitHub{fichiers: trouves, sha: sha}
			var secrets []string
			for _, l := range trouves {
				if !strings.HasSuffix(l, "\tGitHub Actions workflow") {
					secrets = append(secrets, l)
				}
			}
			if len(secrets) > 0 {
				noterGH(d, "bloque", "Not pushed: possible secrets in the commits (see the list). Claude removes them on its next cycle; if they are not real secrets, push anyway.", resume, n)
				// Claude is asked once per set of findings (a false positive it reported is not asked again)
				h := sha1.Sum([]byte(strings.Join(secrets, "\n")))
				empreinte := hex.EncodeToString(h[:])[:16]
				signale, _ := os.ReadFile(d + "/signale")
				if auto && strings.TrimRight(string(signale), "\n") != empreinte {
					if consigneSecretsGH(projet, base, secrets) == nil {
						_ = os.WriteFile(d+"/signale", []byte(empreinte+"\n"), 0o644)
					}
				}
			} else {
				// GitHub runs workflows with the repository's secrets: a change there waits for the user
				noterGH(d, "bloque", "Not pushed: these commits change GitHub Actions workflows (code GitHub runs on each push, with the repository's secrets). Check them, then push anyway.", resume, n)
			}
			_ = os.WriteFile(d+"/bloque", []byte(sha+"\n"), 0o644)
			var places strings.Builder
			for _, l := range trouves {
				f, _, _ := strings.Cut(l, "\t")
				places.WriteString(f + " ")
			}
			fmt.Fprintf(w, "GitHub: not pushed, to check: %s\n", places.String())
			return 4
		}
	}
	sortie, code := "", 1
	for _, dist := range distantsGH(d, r) { // GitHub's SSH port, then its HTTPS one (pcs.go)
		c := gmGH(miroir, nil, append(dist.extra, "push", "--porcelain", "--no-verify", dist.url, sha+":refs/heads/"+branche)...)
		out, err := c.CombinedOutput()
		sortie = strings.TrimRight(string(out), "\n")
		code = 0
		if err != nil {
			code = 1
		}
		journal := []byte(sortie + "\n")
		if len(journal) > 20000 {
			journal = journal[len(journal)-20000:]
		}
		_ = os.WriteFile(d+"/derniere-sortie.log", journal, 0o644) // private, for troubleshooting
		if !reReseauGH.MatchString(sortie) {
			break
		}
	}
	modele, resultat, ret := "", "erreur", 1
	n := noteGitHub{}
	if code == 0 {
		_ = gmGH(miroir, nil, "update-ref", "refs/athanor/pousse/"+branche, sha).Run()
		_ = fichierAtelier(projet, "SECRETS_FOUND.md", true, nil)
		os.Remove(d + "/signale")
		os.Remove(d + "/bloque")
		if regexp.MustCompile("(?m)^=\t").MatchString(sortie) {
			modele = "Already up to date on GitHub."
		} else {
			modele = "Pushed to {0} ({1})."
			n.args = []string{r.Depot, branche}
		}
		m := noterGH(d, "ok", modele, resume, n)
		fmt.Fprintf(w, "GitHub: %s (%s)\n", m, resume)
		return 0
	}
	switch {
	case contientGH(sortie, `GH006|protected branch`):
		modele, resultat, ret = "The branch {0} is protected on GitHub: choose another branch, or allow the push in the repository's settings.", "rejete", 5
		n.args = []string{branche}
	case contientGH(sortie, `GH001|large files|file size limit`):
		modele, resultat, ret = "A file is larger than GitHub's limit (100 MB): keep it out of git (.gitignore) or use Git LFS.", "rejete", 5
	case contientGH(sortie, `non-fast-forward|fetch first|\[rejected\]|stale info`):
		modele, resultat, ret = "GitHub's branch {0} has commits the project does not have. Athanor never overwrites them: choose another branch, or bring those commits into the project.", "rejete", 5
		n.args = []string{branche}
	case contientGH(sortie, `host key verification failed|remote host identification has changed`):
		modele = "GitHub's identity could not be verified: push refused. Link the project again to get GitHub's current keys."
	case contientGH(sortie, `permission denied|repository not found|read only|not authorized|access denied|denied to deploy key`):
		modele, resultat, ret = "GitHub refused Athanor's key for {0}: the key was removed from the repository, or the repository was deleted or renamed. Link the project again.", "rejete", 5
		n.args = []string{r.Depot}
	case reReseauGH.MatchString(sortie):
		modele = "GitHub cannot be reached (network). The next milestone tries again."
	default:
		var dernieres []string
		for _, l := range strings.Split(sortie, "\n") {
			if strings.TrimFunc(l, estEspacePosix) != "" {
				dernieres = append(dernieres, l)
			}
		}
		if len(dernieres) > 3 {
			dernieres = dernieres[len(dernieres)-3:]
		}
		resteSortie := ""
		if len(dernieres) > 0 {
			resteSortie = coupeOctets(strings.Join(dernieres, " ")+" ", 300)
		}
		modele = "Push failed: {0}"
		n.args = []string{resteSortie}
	}
	m := noterGH(d, resultat, modele, resume, n)
	fmt.Fprintf(w, "GitHub: %s\n", m)
	return ret
}

func secoursSSHGH() string {
	if v, ok := os.LookupEnv("ATHANOR_GITHUB_SSH_SECOURS"); ok {
		return v
	}
	return "ssh.github.com:443"
}

// publieGH: one of these files is in a commit already pushed to GitHub (0), else 1.
func publieGH(args []string) int {
	if len(args) < 2 {
		return 1
	}
	projet, ok := cheminProjetGH(args[0])
	if !ok {
		return 1
	}
	d := dossierGH(projet)
	miroir := d + "/miroir.git"
	if !estDossierGH(miroir) || estLienGH(miroir) {
		return 1
	}
	_, objets, raison := lireProjetGH(projet)
	if raison != "" {
		return 1
	}
	v := verrouGH(d, 60*time.Second)
	if v == nil {
		return 1
	}
	defer v.Close()
	if os.WriteFile(miroir+"/objects/info/alternates", []byte(filepath.ToSlash(objets)+"\n"), 0o644) != nil {
		return 1
	}
	out, _ := sortieGH(gmGH(miroir, []string{"GIT_LITERAL_PATHSPECS=1"}, append([]string{"log", "--glob=refs/athanor/pousse/*", "-1", "--format=%H", "--"}, args[1:]...)...))
	if out != "" {
		return 0
	}
	return 1
}

// ---------------------------------------------------------------------------- settings, unlinking

// atelierPartageGH: the project is linked to GitHub with its workshop shared.
func atelierPartageGH(projet string) bool {
	if p, ok := cheminProjetGH(projet); ok {
		projet = p
	}
	r, lie := lireReglagesGH(dossierGH(projet))
	return lie && r.Atelier
}

func reglageGH(cle string, args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		return 2
	}
	d := dossierGH(projet)
	if !existeGH(d + "/reglages") {
		fmt.Fprintln(w, "Not linked to GitHub.")
		return 3
	}
	v := verrouGH(d, 60*time.Second)
	if v != nil {
		defer v.Close()
	}
	r, lie := lireReglagesGH(d)
	if !lie {
		fmt.Fprintln(w, "Not linked to GitHub.")
		return 3
	}
	valeur := argGH(args, 1)
	switch cle {
	case "auto":
		r.Auto = valeur != "0"
	case "partage":
		// at once in .git/info/exclude (Claude's next commit already follows it); switched off, what was
		// shared leaves the index (the files stay in the folder; the next push removes them from GitHub)
		avant := r.Atelier
		r.Atelier = valeur == "1"
		if err := ecrireExclusionAtelier(projet, r.Atelier); err != nil {
			fmt.Fprintln(w, "Cannot write the project's .git/info/exclude: "+err.Error())
			return 1
		}
		if avant && !r.Atelier {
			_ = gitP(projet, nil, "rm", "-r", "--cached", "-q", "--ignore-unmatch", "--", ".athanor").Run()
		}
	case "branche":
		r.Branche = valeur
		if valeur == "" {
			r.Branche = "main"
		}
		if !brancheValideGH(r.Branche) {
			fmt.Fprintf(w, "Invalid branch name: %s\n", valeur)
			return 2
		}
		os.Remove(d + "/dernier.json")
	}
	if ecrireReglagesGH(d, r) != nil {
		return 1
	}
	fmt.Fprintln(w, "Saved.")
	return 0
}

func argGH(a []string, i int) string {
	if i < len(a) {
		return a[i]
	}
	return ""
}

func delierGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		return 2
	}
	d := dossierGH(projet)
	if !estDossierGH(d) {
		fmt.Fprintln(w, "Not linked to GitHub.")
		return 0
	}
	r, _ := lireReglagesGH(d)
	v := verrouGH(d, 300*time.Second)
	if v != nil {
		defer v.Close()
	}
	l, _ := os.ReadDir(d)
	for _, e := range l {
		if e.Name() != "identite" && e.Name() != "compte" { // the commit author stays
			_ = os.RemoveAll(d + "/" + e.Name())
		}
	}
	de := ""
	if r.Depot != "" {
		de = " from " + r.Depot
	}
	fmt.Fprintf(w, "Unlinked%s: the key is deleted from this PC (GitHub keeps an unusable entry under the repository's Settings > Deploy keys, which you can remove).\n", de)
	return 0
}

func suivreGH(args []string, w io.Writer) int {
	ancien, ok1 := cheminProjetGH(argGH(args, 0))
	nouveau, ok2 := cheminProjetGH(argGH(args, 1))
	if !ok1 || !ok2 {
		return 2
	}
	a, n := dossierGH(ancien), dossierGH(nouveau)
	if a == n || !estDossierGH(a) {
		return 0
	}
	if existeGH(n) {
		fmt.Fprintln(w, "The new folder already has a GitHub link: kept.")
		return 0
	}
	if os.Rename(a, n) != nil {
		return 1
	}
	if os.WriteFile(n+"/projet", []byte(cleProjetGH(nouveau)+"\n"), 0o644) != nil {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------- author of the commits

// longueurBash: ${#s}: characters in a UTF-8 locale, bytes otherwise.
func longueurBash(s string) int {
	for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if l := os.Getenv(v); l != "" {
			if m := strings.ToLower(l); strings.Contains(m, "utf-8") || strings.Contains(m, "utf8") {
				return utf8.RuneCountInString(s)
			}
			return len(s)
		}
	}
	return len(s)
}

func nomAuteurValideGH(s string) bool {
	return longueurBash(s) <= 100 && strings.ContainsFunc(s, func(r rune) bool { return !estEspacePosix(r) }) &&
		!strings.ContainsFunc(s, func(r rune) bool { return r == '<' || r == '>' || r < 0x20 || r == 0x7f })
}

func estEspacePosix(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r'
}

func emailValideGH(s string) bool {
	return longueurBash(s) <= 254 && reEmailGH.MatchString(s)
}

type identiteGH struct {
	nom, email     string
	sansCoauteur   bool
	identiteDefini bool
}

func lireIdentiteGH(d string) identiteGH {
	var id identiteGH
	for _, f := range []string{donneesGitHub() + "/identite-defaut", d + "/identite"} {
		if !existeGH(f) {
			continue
		}
		id = identiteGH{identiteDefini: id.identiteDefini} // the project's own choice replaces the default
		if f == d+"/identite" {
			id.identiteDefini = true
		}
		for _, kv := range lignesCleValeur(f) {
			switch kv[0] {
			case "nom":
				if nomAuteurValideGH(kv[1]) {
					id.nom = kv[1]
				}
			case "email":
				if emailValideGH(kv[1]) {
					id.email = kv[1]
				}
			case "sans_coauteur":
				if kv[1] == "1" {
					id.sansCoauteur = true
				}
			}
		}
	}
	return id
}

func gitGlobalGH(cle string) string {
	out, _ := commande("git", "config", "--global", cle).Output()
	return strings.TrimRight(string(out), "\n")
}

func identiteJSONGH(projet string) map[string]any {
	d := dossierGH(projet)
	id := lireIdentiteGH(d)
	login, numero, cnom := "", "", ""
	var emails []string
	if existeGH(d + "/compte") {
		for _, kv := range lignesCleValeur(d + "/compte") {
			switch kv[0] {
			case "login":
				login = kv[1]
			case "id":
				numero = kv[1]
			case "nom":
				cnom = kv[1]
			case "email":
				emails = append(emails, kv[1])
			}
		}
	}
	if login == "" {
		r, _ := lireReglagesGH(d)
		login = r.Compte
	}
	if reCompteGH.MatchString(login) && numero == "" {
		// linked before 2.8.0: the account number (public) gives the private noreply address
		numero = numeroCompteGH(login)
		if reCleIDGH.MatchString(numero) && estDossierGH(d) {
			var b strings.Builder
			fmt.Fprintf(&b, "login=%s\nid=%s\n", login, numero)
			if c, err := os.ReadFile(d + "/compte"); err == nil {
				for _, l := range strings.SplitAfter(string(c), "\n") {
					if l != "" && !strings.HasPrefix(l, "login=") && !strings.HasPrefix(l, "id=") {
						b.WriteString(strings.TrimSuffix(l, "\n") + "\n")
					}
				}
			}
			if os.WriteFile(d+"/compte.neuf", []byte(b.String()), 0o644) == nil {
				_ = os.Rename(d+"/compte.neuf", d+"/compte")
			}
		} else {
			numero = ""
		}
	}
	gnom, gemail := gitGlobalGH("user.name"), gitGlobalGH("user.email")
	noreply := ""
	if regexp.MustCompile(`^[0-9]+$`).MatchString(numero) && login != "" {
		noreply = numero + "+" + login + "@users.noreply.github.com"
	}
	noms := uniqueTrie([]string{cnom, login, gnom})
	type courriel struct {
		Email string `json:"email"`
		Genre string `json:"genre"`
	}
	var liste []courriel
	if noreply != "" {
		liste = append(liste, courriel{noreply, "noreply"})
	}
	for _, e := range emails {
		liste = append(liste, courriel{e, "github"})
	}
	if gemail != "" {
		liste = append(liste, courriel{gemail, "git"})
	}
	// jq's unique_by(.email): sorted by email, the first of each kept
	vus := map[string]bool{}
	var uniques []courriel
	for _, c := range liste {
		if !vus[c.Email] {
			vus[c.Email] = true
			uniques = append(uniques, c)
		}
	}
	sortStable(uniques, func(a, b courriel) bool { return a.Email < b.Email })
	if uniques == nil {
		uniques = []courriel{}
	}
	return map[string]any{"nom": id.nom, "email": id.email, "sans_coauteur": id.sansCoauteur, "defini": id.identiteDefini, "noms": noms, "emails": uniques}
}

func numeroCompteGH(login string) string {
	if _, err := exec.LookPath("curl"); err != nil {
		return ""
	}
	c := http.Client{Timeout: 5 * time.Second}
	r, err := c.Get("https://api.github.com/users/" + login)
	if err != nil {
		return ""
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return ""
	}
	var v struct {
		ID json.Number `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&v) != nil {
		return ""
	}
	return v.ID.String()
}

func uniqueTrie(l []string) []string {
	vus := map[string]bool{}
	out := []string{}
	for _, s := range l {
		if s != "" && !vus[s] {
			vus[s] = true
			out = append(out, s)
		}
	}
	sortStable(out, func(a, b string) bool { return a < b })
	return out
}

func sortStable[T any](l []T, moins func(a, b T) bool) {
	for i := 1; i < len(l); i++ {
		for j := i; j > 0 && moins(l[j], l[j-1]); j-- {
			l[j], l[j-1] = l[j-1], l[j]
		}
	}
}

func reglerIdentiteGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		return 2
	}
	if !nomAuteurValideGH(argGH(args, 1)) {
		fmt.Fprintln(w, "Invalid name.")
		return 2
	}
	if !emailValideGH(argGH(args, 2)) {
		fmt.Fprintln(w, "Invalid email.")
		return 2
	}
	d := dossierGH(projet)
	_ = os.MkdirAll(d, 0o755)
	_ = os.Chmod(d, 0o700)
	_ = os.Chmod(donneesGitHub(), 0o700)
	s := "0"
	if argGH(args, 3) == "1" {
		s = "1"
	}
	contenu := fmt.Sprintf("nom=%s\nemail=%s\nsans_coauteur=%s\n", args[1], args[2], s)
	if os.WriteFile(d+"/identite.neuf", []byte(contenu), 0o644) != nil || os.Rename(d+"/identite.neuf", d+"/identite") != nil {
		return 1
	}
	_ = os.WriteFile(donneesGitHub()+"/identite-defaut", []byte(contenu), 0o644)
	fmt.Fprintln(w, "Saved.")
	return 0
}

func identitePiloteGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		return 2
	}
	id := lireIdentiteGH(dossierGH(projet))
	if id.nom == "" || id.email == "" {
		gn, ge := gitGlobalGH("user.name"), gitGlobalGH("user.email")
		if nomAuteurValideGH(gn) && emailValideGH(ge) {
			id.nom, id.email = gn, ge
		}
	}
	s := "0"
	if id.sansCoauteur {
		s = "1"
	}
	fmt.Fprintf(w, "%s\t%s\t%s\n", id.nom, id.email, s)
	return 0
}

// ---------------------------------------------------------------------------- state, for the app

func etatGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		fmt.Fprintln(w, "{}")
		return 0
	}
	d := dossierGH(projet)
	_, e1 := exec.LookPath("gh")
	e2, e3 := presentGH("ssh"), presentGH("ssh-keygen")
	var dernier any
	if b, err := os.ReadFile(d + "/dernier.json"); err == nil {
		var x any
		if json.Unmarshal(b, &x) == nil {
			dernier = x
		}
	}
	v := map[string]any{"gh": e1 == nil, "ssh": e2 == nil && e3 == nil}
	if r, lie := lireReglagesGH(d); lie {
		b := firstNonEmpty(r.Branche, "main")
		v["lie"], v["depot"], v["url"], v["branche"], v["branche_choisie"] = true, r.Depot, "https://github.com/"+r.Depot, b, r.Branche
		v["auto"], v["compte"], v["lie_le"], v["dernier"] = r.Auto, r.Compte, r.LieLe, dernier
		v["atelier"] = r.Atelier
		v["equipe"] = r.Equipe
	} else {
		v["lie"] = false
	}
	v["identite"] = identiteJSONGH(projet)
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	_ = e.Encode(v)
	return 0
}

func identiteGHCommande(args []string, w io.Writer) int {
	projet, _ := cheminProjetGH(argGH(args, 0))
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	_ = e.Encode(identiteJSONGH(projet))
	return 0
}

// commandeGitHub: the github.sh commands done here; ok false for the others.
func commandeGitHub(args []string, w io.Writer) (int, bool) {
	reste := append([]string{}, args[1:]...)
	// the project folders as this system names them (the application gives them as Windows does)
	switch args[0] {
	case "pousser":
		for i, a := range reste {
			if !strings.HasPrefix(a, "--") {
				reste[i] = cheminNatifPilote(a)
			}
		}
	case "suivre":
		for i := 0; i < len(reste) && i < 2; i++ {
			reste[i] = cheminNatifPilote(reste[i])
		}
	default:
		if len(reste) > 0 {
			reste[0] = cheminNatifPilote(reste[0])
		}
	}
	switch args[0] {
	case "lier":
		return lierGH(reste, w), true
	case "etat":
		return etatGH(reste, w), true
	case "pcs":
		return pcsGH(reste, w), true
	case "pcs-activer":
		return pcsActiverGH(reste, w), true
	case "pcs-machine":
		return pcsMachineGH(args[1:], w), true
	case "pousser":
		return pousserGH(reste, w), true
	case "publie":
		return publieGH(reste), true
	case "auto", "branche", "partage":
		return reglageGH(args[0], reste, w), true
	case "delier":
		return delierGH(reste, w), true
	case "suivre":
		return suivreGH(reste, w), true
	case "identite":
		return identiteGHCommande(reste, w), true
	case "regler-identite":
		return reglerIdentiteGH(reste, w), true
	case "identite-pilote":
		return identitePiloteGH(reste, w), true
	case "tester":
		return testerGH(reste, w), true
	case "enregistrer":
		return enregistrerGH(reste, w), true
	}
	return 0, false
}

// ---------------------------------------------------------------------------- test, work in progress

// reponseTest: what github.sh tester prints (one JSON line): ok, and a sentence the app translates.
func reponseTest(w io.Writer, ok bool, modele string, args ...string) int {
	if args == nil {
		args = []string{}
	}
	b, _ := json.Marshal(struct {
		Ok     bool     `json:"ok"`
		Modele string   `json:"modele"`
		Args   []string `json:"args"`
	}{ok, modele, args})
	fmt.Fprintln(w, string(b))
	if ok {
		return 0
	}
	return 1
}

// testerGH: GitHub accepts Athanor's key for the repository, and a push of the project's commit would go
// through (git push --dry-run: nothing is sent).
func testerGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		return 2
	}
	d := dossierGH(projet)
	r, lie := lireReglagesGH(d)
	if !lie {
		return reponseTest(w, false, "Not linked to GitHub.")
	}
	branche := firstNonEmpty(r.Branche, "main")
	miroir := d + "/miroir.git"
	if !estDossierGH(miroir) {
		if commande("git", "init", "-q", "--bare", miroir).Run() == nil {
			c := commande("git", "config", "gc.auto", "0")
			c.Env = append(os.Environ(), "GIT_DIR="+miroir)
			_ = c.Run()
		}
	}
	sortie, lu := "", false
	var hote, port string
	for _, essai := range []string{firstNonEmpty(os.Getenv("ATHANOR_GITHUB_SSH"), "github.com:22"), secoursSSHGH()} {
		if essai == "" {
			continue
		}
		hote, port = essai, essai
		if i := strings.LastIndex(essai, ":"); i >= 0 {
			hote, port = essai[:i], essai[i+1:]
		}
		out, err := gmGH(miroir, nil, "-c", "protocol.ssh.allow=always", "-c", "core.sshCommand="+commandeSSHGH(d, port),
			"ls-remote", "--heads", fmt.Sprintf("ssh://git@%s:%s/%s.git", hote, port, r.Depot)).CombinedOutput()
		sortie, lu = strings.TrimRight(string(out), "\n"), err == nil
		if !reReseauGH.MatchString(sortie) {
			break
		}
	}
	switch {
	case lu:
	case contientGH(sortie, `host key verification failed|remote host identification has changed`):
		return reponseTest(w, false, "GitHub's identity could not be verified: push refused. Link the project again to get GitHub's current keys.")
	case reReseauGH.MatchString(sortie):
		return reponseTest(w, false, "GitHub cannot be reached (network). The next milestone tries again.")
	default:
		return reponseTest(w, false, "GitHub refused Athanor's key for {0}: the key was removed from the repository, or the repository was deleted or renamed. Link the project again.", r.Depot)
	}
	sha, objets, raison := lireProjetGH(projet)
	if raison != "" {
		return reponseTest(w, true, "Connected: GitHub accepts Athanor's key for {0}. No commit to try a push with yet.", r.Depot)
	}
	v := verrouGH(d, 60*time.Second)
	if v == nil {
		return reponseTest(w, true, "Connected: GitHub accepts Athanor's key for {0}. No commit to try a push with yet.", r.Depot)
	}
	defer v.Close()
	if preparerMiroirGH(miroir, objets) != nil {
		return reponseTest(w, false, "Cannot prepare the push folder.")
	}
	url := fmt.Sprintf("ssh://git@%s:%s/%s.git", hote, port, r.Depot)
	out, err := gmGH(miroir, nil, "-c", "protocol.ssh.allow=always", "-c", "core.sshCommand="+commandeSSHGH(d, port),
		"push", "--dry-run", "--porcelain", "--no-verify", url, sha+":refs/heads/"+branche).CombinedOutput()
	texte := string(out)
	switch {
	case err == nil:
		return reponseTest(w, true, "Connected: Athanor can push to {0}, branch {1}.", r.Depot, branche)
	case contientGH(texte, `non-fast-forward|fetch first|\[rejected\]|stale info`):
		return reponseTest(w, false, "Connected to {0}, but GitHub's branch {1} has commits the project does not have: the push would be refused. Choose another branch, or bring those commits into the project.", r.Depot, branche)
	case contientGH(texte, `GH006|protected branch`):
		return reponseTest(w, false, "The branch {0} is protected on GitHub: choose another branch, or allow the push in the repository's settings.", branche)
	case contientGH(texte, `permission denied|read only|not authorized|access denied|denied to deploy key`):
		return reponseTest(w, false, "GitHub refused Athanor's key for {0}: the key was removed from the repository, or the repository was deleted or renamed. Link the project again.", r.Depot)
	}
	return reponseTest(w, false, "Push failed: {0}", coupeOctets(strings.Join(strings.Fields(texte), " "), 300))
}

// messageEnCours: "M3: title (in progress)", from the plan's first unticked milestone.
func messageEnCours(projet string) string {
	b, _ := os.ReadFile(projet + "/.athanor/PLAN.md")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "- [ ] ") {
			t := decouperJalon(strings.TrimSpace(strings.TrimRight(l[6:], "\r"))).Titre
			t = strings.Replace(t, " : ", ": ", 1)
			if utf8.RuneCountInString(t) > 72 {
				t = string([]rune(t)[:72]) + "…"
			}
			if t != "" {
				return t + " (in progress)"
			}
		}
	}
	return "Work in progress (saved from Athanor)"
}

// enregistrerGH: the work as it stands committed ("M3: title (in progress)"), by the app, without Claude:
// to push it for someone else to take over, when the quota is out for instance.
func enregistrerGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		return 2
	}
	if _, _, raison := lireProjetGH(projet); raison == "detourne" {
		fmt.Fprintln(w, "GitHub: not pushed, the project's .git folder points elsewhere.")
		return 1
	}
	id := lireIdentiteGH(dossierGH(projet))
	if id.nom == "" || id.email == "" {
		gn, ge := gitGlobalGH("user.name"), gitGlobalGH("user.email")
		if nomAuteurValideGH(gn) && emailValideGH(ge) {
			id.nom, id.email = gn, ge
		}
	}
	var env []string
	if id.nom != "" && id.email != "" {
		env = []string{"GIT_AUTHOR_NAME=" + id.nom, "GIT_COMMITTER_NAME=" + id.nom, "GIT_AUTHOR_EMAIL=" + id.email, "GIT_COMMITTER_EMAIL=" + id.email}
	} else if gitP(projet, nil, "config", "user.email").Run() != nil {
		env = []string{"GIT_AUTHOR_NAME=Athanor", "GIT_COMMITTER_NAME=Athanor", "GIT_AUTHOR_EMAIL=athanor@localhost", "GIT_COMMITTER_EMAIL=athanor@localhost"}
	}
	if out, err := gitP(projet, env, "add", "-A").CombinedOutput(); err != nil {
		fmt.Fprintln(w, "Cannot save the work in progress: "+coupeOctets(strings.TrimSpace(string(out)), 300))
		return 1
	}
	if gitP(projet, nil, "diff", "--cached", "--quiet").Run() == nil && gitP(projet, nil, "rev-parse", "-q", "--verify", "HEAD").Run() == nil {
		fmt.Fprintln(w, "Nothing new to save: the last commit holds everything.")
		return 0
	}
	m := messageEnCours(projet)
	if out, err := gitP(projet, env, "commit", "-q", "-m", m).CombinedOutput(); err != nil {
		fmt.Fprintln(w, "Cannot save the work in progress: "+coupeOctets(strings.TrimSpace(string(out)), 300))
		return 1
	}
	fmt.Fprintln(w, "Saved: "+m)
	return 0
}
