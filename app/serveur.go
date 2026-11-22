package main

// Local HTTP server: shared state, request protection (same origin, locking), routes.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type Serveur struct {
	mu           sync.Mutex
	mdpMu        sync.Mutex   // one password change at a time (apiMotDePasse)
	essaisMu     sync.Mutex   // one password check at a time (verifierMdpLent)
	essaisFile   atomic.Int32 // password checks waiting or running (bounded: essaisMdpFile)
	echecsMdp    int          // wrong passwords typed on this PC since the last right one
	mdpCourt     bool         // the password that unlocked is shorter than longueurMinMdp (2.x allowed 4): a new one is asked
	config       Config
	kit          Kit // read with kitCourant() (can change from Options)
	kitMu        sync.RWMutex
	logs         map[string]*Journal
	bureau       *Bureau
	verifs       []*Verif  // WSL mode
	verifsNatif  []*Verif  // native Windows mode
	mdns         etatMDNS  // local network discovery, on only by the owner (hub_mdns.go)
	session      string    // session token when a password is set ("" = locked)
	actifLe      time.Time // the owner's last action in the window (auto-lock, verrou_auto.go)
	garderCoffre bool      // the window locked (padlock or timer): the vault stays open for the background until a new session
	attenteMu    sync.Mutex
	attentes     map[int]attenteInfra // the steps waiting for the infra key (infra_attente.go)
	attenteN     int
	infraOuverte chan struct{} // closed at each unlock
	csrf         string
	cleAcces     string               // cookie of the windows Athanor opened (acces.go); empty = no check (tests)
	ouvertures   map[string]time.Time // single-use window addresses
	relancer     map[string]bool      // projects to restart as soon as their pilot has stopped (settings change)
	dernierActif map[string]bool
	// the restart asked by itself for new settings (reglages_pris.go): project → "pilot's pid|settings" it was
	// asked for; arretTest replaces the command that asks the stop (tests)
	relanceReglages map[string]string
	arretTest       func(Projet) (string, error)
	// last quick check of the milestones of a project waiting for its files (folder → time)
	derniereVerif map[string]time.Time
	occupes       map[string]bool // projects with a management action under way (no pilot start meanwhile)
	majProjets    sync.Mutex      // one update of the folder lists at a time
	// remote access (distance.go): its state, and the routes the tunnel's server shares with the window
	distOnce sync.Once
	// external MCP clients (hub_clients.go), read at first use
	clientsOnce  sync.Once
	clientsEtat  *magasinClients
	distantsOnce sync.Once
	distantsEtat *magasinDistants
	distEtat     *distance
	mux          http.Handler
	// the vault (coffre_serveur.go); coffreErr: its file exists but could not be read
	coffre    *Coffre
	coffreErr error
	// project grants (acces_projets.go); accesErr: hub/acces.json exists but could not be read
	acces    *Acces
	accesErr error
	// the hub's session tokens of the pilots (hub_jetons.go)
	jetons *Sessions
	// the approvals of the agents and the owner's rules (hub_approbations.go)
	approbations *Approbations
	// the last blocker each pilot reported (hub_outils_dev.go), guarded by mu
	signaux map[string]SignalPilote
	// the temporary project of Test the hub's MCP while it runs (hub_test_mcp.go), guarded by mu
	projetTestMCP Projet
	// the Servers operations of each session token, by its hash, cut when it is revoked (C33)
	opsMu sync.Mutex
	ops   map[string]opsSession
}

// opsSession: the context of a session's operations and what cancels it.
type opsSession struct {
	ctx     context.Context
	annuler context.CancelFunc
}

func nouveauServeur(config Config) *Serveur {
	kit := Kit{config.Kit}
	definirKitTextes(kit.Racine) // the texts of its local/ (textes.go)
	if configNeuve.Load() && runtime.GOOS == "windows" {
		_, err := os.Stat(filepath.Join(kit.Racine, "system", "distro.txt"))
		config.ModeNatif = err != nil // a kit already installed in WSL stays in WSL
	}
	modeNatif.Store(config.ModeNatif && runtime.GOOS == "windows")
	s := &Serveur{
		config:          config,
		kit:             kit,
		logs:            map[string]*Journal{},
		verifs:          verificationsSysteme(),
		verifsNatif:     VerificationsNatives(),
		csrf:            aleatoire(16),
		dernierActif:    map[string]bool{},
		relancer:        map[string]bool{},
		relanceReglages: map[string]string{},
	}
	MigrerEnv(kit.Env()) // old French keys of the .env → English names
	s.MigrerUnreal()     // UNREAL_MCP of the .env → the "unreal" server of the MCP library (2.13.0)
	s.MigrerAgents()     // sub-agents "for every project" → the library, switched on in each project (2.49.9)
	for _, p := range s.config.Projets {
		p.MigrerAtelier() // .autopilote/ → .athanor/ (previous name)
	}
	if len(s.config.Projets) > 0 {
		s.ecrireListeProjets() // the admin installer and the firewall rules read it
	}
	s.chargerAcces() // before the vault: its import grants a key shared by two projects (accorderPartagee)
	s.chargerCoffre()
	s.migrerFournisseurs()
	s.chargerSessions()
	s.chargerApprobations()
	s.bureau = NouveauBureau(kit)
	// Windows: a window opened on the screen by a program the application started (a Claude run's): hidden
	demarrerGardeFenetres(uint32(os.Getpid()), func(t string) { noterFenetreKit(s.kitCourant().Racine, t) })
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
	rendreSonOublie(s.kitCourant().Racine) // a program left muted on Claude's desktop by an Athanor stopped short
	return s
}

// ----------------------------------------------------------------------------- security

func aleatoire(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func egal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// verrouillePour says whether the request is locked: password set and no valid session (a remote
// device: unlocked on its own, distance.go).
func (s *Serveur) verrouillePour(r *http.Request) bool {
	s.mu.Lock()
	mdp := s.config.MotDePasseHash != ""
	s.mu.Unlock()
	if id := appareilDe(r); id != "" {
		if !mdp {
			return false
		}
		d := s.dist()
		d.mu.Lock()
		defer d.mu.Unlock()
		return !d.deverrouilles[id]
	}
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
		// nor send it (the Host header check, in main.go, blocks "DNS rebinding"); a foreign Origin is
		// refused too (defence in depth: another local port is the same "site" for SameSite cookies).
		if !s.accesValide(r) || !egal(r.Header.Get("X-Athanor"), s.csrf) || !memeOrigine(r) {
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
	s.actifLe, s.garderCoffre = horlogeVerrou(), false
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

// erreurArgs: an error whose message key takes arguments ({0}…), translated by the interface.
func (s *Serveur) erreurArgs(w http.ResponseWriter, code int, cle string, args ...string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"erreur_key": cle, "args": args, "erreur": erreurEntreeArgs{cle, args}.Error()})
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
	if !actif { // its hub token, once the start-up grace is over
		s.revoquerJetons("pilote arrêté", func(x SessionHub) bool { return x.Projet == p.Nom && !s.piloteVu(p, x) })
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dernierActif[p.Nom] && !actif && s.relancer[p.Nom] {
		delete(s.relancer, p.Nom)
		go func() { _, _ = s.demarrerPilote(p) }()
	}
	s.dernierActif[p.Nom] = actif
}

func (s *Serveur) demarrerProjetsAuto() {
	s.revoquerJetonsArretes() // the tokens of the pilots stopped while Athanor was closed
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
			go func() { _ = garderWSLVivant(s.kitCourant(), pp) }()
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
	mux.HandleFunc("GET /api/hub", s.garde(false, s.apiHub))
	mux.HandleFunc("GET /api/machine", s.garde(false, s.apiMachine)) // this computer's load (machine.go)
	mux.HandleFunc("POST /api/hub/alertes", s.garde(false, s.apiAlertes))
	mux.HandleFunc("GET /api/hub/audit", s.garde(false, s.apiAudit))
	mux.HandleFunc("POST /api/hub/audit/verifier", s.garde(false, s.apiAuditVerifier))
	mux.HandleFunc("GET /api/hub/approbations", s.garde(false, s.apiApprobations))
	mux.HandleFunc("POST /api/hub/approbations", s.garde(false, s.apiApprobationDecider))
	mux.HandleFunc("POST /api/hub/urgence", s.garde(false, s.apiUrgence))
	mux.HandleFunc("GET /api/coffre", s.garde(false, s.apiCoffre))
	mux.HandleFunc("POST /api/coffre", s.garde(false, s.apiCoffreAjouter))
	mux.HandleFunc("POST /api/coffre/{id}", s.garde(false, s.apiCoffreModifier))
	mux.HandleFunc("DELETE /api/coffre/{id}", s.garde(false, s.apiCoffreSupprimer))
	mux.HandleFunc("GET /api/coffre/categories", s.garde(false, s.apiCoffreCategories))
	mux.HandleFunc("POST /api/coffre/categories", s.garde(false, s.apiCoffreCategoriePoser))
	mux.HandleFunc("POST /api/coffre/categories/{id}", s.garde(false, s.apiCoffreCategoriePoser))
	mux.HandleFunc("DELETE /api/coffre/categories/{id}", s.garde(false, s.apiCoffreCategorieSupprimer))
	mux.HandleFunc("POST /api/coffre/coller", s.garde(false, s.apiCoffreColler))
	mux.HandleFunc("POST /api/coffre/tester", s.garde(false, s.apiCoffreTester))
	mux.HandleFunc("POST /api/coffre/migration", s.garde(false, s.apiCoffreMigration))
	mux.HandleFunc("POST /api/coffre/migration/annuler", s.garde(false, s.apiCoffreMigrationAnnuler))
	mux.HandleFunc("GET /api/mentions", s.garde(false, s.apiMentions))
	mux.HandleFunc("GET /api/missions", s.garde(false, s.apiMissions))
	mux.HandleFunc("GET /api/missions/{id}", s.garde(false, s.apiMission))
	mux.HandleFunc("DELETE /api/missions/{id}", s.garde(false, s.apiSupprimerMission))
	mux.HandleFunc("POST /api/missions", s.garde(false, s.apiLancerMission))
	mux.HandleFunc("POST /api/missions/{id}", s.garde(false, s.apiModifierMission))
	mux.HandleFunc("POST /api/missions/{id}/{action}", s.garde(false, s.apiAgirMission))
	mux.HandleFunc("GET /api/serveurs", s.garde(false, s.apiServeurs))
	mux.HandleFunc("GET /api/serveurs/{id}", s.garde(false, s.apiServeur))
	mux.HandleFunc("POST /api/serveurs/empreinte", s.garde(false, s.apiServeursEmpreinte))
	mux.HandleFunc("POST /api/serveurs/ajouter", s.garde(false, s.apiServeursAjouter))
	mux.HandleFunc("POST /api/serveurs/{id}", s.garde(false, s.apiServeurModifier))
	mux.HandleFunc("DELETE /api/serveurs/{id}", s.garde(false, s.apiServeurSupprimer))
	mux.HandleFunc("GET /api/serveurs/{id}/taches", s.garde(false, s.apiTachesServeur))
	mux.HandleFunc("POST /api/serveurs/{id}/taches", s.garde(false, s.apiLancerTacheServeur))
	mux.HandleFunc("POST /api/serveurs/{id}/autorisation", s.garde(false, s.apiAutorisationServeur))
	mux.HandleFunc("GET /api/reglages/permissions", s.garde(false, s.apiPermissionsReglages))
	mux.HandleFunc("POST /api/reglages/permissions", s.garde(false, s.apiChoisirPermissions))
	mux.HandleFunc("POST /api/serveurs/{id}/taches/{tache}/main", s.garde(false, s.apiReprendreMainTache))
	mux.HandleFunc("POST /api/serveurs/{id}/taches/{tache}/arreter", s.garde(false, s.apiArreterTacheServeur))
	mux.HandleFunc("POST /api/serveurs/{id}/terminal", s.garde(false, s.apiTerminalOuvrir))
	mux.HandleFunc("GET /api/serveurs/{id}/terminal/{session}", s.garde(false, s.apiTerminalFlux))
	mux.HandleFunc("POST /api/serveurs/{id}/terminal/{session}", s.garde(false, s.apiTerminalAgir))
	mux.HandleFunc("POST /api/serveurs/terminal-distance", s.garde(false, s.apiTerminalDistance))
	mux.HandleFunc("POST /api/distance/permissions", s.garde(false, s.apiPermissionDistance)) // Remote › Permissions
	mux.HandleFunc("GET /api/serveurs/{id}/sftp/{session}", s.garde(false, s.apiSFTPLister))
	mux.HandleFunc("POST /api/serveurs/{id}/sftp/{session}", s.garde(false, s.apiSFTPAgir))
	mux.HandleFunc("GET /api/serveurs/{id}/sftp/{session}/fichier", s.garde(false, s.apiSFTPTelecharger))
	mux.HandleFunc("POST /api/serveurs/{id}/sftp/{session}/fichier", s.garde(false, s.apiSFTPEnvoyer))
	mux.HandleFunc("POST /api/serveurs/{id}/sftp/{session}/ticket", s.garde(false, s.apiSFTPTicket))
	mux.HandleFunc("GET /api/serveurs/{id}/sftp/{session}/ticket/{ticket}", s.apiSFTPTicketLire)
	mux.HandleFunc("GET /api/serveurs/{id}/sftp/{session}/texte", s.garde(false, s.apiSFTPTexte))
	mux.HandleFunc("POST /api/serveurs/{id}/sftp/{session}/texte", s.garde(false, s.apiSFTPEcrireTexte))
	mux.HandleFunc("GET /api/serveurs/{id}/sftp/{session}/transferts", s.garde(false, s.apiSFTPTransferts))
	mux.HandleFunc("POST /api/serveurs/{id}/sftp/{session}/transferts", s.garde(false, s.apiSFTPTransferer))
	mux.HandleFunc("POST /api/serveurs/{id}/sftp/{session}/transferts/{transfert}", s.garde(false, s.apiSFTPTransfert))
	mux.HandleFunc("GET /api/serveurs/local/sftp", s.garde(false, s.apiLocalLister))
	mux.HandleFunc("POST /api/serveurs/local/sftp", s.garde(false, s.apiLocalAgir))
	mux.HandleFunc("GET /api/acces", s.garde(false, s.apiAcces))
	mux.HandleFunc("POST /api/acces", s.garde(false, s.apiAccesModifier))
	mux.HandleFunc("GET /api/projets/{nom}/environnements", s.garde(false, s.apiEnvironnements))
	mux.HandleFunc("POST /api/projets/{nom}/environnements", s.garde(false, s.apiEnvironnementModifier))
	mux.HandleFunc("POST /api/projets/{nom}/cle-deploiement", s.garde(false, s.apiCleDeploiement))
	mux.HandleFunc("GET /api/projets/{nom}/deploiement", s.garde(false, s.apiDeploiementProjet))
	mux.HandleFunc("POST /api/projets/{nom}/deploiement", s.garde(false, s.apiDeploiementProjetModifier))
	mux.HandleFunc("POST /api/projets/{nom}/preparer-deploiement", s.garde(false, s.apiPreparerDeploiement))
	mux.HandleFunc("GET /api/projets/{nom}/reglages", s.garde(false, s.apiReglagesProjet))
	mux.HandleFunc("POST /api/projets/{nom}/reglages", s.garde(false, s.apiReglageProjetModifier))
	mux.HandleFunc("POST /api/projets/{nom}/revenir", s.garde(false, s.apiRevenir))
	mux.HandleFunc("POST /api/projets/{nom}/incident", s.garde(false, s.apiIncident))
	mux.HandleFunc("POST /api/serveurs/sante", s.garde(false, s.apiSanteReglage))
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
	mux.HandleFunc("GET /api/github/connexion", s.garde(false, s.apiGitHubConnexion))
	mux.HandleFunc("POST /api/github/connexion", s.garde(false, s.apiGitHubConnexion))
	mux.HandleFunc("GET /api/dossiers", s.garde(false, s.apiDossiers))
	mux.HandleFunc("POST /api/dossiers/choisir", s.garde(false, s.apiChoisirDossier))

	mux.HandleFunc("GET /api/demandes", s.garde(false, s.apiDemandes))
	mux.HandleFunc("POST /api/demandes", s.garde(false, s.apiAjouterDemande))
	mux.HandleFunc("POST /api/demandes/ordre", s.garde(false, s.apiOrdreDemandes))
	mux.HandleFunc("DELETE /api/demandes/{fichier}", s.garde(false, s.apiSupprimerDemande))
	mux.HandleFunc("POST /api/remarque", s.garde(false, s.apiRemarque))
	mux.HandleFunc("GET /api/modeles", s.garde(false, s.apiModeles))
	mux.HandleFunc("POST /api/quota/arret", s.garde(false, s.apiReglesQuota))
	mux.HandleFunc("POST /api/depense", s.garde(false, s.apiDepense))
	mux.HandleFunc("GET /api/chat", s.garde(false, s.apiChat))
	mux.HandleFunc("POST /api/chat", s.garde(false, s.apiChatEnvoyer))
	mux.HandleFunc("POST /api/chat/arreter", s.garde(false, s.apiChatArreter))
	mux.HandleFunc("POST /api/chat/effacer", s.garde(false, s.apiChatEffacer))
	mux.HandleFunc("POST /api/chat/note", s.garde(false, s.apiChatNote))
	mux.HandleFunc("POST /api/chat/serveurs", s.garde(false, s.apiChatServeurs))
	mux.HandleFunc("POST /api/chat/agent", s.garde(false, s.apiChatAgent))
	mux.HandleFunc("POST /api/chat/mcp", s.garde(false, s.apiChatMCP))
	mux.HandleFunc("GET /api/hub/chat", s.garde(false, s.apiHubChat)) // the home conversation (hub_chat.go)
	mux.HandleFunc("POST /api/hub/chat", s.garde(false, s.apiHubChatEnvoyer))
	mux.HandleFunc("POST /api/hub/chat/pouvoirs", s.garde(false, s.apiHubChatPouvoirs))
	mux.HandleFunc("POST /api/hub/chat/piece", s.garde(false, s.apiHubChatPiece)) // its attached files (hub_chat_dossiers.go)
	mux.HandleFunc("POST /api/hub/chat/arreter", s.garde(false, s.apiHubChatArreter))
	mux.HandleFunc("POST /api/hub/chat/action", s.garde(false, s.apiHubChatAction)) // its cards (hub_chat_actions.go)
	mux.HandleFunc("POST /api/hub/chat/effacer", s.garde(false, s.apiHubChatEffacer))
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
	mux.HandleFunc("POST /api/sante/coffre", s.garde(false, s.apiTesterCoffre))
	mux.HandleFunc("POST /api/sante/mcp-hub", s.garde(false, s.apiTesterMCP))
	mux.HandleFunc("POST /api/sante/agent-serveurs", s.garde(false, s.apiTesterAgentServeurs))
	mux.HandleFunc("POST /api/sante/recompiler", s.garde(false, s.apiRecompiler))
	mux.HandleFunc("POST /api/sante/installer", s.garde(false, s.apiInstaller))
	mux.HandleFunc("GET /api/sante/terminal", s.garde(false, s.apiEtapeTerminal)) // the terminal interface runs it itself
	mux.HandleFunc("POST /api/mode-systeme", s.garde(false, s.apiModeSysteme))
	mux.HandleFunc("GET /api/tache/{id}", s.garde(false, s.apiTache))

	mux.HandleFunc("GET /api/mode", s.garde(false, s.apiMode))
	mux.HandleFunc("POST /api/mode", s.garde(false, s.apiEcrireMode))
	mux.HandleFunc("GET /api/options", s.garde(false, s.apiOptions))
	mux.HandleFunc("GET /api/raccourci", s.garde(false, s.apiRaccourci))
	mux.HandleFunc("POST /api/raccourci", s.garde(false, s.apiRaccourci))
	mux.HandleFunc("GET /api/maj", s.garde(false, s.apiMaj))
	mux.HandleFunc("POST /api/maj/installer", s.garde(false, s.apiInstallerMaj))
	mux.HandleFunc("POST /api/options", s.garde(false, s.apiEcrireOptions))
	mux.HandleFunc("POST /api/options/rappels-natif", s.garde(false, s.apiRappelsNatif))
	mux.HandleFunc("POST /api/options/verrou-auto", s.garde(false, s.apiVerrouAuto))
	mux.HandleFunc("POST /api/activite", s.garde(false, s.apiActivite))
	mux.HandleFunc("POST /api/bureau/{action}", s.garde(false, s.apiBureau))
	mux.HandleFunc("GET /api/ecran", s.garde(false, s.apiEcran))
	mux.HandleFunc("POST /api/instance/{action}", s.apiInstance)       // second launch (its own secret, see instance.go)
	mux.HandleFunc("POST /api/mcp", s.apiMCP)                          // the hub's MCP: its session token only, outside garde (hub_mcp.go)
	mux.HandleFunc("POST /api/mcp/appairer", s.apiAppairerClient)      // an MCP client's pairing: the 6 digits, outside garde (hub_clients.go)
	mux.HandleFunc("POST /api/mcp/appairer/statut", s.apiStatutClient) // and the owner's answer
	mux.HandleFunc("GET /api/annonce", repondreAnnonce(idInstance))    // public: id and version only (hub_annonce.go)
	mux.HandleFunc("GET /api/instances", s.garde(false, s.apiInstances))
	mux.HandleFunc("POST /api/instances", s.garde(false, s.apiInstances))
	mux.HandleFunc("POST /api/instances/mdns", s.garde(false, s.apiMDNS))
	mux.HandleFunc("POST /api/distants", s.garde(false, s.apiDistants)) // remote Athanors: add, remove (hub_distants.go)
	mux.HandleFunc("GET /api/clients", s.garde(false, s.apiClients))
	mux.HandleFunc("POST /api/clients", s.garde(false, s.apiClients))
	mux.HandleFunc("POST /api/ouvrir", s.garde(false, s.apiOuvrir))
	mux.HandleFunc("POST /api/fenetre", s.garde(false, s.apiFenetre)) // the terminal: Athanor's window as well
	mux.HandleFunc("POST /api/quitter", s.garde(false, s.apiQuitter)) // the terminal: stop the Athanor in the background
	mux.HandleFunc("POST /api/fin-vue", s.garde(false, s.apiFinVue))
	mux.HandleFunc("GET /api/extensions", s.garde(false, s.apiExtensions))
	mux.HandleFunc("GET /api/extensions/voir", s.garde(false, s.apiExtVoir))
	mux.HandleFunc("POST /api/extensions/catalogue", s.garde(false, s.apiExtCatalogue))
	mux.HandleFunc("POST /api/extensions/github", s.garde(false, s.apiExtGitHub))
	mux.HandleFunc("POST /api/extensions/skill", s.garde(false, s.apiExtSkillPerso))
	mux.HandleFunc("POST /api/extensions/mcp", s.garde(false, s.apiExtMCPPerso))
	mux.HandleFunc("POST /api/extensions/mcp/modifier", s.garde(false, s.apiExtMCPModifier))
	mux.HandleFunc("POST /api/extensions/projet", s.garde(false, s.apiExtProjet))
	mux.HandleFunc("POST /api/extensions/secret", s.garde(false, s.apiExtSecret))
	mux.HandleFunc("POST /api/extensions/retirer", s.garde(false, s.apiExtRetirer))
	mux.HandleFunc("POST /api/extensions/tester", s.garde(false, s.apiExtTester))
	mux.HandleFunc("POST /api/extensions/corriger", s.garde(false, s.apiExtCorriger))
	mux.HandleFunc("GET /api/extensions/corriger", s.garde(false, s.apiExtCorriger))
	mux.HandleFunc("POST /api/extensions/correction/appliquer", s.garde(false, s.apiExtCorrectionAppliquer))
	mux.HandleFunc("POST /api/extensions/correction/annuler", s.garde(false, s.apiExtCorrectionAnnuler))
	mux.HandleFunc("POST /api/extensions/tester-skill", s.garde(false, s.apiExtTesterSkill))
	mux.HandleFunc("POST /api/extensions/mode-blender", s.garde(false, s.apiExtModeBlender))
	mux.HandleFunc("GET /api/extensions/corrections", s.garde(false, s.apiExtCorrections))
	mux.HandleFunc("POST /api/extensions/corrections/appliquer", s.garde(false, s.apiExtCorrectionsAppliquer))
	mux.HandleFunc("POST /api/extensions/recherche", s.garde(false, s.apiExtRecherche))
	mux.HandleFunc("POST /api/extensions/registre", s.garde(false, s.apiExtRegistre))

	// remote access (distance.go): Athanor's window only (the tunnel's server refuses these routes)
	mux.HandleFunc("GET /api/distance", s.garde(false, s.apiDistance))
	mux.HandleFunc("POST /api/distance", s.garde(false, s.apiDistanceActif))
	mux.HandleFunc("POST /api/distance/appareils/{id}", s.garde(false, s.apiDistanceAppareil))
	mux.HandleFunc("DELETE /api/distance/appareils/{id}", s.garde(false, s.apiDistanceAppareil))
	mux.HandleFunc("POST /api/distance/domaine", s.garde(false, s.apiDistanceDomaine))

	// several computers on a project (api_pcs.go)
	mux.HandleFunc("GET /api/pcs", s.garde(false, s.apiPCs))
	mux.HandleFunc("GET /api/pcs-direct", s.garde(false, s.apiDirect))  // the direct link between the computers (equipe_directe.go)
	mux.HandleFunc("POST /api/pcs-direct", s.garde(false, s.apiDirect)) //
	mux.HandleFunc("POST /api/pcs", s.garde(false, s.apiPCsActiver))
	mux.HandleFunc("GET /api/pcs/machine", s.garde(false, s.apiPCsMachine))
	mux.HandleFunc("POST /api/pcs/machine", s.garde(false, s.apiPCsMachine))
	mux.HandleFunc("GET /api/push", s.garde(false, s.apiPush))
	mux.HandleFunc("POST /api/push", s.garde(false, s.apiPush))
	for _, m := range modulesHub() {
		if r, ok := m.(moduleRoutes); ok {
			r.Routes(mux, s)
		}
	}
}

// ----------------------------------------------------------------------------- locking

// longueurMinMdp: the shortest password Athanor accepts since 3.0.0; an older, shorter one still unlocks, then
// the interface asks for a new one at once (EtatVue.ChangerMdp).
const longueurMinMdp = 12

// essaisMdpDistance: wrong passwords a remote device may type before it is removed from the trusted ones
// (a stolen phone cannot try password after password).
const essaisMdpDistance = 5

// essaisMdpFile: password checks that may wait their turn; one more is refused at once (errTropEssaisMdp)
// instead of making the right password wait behind a pile of wrong ones.
const essaisMdpFile = 3

var errTropEssaisMdp = errors.New("Too many password attempts at once: wait a few seconds and try again")

// verifierMdpLent checks a typed password one attempt at a time (parallel attempts wait their turn, at most
// essaisMdpFile: plein when the queue is full); each wrong password typed on this PC lengthens the pause
// (0.4 s, 0.8 s… up to 5 s), a right one resets it.
func (s *Serveur) verifierMdpLent(sel, mdp, hash string, local bool) (ok, aRemplacer, plein bool) {
	if s.essaisFile.Add(1) > essaisMdpFile {
		s.essaisFile.Add(-1)
		return false, false, true
	}
	defer s.essaisFile.Add(-1)
	s.essaisMu.Lock()
	defer s.essaisMu.Unlock()
	time.Sleep(pauseMdp(s.echecsMdp))
	ok, aRemplacer = verifierMotDePasse(sel, mdp, hash)
	switch {
	case ok:
		s.echecsMdp = 0
	case local: // a remote device has its own count (essaisMdpDistance)
		s.echecsMdp++
	}
	return ok, aRemplacer, false
}

func pauseMdp(echecs int) time.Duration {
	return min(400*time.Millisecond<<min(echecs, 4), 5*time.Second)
}

func (s *Serveur) apiDeverrouiller(w http.ResponseWriter, r *http.Request) {
	var c struct {
		MotDePasse string `json:"mot_de_passe"`
	}
	_ = lireCorps(r, &c)
	s.mu.Lock()
	sel, hash := s.config.Sel, s.config.MotDePasseHash
	s.mu.Unlock()
	if hash == "" {
		s.ecrireJSON(w, map[string]bool{"ok": true})
		return
	}
	id := appareilDe(r)
	ok, aRemplacer, plein := s.verifierMdpLent(sel, c.MotDePasse, hash, id == "")
	if plein {
		s.erreur(w, http.StatusTooManyRequests, errTropEssaisMdp.Error())
		return
	}
	if aRemplacer {
		s.remplacerHashMdp(hash, c.MotDePasse)
	}
	if !ok {
		if id == "" {
			s.erreur(w, http.StatusUnauthorized, "Wrong password")
			return
		}
		d := s.dist()
		d.mu.Lock()
		d.essaisMdp[id]++
		reste := essaisMdpDistance - d.essaisMdp[id]
		d.mu.Unlock()
		if reste > 0 {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"erreur_key": "Wrong password: {0} attempt(s) left, then this device is removed", "args": []string{strconv.Itoa(reste)}})
			return
		}
		s.retirerAppareilDistance(id, "mdp")
		s.erreur(w, http.StatusUnauthorized, "Not paired")
		return
	}
	s.deverrouillerCoffre(c.MotDePasse)
	court := utf8.RuneCountInString(c.MotDePasse) < longueurMinMdp
	s.mu.Lock()
	s.mdpCourt = court
	s.mu.Unlock()
	if id != "" {
		d := s.dist()
		d.mu.Lock()
		d.deverrouilles[id] = true
		delete(d.essaisMdp, id)
		d.mu.Unlock()
	} else {
		s.mu.Lock()
		s.ouvrirSession(w)
		s.mu.Unlock()
	}
	s.ecrireJSON(w, map[string]bool{"ok": true, "changer_mdp": court})
}

// remplacerHashMdp rewrites a hash of the old format (or weaker parameters) once the right password
// is typed, unless the password changed meanwhile.
func (s *Serveur) remplacerHashMdp(ancien, mdp string) {
	nouveau := hacherMotDePasse(mdp)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.MotDePasseHash != ancien {
		return
	}
	s.config.MotDePasseHash, s.config.Sel = nouveau, ""
	_ = ecrireConfig(s.config) // on failure, the next save of the config writes it
}

func (s *Serveur) apiVerrouiller(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.MotDePasseHash == "" {
		s.erreur(w, http.StatusBadRequest, "Set a password in Options first")
		return
	}
	// like the timer (verrou_auto.go), the padlock locks the screens only: the vault stays open, so the pilots,
	// missions, tasks and SSH sessions go on while the owner is away; the emergency stop closes everything
	d := s.dist()
	d.mu.Lock()
	if id := appareilDe(r); id != "" {
		delete(d.deverrouilles, id) // a device locks itself only
	} else {
		d.deverrouilles = map[string]bool{} // the computer locks every device too
		s.session, s.garderCoffre = "", true
		http.SetCookie(w, &http.Cookie{Name: "athanor_session", Value: "", Path: "/", MaxAge: -1})
	}
	d.mu.Unlock()
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "verrouillage", Resultat: "cadenas"})
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

func (s *Serveur) apiMotDePasse(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Ancien  string `json:"ancien"`
		Nouveau string `json:"nouveau"`
		Vider   bool   `json:"vider_coffre"` // note 01h54: the old password is lost, the user confirmed the empty vault
	}
	_ = lireCorps(r, &c)
	// C14: the PBKDF2 runs (check, vault rewrap, new hash) happen outside s.mu, which every route takes;
	// mdpMu keeps two changes from crossing
	s.mdpMu.Lock()
	defer s.mdpMu.Unlock()
	s.mu.Lock()
	actuel, actuelSel := s.config.MotDePasseHash, s.config.Sel
	s.mu.Unlock()
	premier := actuel == ""
	if premier { // C25: an agent must not choose the password it would then retype
		if code, err := s.preuveAbsence(); err != nil {
			s.erreur(w, code, err.Error())
			return
		}
	}
	if ok, _ := verifierMotDePasse(actuelSel, c.Ancien, actuel); !premier && !ok {
		s.erreur(w, http.StatusUnauthorized, "Wrong current password")
		return
	}
	retrait := strings.TrimSpace(c.Nouveau) == ""
	if !retrait && utf8.RuneCountInString(c.Nouveau) < longueurMinMdp { // it alone guards the infra key against a copied disk (I3)
		s.erreur(w, http.StatusBadRequest, "At least 12 characters")
		return
	}
	if premier && s.coffreAncienMdp() {
		switch {
		case c.Vider && retrait:
			s.erreur(w, http.StatusBadRequest, "At least 12 characters")
			return
		case c.Vider:
			copie, err := s.coffre.RepartirVide()
			if err != nil {
				s.erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			c.Ancien = ""
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "coffre vidé (ancien mot de passe perdu)", Resultat: filepath.Base(copie)})
		case c.Ancien == "":
			s.erreur(w, http.StatusConflict, errCoffreAncienMdp.Error())
			return
		}
	}
	nouveau, nouveauHash := c.Nouveau, ""
	if retrait {
		nouveau = ""
	} else {
		nouveauHash = hacherMotDePasse(c.Nouveau)
	}
	if code, err := s.changerMdpCoffre(c.Ancien, nouveau); err != nil {
		s.erreur(w, code, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// read again: a login may have rewritten the old hash in the new format meanwhile (remplacerHashMdp)
	hash, sel, session := s.config.MotDePasseHash, s.config.Sel, s.session
	if retrait {
		s.config.MotDePasseHash, s.config.Sel, s.session = "", "", ""
	} else {
		s.config.Sel = "" // only the old format used it
		s.config.MotDePasseHash = nouveauHash
		s.ouvrirSession(w)
	}
	if err := ecrireConfig(s.config); err != nil {
		// the vault must keep opening with the password the config still holds
		s.config.MotDePasseHash, s.config.Sel, s.session = hash, sel, session
		if _, e := s.changerMdpCoffre(nouveau, c.Ancien); e != nil {
			fmt.Fprintln(os.Stderr, "vault: password change not undone:", e)
		}
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mdpCourt = false // 12 characters at least, or no password any more
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "mot de passe", Resultat: map[bool]string{true: "créé", false: "changé"}[premier] + map[bool]string{true: " (retiré)", false: ""}[retrait]})
	s.ecrireJSON(w, map[string]bool{"ok": true, "defini": s.config.MotDePasseHash != ""})
}

// kitCourant returns the tool folder; it can be changed on the fly from Options.
func (s *Serveur) kitCourant() Kit {
	s.kitMu.RLock()
	defer s.kitMu.RUnlock()
	return s.kit
}
