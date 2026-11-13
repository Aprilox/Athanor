package main

// The hub: Athanor is a set of modules (Dev, the vault; Servers… later). Each module declares itself
// from init() with enregistrerModule, like the commands with enregistrerCommande (Dev excepted: it is
// first by construction); the home page, the menu and GET /api/hub are built from this registry only.

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Module: what the hub needs from a module. A method enters this interface only once a second
// module needs it.
type Module interface {
	ID() string          // stable, lowercase: "dev", "servers"
	Nom() string         // i18n key (English text): the only place the display name lives
	Pages() []PageModule // its pages (menu entry, tab bar); empty = module hidden from the menu
	// Resume: the home tile's data (no figure = no tile); must be cheap (cached data, < 50 ms)
	Resume(s *Serveur) ResumeModule
	// Elements: what it has at work (the Projects page, the home's "In progress"); cheap like Resume
	Elements(s *Serveur) []ElementHub
	// Tutoriel: its tour, shown once at the first visit of one of its pages (4 to 6 steps); nil = none
	Tutoriel() []EtapeTutoriel
}

// EtapeTutoriel: one step of a module's tour: its title and text (i18n keys, English), the element it
// shows (a CSS selector, "" = none, the bubble in the middle) and its page ("" = the module's first page).
type EtapeTutoriel struct {
	Titre string `json:"titre"`
	Texte string `json:"texte"`
	Cible string `json:"cible,omitempty"`
	Page  string `json:"page,omitempty"`
}

// tutorielConnu: a module with that id declares a tour (the only ids /api/preferences keeps as seen).
func tutorielConnu(id string) bool {
	for _, m := range modulesHub() {
		if m.ID() == id {
			return len(m.Tutoriel()) > 0
		}
	}
	return false
}

// PageModule: one page of the web interface (its hash), its i18n key and its CSS icon name. Ordre is
// its rank in the whole menu (Home 0, Projects 1, Missions 10, Dev 20, Servers 30, Vault 40, Settings
// 90): it sorts the tabs of a module and the modules of the menu. Module is set by pagesModule.
type PageModule struct {
	Module  string   `json:"module"`
	Hash    string   `json:"hash"`
	Ordre   int      `json:"ordre"`
	Icone   string   `json:"icone"`
	Libelle string   `json:"libelle"`
	Fils    []string `json:"fils,omitempty"` // pages opened from this one that keep its menu entry lit (#nouveau under #global)
	Tour    string   `json:"tour,omitempty"` // id of the module whose tour explains this page, if not its own
}

// ElementHub: one thing a module has at work (a Dev project, later a server, a mission), as the hub
// lists it without knowing the module: its progress, what it does now, what waits for the owner.
type ElementHub struct {
	Module     string          `json:"module"` // set by the hub
	ID         string          `json:"id"`
	Titre      string          `json:"titre"`
	Etat       string          `json:"etat"` // "actif" | "inactif" | "termine"
	Avancement *Avancement     `json:"avancement,omitempty"`
	Activite   string          `json:"activite,omitempty"` // what it is on (a milestone's title)
	Action     string          `json:"action,omitempty"`   // what its worker does now (i18n key or text)
	Attente    *AttenteElement `json:"attente,omitempty"`  // nil: nothing waits for the owner
	Lien       string          `json:"lien"`               // the page that opens it
	Projet     string          `json:"projet,omitempty"`   // the Dev project to select before opening Lien
}

// Avancement: the module's own words for it (i18n keys), so that a server is not told in milestones.
type Avancement struct {
	Fait  int    `json:"fait"`
	Total int    `json:"total"`
	Texte string `json:"texte"`           // under the bar, {0} done of {1}; alone when Total is 0 ("No plan yet")
	Etape string `json:"etape,omitempty"` // the step in progress, {0} of {1} ("Milestone {0}/{1}")
	// a Dev plan cut into stages: the stage under way, out of how many (etapesPlan)
	EtapePlan  int `json:"etape_plan,omitempty"`
	EtapesPlan int `json:"etapes_plan,omitempty"`
}

// AttenteElement: why the owner is needed (an i18n key and its arguments) and the page to answer it.
type AttenteElement struct {
	Raison string `json:"raison"`
	Args   []any  `json:"args,omitempty"`
	Page   string `json:"page"`
	Cle    string `json:"-"` // what the situation is about (ids of the waiting tasks): a new one is a new alert
	// Actions: the alert's own buttons, before View and Ignore (a question's choices, M158)
	Actions []ActionAlerte `json:"-"`
}

// StatModule: one figure of a home tile (Valeur: an int or a string), Page = the hash it opens.
type StatModule struct {
	Cle    string `json:"cle"`
	Valeur any    `json:"valeur"`
	Page   string `json:"page,omitempty"`
}

type ResumeModule struct {
	Stats  []StatModule `json:"stats"`
	Besoin int          `json:"besoin"` // items needing the owner (the bell)
	Etat   string       `json:"etat"`   // "ok" | "attention" | "inactif"
}

var (
	modulesMu sync.Mutex
	modules   = []Module{moduleHub{}, moduleDev{}} // built in, before any init(); the menu order is PageModule.Ordre
)

// enregistrerModule: called from init(); an id given twice is a programming error.
func enregistrerModule(m Module) {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	for _, x := range modules {
		if x.ID() == m.ID() {
			panic("module registered twice: " + m.ID())
		}
	}
	modules = append(modules, m)
}

// modulesHub: the registered modules in the menu's order (their first page's Ordre; no page = last).
func modulesHub() []Module {
	modulesMu.Lock()
	m := append([]Module{}, modules...)
	modulesMu.Unlock()
	rang := func(x Module) int {
		if p := pagesModule(x); len(p) > 0 {
			return p[0].Ordre
		}
		return 1 << 30
	}
	sort.SliceStable(m, func(i, j int) bool { return rang(m[i]) < rang(m[j]) })
	return m
}

// pagesModule: a module's pages sorted by Ordre, each marked with its module.
func pagesModule(m Module) []PageModule {
	p := append([]PageModule{}, m.Pages()...)
	for i := range p {
		p[i].Module = m.ID()
	}
	sort.SliceStable(p, func(i, j int) bool { return p[i].Ordre < p[j].Ordre })
	return p
}

// dossierModule: the data folder of a module, dossierAthanor()/modules/<id>, created on first use.
func dossierModule(id string) string {
	d := filepath.Join(dossierAthanor(), "modules", id)
	_ = os.MkdirAll(d, 0o700)
	return d
}

type ModuleVue struct {
	ID       string          `json:"id"`
	Nom      string          `json:"nom"`
	Pages    []PageModule    `json:"pages"`
	Resume   ResumeModule    `json:"resume"`
	Tutoriel []EtapeTutoriel `json:"tutoriel,omitempty"`
	Projet   bool            `json:"projet,omitempty"` // its pages work on the project of the selector (moduleAProjet)
}

// moduleAProjet: a module whose pages work on one project picked in the selector, with the header's
// project buttons (Start, access); the hub's and the other modules' pages show neither.
type moduleAProjet interface{ ProjetCourant() bool }

// apiHub: GET /api/hub, every module with its pages and its home tile, every module's elements (those
// waiting for the owner first), the bell's alerts and the history of the ignored ones.
func (s *Serveur) apiHub(w http.ResponseWriter, r *http.Request) {
	v, el := s.vueHub()
	actives, cachees := s.alertesHub(el)
	s.ecrireJSON(w, map[string]any{"modules": v, "elements": el, "alertes": actives, "ignorees": cachees})
}

func (s *Serveur) vueHub() ([]ModuleVue, []ElementHub) {
	v, el := []ModuleVue{}, []ElementHub{}
	for _, m := range s.modulesTous() {
		_, projet := m.(moduleAProjet)
		v = append(v, ModuleVue{ID: m.ID(), Nom: m.Nom(), Pages: pagesModule(m), Resume: m.Resume(s), Tutoriel: m.Tutoriel(), Projet: projet})
		for _, e := range m.Elements(s) {
			e.Module = m.ID()
			el = append(el, e)
		}
	}
	sort.SliceStable(el, func(i, j int) bool { return el[i].Attente != nil && el[j].Attente == nil })
	return v, el
}

// The hub's own pages, declared like a module's: Home and Projects (every module's elements).
type moduleHub struct{}

func (moduleHub) ID() string  { return "hub" }
func (moduleHub) Nom() string { return "Home" }
func (moduleHub) Pages() []PageModule {
	return []PageModule{{Hash: "#accueil", Ordre: 0, Icone: "accueil", Libelle: "Home"}, {Hash: "#global", Ordre: 1, Icone: "projets", Libelle: "Projects", Fils: []string{"#nouveau", "#archives"}},
		{Hash: "#audit", Ordre: 90, Icone: "audit", Libelle: "Audit log"}} // 90 and up: at the bottom of the menu
}
func (moduleHub) Resume(*Serveur) ResumeModule {
	return ResumeModule{Stats: []StatModule{}, Etat: "inactif"}
}
func (moduleHub) Elements(*Serveur) []ElementHub { return nil }
func (moduleHub) Tutoriel() []EtapeTutoriel {
	return []EtapeTutoriel{
		{Titre: "What do you want to do?", Texte: "Say it here in one sentence: it goes to the chat of the current project, or starts a new project. Nothing is sent before you press Send there.", Cible: "#accueil-form"},
		{Titre: "One tile per part", Texte: "Each part of Athanor has its tile with what matters now. Click a figure to open its page.", Cible: ".grille-hub"},
		{Titre: "In progress", Texte: "What is working right now, and first what waits for you, with the reason. “See all” opens the Projects page.", Cible: ".en-cours"},
		{Titre: "The bell", Texte: "Everything that needs you, with why. “View” opens the right page, “Ignore” hides an alert until something changes.", Cible: "[data-notifs]"},
		{Titre: "Projects", Texte: "Every project of every part on one page, with filters. This tutorial and the others stay in the “?”, at the top of the Guide.", Cible: `nav a[data-href="#global"]`},
	}
}
