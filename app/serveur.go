package main

// Local HTTP server: shared state, request protection (same origin, locking), routes.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Serveur struct {
	mu           sync.Mutex
	config       Config
	kit          Kit // read with kitCourant() (can change from Options)
	kitMu        sync.RWMutex
	logs         map[string]*Journal
	bureau       *Bureau
	verifs       []*Verif // WSL mode
	verifsNatif  []*Verif // native Windows mode
	session      string   // session token when a password is set ("" = locked)
	csrf         string
	cleAcces     string               // cookie of the windows Athanor opened (acces.go); empty = no check (tests)
	ouvertures   map[string]time.Time // single-use window addresses
	relancer     map[string]bool      // projects to restart as soon as their pilot has stopped (settings change)
	dernierActif map[string]bool
	// last quick check of the milestones of a project waiting for its files (folder → time)
	derniereVerif map[string]time.Time
	occupes       map[string]bool // projects with a management action under way (no pilot start meanwhile)
	majProjets    sync.Mutex      // one update of the folder lists at a time
}

func nouveauServeur(config Config) *Serveur {
	kit := Kit{config.Kit}
	definirKitTextes(kit.Racine) // the texts of its local/ (textes.go)
	modeNatif.Store(config.ModeNatif && runtime.GOOS == "windows")
	s := &Serveur{
		config:       config,
		kit:          kit,
		logs:         map[string]*Journal{},
		verifs:       Verifications(),
		verifsNatif:  VerificationsNatives(),
		csrf:         aleatoire(16),
		dernierActif: map[string]bool{},
		relancer:     map[string]bool{},
	}
	MigrerEnv(kit.Env()) // old French keys of the .env → English names
	s.MigrerUnreal()     // UNREAL_MCP of the .env → the "unreal" server of the MCP library (2.13.0)
	for _, p := range s.config.Projets {
		p.MigrerAtelier() // .autopilote/ → .athanor/ (previous name)
	}
	if len(s.config.Projets) > 0 {
		s.ecrireListeProjets() // the admin installer and the firewall rules read it
	}
	s.bureau = NouveauBureau(kit)
	s.bureau.PilotesActifs = func() bool {
		s.mu.Lock()
		projets := append([]Projet{}, s.config.Projets...)
		s.mu.Unlock()
		for _, p := range projets {
			if p.PiloteActif() {
				return true
			}
		}
		return false
	}
	s.bureau.Langue = func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.config.Langue
	}
	s.bureau.DefinirReglages(config.BureauVirtuel, strings.Split(config.ProgrammesSupplementaires, "\n"))
	s.bureau.DefinirDossiers(s.dossiersProjets())
	return s
}

// ----------------------------------------------------------------------------- security

func aleatoire(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hacher(sel, mdp string) string {
	h := sha256.Sum256([]byte(sel + ":" + mdp))
	return hex.EncodeToString(h[:])
}

func egal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// verrouillePour says whether the request is locked: password set and no valid session.
func (s *Serveur) verrouillePour(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.MotDePasseHash == "" {
		return false
	}
	if s.session == "" {
		return true
	}
	c, err := r.Cookie("athanor_session")
	return err != nil || !egal(c.Value, s.session)
}

// garde protects a route: anti-CSRF token on every write, and session when a password exists.
func (s *Serveur) garde(libre bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Every API route requires the secret header, reads included: a web page can neither know it
		// nor send it (the Host header check, in main.go, blocks "DNS rebinding").
		if !s.accesValide(r) || !egal(r.Header.Get("X-Athanor"), s.csrf) {
			s.erreur(w, http.StatusForbidden, "Request refused (origin)")
			return
		}
		if !libre && s.verrouillePour(r) {
			s.erreur(w, http.StatusUnauthorized, "Locked")
			return
		}
		h(w, r)
	}
}

func (s *Serveur) ouvrirSession(w http.ResponseWriter) {
	s.session = aleatoire(24)
	http.SetCookie(w, &http.Cookie{Name: "athanor_session", Value: s.session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// ----------------------------------------------------------------------------- responses

func (s *Serveur) ecrireJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// erreur answers with a message key (translated by the interface); system errors pass through as is.
func (s *Serveur) erreur(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"erreur_key": msg, "erreur": msg})
}

// msg answers with a message key and its arguments ({0}, {1}…), translated by the interface.
func (s *Serveur) msg(w http.ResponseWriter, cle string, args ...string) {
	if args == nil {
		args = []string{}
	}
	s.ecrireJSON(w, map[string]any{"message_key": cle, "args": args})
}

func lireCorps(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20)).Decode(v)
}

// ----------------------------------------------------------------------------- projects

func (s *Serveur) dossiersProjets() []string {
	var d []string
	for _, p := range s.config.Projets {
		if !p.Archive {
			d = append(d, p.Dossier)
		}
	}
	return d
}

// ecrireListeProjets publishes the list of project folders for the kit scripts
// (deliverables firewall, for example). Never a secret in it.
func (s *Serveur) ecrireListeProjets() {
	_ = os.MkdirAll(s.kitCourant().Journaux(), 0o755)
	_ = os.WriteFile(filepath.Join(s.kitCourant().Journaux(), "projects.txt"), []byte(strings.Join(s.dossiersProjets(), "\n")+"\n"), 0o644)
}

// projet returns the project named by ?p=name, else the active project, else the first one.
func (s *Serveur) projet(r *http.Request) (Projet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nom := r.URL.Query().Get("p")
	if nom == "" {
		nom = s.config.ProjetActif
	}
	explicite := r.URL.Query().Get("p") != ""
	for _, p := range s.config.Projets {
		if p.Nom == nom {
			if p.Archive {
				if explicite { // never swapped for another project behind the user's back
					return Projet{}, false
				}
				break
			}
			return p, true
		}
	}
	if !explicite { // no active project (or an archived one): the first project in use
		for _, p := range s.config.Projets {
			if !p.Archive {
				return p, true
			}
		}
	}
	return Projet{}, false
}

// projetRequis: like projet, but answers 400 when no project is chosen.
func (s *Serveur) projetRequis(w http.ResponseWriter, r *http.Request) (Projet, bool) {
	p, ok := s.projet(r)
	if !ok {
		s.erreur(w, http.StatusBadRequest, "No project selected: add one (+ New project)")
	}
	return p, ok
}

func (s *Serveur) journalDe(p Projet) *Journal {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.logs[p.Nom]
	if j == nil {
		j = NouveauJournal(p)
		s.logs[p.Nom] = j
	}
	return j
}

// marqueAuto: a project started from the application is restarted the next time the application starts.
func marqueAuto(p Projet, actif bool) {
	f := filepath.Join(p.Atelier(), "auto")
	if actif {
		_ = os.MkdirAll(p.Atelier(), 0o755)
		_ = os.WriteFile(f, []byte("restart when the application starts\n"), 0o644)
	} else {
		_ = os.Remove(f)
	}
}

func lancerAuDemarrage(p Projet) bool {
	_, err := os.Stat(filepath.Join(p.Atelier(), "auto"))
	return err == nil
}

// restarts: after "restart after the cycle", restarts the pilot as soon as it has stopped.
func (s *Serveur) surveillerRelance(p Projet, actif bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dernierActif[p.Nom] && !actif && s.relancer[p.Nom] {
		delete(s.relancer, p.Nom)
		go func() { _, _ = s.demarrerPilote(p) }()
	}
	s.dernierActif[p.Nom] = actif
}

func (s *Serveur) demarrerProjetsAuto() {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	lancer := s.config.LancerPiloteAuDemarrage
	s.mu.Unlock()
	if !s.kitCourant().Valide() {
		return
	}
	// Pilots that survived the application being closed: attach them a command that keeps WSL
	// alive. --attach never starts a pilot: a pilot stopped meanwhile stays stopped (only the
	// "restart the pilots" option below starts anything).
	for _, p := range projets {
		if p.Archive {
			continue
		}
		if p.Existe() && p.PiloteActif() {
			pp := p
			go func() { _ = DemarrerDetache(s.kitCourant(), ligneProjet(pp, "start.sh", "--attach")) }()
		}
	}
	if !lancer {
		return
	}
	for _, p := range projets {
		if !p.Archive && p.Existe() && !p.PiloteActif() && lancerAuDemarrage(p) {
			pp := p
			go func() { _, _ = s.demarrerPilote(pp) }()
		}
	}
}

// ----------------------------------------------------------------------------- routes

func (s *Serveur) routes(mux *http.ServeMux) {
	// reachable even when locked (reduced answers)
	mux.HandleFunc("GET /api/etat", s.garde(true, s.apiEtat))
	mux.HandleFunc("POST /api/deverrouiller", s.garde(true, s.apiDeverrouiller))
	mux.HandleFunc("POST /api/verrouiller", s.garde(true, s.apiVerrouiller))

	mux.HandleFunc("POST /api/mot-de-passe", s.garde(false, s.apiMotDePasse))
	mux.HandleFunc("POST /api/preferences", s.garde(false, s.apiPreferences))
	mux.HandleFunc("GET /api/journal", s.garde(false, s.apiJournal))
	mux.HandleFunc("GET /api/agents", s.garde(false, s.apiAgents))
	mux.HandleFunc("POST /api/pilote/{action}", s.garde(false, s.apiPilote))

	mux.HandleFunc("GET /api/projets", s.garde(false, s.apiProjets))
	mux.HandleFunc("POST /api/projets", s.garde(false, s.apiAjouterProjet))
	mux.HandleFunc("POST /api/projets/choisir", s.garde(false, s.apiChoisirProjet))
	mux.HandleFunc("DELETE /api/projets/{nom}", s.garde(false, s.apiRetirerProjet))
	mux.HandleFunc("GET /api/projets/gestion", s.garde(false, s.apiGestionVoir))
	mux.HandleFunc("POST /api/projets/gestion", s.garde(false, s.apiGestion))
	mux.HandleFunc("GET /api/github", s.garde(false, s.apiGitHubVoir))
	mux.HandleFunc("POST /api/github", s.garde(false, s.apiGitHub))
	mux.HandleFunc("GET /api/dossiers", s.garde(false, s.apiDossiers))

	mux.HandleFunc("GET /api/demandes", s.garde(false, s.apiDemandes))
	mux.HandleFunc("POST /api/demandes", s.garde(false, s.apiAjouterDemande))
	mux.HandleFunc("POST /api/demandes/ordre", s.garde(false, s.apiOrdreDemandes))
	mux.HandleFunc("DELETE /api/demandes/{fichier}", s.garde(false, s.apiSupprimerDemande))
	mux.HandleFunc("POST /api/remarque", s.garde(false, s.apiRemarque))
	mux.HandleFunc("GET /api/modeles", s.garde(false, s.apiModeles))
	mux.HandleFunc("POST /api/quota/arret", s.garde(false, s.apiReglesQuota))
	mux.HandleFunc("GET /api/chat", s.garde(false, s.apiChat))
	mux.HandleFunc("POST /api/chat", s.garde(false, s.apiChatEnvoyer))
	mux.HandleFunc("POST /api/chat/arreter", s.garde(false, s.apiChatArreter))
	mux.HandleFunc("POST /api/chat/effacer", s.garde(false, s.apiChatEffacer))
	mux.HandleFunc("POST /api/chat/note", s.garde(false, s.apiChatNote))
	mux.HandleFunc("POST /api/ameliorer", s.garde(false, s.apiAmeliorerDemande))
	mux.HandleFunc("PUT /api/demande", s.garde(false, s.apiEcrireDemande))
	mux.HandleFunc("POST /api/pieces", s.garde(false, s.apiPiece))
	mux.HandleFunc("POST /api/equipe/agents", s.garde(false, s.apiAgentsPerso))
	mux.HandleFunc("GET /api/pourvous", s.garde(false, s.apiPourVousVoir))
	mux.HandleFunc("POST /api/pourvous", s.garde(false, s.apiPourVous))
	mux.HandleFunc("GET /api/plugins", s.garde(false, s.apiPlugins))
	mux.HandleFunc("POST /api/plugins/marche", s.garde(false, s.apiPluginsMarche))
	mux.HandleFunc("POST /api/plugins/installer", s.garde(false, s.apiPluginsInstaller))
	mux.HandleFunc("POST /api/plugins/projet", s.garde(false, s.apiPluginsProjet))
	mux.HandleFunc("POST /api/plugins/retirer", s.garde(false, s.apiPluginsRetirer))
	mux.HandleFunc("POST /api/extensions/connexion", s.garde(false, s.apiExtConnexion))
	mux.HandleFunc("POST /api/lien", s.garde(false, s.apiLien))
	mux.HandleFunc("GET /api/plan", s.garde(false, s.apiPlanTexte))
	mux.HandleFunc("PUT /api/plan", s.garde(false, s.apiEcrirePlan))
	mux.HandleFunc("POST /api/plan/jalon", s.garde(false, s.apiActionJalon))

	mux.HandleFunc("GET /api/env", s.garde(false, s.apiEnv))
	mux.HandleFunc("POST /api/env", s.garde(false, s.apiEcrireEnv))

	mux.HandleFunc("GET /api/sante", s.garde(false, s.apiSante))
	mux.HandleFunc("POST /api/sante/verifier", s.garde(false, s.apiVerifier))
	mux.HandleFunc("POST /api/sante/reparer", s.garde(false, s.apiReparer))
	mux.HandleFunc("POST /api/sante/claude", s.garde(false, s.apiClaude))
	mux.HandleFunc("POST /api/sante/libre", s.garde(false, s.apiLibre))
	mux.HandleFunc("POST /api/sante/barriere", s.garde(false, s.apiBarriere))
	mux.HandleFunc("POST /api/sante/recompiler", s.garde(false, s.apiRecompiler))
	mux.HandleFunc("POST /api/sante/installer", s.garde(false, s.apiInstaller))
	mux.HandleFunc("POST /api/mode-systeme", s.garde(false, s.apiModeSysteme))
	mux.HandleFunc("GET /api/tache/{id}", s.garde(false, s.apiTache))

	mux.HandleFunc("GET /api/mode", s.garde(false, s.apiMode))
	mux.HandleFunc("POST /api/mode", s.garde(false, s.apiEcrireMode))
	mux.HandleFunc("GET /api/options", s.garde(false, s.apiOptions))
	mux.HandleFunc("POST /api/options", s.garde(false, s.apiEcrireOptions))
	mux.HandleFunc("POST /api/bureau/{action}", s.garde(false, s.apiBureau))
	mux.HandleFunc("GET /api/ecran", s.garde(false, s.apiEcran))
	mux.HandleFunc("POST /api/instance/{action}", s.apiInstance) // second launch (its own secret, see instance.go)
	mux.HandleFunc("POST /api/ouvrir", s.garde(false, s.apiOuvrir))
	mux.HandleFunc("POST /api/fin-vue", s.garde(false, s.apiFinVue))
	mux.HandleFunc("GET /api/extensions", s.garde(false, s.apiExtensions))
	mux.HandleFunc("GET /api/extensions/voir", s.garde(false, s.apiExtVoir))
	mux.HandleFunc("POST /api/extensions/catalogue", s.garde(false, s.apiExtCatalogue))
	mux.HandleFunc("POST /api/extensions/github", s.garde(false, s.apiExtGitHub))
	mux.HandleFunc("POST /api/extensions/skill", s.garde(false, s.apiExtSkillPerso))
	mux.HandleFunc("POST /api/extensions/mcp", s.garde(false, s.apiExtMCPPerso))
	mux.HandleFunc("POST /api/extensions/projet", s.garde(false, s.apiExtProjet))
	mux.HandleFunc("POST /api/extensions/secret", s.garde(false, s.apiExtSecret))
	mux.HandleFunc("POST /api/extensions/retirer", s.garde(false, s.apiExtRetirer))
	mux.HandleFunc("POST /api/extensions/tester", s.garde(false, s.apiExtTester))
	mux.HandleFunc("POST /api/extensions/recherche", s.garde(false, s.apiExtRecherche))
	mux.HandleFunc("POST /api/extensions/registre", s.garde(false, s.apiExtRegistre))
}

// ----------------------------------------------------------------------------- locking

func (s *Serveur) apiDeverrouiller(w http.ResponseWriter, r *http.Request) {
	var c struct {
		MotDePasse string `json:"mot_de_passe"`
	}
	_ = lireCorps(r, &c)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.MotDePasseHash == "" {
		s.ecrireJSON(w, map[string]bool{"ok": true})
		return
	}
	time.Sleep(400 * time.Millisecond) // slows down bursts of attempts
	if !egal(hacher(s.config.Sel, c.MotDePasse), s.config.MotDePasseHash) {
		s.erreur(w, http.StatusUnauthorized, "Wrong password")
		return
	}
	s.ouvrirSession(w)
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

func (s *Serveur) apiVerrouiller(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.MotDePasseHash == "" {
		s.erreur(w, http.StatusBadRequest, "Set a password in Options first")
		return
	}
	s.session = ""
	http.SetCookie(w, &http.Cookie{Name: "athanor_session", Value: "", Path: "/", MaxAge: -1})
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

func (s *Serveur) apiMotDePasse(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Ancien  string `json:"ancien"`
		Nouveau string `json:"nouveau"`
	}
	_ = lireCorps(r, &c)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.MotDePasseHash != "" && !egal(hacher(s.config.Sel, c.Ancien), s.config.MotDePasseHash) {
		s.erreur(w, http.StatusUnauthorized, "Wrong current password")
		return
	}
	if strings.TrimSpace(c.Nouveau) == "" {
		s.config.MotDePasseHash, s.config.Sel, s.session = "", "", ""
	} else {
		if len(c.Nouveau) < 4 {
			s.erreur(w, http.StatusBadRequest, "At least 4 characters")
			return
		}
		s.config.Sel = aleatoire(8)
		s.config.MotDePasseHash = hacher(s.config.Sel, c.Nouveau)
		s.ouvrirSession(w)
	}
	if err := ecrireConfig(s.config); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true, "defini": s.config.MotDePasseHash != ""})
}

// kitCourant returns the tool folder; it can be changed on the fly from Options.
func (s *Serveur) kitCourant() Kit {
	s.kitMu.RLock()
	defer s.kitMu.RUnlock()
	return s.kit
}
