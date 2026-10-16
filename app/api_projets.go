package main

// General state, projects, pilot, log, team.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ProjetVue struct {
	Nom     string `json:"nom"`
	Dossier string `json:"dossier"`
	Existe  bool   `json:"existe"`
	Actif   bool   `json:"actif"`
	Phase   string `json:"phase"`
	Jalon   string `json:"jalon"`
	Attente int    `json:"attente"`
	Courant bool   `json:"courant"`
}

func (s *Serveur) vuesProjets(courant string) []ProjetVue {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	v := []ProjetVue{}
	for _, p := range projets {
		pv := ProjetVue{Nom: p.Nom, Dossier: p.Dossier, Existe: p.Existe(), Actif: p.PiloteActif(), Courant: p.Nom == courant}
		if e, ok := p.LireEtat(); ok {
			pv.Phase, pv.Jalon = e.Phase, e.JalonEnCours
		}
		pv.Attente = len(p.LireFileAttente())
		s.surveillerRelance(p, pv.Actif) // "restart after the cycle" works for every project
		v = append(v, pv)
	}
	return v
}

type EtatVue struct {
	KitValide   bool        `json:"kit_valide"`
	KitInstalle bool        `json:"kit_installe"`
	Kit         string      `json:"kit"`
	Verrouille  bool        `json:"verrouille"`
	MotDePasse  bool        `json:"mot_de_passe"`
	Projets     []ProjetVue `json:"projets"`
	Projet      *ProjetVue  `json:"projet"`
	Actif       bool        `json:"actif"`
	Etat        *Etat       `json:"etat"`
	Contexte    int64       `json:"contexte"`
	Plan        []Jalon     `json:"plan"`
	Attente     int         `json:"attente"`
	Stats       []LigneStat `json:"stats"`
	Bureau      EtatBureau  `json:"bureau"`
	Maintenant  string      `json:"maintenant"`
	DemandeVide bool        `json:"demande_vide"`
	Langue      string      `json:"langue"`
	Theme       string      `json:"theme"`
	Accent      string      `json:"accent"`
	Bienvenue   bool        `json:"bienvenue"`
}

func (s *Serveur) apiEtat(w http.ResponseWriter, r *http.Request) {
	verrou := s.verrouillePour(r)
	s.mu.Lock()
	kit, mdp, courant := s.kitCourant(), s.config.MotDePasseHash != "", s.config.ProjetActif
	langue, bienvenue := s.config.Langue, !s.config.BienvenueVue
	theme, accent := s.config.Theme, s.config.Accent
	if !languesConnues[langue] {
		langue = "en"
	}
	s.mu.Unlock()
	if nom := r.URL.Query().Get("p"); nom != "" {
		courant = nom
	}
	vue := EtatVue{KitValide: kit.Valide(), KitInstalle: kit.Installe(), Verrouille: verrou, MotDePasse: mdp, Maintenant: time.Now().Format("15:04"), DemandeVide: true, Langue: langue, Theme: theme, Accent: accent, Bienvenue: bienvenue}
	p, ok := s.projet(r)
	if ok {
		vue.Actif = p.PiloteActif()
		if e, ok := p.LireEtat(); ok {
			vue.Etat = &e
		}
		s.surveillerRelance(p, vue.Actif)
	}
	if verrou {
		// locked: only "running / phase / cycle", no text from the project
		if vue.Etat != nil {
			vue.Etat = &Etat{Phase: vue.Etat.Phase, Cycle: vue.Etat.Cycle}
		}
		s.ecrireJSON(w, vue)
		return
	}
	vue.Kit = kit.Racine
	vue.Projets = s.vuesProjets(courant)
	vue.Bureau = s.bureau.Etat()
	if ok {
		for i := range vue.Projets {
			if vue.Projets[i].Nom == p.Nom {
				pv := vue.Projets[i]
				vue.Projet = &pv
			}
		}
		_, vue.Contexte, _ = s.journalDe(p).Lire(1 << 62)
		vue.Plan = p.LirePlan()
		vue.Attente = len(p.LireFileAttente())
		vue.Stats = p.LireStats()
		b, err := os.ReadFile(p.Demande())
		vue.DemandeVide = err != nil || demandeVide(string(b))
	}
	s.ecrireJSON(w, vue)
}

func (s *Serveur) apiJournal(w http.ResponseWriter, r *http.Request) {
	var apres int64
	_, _ = fmt.Sscan(r.URL.Query().Get("apres"), &apres)
	p, ok := s.projet(r)
	if !ok {
		s.ecrireJSON(w, map[string]any{"evenements": []Evenement{}, "contexte": 0, "fichier": ""})
		return
	}
	ev, contexte, fichier := s.journalDe(p).Lire(apres)
	if ev == nil {
		ev = []Evenement{}
	}
	s.ecrireJSON(w, map[string]any{"evenements": ev, "contexte": contexte, "fichier": filepath.Base(fichier)})
}

func (s *Serveur) apiAgents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projet(r)
	if !ok {
		s.ecrireJSON(w, []Agent{})
		return
	}
	a := s.journalDe(p).Agents()
	if a == nil {
		a = []Agent{}
	}
	// the available team (template/.claude/agents/*.md) appears even before being called
	vus := map[string]bool{}
	for _, x := range a {
		vus[strings.ToLower(x.Nom)] = true
	}
	for _, x := range s.kitCourant().Equipe() {
		if !vus[strings.ToLower(x.Nom)] {
			a = append(a, Agent{ID: x.Nom, Nom: x.Nom, Mission: x.Mission, Statut: "disponible"})
		}
	}
	s.ecrireJSON(w, a)
}

func (s *Serveur) apiPilote(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	var sortie string
	var err error
	switch r.PathValue("action") {
	case "demarrer":
		marqueAuto(p, true)
		cle, err := s.demarrerPilote(p)
		if err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.msg(w, cle)
		return
	case "arreter":
		marqueAuto(p, false)
		sortie, err = s.scriptProjet(p, "stop.sh")
	case "arreter-apres-cycle":
		marqueAuto(p, false)
		sortie, err = s.scriptProjet(p, "stop-after-cycle.sh")
	case "redemarrer-apres-cycle":
		s.mu.Lock()
		s.relancer[p.Nom] = true
		s.mu.Unlock()
		sortie, err = s.scriptProjet(p, "stop-after-cycle.sh")
	default:
		s.erreur(w, http.StatusNotFound, "Unknown action")
		return
	}
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]string{"message": sortie})
}

// ----------------------------------------------------------------------------- projects

var reNomProjet = regexp.MustCompile(`^[\p{L}\p{N} _.-]{1,60}$`)

func (s *Serveur) apiProjets(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	courant := s.config.ProjetActif
	s.mu.Unlock()
	s.ecrireJSON(w, s.vuesProjets(courant))
}

func (s *Serveur) apiAjouterProjet(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom     string `json:"nom"`
		Dossier string `json:"dossier"`
		Demande string `json:"demande"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	c.Nom, c.Dossier = strings.TrimSpace(c.Nom), strings.TrimSpace(c.Dossier)
	if !reNomProjet.MatchString(c.Nom) {
		s.erreur(w, http.StatusBadRequest, "Invalid project name (letters, digits, spaces, dashes, 60 characters at most)")
		return
	}
	if !filepath.IsAbs(c.Dossier) {
		s.erreur(w, http.StatusBadRequest, "Give the full path of the development folder (e.g. D:\\Dev\\my-site)")
		return
	}
	dossier := filepath.Clean(c.Dossier)
	if cle := dossierInterdit(dossier, s.kitCourant().Racine); cle != "" {
		s.erreur(w, http.StatusBadRequest, cle)
		return
	}
	s.mu.Lock()
	for _, p := range s.config.Projets {
		if strings.EqualFold(p.Nom, c.Nom) || strings.EqualFold(filepath.Clean(p.Dossier), dossier) {
			s.mu.Unlock()
			s.erreur(w, http.StatusBadRequest, "A project with this name or folder already exists")
			return
		}
	}
	s.mu.Unlock()
	p := Projet{Nom: c.Nom, Dossier: dossier}
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		s.erreur(w, http.StatusInternalServerError, "Cannot create the folder: "+err.Error())
		return
	}
	if strings.TrimSpace(c.Demande) != "" {
		if b, _ := os.ReadFile(p.Demande()); demandeVide(string(b)) {
			_ = os.WriteFile(p.Demande(), []byte(s.texteDemande(c.Nom, c.Demande)), 0o644)
		}
	}
	s.mu.Lock()
	p.MigrerAtelier()
	s.config.Projets = append(s.config.Projets, p)
	s.config.ProjetActif = p.Nom
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.bureau.DefinirDossiers(s.dossiersProjets())
	s.ecrireListeProjets()
	s.ecrireJSON(w, map[string]any{"message_key": "Project “{0}” added.", "args": []string{p.Nom}, "nom": p.Nom})
}

// texteDemande composes the first request of a project (title, date, text, notes section).
func (s *Serveur) texteDemande(titre, texte string) string {
	l := s.langue()
	return "# " + strings.TrimSpace(titre) + "\n\n" + libelle(l, "demande_le") + " " + DateCourte() + ".\n\n" + strings.TrimSpace(texte) +
		"\n\n" + libelle(l, "remarques") + "\n" + libelle(l, "remarques_aide") + "\n\n"
}

func (s *Serveur) langue() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if languesConnues[s.config.Langue] {
		return s.config.Langue
	}
	return "en"
}

// apiPreferences saves the language, the theme, the accent and the state of the welcome wizard.
// The language is also written into the tool's .env (LANGUAGE) so that Claude works in that language.
func (s *Serveur) apiPreferences(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Langue       *string `json:"langue"`
		Theme        *string `json:"theme"`
		Accent       *string `json:"accent"`
		BienvenueVue *bool   `json:"bienvenue_vue"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	if c.Langue != nil && !languesConnues[*c.Langue] {
		s.erreur(w, http.StatusBadRequest, "Unknown language")
		return
	}
	if c.Theme != nil && !themesConnus[*c.Theme] || c.Accent != nil && !accentsConnus[*c.Accent] {
		s.erreur(w, http.StatusBadRequest, "Unknown theme")
		return
	}
	s.mu.Lock()
	if c.Langue != nil {
		s.config.Langue = *c.Langue
	}
	if c.Theme != nil {
		s.config.Theme = *c.Theme
	}
	if c.Accent != nil {
		s.config.Accent = *c.Accent
	}
	if c.BienvenueVue != nil {
		s.config.BienvenueVue = *c.BienvenueVue
	}
	langue := s.config.Langue
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Langue != nil {
		if _, err := os.Stat(s.kitCourant().Env()); err == nil {
			_ = EcrireEnv(s.kitCourant().Env(), map[string]string{"LANGUAGE": langue})
		}
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

func (s *Serveur) apiChoisirProjet(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom string `json:"nom"`
	}
	_ = lireCorps(r, &c)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.config.Projets {
		if p.Nom == c.Nom {
			s.config.ProjetActif = p.Nom
			_ = ecrireConfig(s.config)
			s.ecrireJSON(w, map[string]bool{"ok": true})
			return
		}
	}
	s.erreur(w, http.StatusNotFound, "Unknown project")
}

// Removes a project from the list. The folder and its workshop are never deleted by the application.
func (s *Serveur) apiRetirerProjet(w http.ResponseWriter, r *http.Request) {
	nom := r.PathValue("nom")
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.config.Projets {
		if p.Nom != nom {
			continue
		}
		if p.PiloteActif() {
			s.erreur(w, http.StatusBadRequest, "Stop its pilot first")
			return
		}
		s.config.Projets = append(s.config.Projets[:i], s.config.Projets[i+1:]...)
		if s.config.ProjetActif == nom {
			s.config.ProjetActif = ""
		}
		delete(s.logs, nom)
		_ = ecrireConfig(s.config)
		s.bureau.DefinirDossiers(s.dossiersProjets())
		s.ecrireListeProjets()
		s.msg(w, "Project removed from the list. Its folder is intact; delete its .athanor subfolder to clean it.")
		return
	}
	s.erreur(w, http.StatusNotFound, "Unknown project")
}

// Small folder browser to pick a project's folder.
func (s *Serveur) apiDossiers(w http.ResponseWriter, r *http.Request) {
	type entree struct {
		Nom    string `json:"nom"`
		Chemin string `json:"chemin"`
	}
	chemin := r.URL.Query().Get("chemin")
	if chemin == "" {
		chemin, _ = os.UserHomeDir()
	}
	chemin = filepath.Clean(chemin)
	dossiers := []entree{}
	if entrees, err := os.ReadDir(chemin); err == nil {
		for _, e := range entrees {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasPrefix(e.Name(), "$") {
				dossiers = append(dossiers, entree{e.Name(), filepath.Join(chemin, e.Name())})
			}
		}
	}
	parent := filepath.Dir(chemin)
	if parent == chemin {
		parent = ""
	}
	racines := []entree{}
	for _, l := range "CDEFGH" {
		d := string(l) + ":\\"
		if _, err := os.Stat(d); err == nil {
			racines = append(racines, entree{string(l) + ":", d})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		racines = append(racines, entree{"~", home})
	}
	s.ecrireJSON(w, map[string]any{"chemin": chemin, "parent": parent, "dossiers": dossiers, "racines": racines})
}

// scriptProjet runs commands/<script> in WSL for a project (Windows path converted by wslpath).
func (s *Serveur) scriptProjet(p Projet, script string, args ...string) (string, error) {
	return executerKit(s.kitCourant(), ligneProjet(p, script, args...))
}

// ligneProjet builds the call of a commands/ script with the project folder converted to a WSL path.
func ligneProjet(p Projet, script string, args ...string) string {
	dossier := strings.ReplaceAll(p.Dossier, "'", "'\\''")
	return "P=\"$(wslpath -u '" + dossier + "' 2>/dev/null || printf '%s' '" + dossier + "')\"; bash ./commands/" + script + " " + strings.Join(args, " ") + " \"$P\""
}

// demarrerPilote starts a project's pilot. On Windows, WSL stops the distribution a few seconds after
// the last wsl.exe ends, which would kill a backgrounded pilot: the pilot therefore runs in the
// foreground of a hidden, detached wsl.exe that survives the application being closed.
func (s *Serveur) demarrerPilote(p Projet) (string, error) {
	if p.PiloteActif() {
		return "The pilot is already running on this project.", nil // message key
	}
	if err := DemarrerDetache(s.kitCourant(), ligneProjet(p, "start.sh", "--keep")); err != nil {
		return "", err
	}
	return "Pilot started: the first cycle (framing) takes a few minutes.", nil
}

// ScriptKit runs commands/<script> in WSL without a project (short, blocking).
func ScriptKit(k Kit, script string, args ...string) (string, error) {
	return executerKit(k, "bash ./commands/"+script+" "+strings.Join(args, " "))
}

func executerKit(k Kit, ligne string) (string, error) {
	c := commandeWSL(k, ligne)
	cacherFenetre(c)
	out, err := c.CombinedOutput()
	texte := strings.TrimSpace(nettoyerANSI(string(out)))
	if err != nil {
		return texte, fmt.Errorf("%s (%v)", texte, err)
	}
	return texte, nil
}

// ----------------------------------------------------------------------------- working mode of the project

// ModeVue: project-specific settings, in PROJECT/.athanor/.env (read by the pilot at every cycle).
type ModeVue struct {
	Parallele    bool `json:"parallele"`
	ParalleleMax int  `json:"parallele_max"`
}

func (p Projet) envProjet() string { return filepath.Join(p.Atelier(), ".env") }

func (p Projet) LireMode() ModeVue {
	m := ModeVue{ParalleleMax: 3}
	MigrerEnv(p.envProjet())
	for _, l := range LireEnv(p.envProjet()) {
		switch l.Cle {
		case "PARALLEL":
			m.Parallele = l.Valeur == "1"
		case "PARALLEL_MAX":
			if n, err := strconv.Atoi(l.Valeur); err == nil && n >= 2 && n <= 6 {
				m.ParalleleMax = n
			}
		}
	}
	return m
}

func (s *Serveur) apiMode(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	s.ecrireJSON(w, p.LireMode())
}

func (s *Serveur) apiEcrireMode(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	var m ModeVue
	if err := lireCorps(r, &m); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	if m.ParalleleMax < 2 || m.ParalleleMax > 6 {
		m.ParalleleMax = 3
	}
	v := map[string]string{"PARALLEL": "0", "PARALLEL_MAX": strconv.Itoa(m.ParalleleMax)}
	if m.Parallele {
		v["PARALLEL"] = "1"
	}
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := EcrireEnv(p.envProjet(), v); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m.Parallele {
		s.msg(w, "Parallel mode on: it applies from the next cycle (the plan is first organized in waves).")
	} else {
		s.msg(w, "Parallel mode off: from the next cycle, the lead works milestone by milestone.")
	}
}

// dossierInterdit refuses a project folder that would contain the tool (Claude could rewrite the pilot
// and the gateways that run outside the sandbox), that would be inside the tool, or that would be a drive
// root or the home folder (git and Claude would see everything). Returns the message key, or "".
func dossierInterdit(dossier, kit string) string {
	norm := func(p string) string {
		p = strings.ToLower(filepath.Clean(p))
		return strings.TrimRight(p, `\/`) + string(filepath.Separator)
	}
	d := norm(dossier)
	if kit != "" {
		k := norm(kit)
		if strings.HasPrefix(d, k) {
			return "The project must be outside the tool folder"
		}
		if strings.HasPrefix(k, d) {
			return "The project folder must not contain the tool folder"
		}
	}
	if filepath.Dir(filepath.Clean(dossier)) == filepath.Clean(dossier) || len(strings.Trim(filepath.Clean(dossier), `\/:.`)) <= 1 {
		return "Choose a folder for the project, not a whole drive"
	}
	if home, err := os.UserHomeDir(); err == nil && norm(home) == d {
		return "Choose a folder for the project, not your whole personal folder"
	}
	return ""
}
