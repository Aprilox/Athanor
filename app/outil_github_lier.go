package main

// athanor github lier PROJECT creer NAME [AUTO] [BRANCH] | existant OWNER/REPO [AUTO] [BRANCH]: linking a
// project to ONE GitHub repository, with this computer's connection to GitHub (outil_github_connexion.go:
// one sign-in, kept; required first). An existing repository needs no sign-in (lier-compte); creating one
// signs in once for GitHub's API, in a temporary folder deleted right after (nothing of it is kept). The
// projects linked by older versions with a key of their own (a deploy key) still push with it.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	reNomDepotGH = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	reNombreGH   = regexp.MustCompile(`^[0-9]+$`)
	reCleSSHGH   = regexp.MustCompile(`^(ssh-ed25519|ecdsa-sha2-nistp256|ssh-rsa) [A-Za-z0-9+/=]+$`)
	reNonNomCle  = regexp.MustCompile(`[^A-Za-z0-9._-]`)
)

func nomDepotValideGH(n string) bool { return reNomDepotGH.MatchString(n) && n != "." && n != ".." }

// coupeValide: the first n bytes (cut -c1-n), without a character cut in two.
func coupeValide(s string, n int) string { return strings.ToValidUTF8(coupeOctets(s, n), "") }

// ghLier: one gh call with the sign-in's environment; its output, its errors, and whether it succeeded.
type ghLier struct {
	env []string
}

func (g ghLier) api(args ...string) ([]byte, string, error) {
	c := commande("gh", append([]string{"api"}, args...)...)
	c.Env = g.env
	var e strings.Builder
	c.Stderr = &e
	out, err := c.Output()
	return out, e.String(), err
}

// navigateurGH: what opens GitHub's page during the sign-in: Windows' browser from WSL, the desktop's on
// Linux; on macOS and in native Windows, gh opens the default browser itself.
func navigateurGH() string {
	switch runtime.GOOS {
	case "darwin", "windows":
		return ""
	}
	for _, n := range []string{"explorer.exe", "wslview", "xdg-open"} {
		if _, err := exec.LookPath(n); err == nil {
			return n
		}
	}
	return ""
}

func lierGH(args []string, w io.Writer) int {
	errw := w
	if w == os.Stdout {
		errw = os.Stderr
	}
	usage := "Usage: athanor github lier PROJECT creer NAME | existant OWNER/REPO [AUTO 0|1] [BRANCH] [SHARED WORKSHOP 0|1] [TEAM 0|1]"
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		fmt.Fprintln(w, usage)
		return 2
	}
	mode, cible, auto, branche, partage := argGH(args, 1), argGH(args, 2), argGH(args, 3), argGH(args, 4), argGH(args, 5)
	equipe := argGH(args, 6) == "1" && mode == "existant" // joining a team: its work comes in, nothing is pushed first
	switch mode {
	case "creer":
		if !nomDepotValideGH(cible) {
			fmt.Fprintf(w, "Invalid repository name (letters, digits, . - _): %s\n", cible)
			return 2
		}
	case "existant":
		if !depotValideGH(cible) {
			fmt.Fprintf(w, "Invalid repository (expected OWNER/NAME): %s\n", cible)
			return 2
		}
	default:
		fmt.Fprintln(w, usage)
		return 2
	}
	autoOn := auto != "0"
	if branche != "" && !brancheValideGH(branche) {
		fmt.Fprintf(w, "Invalid branch name: %s\n", branche)
		return 2
	}
	// every project pushes with this computer's connection to GitHub (outil_github_connexion.go)
	if !connexionPresenteGH() {
		fmt.Fprintln(w, "This computer is not connected to GitHub: Settings > Options > GitHub > Connect to GitHub, then link the project again.")
		return 1
	}
	un := func(x bool) string {
		if x {
			return "1"
		}
		return "0"
	}
	if mode == "existant" {
		// no sign-in: the connection reaches the repository; a repository with workflows asks first
		lancer := func(ok string) ResultatLierCompte {
			var b strings.Builder
			lierCompteGH([]string{projet, cible, un(autoOn), branche, partage, un(equipe), ok}, &b)
			var r ResultatLierCompte
			_ = json.Unmarshal([]byte(derniereLigneJSON(b.String())), &r)
			return r
		}
		r := lancer("0")
		if r.Resultat == "workflows" {
			fmt.Fprintf(w, "\nWARNING: %s runs %d GitHub Actions workflow(s): every push runs them on the code Claude writes, with\n", cible, r.Workflows)
			fmt.Fprint(w, "the repository's secrets if it has any (a secret able to change other repositories could be used by that\n"+
				"code). Prefer a repository without secrets. Athanor never pushes a change to the workflows themselves without you.\n")
			fmt.Fprint(errw, "Type yes to link anyway: ")
			rep, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimRight(rep, "\r\n") != "yes" {
				fmt.Fprintln(w, "Not linked.")
				return 1
			}
			r = lancer("1")
		}
		switch r.Resultat {
		case "lie":
			fmt.Fprintf(w, "Linked: %s (branch %s), with this computer's connection to GitHub (%s).\n", r.Depot, r.Branche, r.Login)
			return 0
		case "lie-sans-equipe":
			fmt.Fprintf(w, "Linked: %s, but not joined to the other computers: %s\n", r.Depot, r.Detail)
			return 1
		case "introuvable":
			fmt.Fprintf(w, "Your GitHub account (%s) cannot see %s: check the name, or ask its owner to add you as a collaborator.\n", r.Login, cible)
		case "refuse":
			fmt.Fprintf(w, "Your GitHub account (%s) can read %s but not push to it: its owner must give you write access (Settings > Collaborators).\n", r.Login, cible)
		case "reseau":
			fmt.Fprintln(w, "GitHub cannot be reached (network): try again in a moment.")
		default:
			fmt.Fprintf(w, "Not linked: %s\n", firstNonEmpty(r.Detail, r.Resultat))
		}
		return 1
	}

	// a new repository: GitHub's API, hence one sign-in (never kept), then the connection pushes to it
	d := dossierGH(projet)
	if err := os.MkdirAll(d, 0o700); err != nil || os.Chmod(d, 0o700) != nil {
		fmt.Fprintf(w, "Cannot create %s\n", d)
		return 1
	}
	liaison := verrouFichierGH(d+"/liaison", 0)
	if liaison == nil {
		fmt.Fprintln(w, "Another GitHub window is already open for this project: finish it first.")
		return 1
	}
	defer liaison.Close()
	// the access kept on this computer (Settings > GitHub): no window; otherwise one sign-in, never kept
	ses := sessionGardeeGH()
	if ses != nil {
		fmt.Fprintln(w, "Creating the repository with the access kept on this computer (Settings > Options > GitHub).")
	} else {
		var code int
		ses, code = ouvrirSessionGH(w, errw, "user:email", "=== Create a GitHub repository for this project ===\n\n"+
			"1. Below, GitHub shows a one-time code. Press Enter: your browser opens on GitHub.\n"+
			"2. Paste the code there and authorize.\n\n"+
			"Athanor creates the private repository, then signs out: nothing of this sign-in is kept. The project\n"+
			"pushes with this computer's connection to GitHub, out of Claude's reach. (To create repositories\n"+
			"without this window: Settings > Options > GitHub > Create repositories without a window.)\n\n")
		if ses == nil {
			return code
		}
	}
	defer ses.fermer()
	gh := ses.gh
	fmt.Fprintf(w, "\nSigned in as %s.\n", ses.login)
	out, erreur, _ := gh.api("-X", "POST", "user/repos", "-f", "name="+cible, "-F", "private=true", "-F", "auto_init=false", "-f", "description=Made with Athanor")
	var rp struct {
		FullName string `json:"full_name"`
	}
	_ = json.Unmarshal(out, &rp)
	depot := rp.FullName
	if !depotValideGH(depot) {
		if strings.Contains(strings.ToLower(erreur), "already exists") {
			fmt.Fprintf(w, "You already have a repository named %s: choose \"Use one of my repositories\" (%s/%s), or another name.\n", cible, ses.login, cible)
		} else {
			fmt.Fprintf(w, "GitHub refused to create the repository: %s\n", coupeOctets(erreur, 300))
		}
		return 1
	}
	fmt.Fprintf(w, "Private repository created: %s\n", depot)
	if branche == "" {
		branche = "main" // fixed now: never the branch the project's HEAD (Claude's) points to
	}
	ancienDepot, ancienID, ancienCompte := "", "", false
	if r, lie := lireReglagesGH(d); lie && !r.Connexion {
		ancienDepot, ancienID, ancienCompte = r.Depot, r.CleID, r.CleCompte
	}
	return lierAvecConnexionFinGH(projet, d, depot, branche, autoOn, partage == "1", false, ancienDepot, ancienID, ancienCompte, gh, w)
}

// pauseLierGH: the pause before the first push (GitHub takes a moment to accept a new key).
var pauseLierGH = 2 * time.Second

// sessionGH: a GitHub sign-in (gh auth login) in a temporary folder of donneesGitHub, signed out and
// deleted by fermer (and on an interruption): nothing of it is ever kept.
type sessionGH struct {
	gh             ghLier
	env            []string
	aide           string
	login, id, nom string
	fiche          string // login=, id=, nom=, email= lines (the commit author proposed)
	fermer         func()
}

func ouvrirSessionGH(w, errw io.Writer, scopes, intro string) (*sessionGH, int) {
	reparer := "run Repair in the application's Options (Linux packages)"
	switch runtime.GOOS {
	case "windows":
		reparer = "Setup & health -> Install everything (native Windows mode)"
	case "darwin":
		reparer = "Setup & health -> Basic packages (Homebrew)"
	}
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintf(w, "GitHub CLI (gh) is missing: %s.\n", reparer)
		return nil, 1
	}
	if presentGH("ssh-keygen") != nil || presentGH("ssh") != nil {
		fmt.Fprintf(w, "OpenSSH is missing: %s.\n", reparer)
		return nil, 1
	}
	aideOut, _ := commande("gh", "auth", "login", "--help").CombinedOutput()
	aide := string(aideOut)
	if !strings.Contains(aide, "--skip-ssh-key") {
		fmt.Fprintln(w, "GitHub CLI is too old: run Repair in the application's Options to update it.")
		return nil, 1
	}
	donnees := donneesGitHub()
	if os.MkdirAll(donnees, 0o700) == nil {
		_ = os.Chmod(donnees, 0o700)
		_ = os.Chmod(filepath.Dir(donnees), 0o700)
	}
	tmp, err := os.MkdirTemp(donnees, ".connexion.")
	if err != nil {
		return nil, 1
	}
	_ = os.Chmod(tmp, 0o700)
	env := []string{}
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		switch strings.ToUpper(k) {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST", "GH_CONFIG_DIR",
			"XDG_STATE_HOME", "XDG_CACHE_HOME", "GH_NO_UPDATE_NOTIFIER", "BROWSER":
			continue
		}
		env = append(env, e)
	}
	env = append(env, "GH_CONFIG_DIR="+tmp, "XDG_STATE_HOME="+filepath.Join(tmp, "etat"), "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"), "GH_NO_UPDATE_NOTIFIER=1")
	if b := navigateurGH(); b != "" {
		env = append(env, "BROWSER="+b)
	}
	ses := &sessionGH{gh: ghLier{env}, env: env, aide: aide}
	// nothing of this sign-in survives: signed out and folder deleted whatever happens
	var une sync.Once
	nettoyer := func() {
		une.Do(func() {
			c := commande("gh", "auth", "logout", "--hostname", "github.com")
			c.Env, c.Stdin = env, strings.NewReader("y\n")
			_ = c.Run()
			_ = os.RemoveAll(tmp)
		})
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	fini := make(chan struct{})
	var arret sync.Once
	ses.fermer = func() {
		nettoyer()
		arret.Do(func() { signal.Stop(sig); close(fini) })
	}
	go func() {
		select {
		case <-sig:
			nettoyer()
			os.Exit(130)
		case <-fini:
		}
	}()

	fmt.Fprint(w, intro)
	opts := []string{"auth", "login", "--hostname", "github.com", "--git-protocol", "ssh", "--skip-ssh-key", "--web", "--scopes", scopes}
	if strings.Contains(aide, "--insecure-storage") {
		opts = append(opts, "--insecure-storage") // never the system keyring
	}
	c := commande("gh", opts...)
	c.Env, c.Stdin, c.Stdout, c.Stderr = env, os.Stdin, w, errw
	if c.Run() != nil {
		fmt.Fprint(w, "\nSign-in cancelled or failed: nothing was changed.\n")
		ses.fermer()
		return nil, 1
	}
	var u struct {
		Login string      `json:"login"`
		ID    json.Number `json:"id"`
		Name  string      `json:"name"`
	}
	if out, _, err := ses.gh.api("user"); err == nil {
		_ = json.Unmarshal(out, &u)
	}
	if !reCompteGH.MatchString(u.Login) {
		fmt.Fprintln(w, "Cannot read your GitHub account.")
		ses.fermer()
		return nil, 1
	}
	ses.login = u.Login
	// the account, to propose the commit author (name, verified emails, the private noreply address)
	var fc strings.Builder
	fmt.Fprintf(&fc, "login=%s\n", u.Login)
	if reCleIDGH.MatchString(u.ID.String()) {
		ses.id = u.ID.String()
		fmt.Fprintf(&fc, "id=%s\n", u.ID.String())
	}
	if nomAuteurValideGH(u.Name) {
		ses.nom = u.Name
		fmt.Fprintf(&fc, "nom=%s\n", u.Name)
	}
	if out, _, err := ses.gh.api("user/emails"); err == nil {
		var emails []struct {
			Email    string `json:"email"`
			Verified bool   `json:"verified"`
		}
		_ = json.Unmarshal(out, &emails)
		for _, e := range emails {
			if e.Verified && emailValideGH(e.Email) {
				fmt.Fprintf(&fc, "email=%s\n", e.Email)
			}
		}
	}
	ses.fiche = fc.String()
	return ses, 0
}

// knownHostsGH: GitHub's SSH keys as GitHub publishes them (meta), for github.com and the fallback host.
func knownHostsGH(gh ghLier) (string, error) {
	var cles []string
	if out, _, err := gh.api("meta"); err == nil {
		var m struct {
			SSH []string `json:"ssh_keys"`
		}
		_ = json.Unmarshal(out, &m)
		for _, c := range m.SSH {
			if reCleSSHGH.MatchString(c) {
				cles = append(cles, c)
			}
		}
	}
	if len(cles) == 0 {
		return "", fmt.Errorf("no SSH key")
	}
	var noms []string
	for _, h := range []string{firstNonEmpty(os.Getenv("ATHANOR_GITHUB_SSH"), "github.com:22"), secoursSSHGH()} {
		if h == "" {
			continue
		}
		hote, port := h, h
		if i := strings.LastIndex(h, ":"); i >= 0 {
			hote, port = h[:i], h[i+1:]
		}
		if port == "22" {
			noms = append(noms, hote)
		} else {
			noms = append(noms, "["+hote+"]:"+port)
		}
	}
	var kh strings.Builder
	for _, c := range cles {
		for _, h := range noms {
			fmt.Fprintf(&kh, "%s %s\n", h, c)
		}
	}
	return kh.String(), nil
}
