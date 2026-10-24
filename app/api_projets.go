package main

// General state, projects, pilot, log, team.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type ProjetVue struct {
	Nom      string  `json:"nom"`
	Dossier  string  `json:"dossier"`
	Existe   bool    `json:"existe"`
	Actif    bool    `json:"actif"`
	Phase    string  `json:"phase"`
	Jalon    string  `json:"jalon"`
	Attente  int     `json:"attente"`
	PourVous int     `json:"pour_vous,omitempty"` // tasks waiting for the user (api_pourvous.go)
	Courant  bool    `json:"courant"`
	Fin      *FinVue `json:"fin,omitempty"`
	Archive  bool    `json:"archive,omitempty"`
	// milestones unticked by the folder check, waiting for their files
	AVerifier bool `json:"a_verifier,omitempty"`
}

// FinVue: the pilot of a project stopped by itself (delivery, maximum duration…) and the user has not
// been told yet; the interface shows it in a dialog, then acknowledges it (POST /api/fin-vue).
type FinVue struct {
	Cle     string `json:"cle"`
	Livre   bool   `json:"livre"`
	Message string `json:"message"`
}

func (s *Serveur) vuesProjets(courant string) []ProjetVue {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	vues := map[string]string{}
	for k, x := range s.config.FinsVues {
		vues[k] = x
	}
	s.mu.Unlock()
	v := []ProjetVue{}
	for _, p := range projets {
		pv := ProjetVue{Nom: p.Nom, Dossier: p.Dossier, Existe: p.Existe(), Actif: p.PiloteActif(), Courant: p.Nom == courant, Archive: p.Archive}
		if p.Archive {
			v = append(v, pv) // archived: listed (archived projects page), nothing else
			continue
		}
		if pv.Courant {
			s.verifierEnFond(p)
		}
		pv.AVerifier = p.AVerifier()
		if e, ok := p.LireEtat(); ok {
			pv.Phase, pv.Jalon = e.Phase, e.JalonEnCours
			// "termine": the pilot ended on its own (a stop asked by the user is "arrete")
			if e.Phase == "termine" && e.Maj != "" && vues[p.Dossier] != e.Maj {
				pv.Fin = &FinVue{Cle: e.Maj, Livre: projetLivre(p), Message: e.Etat}
			}
		}
		pv.Attente = len(p.LireFileAttente())
		pv.PourVous = p.PourVous()
		s.surveillerRelance(p, pv.Actif) // "restart after the cycle" works for every project
		v = append(v, pv)
	}
	return v
}

type EtatVue struct {
	KitValide   bool                  `json:"kit_valide"`
	KitInstalle bool                  `json:"kit_installe"`
	Kit         string                `json:"kit"`
	Verrouille  bool                  `json:"verrouille"`
	MotDePasse  bool                  `json:"mot_de_passe"`
	Projets     []ProjetVue           `json:"projets"`
	Projet      *ProjetVue            `json:"projet"`
	Actif       bool                  `json:"actif"`
	Etat        *Etat                 `json:"etat"`
	Contexte    int64                 `json:"contexte"`
	Plan        []Jalon               `json:"plan"`
	Attente     int                   `json:"attente"`
	PourVous    int                   `json:"pour_vous"`
	Stats       []LigneStat           `json:"stats"`
	Bureau      EtatBureau            `json:"bureau"`
	Maintenant  string                `json:"maintenant"`
	DemandeVide bool                  `json:"demande_vide"`
	Langue      string                `json:"langue"`
	Theme       string                `json:"theme"`
	Accent      string                `json:"accent"`
	Bienvenue   bool                  `json:"bienvenue"`
	Version     string                `json:"version"` // Athanor's version (the sidebar, Settings › Options)
	Natif       bool                  `json:"natif"`   // native Windows mode: no Linux screen
	Windows     bool                  `json:"windows"` // the app runs on Windows: WSL or native mode to choose
	Systeme     string                `json:"systeme"` // runtime.GOOS: windows, linux, darwin (the Setup page's tabs)
	Quota       *EtatQuota            `json:"quota,omitempty"`
	QuotaArret  map[string]RegleQuota `json:"quota_arret"`
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
	vue := EtatVue{KitValide: kit.Valide(), KitInstalle: kit.Installe(), Verrouille: verrou, MotDePasse: mdp, Maintenant: time.Now().Format("15:04"), DemandeVide: true, Langue: langue, Theme: theme, Accent: accent, Bienvenue: bienvenue, Version: versionAthanor}
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
	vue.Natif = modeNatif.Load()
	vue.Windows = runtime.GOOS == "windows"
	vue.Systeme = runtime.GOOS
	s.mu.Lock()
	tous := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	vue.Quota = quotaActuel(tous)
	vue.QuotaArret = lireReglesQuota(kit)
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
		vue.PourVous = p.PourVous()
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

// MembreVue: a member of the team that can be called, with its model and who may call it.
type MembreVue struct {
	Nom       string `json:"nom"`
	Mission   string `json:"mission"`
	Modele    string `json:"modele"`     // "" = the lead's model
	AppelePar string `json:"appele_par"` // chef, chef_ouvriers
	// the project's own sub-agents (Team page, or proposed by the lead): their definition, to edit it
	Perso *AgentPerso `json:"perso,omitempty"`
}

// apiAgents: the team of the current cycle (sessions and the sub-agents each one called) and the
// members that can be called.
func (s *Serveur) apiAgents(w http.ResponseWriter, r *http.Request) {
	reponse := map[string]any{"agents": []Agent{}, "equipe": []MembreVue{}}
	p, ok := s.projet(r)
	if !ok {
		s.ecrireJSON(w, reponse)
		return
	}
	agents := s.journalDe(p).Agents()
	if agents == nil {
		agents = []Agent{}
	}
	titres := map[string]string{}
	for _, jl := range p.LirePlan() {
		titres[jl.ID] = jl.Titre
	}
	for i := range agents {
		if agents[i].Genre == "ouvrier" {
			if t := titres[agents[i].Jalon]; t != "" {
				agents[i].Mission = t
			}
		}
	}
	// models as the pilot gives them (system/pilot.sh): CHEAP_MODEL for executor (absent = haiku,
	// empty = like the others), SUBAGENT_MODEL for the others (empty = the lead's, or sonnet for scout); never the token.
	reglages := map[string]string{}
	presents := map[string]bool{}
	for _, l := range LireEnv(s.kitCourant().Env()) {
		if l.Cle == "MODEL" || l.Cle == "SUBAGENT_MODEL" || l.Cle == "CHEAP_MODEL" {
			reglages[l.Cle], presents[l.Cle] = strings.TrimSpace(l.Valeur), true
		}
	}
	simple := "haiku"
	if presents["CHEAP_MODEL"] {
		simple = reglages["CHEAP_MODEL"]
	}
	equipe := []MembreVue{}
	for _, x := range s.kitCourant().Equipe() {
		m := MembreVue{Nom: x.Nom, Mission: x.Mission, Modele: x.Modele, AppelePar: "chef"}
		if m.Modele == "" && x.Nom == "executor" {
			m.Modele = simple
		}
		if m.Modele == "" {
			m.Modele = reglages["SUBAGENT_MODEL"]
		}
		if strings.EqualFold(m.Modele, "auto") && (x.Nom == "executor" || x.Nom == "scout") {
			m.Modele = map[string]string{"executor": simple, "scout": "sonnet"}[x.Nom] // their default when the lead sets none
		}
		if m.Modele == "" && x.Nom == "scout" {
			m.Modele = "sonnet" // reading is execution work: Sonnet unless SUBAGENT_MODEL says otherwise
		}
		if x.Nom == "executor" {
			m.AppelePar = "chef_ouvriers"
		}
		equipe = append(equipe, m)
	}
	perso, refus := s.agentsDuProjet(p)
	for i := range perso {
		a := perso[i]
		m := MembreVue{Nom: a.Nom, Mission: a.Description, Modele: reglages["SUBAGENT_MODEL"], AppelePar: "chef_ouvriers", Perso: &a}
		switch a.Modele {
		case "eco":
			if simple != "" {
				m.Modele = simple
			}
		case "fort":
			m.Modele = "opus"
		}
		equipe = append(equipe, m)
	}
	reponse["agents"], reponse["equipe"], reponse["modele_chef"], reponse["refuses"] = agents, equipe, reglages["MODEL"], refus
	s.ecrireJSON(w, reponse)
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
	dossier := cheminWindowsPropre(filepath.Clean(c.Dossier))
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
	if err := s.conflitProjets(dossier, ""); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
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
	s.apresChangementProjets()
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
		if p.Nom == c.Nom && !p.Archive {
			s.config.ProjetActif = p.Nom
			_ = ecrireConfig(s.config)
			s.ecrireJSON(w, map[string]bool{"ok": true})
			return
		}
	}
	s.erreur(w, http.StatusNotFound, "Unknown project")
}

// Removes a project from the list. The folder and its workshop are never deleted here.
func (s *Serveur) apiRetirerProjet(w http.ResponseWriter, r *http.Request) {
	nom := r.PathValue("nom")
	s.mu.Lock()
	var p Projet
	trouve := false
	for _, x := range s.config.Projets {
		if x.Nom == nom {
			p, trouve = x, true
		}
	}
	s.mu.Unlock()
	if !trouve {
		s.erreur(w, http.StatusNotFound, "Unknown project")
		return
	}
	if p.PiloteActif() {
		s.erreur(w, http.StatusBadRequest, "Stop its pilot first")
		return
	}
	if !s.occuper(nom) {
		s.erreur(w, http.StatusBadRequest, "Another action is under way on this project.")
		return
	}
	defer s.liberer(nom)
	if err := s.retirerProjet(p); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Project removed from the list. Its folder is intact; delete its .athanor subfolder to clean it.")
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
	if s.estOccupe(p.Nom) {
		return "", errors.New("Another action is under way on this project.")
	}
	if p.Archive {
		return "", errors.New("This project is archived: bring it back first.")
	}
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
	return executerKitDelai(k, ligne, 20*time.Minute)
}

// executerKitDelai: executerKit, stopped after delai (WSL that does not answer, a command that hangs):
// the caller gets an error instead of waiting for ever.
func executerKitDelai(k Kit, ligne string, delai time.Duration) (string, error) {
	c := commandeWSL(k, ligne)
	cacherFenetre(c)
	c.WaitDelay = 5 * time.Second
	var bout strings.Builder
	c.Stdout, c.Stderr = &bout, &bout
	err := c.Start()
	if err == nil {
		minuteur := time.AfterFunc(delai, func() { _ = c.Process.Kill() })
		err = c.Wait()
		if !minuteur.Stop() {
			err = fmt.Errorf("no answer after %d s", int(delai.Seconds()))
		}
	}
	out := []byte(bout.String())
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
			if n, err := strconv.Atoi(l.Valeur); err == nil && n >= 2 && n <= 10 {
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
	if m.ParalleleMax < 2 || m.ParalleleMax > 10 {
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
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(norm(home), d) {
		return "Choose a folder for the project, not your whole personal folder"
	}
	// system folders: never a project there (deleting a project can send its folder to the recycle bin)
	for _, sys := range dossiersSysteme() {
		if sys != "" && strings.HasPrefix(d, norm(sys)) {
			return "Choose a folder for the project, not a system folder"
		}
	}
	return ""
}

// dossiersSysteme: folders a project can never be in.
func dossiersSysteme() []string {
	if runtime.GOOS == "windows" {
		return []string{os.Getenv("WINDIR"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramData"),
			os.Getenv("ProgramW6432"), os.Getenv("LocalAppData") + `\Programs`}
	}
	return []string{"/usr", "/etc", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/boot", "/proc", "/sys", "/dev", "/run", "/var/lib", "/var/log", "/snap", "/root"}
}

// apiFinVue: the user has seen the end of this project's pilot; it is not announced again.
func (s *Serveur) apiFinVue(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Cle string `json:"cle"`
	}
	if err := lireCorps(r, &c); err != nil || c.Cle == "" {
		s.erreur(w, http.StatusBadRequest, "Missing text")
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.FinsVues == nil {
		s.config.FinsVues = map[string]string{}
	}
	s.config.FinsVues[p.Dossier] = c.Cle
	if err := ecrireConfig(s.config); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}
