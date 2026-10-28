package main

// Settings (the tool's .env), tooling health, tasks, options, virtual desktop, opening folders.

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ----------------------------------------------------------------------------- .env

const masque = "••••••••"

// clesEnvCachees: settings of older versions, still in an older .env but read by nothing any more:
// never shown (Unreal: the MCP library; PILOT_ENGINE: there is one pilot only, in Go, since 2.25).
var clesEnvCachees = map[string]bool{"UNREAL_MCP": true, "UNREAL_MCP_URL": true, "PILOT_ENGINE": true}

func (s *Serveur) apiEnv(w http.ResponseWriter, r *http.Request) {
	chemin := s.kitCourant().Env()
	if _, err := os.Stat(chemin); err != nil {
		chemin = s.kitCourant().EnvExemple()
	}
	montrer := r.URL.Query().Get("secrets") == "1"
	out := []LigneEnv{}
	lignes := LireEnv(chemin)
	// Settings added by a newer version of the tool: present in .env.example but not yet in the
	// user's .env. They appear with their default value and are written on save. The help text
	// always comes from .env.example (up to date, translatable).
	presents, aides := map[string]bool{}, map[string]string{}
	for _, l := range lignes {
		presents[l.Cle] = true
	}
	for _, l := range LireEnv(s.kitCourant().EnvExemple()) {
		if l.Cle == "" {
			continue
		}
		aides[l.Cle] = l.Aide
		if !presents[l.Cle] {
			lignes = append(lignes, l)
		}
	}
	for _, l := range lignes {
		if l.Cle == "" || clesEnvCachees[l.Cle] {
			continue
		}
		if a := aides[l.Cle]; a != "" {
			l.Aide = a
		}
		if l.Secret && !montrer && l.Valeur != "" {
			l.Valeur = masque // never the real value without an explicit request
		}
		out = append(out, l)
	}
	s.ecrireJSON(w, out)
}

func (s *Serveur) apiEcrireEnv(w http.ResponseWriter, r *http.Request) {
	var valeurs map[string]string
	if err := lireCorps(r, &valeurs); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	for k, v := range valeurs {
		if !reCle.MatchString(k) || strings.ContainsAny(v, "\n\r") {
			s.erreur(w, http.StatusBadRequest, "Invalid setting: "+k)
			return
		}
		if v == masque {
			delete(valeurs, k) // secret unchanged
		}
	}
	chemin := s.kitCourant().Env()
	if _, err := os.Stat(chemin); err != nil {
		if b, err := os.ReadFile(s.kitCourant().EnvExemple()); err == nil {
			_ = os.WriteFile(chemin, b, 0o600)
		}
	}
	if err := EcrireEnv(chemin, valeurs); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Settings saved. They apply to pilots started afterwards (“Restart after cycle” on a running project).")
}

// ----------------------------------------------------------------------------- health

func (s *Serveur) apiSante(w http.ResponseWriter, r *http.Request) {
	vues := []VerifVue{}
	for _, v := range s.listeVerifs() {
		vues = append(vues, v.Vue())
	}
	s.ecrireJSON(w, vues)
}

// listeVerifs: the checks of the current mode (WSL, or native Windows).
func (s *Serveur) listeVerifs() []*Verif {
	if modeNatif.Load() {
		return s.verifsNatif
	}
	return s.verifs
}

func (s *Serveur) trouverVerif(id string) *Verif {
	for _, v := range s.listeVerifs() {
		if v.ID == id {
			return v
		}
	}
	return nil
}

func (s *Serveur) apiVerifier(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID string `json:"id"`
	}
	_ = lireCorps(r, &c)
	if c.ID == "" || c.ID == "tout" {
		for _, v := range s.listeVerifs() {
			v.Verifier(s.kitCourant())
		}
		s.msg(w, "Checks started (the model connection takes up to a minute).")
		return
	}
	v := s.trouverVerif(c.ID)
	if v == nil {
		s.erreur(w, http.StatusNotFound, "Unknown check")
		return
	}
	v.Verifier(s.kitCourant())
	s.ecrireJSON(w, map[string]any{"message_key": "Check started again.", "tache": v.tache.ID})
}

func (s *Serveur) apiReparer(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID string `json:"id"`
	}
	_ = lireCorps(r, &c)
	v := s.trouverVerif(c.ID)
	if v == nil {
		s.erreur(w, http.StatusNotFound, "Unknown check")
		return
	}
	msg, err := v.Reparer(s.kitCourant())
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"message_key": msg, "tache": v.Vue().ReparationID})
}

func (s *Serveur) apiClaude(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID         string `json:"id"`
		Complement string `json:"complement"`
	}
	_ = lireCorps(r, &c)
	v := s.trouverVerif(c.ID)
	if v == nil {
		s.erreur(w, http.StatusNotFound, "Unknown check")
		return
	}
	t, err := v.DemanderAClaude(s.kitCourant(), c.Complement)
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"message_key": "Claude is repairing “{0}”.", "args": []string{v.Titre}, "tache": t.ID})
}

func (s *Serveur) apiLibre(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Texte string `json:"texte"`
	}
	_ = lireCorps(r, &c)
	if strings.TrimSpace(c.Texte) == "" {
		s.erreur(w, http.StatusBadRequest, "Missing text")
		return
	}
	t, err := DemandeLibre(s.kitCourant(), c.Texte)
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"message_key": "Request sent to Claude.", "tache": t.ID})
}

func (s *Serveur) apiBarriere(w http.ResponseWriter, r *http.Request) {
	t := TestBarriere(s.kitCourant())
	s.ecrireJSON(w, map[string]any{"message_key": "Sandbox test started (1 to 2 minutes).", "tache": t.ID})
}

// apiRecompiler: athanor reparer --recompiler, by this very executable, its output in the task.
func (s *Serveur) apiRecompiler(w http.ResponseWriter, r *http.Request) {
	exe, err := os.Executable()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	t := NouvelleTache("recompiler", "Rebuild Athanor")
	t.Lancer(commande(exe, "reparer", "--recompiler", s.kitCourant().Racine), nil)
	s.ecrireJSON(w, map[string]any{"message_key": "Rebuild started: the output is below; restart the app when it is done.", "tache": t.ID})
}

func (s *Serveur) apiInstaller(w http.ResponseWriter, r *http.Request) {
	// "Install everything for native Windows" / "for WSL": the mode chosen first (never while a pilot runs)
	var c struct {
		Mode string `json:"mode"`
	}
	_ = lireCorps(r, &c)
	if runtime.GOOS == "windows" && (c.Mode == "natif" || c.Mode == "wsl") {
		s.mu.Lock()
		code, err := s.changerMode(c.Mode == "natif")
		if err == nil {
			code, err = http.StatusInternalServerError, ecrireConfig(s.config)
		}
		s.mu.Unlock()
		if err != nil {
			s.erreur(w, code, err.Error())
			return
		}
	}
	if runtime.GOOS != "windows" { // Linux, macOS: the installer runs in a terminal (it asks for the password once)
		if err := fenetreKit(s.kitCourant(), "Athanor", "installer"); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.msg(w, "Installer opened in a window. When it is done, come back here and run “Check everything”.")
		return
	}
	commande := "installer-windows" // WSL2, its network, then athanor installer in WSL, administrator rights
	if modeNatif.Load() {
		commande = "installer-natif" // native Windows: Git for Windows, jq, Python, Claude Code for Windows
	}
	if err := fenetreWindows(s.kitCourant(), "Athanor - installation", commande); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Installer opened in a window. When it is done, come back here and run “Check everything”.")
}

func (s *Serveur) apiTache(w http.ResponseWriter, r *http.Request) {
	t := TrouverTache(r.PathValue("id"))
	if t == nil {
		s.erreur(w, http.StatusNotFound, "Unknown task")
		return
	}
	vue := t.Vue(600)
	ev := []Evenement{}
	for _, l := range vue.Sortie {
		if strings.HasPrefix(l, "{") {
			ev = append(ev, Traduire(l)...)
		} else {
			ev = append(ev, Evenement{Genre: "brut", Titre: l, Brut: l})
		}
	}
	s.ecrireJSON(w, map[string]any{"tache": vue, "evenements": ev})
}

// ----------------------------------------------------------------------------- options

type OptionsVue struct {
	Projets                   []Projet   `json:"projets"`
	Kit                       string     `json:"kit"`
	KitValide                 bool       `json:"kit_valide"`
	LancerPiloteAuDemarrage   bool       `json:"lancer_pilote_au_demarrage"`
	BureauVirtuel             bool       `json:"bureau_virtuel"`
	ProgrammesSupplementaires string     `json:"programmes_supplementaires"`
	DemarrageWindows          bool       `json:"demarrage_windows"`
	MotDePasse                bool       `json:"mot_de_passe"`
	Bureau                    EtatBureau `json:"bureau"`
	ModeNatif                 bool       `json:"mode_natif"`
	Windows                   bool       `json:"windows"`
	GitBash                   string     `json:"git_bash"`        // its bash.exe, "" when missing
	VerifierMaj               bool       `json:"verifier_maj"`    // look for an update when Athanor opens (maj.go)
	EntendreBureau            bool       `json:"entendre_bureau"` // the programs on Claude's desktop are heard
}

func (s *Serveur) apiOptions(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	c := s.config
	s.mu.Unlock()
	projets := append([]Projet{}, c.Projets...)
	if projets == nil {
		projets = []Projet{}
	}
	s.ecrireJSON(w, OptionsVue{projets, c.Kit, s.kitCourant().Valide(), c.LancerPiloteAuDemarrage, c.BureauVirtuel, c.ProgrammesSupplementaires, DemarrageWindowsActif(), c.MotDePasseHash != "", s.bureau.Etat(), modeNatif.Load(), runtime.GOOS == "windows", bashNatif(), !c.PasVerifierMaj, c.EntendreBureauClaude})
}

func (s *Serveur) apiEcrireOptions(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Kit                       string `json:"kit"`
		LancerPiloteAuDemarrage   bool   `json:"lancer_pilote_au_demarrage"`
		BureauVirtuel             bool   `json:"bureau_virtuel"`
		ProgrammesSupplementaires string `json:"programmes_supplementaires"`
		DemarrageWindows          bool   `json:"demarrage_windows"`
		ModeNatif                 *bool  `json:"mode_natif"`
		VerifierMaj               *bool  `json:"verifier_maj"`
		EntendreBureau            *bool  `json:"entendre_bureau"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nouveau := Kit{strings.TrimSpace(c.Kit)}
	if nouveau.Racine != s.kitCourant().Racine {
		if !nouveau.Valide() {
			s.erreur(w, http.StatusBadRequest, "This folder does not contain template\\CLAUDE.md")
			return
		}
		s.kitMu.Lock()
		s.kit = nouveau
		definirKitTextes(nouveau.Racine)
		s.kitMu.Unlock()
		s.bureau.DefinirKit(nouveau)
		s.ecrireListeProjets()
	}
	if c.ModeNatif != nil {
		if code, err := s.changerMode(*c.ModeNatif); err != nil {
			s.erreur(w, code, err.Error())
			return
		}
	}
	if c.VerifierMaj != nil {
		s.config.PasVerifierMaj = !*c.VerifierMaj
	}
	if c.EntendreBureau != nil {
		s.config.EntendreBureauClaude = *c.EntendreBureau
	}
	s.config.Kit = nouveau.Racine
	s.config.LancerPiloteAuDemarrage = c.LancerPiloteAuDemarrage
	s.config.BureauVirtuel = c.BureauVirtuel
	s.config.ProgrammesSupplementaires = c.ProgrammesSupplementaires
	s.bureau.DefinirReglages(c.BureauVirtuel, strings.Split(c.ProgrammesSupplementaires, "\n"))
	if c.DemarrageWindows != DemarrageWindowsActif() {
		if err := DemarrageWindows(c.DemarrageWindows); err != nil {
			s.erreur(w, http.StatusInternalServerError, "Start with Windows: "+err.Error())
			return
		}
	}
	if err := ecrireConfig(s.config); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Options saved.")
}

// apiEcran returns a picture of the project's Claude screen on the Linux side (system/ecran.sh: the
// Linux windows of a game under test, Godot, a headed browser), for the "Claude's screen" window.
func (s *Serveur) apiEcran(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	c := commandeKit(s.kitCourant(), "ecran", "capture", p.Dossier)
	cacherFenetre(c)
	png, err := c.Output()
	if err != nil || len(png) < 8 || string(png[1:4]) != "PNG" {
		s.erreur(w, http.StatusNotFound, "Claude's screen is not running for this project (pilot stopped, or the option is off).")
		return
	}
	s.ecrireJSON(w, map[string]string{"png": base64.StdEncoding.EncodeToString(png)})
}

func (s *Serveur) apiBureau(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.PathValue("action") {
	case "voir":
		err = s.bureau.Voir()
	case "revenir":
		err = s.bureau.Revenir()
	case "ramener":
		err = s.bureau.RamenerTout()
	default:
		s.erreur(w, http.StatusNotFound, "Unknown action")
		return
	}
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

func (s *Serveur) apiOuvrir(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Quoi string `json:"quoi"`
	}
	_ = lireCorps(r, &c)
	k := s.kitCourant()
	chemins := map[string]string{"kit": k.Racine, "logs": k.Journaux(), "a_faire": filepath.Join(k.Journaux(), "TODO_FOR_YOU.md")}
	if p, ok := s.projet(r); ok {
		chemins["projet"] = p.Dossier
		chemins["atelier"] = p.Atelier()
		chemins["plan"] = p.Plan()
		chemins["log"] = p.PiloteLog()
		chemins["demande"] = p.Demande()
		chemins["attente"] = p.FileAttente()
		chemins["deliverable"] = filepath.Join(p.Dossier, "deliverable")
	}
	ch, ok := chemins[c.Quoi]
	if !ok {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	if err := Ouvrir(ch); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

// changerMode: WSL or native Windows (s.mu held). Never while a pilot runs: the one started in the other
// mode would be left on its own.
func (s *Serveur) changerMode(natif bool) (int, error) {
	if natif == modeNatif.Load() {
		return 0, nil
	}
	for _, p := range s.config.Projets {
		if p.PiloteActif() {
			return http.StatusConflict, errors.New("Stop the running pilots first: the mode changes for the pilots started afterwards.")
		}
	}
	if natif && runtime.GOOS != "windows" {
		return http.StatusBadRequest, errors.New("Native Windows mode: Windows only.")
	}
	s.config.ModeNatif = natif
	modeNatif.Store(natif)
	return 0, nil
}

// apiModeSysteme: the choice of the first steps (WSL or native Windows), on its own.
func (s *Serveur) apiModeSysteme(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Natif bool `json:"natif"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if code, err := s.changerMode(c.Natif); err != nil {
		s.erreur(w, code, err.Error())
		return
	}
	if err := ecrireConfig(s.config); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Options saved.")
}

// apiEtapeTerminal: a repair (?id=) or the installation (?id=installer) as the command line the terminal
// interface runs in its own terminal, in front of the user (on a computer without a screen there is no
// window to open; there, the user sees each command and answers sudo). Linux and macOS.
func (s *Serveur) apiEtapeTerminal(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS == "windows" {
		s.erreur(w, http.StatusBadRequest, "No automatic repair: ask Claude")
		return
	}
	k := s.kitCourant()
	id := r.URL.Query().Get("id")
	titre, args := "Install everything", []string{"installer"}
	switch id {
	case "installer":
	case "github-connecter": // gh's sign-in: a code and an address to open on any device
		titre, args = "Connect to GitHub", []string{"github", "connecter"}
	case "github-garder-acces": // one sign-in kept: repositories created without a window
		titre, args = "Create repositories without a window", []string{"github", "garder-acces"}
	case "github-creer": // a new private repository: one sign-in to create it, then the computer's connection
		p, ok := s.projetRequis(w, r)
		if !ok {
			return
		}
		nom, branche := strings.TrimSpace(r.URL.Query().Get("nom")), strings.TrimSpace(r.URL.Query().Get("branche"))
		if !reNomGitHub.MatchString(nom) || nom == "." || nom == ".." {
			s.erreur(w, http.StatusBadRequest, "Invalid repository name: letters, digits, dots, dashes and underscores only.")
			return
		}
		if branche != "" && (!reBrancheGitHub.MatchString(branche) || strings.Contains(branche, "..")) {
			s.erreur(w, http.StatusBadRequest, "Invalid branch name.")
			return
		}
		auto := "1"
		if r.URL.Query().Get("auto") == "0" {
			auto = "0"
		}
		titre, args = "Create a private repository", argsGitHub(p, "lier", "creer", nom, auto, branche, "0")
	default:
		v := s.trouverVerif(id)
		if v == nil {
			s.erreur(w, http.StatusNotFound, "Unknown check")
			return
		}
		if g := v.Reparation.Genre; g != "kit" && g != "fenetre" {
			s.erreur(w, http.StatusBadRequest, "No automatic repair: ask Claude")
			return
		}
		titre, args = firstNonEmpty(v.Reparation.Titre, v.Titre), v.Reparation.Args
	}
	s.ecrireJSON(w, map[string]any{"titre": titre, "ligne": append([]string{exeKit(k)}, argsKit(args)...), "dossier": k.Racine})
}
