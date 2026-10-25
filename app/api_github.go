package main

// GitHub link of a project. The work is done on the pilot's side by the kit's executable (athanor github,
// outil_github_cmd.go), outside Claude's reach: linking signs in to GitHub once, in a visible window,
// gives the repository its own key (valid for that repository alone) and signs out; the pilot then pushes
// after each milestone with that key.

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var (
	reDepotGitHub   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
	reNomGitHub     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	reBrancheGitHub = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)
	reShaGitHub     = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	reEmailAuteur   = regexp.MustCompile(`^[^\s<>@"]{1,64}@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)
)

// GitHubVue: what athanor github etat prints.
type GitHubVue struct {
	Gh      bool            `json:"gh"`
	Ssh     bool            `json:"ssh"`
	Lie     bool            `json:"lie"`
	Depot   string          `json:"depot,omitempty"`
	URL     string          `json:"url,omitempty"`
	Branche string          `json:"branche,omitempty"`
	Auto    bool            `json:"auto"`
	Compte  string          `json:"compte,omitempty"`
	LieLe   string          `json:"lie_le,omitempty"`
	Dernier json.RawMessage `json:"dernier,omitempty"`
	// author of the commits: chosen name and email, Claude co-author or not, and what to propose
	Identite json.RawMessage `json:"identite,omitempty"`
}

func citerShell(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// argsGitHub: athanor github COMMAND PROJECT ARGS… (the project as Windows names it: the pilot's side
// converts it).
func argsGitHub(p Projet, commande string, args ...string) []string {
	return append([]string{"github", commande, p.Dossier}, args...)
}

// executerGitHub: athanor github COMMAND on the pilot's side, its output.
func (s *Serveur) executerGitHub(p Projet, delai time.Duration, commande string, args ...string) (string, error) {
	return executerKitArgs(s.kitCourant(), delai, argsGitHub(p, commande, args...)...)
}

func nomDepotPropose(nom string) string {
	b := strings.Builder{}
	for _, r := range nom {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	s := strings.Trim(b.String(), ".-")
	if len(s) > 100 {
		s = s[:100]
	}
	if s == "" {
		s = "athanor-project"
	}
	return s
}

func (s *Serveur) etatGitHub(p Projet) (GitHubVue, error) {
	var v GitHubVue
	out, err := s.executerGitHub(p, 45*time.Second, "etat")
	if err != nil {
		if strings.Contains(err.Error(), "no answer after") {
			return v, errors.New("GitHub: no answer from WSL after 45 s (still starting?). Try Refresh in a moment.")
		}
		return v, err
	}
	if i := strings.Index(out, "{"); i > 0 { // a login shell may print something first
		out = out[i:]
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return v, errors.New("GitHub: unreadable answer from WSL")
	}
	return v, nil
}

func (s *Serveur) apiGitHubVoir(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	v, err := s.etatGitHub(p)
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"github": v, "nom_propose": nomDepotPropose(p.Nom)})
}

// lierGitHub opens the window where the user signs in to GitHub once and the repository gets its key.
func (s *Serveur) lierGitHub(p Projet, mode, cible string, auto bool, branche string, atelier, equipe bool) error {
	switch mode {
	case "creer":
		if !reNomGitHub.MatchString(cible) || cible == "." || cible == ".." {
			return errors.New("Invalid repository name: letters, digits, dots, dashes and underscores only.")
		}
	case "existant":
		cible = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(cible), "https://github.com/"), ".git")
		if !reDepotGitHub.MatchString(cible) {
			return errors.New("Invalid repository: write it as OWNER/NAME (e.g. octocat/my-site).")
		}
	default:
		return errors.New("Unknown")
	}
	if branche != "" && (!reBrancheGitHub.MatchString(branche) || strings.Contains(branche, "..")) {
		return errors.New("Invalid branch name.")
	}
	a := "1"
	if !auto {
		a = "0"
	}
	partage := "0"
	if atelier {
		partage = "1"
	}
	args := []string{mode, cible, a, branche, partage}
	if equipe && mode == "existant" {
		args = append(args, "1") // joining the computers of a project (pcs.go): linked, then the team's work comes in
	}
	return fenetreKit(s.kitCourant(), "GitHub", argsGitHub(p, "lier", args...)...)
}

func ouvrirLien(url string) error {
	if runtime.GOOS == "windows" {
		return commande("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return commande("xdg-open", url).Start()
}

func (s *Serveur) apiGitHub(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	var c struct {
		Action  string `json:"action"`
		Mode    string `json:"mode"`
		Valeur  string `json:"valeur"`
		Auto    bool   `json:"auto"`
		Atelier bool   `json:"atelier"`
		Branche string `json:"branche"`
		Nom     string `json:"nom"`
		Email   string `json:"email"`
		SansCo  bool   `json:"sans_coauteur"`
		Equipe  bool   `json:"equipe"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	switch c.Action {
	case "lier":
		if err := s.lierGitHub(p, c.Mode, strings.TrimSpace(c.Valeur), c.Auto, strings.TrimSpace(c.Branche), c.Atelier || c.Equipe, c.Equipe); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		s.msg(w, "A window opens: sign in to GitHub there. Athanor then gives the repository its own key and signs out.")
	case "pousser", "malgre":
		args := []string{}
		if c.Action == "malgre" {
			// exactly the commit the user reviewed (athanor github refuses any other)
			if !reShaGitHub.MatchString(c.Valeur) {
				s.erreur(w, http.StatusBadRequest, "Invalid request")
				return
			}
			args = append(args, "--malgre="+c.Valeur)
		}
		avant, _ := s.etatGitHub(p)
		out, _ := s.executerGitHub(p, 20*time.Minute, "pousser", args...)
		v, err := s.etatGitHub(p)
		if err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		if quandDernier(v) == quandDernier(avant) { // nothing new recorded: say why
			s.erreur(w, http.StatusConflict, raisonPoussee(out))
			return
		}
		s.ecrireJSON(w, map[string]any{"github": v})
	case "tester": // GitHub accepts the key, a push would go through (nothing is sent)
		out, _ := s.executerGitHub(p, 2*time.Minute, "tester")
		if i := strings.LastIndex(out, "{"); i >= 0 {
			out = out[i:]
		}
		var t struct {
			Ok     bool     `json:"ok"`
			Modele string   `json:"modele"`
			Args   []string `json:"args"`
		}
		if json.Unmarshal([]byte(out), &t) != nil {
			s.erreur(w, http.StatusBadGateway, raisonPoussee(out))
			return
		}
		s.ecrireJSON(w, map[string]any{"ok": t.Ok, "message_key": t.Modele, "args": t.Args})
	case "wip": // the work as it stands committed by Athanor itself, then pushed: no Claude, no quota needed
		if e, ok := p.LireEtat(); ok && p.PiloteActif() && e.Phase == "travail" {
			s.erreur(w, http.StatusConflict, "Claude is working on the project right now: use “Stop after cycle”, or wait for the end of the cycle, then save the work in progress.")
			return
		}
		out, err := s.executerGitHub(p, 2*time.Minute, "enregistrer")
		if err != nil {
			s.erreur(w, http.StatusConflict, raisonPoussee(out))
			return
		}
		enregistre := ""
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "Saved: ") {
				enregistre = strings.TrimPrefix(l, "Saved: ")
			}
		}
		avant, _ := s.etatGitHub(p)
		sortie, _ := s.executerGitHub(p, 20*time.Minute, "pousser")
		v, err := s.etatGitHub(p)
		if err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		if quandDernier(v) == quandDernier(avant) {
			s.erreur(w, http.StatusConflict, raisonPoussee(sortie))
			return
		}
		s.ecrireJSON(w, map[string]any{"github": v, "enregistre": enregistre})
	case "auto":
		a := "0"
		if c.Auto {
			a = "1"
		}
		if _, err := s.executerGitHub(p, 2*time.Minute, "auto", a); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.msg(w, "Saved.")
	case "atelier": // the workshop shared on GitHub, or not
		a := "0"
		if c.Atelier {
			a = "1"
		}
		if _, err := s.executerGitHub(p, 2*time.Minute, "partage", a); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.msg(w, "Saved.")
	case "branche":
		b := strings.TrimSpace(c.Valeur)
		if b != "" && (!reBrancheGitHub.MatchString(b) || strings.Contains(b, "..")) {
			s.erreur(w, http.StatusBadRequest, "Invalid branch name.")
			return
		}
		if _, err := s.executerGitHub(p, 2*time.Minute, "branche", b); err != nil {
			if strings.Contains(err.Error(), "Not linked") {
				s.erreur(w, http.StatusBadRequest, "Not linked to GitHub.")
			} else {
				s.erreur(w, http.StatusBadRequest, "Invalid branch name.")
			}
			return
		}
		s.msg(w, "Saved.")
	case "identite": // author of the commits (name, email) and Claude as co-author or not
		nom, email := strings.TrimSpace(c.Nom), strings.TrimSpace(c.Email)
		if nom == "" || email == "" { // the co-author switch alone: the author as it stands, else "Athanor"
			if v, err := s.etatGitHub(p); err == nil {
				var id struct {
					Nom    string `json:"nom"`
					Email  string `json:"email"`
					Noms   []string
					Emails []struct {
						Email string `json:"email"`
					} `json:"emails"`
				}
				_ = json.Unmarshal(v.Identite, &id)
				nom, email = firstNonEmpty(nom, id.Nom), firstNonEmpty(email, id.Email)
				if nom == "" && len(id.Noms) > 0 {
					nom = id.Noms[0]
				}
				if email == "" && len(id.Emails) > 0 {
					email = id.Emails[0].Email
				}
			}
			nom, email = firstNonEmpty(nom, "Athanor"), firstNonEmpty(email, "athanor@localhost.localdomain")
		}
		if nom == "" || len(nom) > 100 || strings.ContainsAny(nom, "<>\n\r\t") {
			s.erreur(w, http.StatusBadRequest, "Invalid name.")
			return
		}
		if !reEmailAuteur.MatchString(email) {
			s.erreur(w, http.StatusBadRequest, "Invalid email.")
			return
		}
		sans := "0"
		if c.SansCo {
			sans = "1"
		}
		if _, err := s.executerGitHub(p, 2*time.Minute, "regler-identite", nom, email, sans); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		s.msg(w, "Saved: the next commits are signed this way (from the next cycle).")
	case "delier":
		if _, err := s.executerGitHub(p, 5*time.Minute, "delier"); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.msg(w, "Unlinked: the key is deleted from this PC. GitHub keeps an unusable entry under the repository's Settings › Deploy keys, which you can remove.")
	case "ouvrir":
		v, err := s.etatGitHub(p)
		if err != nil || !v.Lie || !reDepotGitHub.MatchString(v.Depot) {
			s.erreur(w, http.StatusBadRequest, "Not linked to GitHub.")
			return
		}
		if err := ouvrirLien("https://github.com/" + v.Depot); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.ecrireJSON(w, map[string]bool{"ok": true})
	default:
		s.erreur(w, http.StatusBadRequest, "Unknown")
	}
}

func quandDernier(v GitHubVue) string {
	var d struct {
		Quand string `json:"quand"`
	}
	_ = json.Unmarshal(v.Dernier, &d)
	return d.Quand
}

// raisonPoussee: why athanor github pousser recorded nothing (a key translated by the interface).
func raisonPoussee(out string) string {
	for _, c := range []string{"Another push is under way.", "Not linked to GitHub.", "This push is no longer the one that was blocked: refresh.", "Cannot prepare the push folder."} {
		if strings.Contains(out, c) {
			return c
		}
	}
	if strings.Contains(out, "Automatic push is off.") {
		return "Not linked to GitHub."
	}
	if out = strings.TrimSpace(out); out != "" {
		return out
	}
	return "GitHub: unreadable answer from WSL"
}

// suivreGitHub: the project's folder changed, its GitHub link follows (best effort).
func (s *Serveur) suivreGitHub(ancien, nouveau string) {
	_, _ = executerKitArgs(s.kitCourant(), 2*time.Minute, "github", "suivre", ancien, nouveau)
}

// oublierGitHub: the project's folder went to the recycle bin, its key is deleted (best effort).
func (s *Serveur) oublierGitHub(p Projet) {
	_, _ = s.executerGitHub(p, 5*time.Minute, "delier")
}
