package main

// The access kept to create repositories without a window (an option of this computer's connection to
// GitHub, off by default). Creating a repository goes through GitHub's API, which the connection's key
// cannot reach: by default each creation signs in, and nothing of that sign-in is kept. With this option,
// one sign-in is kept: GitHub's token, in Athanor's data next to the connection's key (out of Claude's
// reach in a sandbox), used only to create a repository; Disconnect and Forget remove it from this
// computer. The token opens the account's repositories: keeping it is keeping that power on the disk.
//
//   athanor github garder-acces     the sign-in window, its token kept
//   athanor github oublier-acces    the token removed from this computer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type accesGardeGH struct {
	Jeton string `json:"jeton"`
	Login string `json:"login"`
	Le    string `json:"le"`
}

var reJetonGH = regexp.MustCompile(`^[A-Za-z0-9_]{20,255}$`)

func fichierAccesGH() string { return dossierConnexionGH() + "/acces.json" }

func lireAccesGH() (accesGardeGH, bool) {
	var a accesGardeGH
	b, err := os.ReadFile(fichierAccesGH())
	if err != nil || json.Unmarshal(b, &a) != nil || !reJetonGH.MatchString(a.Jeton) || !reCompteGH.MatchString(a.Login) {
		return a, false
	}
	return a, true
}

// accesGardePresentGH: this computer creates repositories without a window.
func accesGardePresentGH() bool { return lireConnexionGH().AccesGarde }

// garderAccesGH: athanor github garder-acces: one sign-in, its token kept (the connection's account only).
func garderAccesGH(args []string, w io.Writer) int {
	errw := w
	if w == os.Stdout {
		errw = os.Stderr
	}
	c := lireConnexionGH()
	if !c.Connecte {
		fmt.Fprintln(w, "This computer is not connected to GitHub: connect it first (Settings > Options > GitHub).")
		return 1
	}
	verrou := verrouFichierGH(donneesGitHub()+"/connexion.verrou", 0)
	if verrou == nil {
		fmt.Fprintln(w, "Another GitHub window is already open: finish it first.")
		return 1
	}
	defer verrou.Close()
	ses, code := ouvrirSessionGH(w, errw, "user:email", "=== Create repositories without a window ===\n\n"+
		"1. Below, GitHub shows a one-time code. Press Enter: your browser opens on GitHub.\n"+
		"2. Paste the code there and authorize.\n\n"+
		"Athanor keeps this sign-in on this computer, out of Claude's reach, to create your repositories without\n"+
		"this window. It opens your account's repositories: Settings > GitHub > Forget removes it, and you can\n"+
		"revoke it at any time on GitHub (Settings > Applications).\n\n")
	if ses == nil {
		return code
	}
	// the token survives the session's sign-out (a sign-out only forgets it locally)
	t := commande("gh", "auth", "token", "--hostname", "github.com")
	t.Env = ses.env
	out, err := t.Output()
	jeton := strings.TrimSpace(string(out))
	login := ses.login
	ses.fermer()
	if err != nil || !reJetonGH.MatchString(jeton) {
		fmt.Fprintln(w, "\nGitHub gave no token: nothing was kept.")
		return 1
	}
	if !strings.EqualFold(login, c.Login) {
		fmt.Fprintf(w, "\nSigned in as %s, but this computer is connected to GitHub as %s: nothing was kept. Sign in with %s.\n", login, c.Login, c.Login)
		return 1
	}
	b, _ := json.Marshal(accesGardeGH{Jeton: jeton, Login: login, Le: time.Now().Format("2006-01-02T15:04:05")})
	if err := ecrireAtomique(fichierAccesGH(), b, 0o600); err != nil {
		fmt.Fprintln(w, "\nCannot keep the access: "+err.Error())
		return 1
	}
	fmt.Fprintf(w, "\nKept: this computer creates repositories as %s without a window. You can close this window.\n", login)
	return 0
}

// oublierAccesGH: athanor github oublier-acces.
func oublierAccesGH(args []string, w io.Writer) int {
	if err := os.Remove(fichierAccesGH()); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(w, "Cannot remove it: "+err.Error())
		return 1
	}
	fmt.Fprintln(w, "Forgotten: creating a repository signs in again. GitHub still lists the authorization in Settings > Applications, where you can revoke it.")
	return 0
}

// sessionGardeeGH: a session from the kept access (nothing to sign in); nil when there is none, or when
// GitHub no longer accepts it (revoked: forgotten here too).
func sessionGardeeGH() *sessionGH {
	a, ok := lireAccesGH()
	if !ok || !accesGardePresentGH() {
		return nil
	}
	_ = os.MkdirAll(donneesGitHub(), 0o700)
	tmp, err := os.MkdirTemp(donneesGitHub(), ".acces.")
	if err != nil {
		return nil
	}
	var env []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		switch strings.ToUpper(k) {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST", "GH_CONFIG_DIR",
			"XDG_STATE_HOME", "XDG_CACHE_HOME", "GH_NO_UPDATE_NOTIFIER", "BROWSER":
			continue
		}
		env = append(env, e)
	}
	env = append(env, "GH_TOKEN="+a.Jeton, "GH_CONFIG_DIR="+tmp, "XDG_STATE_HOME="+filepath.Join(tmp, "etat"),
		"XDG_CACHE_HOME="+filepath.Join(tmp, "cache"), "GH_NO_UPDATE_NOTIFIER=1")
	ses := &sessionGH{gh: ghLier{env}, env: env, fermer: func() { _ = os.RemoveAll(tmp) }}
	out, erreur, err := ses.gh.api("user")
	var u struct {
		Login string `json:"login"`
	}
	_ = json.Unmarshal(out, &u)
	if err != nil || !strings.EqualFold(u.Login, a.Login) {
		ses.fermer()
		if strings.Contains(erreur, "401") || strings.Contains(strings.ToLower(erreur), "bad credentials") {
			_ = os.Remove(fichierAccesGH()) // revoked on GitHub: the next creation signs in
		}
		return nil
	}
	ses.login = u.Login
	return ses
}
