package main

// athanor github lier PROJECT creer NAME [AUTO] [BRANCH] | existant OWNER/REPO [AUTO] [BRANCH]: linking a
// project to ONE GitHub repository (interactive, in a window the user sees; github.sh lier in Go).
//
//   - the GitHub sign-in (gh auth login) happens only now, in a temporary folder deleted right after
//     (signed out whatever happens): no GitHub sign-in is ever kept;
//   - while signed in, the repository is created (private) or checked (the user administers it), and it
//     gets its own key, a deploy key with write access, valid for that repository alone;
//   - the key, the repository's name and the settings go to the user's home (donneesGitHub), out of
//     Claude's reach, with GitHub's SSH keys as GitHub publishes them (the push refuses any other server);
//   - then a first push.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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
	ses, code := ouvrirSessionGH(w, errw, "user:email", "=== Link this project to GitHub ===\n\n"+
		"1. Below, GitHub shows a one-time code. Press Enter: your browser opens on GitHub.\n"+
		"2. Paste the code there and authorize.\n\n"+
		"Athanor keeps nothing of this sign-in: it only gives the repository its own key (valid for\n"+
		"that repository alone), then signs out. Claude never sees the sign-in nor the key.\n\n")
	if ses == nil {
		return code
	}
	defer ses.fermer()
	gh, env, aide, compte := ses.gh, ses.env, ses.aide, ses.login
	if os.WriteFile(d+"/compte.neuf", []byte(ses.fiche), 0o644) == nil {
		_ = os.Rename(d+"/compte.neuf", d+"/compte")
	}
	fmt.Fprintf(w, "\nSigned in as %s.\n", compte)

	depot := ""
	// the repository uses this computer's connection to the account (outil_github_connexion.go) rather
	// than a key of its own: a collaborator, or a repository created while the computer is connected
	viaConnexion := mode == "creer" && connexionPresenteGH()
	if mode == "creer" {
		out, erreur, _ := gh.api("-X", "POST", "user/repos", "-f", "name="+cible, "-F", "private=true", "-F", "auto_init=false", "-f", "description=Made with Athanor")
		var r struct {
			FullName string `json:"full_name"`
		}
		_ = json.Unmarshal(out, &r)
		depot = r.FullName
		if !depotValideGH(depot) {
			if strings.Contains(strings.ToLower(erreur), "already exists") {
				fmt.Fprintf(w, "You already have a repository named %s: choose \"Use an existing repository\" (%s/%s), or another name.\n", cible, compte, cible)
			} else {
				fmt.Fprintf(w, "GitHub refused to create the repository: %s\n", coupeOctets(erreur, 300))
			}
			return 1
		}
		fmt.Fprintf(w, "Private repository created: %s\n", depot)
	} else {
		out, _, _ := gh.api("repos/" + cible)
		var r struct {
			FullName    string `json:"full_name"`
			Archived    bool   `json:"archived"`
			Default     string `json:"default_branch"`
			Permissions struct {
				Admin bool `json:"admin"`
				Push  bool `json:"push"`
			} `json:"permissions"`
		}
		_ = json.Unmarshal(out, &r)
		depot = r.FullName
		if !depotValideGH(depot) {
			fmt.Fprintf(w, "Repository %s not found, or not visible from your account.\n", cible)
			return 1
		}
		if !r.Permissions.Admin && !r.Permissions.Push {
			fmt.Fprintf(w, "You can read %s but not push to it: its owner must add you as a collaborator with write access\n", depot)
			fmt.Fprintln(w, "(on GitHub: the repository's Settings > Collaborators), then link again.")
			return 1
		}
		if !r.Permissions.Admin && connexionPresenteGH() {
			viaConnexion = true // already connected: nothing more to ask
		} else if !r.Permissions.Admin {
			// a collaborator: GitHub only lets an administrator add a key to the repository; this computer
			// is connected to the user's own account instead (one key, kept, for every project)
			fmt.Fprintf(w, "\nYou are a collaborator of %s, not its administrator: GitHub only lets an administrator give a\n", depot)
			fmt.Fprint(w, "repository a key of its own. Athanor can connect this computer to YOUR GitHub account instead: a key\n"+
				"\"Athanor (this computer)\" on your account, kept by Athanor, that every project of this computer then uses\n"+
				"without signing in again. It opens every repository your account can push to (Claude never sees it;\n"+
				"Settings > GitHub in Athanor disconnects, and GitHub's Settings > SSH and GPG keys removes it).\n"+
				"GitHub asks you once more in the browser, for that permission (admin:public_key).\n")
			fmt.Fprint(errw, "Type yes to connect this computer to your account: ")
			rep, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimRight(rep, "\r\n") != "yes" {
				fmt.Fprintln(w, "Not linked. Another way: the repository's owner signs in in this window instead of you (only a key")
				fmt.Fprintln(w, "for this repository then stays), or moves it to an organization where you are Admin.")
				return 1
			}
			ref := []string{"auth", "refresh", "--hostname", "github.com", "--scopes", "admin:public_key"}
			if strings.Contains(aide, "--insecure-storage") {
				ref = append(ref, "--insecure-storage")
			}
			c := commande("gh", ref...)
			c.Env, c.Stdin, c.Stdout, c.Stderr = env, os.Stdin, w, errw
			if c.Run() != nil {
				fmt.Fprint(w, "\nPermission not given: nothing was changed.\n")
				return 1
			}
			if err := installerConnexionGH(ses, w); err != nil {
				fmt.Fprintf(w, "This computer could not be connected: %v\n", err)
				return 1
			}
			viaConnexion = true
		}
		if r.Archived {
			fmt.Fprintf(w, "%s is archived on GitHub (read-only): unarchive it first.\n", depot)
			return 1
		}
		fmt.Fprintf(w, "Repository: %s\n", depot)
		if branche == "" && brancheValideGH(r.Default) {
			branche = r.Default
		}
		// GitHub Actions run the pushed code with the repository's secrets: with workflows and secrets, the
		// code Claude writes would run there (a secret could reach other repositories). The user decides.
		compter := func(chemin string) string {
			out, _, err := gh.api(chemin)
			if err != nil {
				return ""
			}
			var t struct {
				Total *json.Number `json:"total_count"`
			}
			if json.Unmarshal(out, &t) != nil || t.Total == nil || !reNombreGH.MatchString(t.Total.String()) {
				return ""
			}
			return t.Total.String()
		}
		nwf, nsec, norg := compter("repos/"+depot+"/actions/workflows"), compter("repos/"+depot+"/actions/secrets"), compter("repos/"+depot+"/actions/organization-secrets")
		n := func(s string) int { v, _ := strconv.Atoi(s); return v }
		if nwf == "" || (n(nwf) > 0 && (nsec == "" || n(nsec) > 0 || (norg != "" && n(norg) > 0))) {
			q := func(s string) string { return firstNonEmpty(s, "?") }
			fmt.Fprintf(w, "\nWARNING: %s runs GitHub Actions workflows (%s) and has Actions secrets (%s of its own, %s from its organization).\n", depot, q(nwf), q(nsec), q(norg))
			fmt.Fprint(w, "Every push runs those workflows on the code Claude writes, with those secrets: a secret able to\n"+
				"change other repositories could be used by that code. Prefer a new private repository, or remove\n"+
				"such secrets first. Athanor never pushes a change to the workflows themselves without you.\n")
			fmt.Fprint(errw, "Type yes to link anyway: ")
			rep, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimRight(rep, "\r\n") != "yes" {
				fmt.Fprintln(w, "Not linked.")
				return 1
			}
		}
	}
	if branche == "" {
		branche = "main" // fixed now: never the branch the project's HEAD (Claude's) points to
	}

	// the repository's own key; the previous key of this project (if any) is removed from GitHub
	ancienDepot, ancienID, ancienCompte := "", "", false
	if r, lie := lireReglagesGH(d); lie {
		ancienDepot, ancienID, ancienCompte = r.Depot, r.CleID, r.CleCompte
	}
	if viaConnexion {
		return lierAvecConnexionFinGH(projet, d, depot, branche, autoOn, partage == "1", equipe, ancienDepot, ancienID, ancienCompte, gh, w)
	}
	os.Remove(d + "/cle.neuve")
	os.Remove(d + "/cle.neuve.pub")
	base := path.Base(filepath.ToSlash(cleProjetGH(projet)))
	commentaire := "athanor-" + coupeOctets(reNonNomCle.ReplaceAllStringFunc(compte+"-"+base, func(s string) string { return strings.Repeat("-", len(s)) }), 60)
	cle := filepath.ToSlash(d + "/cle.neuve")
	kg := commande(programmeGH("ssh-keygen"), "-q", "-t", "ed25519", "-N", "", "-C", commentaire, "-f", cle)
	if kg.Run() != nil {
		fmt.Fprintln(w, "Cannot create the key.")
		return 1
	}
	pub, _ := os.ReadFile(d + "/cle.neuve.pub")
	out, erreur, _ := gh.api("-X", "POST", "repos/"+depot+"/keys", "-f", "title=Athanor ("+coupeValide(base, 60)+")",
		"-f", "key="+strings.TrimRight(string(pub), "\r\n"), "-F", "read_only=false")
	var k struct {
		ID json.Number `json:"id"`
	}
	_ = json.Unmarshal(out, &k)
	id := k.ID.String()
	if !reCleIDGH.MatchString(id) {
		os.Remove(d + "/cle.neuve")
		os.Remove(d + "/cle.neuve.pub")
		fmt.Fprintf(w, "GitHub refused the key: %s\n", coupeOctets(erreur, 300))
		return 1
	}
	retirerAncienneCleGH(gh, ancienDepot, ancienID, ancienCompte, w)
	// GitHub's SSH identity, as GitHub publishes it (the push refuses any other server)
	kh, err := knownHostsGH(gh)
	if err != nil {
		fmt.Fprintln(w, "Cannot read GitHub's SSH keys.")
		return 1
	}
	_ = os.WriteFile(d+"/known_hosts.neuf", []byte(kh), 0o644)

	v := verrouGH(d, 300*time.Second)
	if os.Rename(d+"/cle.neuve", d+"/cle") == nil && os.Rename(d+"/cle.neuve.pub", d+"/cle.pub") == nil {
		_ = os.Rename(d+"/known_hosts.neuf", d+"/known_hosts")
	}
	_ = os.Chmod(d+"/cle", 0o600)
	// another repository: what was pushed before means nothing for this one
	if ancienDepot != depot {
		_ = os.RemoveAll(d + "/miroir.git")
		os.Remove(d + "/dernier.json")
	}
	if ecrireReglagesGH(d, reglagesGH{Depot: depot, Branche: branche, Auto: autoOn, Compte: compte,
		LieLe: time.Now().Format("2006-01-02T15:04:05"), CleID: id, Atelier: partage == "1"}) != nil {
		if v != nil {
			v.Close()
		}
		fmt.Fprintln(w, "Cannot save the settings.")
		return 1
	}
	_ = os.WriteFile(d+"/projet", []byte(cleProjetGH(projet)+"\n"), 0o644)
	_ = ecrireExclusionAtelier(projet, partage == "1")
	if v != nil {
		v.Close()
	}
	fmt.Fprintf(w, "\nLinked: %s. The sign-in is now closed and deleted from this PC (GitHub still lists \"GitHub CLI\"\n", depot)
	fmt.Fprint(w, "under Settings > Applications > Authorized OAuth Apps: you can revoke it there, the push key does not need it).\n\n")
	if equipe {
		fmt.Fprintln(w, "Joining the other computers of the project...")
		time.Sleep(pauseLierGH) // GitHub takes a moment to accept a new key
		if code := pcsActiverGH([]string{projet, "1"}, w); code != 0 {
			fmt.Fprintln(w, "\nNot joined: the project is linked, the switch is in its Overview (Other computers).")
			return code
		}
		fmt.Fprintln(w, "\nDone: start the pilot in Athanor's Overview. You can close this window.")
		return 0
	}
	fmt.Fprintln(w, "First push...")
	time.Sleep(pauseLierGH)
	pousserGH([]string{projet}, w)
	return 0
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
