package main

// This computer's connection to the user's GitHub account: one sign-in, kept. A key "Athanor (this
// computer)" goes on the account (its Settings > SSH and GPG keys) and stays in Athanor's data
// (donneesGitHub/connexion, out of Claude's reach in a sandbox); every project of this computer can then
// be linked to a repository the account can push to — its own, one it is a collaborator of, a team's —
// without a window: athanor github lier-compte checks the access at once and says why when it fails.
// Creating a repository still signs in (GitHub's API), never kept.
//
//   athanor github connecter            the sign-in window: the key on the account
//   athanor github connexion            {connecte, login, nom, lie_le} (JSON)
//   athanor github deconnecter          the key out of this computer (GitHub keeps an unusable entry)
//   athanor github lier-compte PROJECT OWNER/REPO [AUTO] [BRANCH] [SHARED] [TEAM] [WORKFLOWS-OK]
//   athanor github garder-acces / oublier-acces   creating a repository without a window, or not (outil_github_acces.go)

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

func dossierConnexionGH() string { return donneesGitHub() + "/connexion" }

// fichiersCleGH: the key and GitHub's SSH identity the project pushes with.
func fichiersCleGH(d string, r reglagesGH) (cle, knownHosts string) {
	if r.Connexion {
		c := dossierConnexionGH()
		return c + "/cle", c + "/known_hosts"
	}
	return d + "/cle", d + "/known_hosts"
}

// ConnexionGH: what is known of this computer's connection.
type ConnexionGH struct {
	Connecte bool   `json:"connecte"`
	Login    string `json:"login,omitempty"`
	Nom      string `json:"nom,omitempty"`
	LieLe    string `json:"lie_le,omitempty"`
	CleID    string `json:"-"`
	// creating a repository without a window (outil_github_acces.go)
	AccesGarde bool `json:"acces_garde,omitempty"`
}

func lireConnexionGH() ConnexionGH {
	var c ConnexionGH
	d := dossierConnexionGH()
	for _, kv := range lignesCleValeur(d + "/info") {
		switch kv[0] {
		case "login":
			if reCompteGH.MatchString(kv[1]) {
				c.Login = kv[1]
			}
		case "nom":
			if nomAuteurValideGH(kv[1]) {
				c.Nom = kv[1]
			}
		case "lie_le":
			if reLieLeGH.MatchString(kv[1]) {
				c.LieLe = kv[1]
			}
		case "cle_id":
			if reCleIDGH.MatchString(kv[1]) {
				c.CleID = kv[1]
			}
		}
	}
	cle, err1 := os.Stat(d + "/cle")
	kh, err2 := os.Stat(d + "/known_hosts")
	c.Connecte = c.Login != "" && err1 == nil && cle.Mode().IsRegular() && err2 == nil && kh.Size() > 0
	if a, ok := lireAccesGH(); ok && c.Connecte && strings.EqualFold(a.Login, c.Login) {
		c.AccesGarde = true
	}
	return c
}

func connexionPresenteGH() bool { return lireConnexionGH().Connecte }

// installerConnexionGH: a new key on the signed-in account (the session needs admin:public_key), kept as
// this computer's connection; the previous one, if any, removed from the account.
func installerConnexionGH(ses *sessionGH, w io.Writer) error {
	d := dossierConnexionGH()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(d, 0o700)
	ancien := lireConnexionGH()
	os.Remove(d + "/cle.neuve")
	os.Remove(d + "/cle.neuve.pub")
	pc := coupeValide(firstNonEmpty(lireMachinePC().Nom, "PC"), 50)
	commentaire := "athanor-" + coupeOctets(reNonNomCle.ReplaceAllStringFunc(ses.login+"-"+pc, func(s string) string { return strings.Repeat("-", len(s)) }), 60)
	if commande(programmeGH("ssh-keygen"), "-q", "-t", "ed25519", "-N", "", "-C", commentaire, "-f", filepath.ToSlash(d+"/cle.neuve")).Run() != nil {
		return fmt.Errorf("cannot create the key")
	}
	pub, _ := os.ReadFile(d + "/cle.neuve.pub")
	out, erreur, _ := ses.gh.api("-X", "POST", "user/keys", "-f", "title=Athanor ("+pc+")", "-f", "key="+strings.TrimRight(string(pub), "\r\n"))
	var k struct {
		ID json.Number `json:"id"`
	}
	_ = json.Unmarshal(out, &k)
	if !reCleIDGH.MatchString(k.ID.String()) {
		os.Remove(d + "/cle.neuve")
		os.Remove(d + "/cle.neuve.pub")
		return fmt.Errorf("GitHub refused the key: %s", coupeOctets(erreur, 300))
	}
	kh, err := knownHostsGH(ses.gh)
	if err != nil {
		return fmt.Errorf("cannot read GitHub's SSH keys")
	}
	if ancien.CleID != "" && ancien.CleID != k.ID.String() {
		if _, _, err := ses.gh.api("-X", "DELETE", "user/keys/"+ancien.CleID); err == nil {
			fmt.Fprintln(w, "The previous key of this computer was removed from the account.")
		}
	}
	_ = os.WriteFile(d+"/known_hosts.neuf", []byte(kh), 0o644)
	if os.Rename(d+"/cle.neuve", d+"/cle") != nil || os.Rename(d+"/cle.neuve.pub", d+"/cle.pub") != nil || os.Rename(d+"/known_hosts.neuf", d+"/known_hosts") != nil {
		return fmt.Errorf("cannot save the key")
	}
	_ = os.Chmod(d+"/cle", 0o600)
	info := fmt.Sprintf("login=%s\nnom=%s\ncle_id=%s\nlie_le=%s\n", ses.login, ses.nom, k.ID.String(), time.Now().Format("2006-01-02T15:04:05"))
	if err := ecrireFichierPrive(d+"/info", info); err != nil {
		return err
	}
	_ = ecrireFichierPrive(d+"/compte", ses.fiche) // the commit author proposed to each project
	return nil
}

// retirerAncienneCleGH: the key a project had before (its repository's, or one of 2.37's on the account),
// removed from GitHub while signed in.
func retirerAncienneCleGH(gh ghLier, depot, id string, surCompte bool, w io.Writer) {
	switch {
	case id == "":
	case surCompte:
		if _, _, err := gh.api("-X", "DELETE", "user/keys/"+id); err == nil {
			fmt.Fprintln(w, "Previous key removed from your account.")
		}
	case depotValideGH(depot):
		if _, _, err := gh.api("-X", "DELETE", "repos/"+depot+"/keys/"+id); err == nil {
			fmt.Fprintf(w, "Previous key removed from %s.\n", depot)
		}
	}
}

// connecterGH: athanor github connecter (the window).
func connecterGH(args []string, w io.Writer) int {
	errw := w
	if w == os.Stdout {
		errw = os.Stderr
	}
	verrou := verrouFichierGH(donneesGitHub()+"/connexion.verrou", 0)
	if verrou == nil {
		_ = os.MkdirAll(donneesGitHub(), 0o700)
		if verrou = verrouFichierGH(donneesGitHub()+"/connexion.verrou", 0); verrou == nil {
			fmt.Fprintln(w, "Another GitHub window is already open: finish it first.")
			return 1
		}
	}
	defer verrou.Close()
	ses, code := ouvrirSessionGH(w, errw, "user:email,admin:public_key", "=== Connect this computer to GitHub ===\n\n"+
		"1. Below, GitHub shows a one-time code. Press Enter: your browser opens on GitHub.\n"+
		"2. Paste the code there and authorize.\n\n"+
		"Athanor puts a key \"Athanor (this computer)\" on your GitHub account and keeps it: every project of\n"+
		"this computer is then linked to a repository of yours, or one you are a collaborator of, without\n"+
		"signing in again. The sign-in itself is closed and deleted right after. Claude never sees the key.\n\n")
	if ses == nil {
		return code
	}
	defer ses.fermer()
	if err := installerConnexionGH(ses, w); err != nil {
		fmt.Fprintf(w, "\nNot connected: %v\n", err)
		return 1
	}
	fmt.Fprintf(w, "\nConnected as %s. You can close this window: the projects of this computer are now linked without signing in.\n", ses.login)
	return 0
}

// deconnecterGH: athanor github deconnecter: the key leaves this computer (the projects that used it are
// no longer linked); GitHub keeps an entry that opens nothing any more, removable from its settings.
func deconnecterGH(args []string, w io.Writer) int {
	c := lireConnexionGH()
	_ = os.RemoveAll(dossierConnexionGH())
	if c.Login != "" {
		fmt.Fprintf(w, "Disconnected from %s.\n", c.Login)
	}
	return 0
}

// connexionGHCommande: athanor github connexion: the state, for the app.
func connexionGHCommande(args []string, w io.Writer) int {
	b, _ := json.Marshal(lireConnexionGH())
	fmt.Fprintln(w, string(b))
	return 0
}

// ResultatLierCompte: what lier-compte says (its last line).
type ResultatLierCompte struct {
	Resultat  string `json:"resultat"` // lie, workflows, refuse, introuvable, reseau, erreur, deconnecte
	Detail    string `json:"detail,omitempty"`
	Depot     string `json:"depot,omitempty"`
	Branche   string `json:"branche,omitempty"`
	Workflows int    `json:"workflows,omitempty"` // workflows: how many, the user confirms
	Login     string `json:"login,omitempty"`
}

var reWorkflowGH = regexp.MustCompile(`\.ya?ml$`)

// lierCompteGH: the project linked to OWNER/REPO with this computer's connection, no window.
func lierCompteGH(args []string, w io.Writer) int {
	fin := func(r ResultatLierCompte, code int) int {
		b, _ := json.Marshal(r)
		fmt.Fprintln(w, string(b))
		return code
	}
	projet, ok := cheminProjetGH(argGH(args, 0))
	depot, auto, branche, partage, equipe, workflowsOK := argGH(args, 1), argGH(args, 2) != "0", argGH(args, 3), argGH(args, 4) == "1", argGH(args, 5) == "1", argGH(args, 6) == "1"
	if !ok || !depotValideGH(depot) || (branche != "" && !brancheValideGH(branche)) {
		return fin(ResultatLierCompte{Resultat: "erreur", Detail: "invalid request"}, 2)
	}
	c := lireConnexionGH()
	if !c.Connecte {
		return fin(ResultatLierCompte{Resultat: "deconnecte"}, 1)
	}
	res := ResultatLierCompte{Depot: depot, Login: c.Login}
	d := dossierGH(projet)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return fin(ResultatLierCompte{Resultat: "erreur", Detail: err.Error()}, 1)
	}
	_ = os.Chmod(d, 0o700)
	liaison := verrouFichierGH(d+"/liaison", 0)
	if liaison == nil {
		return fin(ResultatLierCompte{Resultat: "erreur", Detail: "Another GitHub window is already open for this project: finish it first."}, 1)
	}
	defer liaison.Close()
	// the settings written first (the remote's commands read them), put back as they were if anything fails
	ancien, ancienLie := lireReglagesGH(d)
	ancienTexte, errAncien := os.ReadFile(d + "/reglages")
	remettre := func() {
		if errAncien == nil {
			_ = ecrireFichierPrive(d+"/reglages", string(ancienTexte))
		} else {
			os.Remove(d + "/reglages")
		}
	}
	r := reglagesGH{Depot: depot, Branche: branche, Auto: auto, Compte: c.Login, LieLe: time.Now().Format("2006-01-02T15:04:05"),
		Atelier: partage || equipe, Connexion: true}
	if ancienLie && ancien.Depot == depot {
		r.Equipe = ancien.Equipe
	}
	if ancien.Depot != depot {
		// another repository: what the private folder fetched or pushed before means nothing for this one
		_ = os.RemoveAll(d + "/miroir.git")
		os.Remove(d + "/dernier.json")
	}
	if err := ecrireReglagesGH(d, r); err != nil {
		return fin(ResultatLierCompte{Resultat: "erreur", Detail: err.Error()}, 1)
	}
	_, miroir, _, err := contexteGH(projet)
	if err != nil {
		remettre()
		return fin(ResultatLierCompte{Resultat: "erreur", Detail: err.Error()}, 1)
	}
	// the account reaches the repository (read), and may push to it (GitHub checks it before any change)
	out, err := gitDistantGH(d, miroir, r, "ls-remote", "--symref", "{url}", "HEAD")
	if err != nil {
		remettre()
		switch {
		case reReseauGH.MatchString(out):
			res.Resultat = "reseau"
		case contientGH(out, `not found|does not exist|permission denied|denied|access rights`):
			res.Resultat = "introuvable"
		default:
			res.Resultat, res.Detail = "erreur", coupeOctets(derniereLigneSortie(out), 300)
		}
		return fin(res, 1)
	}
	if out, err := gitDistantGH(d, miroir, r, "push", "--dry-run", "--porcelain", "--no-verify", "{url}", ":refs/heads/athanor-verification-acces"); err != nil &&
		contientGH(out, `permission to .* denied|denied to|read only|not authorized|write access`) {
		remettre()
		res.Resultat = "refuse"
		return fin(res, 1)
	}
	if branche == "" {
		branche = "main"
		for _, l := range strings.Split(out, "\n") {
			if f := strings.Fields(l); len(f) == 3 && f[0] == "ref:" && f[2] == "HEAD" && strings.HasPrefix(f[1], "refs/heads/") && brancheValideGH(strings.TrimPrefix(f[1], "refs/heads/")) {
				branche = strings.TrimPrefix(f[1], "refs/heads/")
			}
		}
		r.Branche = branche
	}
	res.Branche = branche
	// GitHub Actions run the pushed code with the repository's secrets: the user decides (without the API,
	// only the workflows can be seen, not the secrets)
	if !workflowsOK {
		if _, err := gitDistantGH(d, miroir, r, "fetch", "--no-tags", "--no-write-fetch-head", "{url}", "+refs/heads/"+branche+":"+refsDistantPC+branche); err == nil {
			l, _ := sortieGH(gmGH(miroir, nil, "ls-tree", "--name-only", refsDistantPC+branche+":.github/workflows"))
			n := 0
			for _, f := range strings.Fields(l) {
				if reWorkflowGH.MatchString(f) {
					n++
				}
			}
			if n > 0 {
				remettre()
				res.Resultat, res.Workflows = "workflows", n
				return fin(res, 0)
			}
		}
	}
	if err := ecrireReglagesGH(d, r); err != nil {
		remettre()
		return fin(ResultatLierCompte{Resultat: "erreur", Detail: err.Error()}, 1)
	}
	var sortie strings.Builder
	res.Resultat = "lie"
	if lierAvecConnexionFinGH(projet, d, depot, branche, auto, r.Atelier, equipe, "", "", false, ghLier{}, &sortie) != 0 {
		res.Resultat, res.Detail = "lie-sans-equipe", coupeOctets(derniereLigneSortie(strings.TrimSpace(sortie.String())), 300)
		return fin(res, 0)
	}
	if ancienLie && ancien.CleID != "" && !ancien.Connexion {
		res.Detail = "ancienne-cle" // the previous key stays on GitHub (no sign-in to remove it): the app says where
	}
	return fin(res, 0)
}

// lierAvecConnexionFinGH: the project's settings with the connection, then what follows a link (the
// first push, or joining the other computers). gh: a session to remove the previous key with (lierGH),
// none from lier-compte.
func lierAvecConnexionFinGH(projet, d, depot, branche string, auto, partage, equipe bool, ancienDepot, ancienID string, ancienCompte bool, gh ghLier, w io.Writer) int {
	c := lireConnexionGH()
	if gh.env != nil {
		retirerAncienneCleGH(gh, ancienDepot, ancienID, ancienCompte, w)
	}
	r, _ := lireReglagesGH(d)
	equipeAvant := r.Equipe && r.Depot == depot
	if ancienDepot != "" && ancienDepot != depot {
		_ = os.RemoveAll(d + "/miroir.git")
		os.Remove(d + "/dernier.json")
	}
	if err := ecrireReglagesGH(d, reglagesGH{Depot: depot, Branche: branche, Auto: auto, Compte: c.Login,
		LieLe: time.Now().Format("2006-01-02T15:04:05"), Atelier: partage || equipe, Connexion: true, Equipe: equipeAvant}); err != nil {
		fmt.Fprintln(w, "Cannot save the settings.")
		return 1
	}
	// the project's own key, if it had one, is of no use any more
	for _, f := range []string{"cle", "cle.pub", "known_hosts"} {
		os.Remove(d + "/" + f)
	}
	if b, err := os.ReadFile(dossierConnexionGH() + "/compte"); err == nil && !existeGH(d+"/compte") {
		_ = os.WriteFile(d+"/compte", b, 0o644)
	}
	_ = os.WriteFile(d+"/projet", []byte(cleProjetGH(projet)+"\n"), 0o644)
	_ = ecrireExclusionAtelier(projet, partage || equipe)
	fmt.Fprintf(w, "\nLinked: %s, with this computer's connection to GitHub (%s).\n", depot, c.Login)
	time.Sleep(pauseLierGH) // a key just put on the account: GitHub takes a moment to accept it
	if equipe {
		fmt.Fprintln(w, "Joining the other computers of the project...")
		if code := pcsActiverGH([]string{projet, "1"}, w); code != 0 {
			fmt.Fprintln(w, "\nNot joined: the project is linked, the switch is in its Overview (Other computers).")
			return code
		}
		fmt.Fprintln(w, "\nDone: start the pilot in Athanor's Overview. You can close this window.")
		return 0
	}
	fmt.Fprintln(w, "First push...")
	pousserGH([]string{projet}, w)
	return 0
}
