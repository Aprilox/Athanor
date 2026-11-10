package main

// The hub: Athanor is a set of modules (Dev, the vault; Servers… later). Each module declares itself
// from init() with enregistrerModule, like the commands with enregistrerCommande (Dev excepted: it is
// first by construction); the home page, the menu and GET /api/hub are built from this registry only.

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Module: what the hub needs from a module. A method enters this interface only once a second
// module needs it.
type Module interface {
	ID() string          // stable, lowercase: "dev", "servers"
	Nom() string         // i18n key (English text): the only place the display name lives
	Pages() []PageModule // menu entries; empty = module hidden from the menu
	// Resume: the home tile's data; must be cheap (cached data, < 50 ms)
	Resume(s *Serveur) ResumeModule
}

// PageModule: one menu entry, a page of the web interface (its hash) and its i18n key.
type PageModule struct {
	Hash    string `json:"hash"`
	Libelle string `json:"libelle"`
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
	modules   = []Module{moduleDev{}} // Dev first, before any init(): the home and the menu start with it
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

// modulesHub: the registered modules, in registration order.
func modulesHub() []Module {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	return append([]Module{}, modules...)
}

// dossierModule: the data folder of a module, dossierAthanor()/modules/<id>, created on first use.
func dossierModule(id string) string {
	d := filepath.Join(dossierAthanor(), "modules", id)
	_ = os.MkdirAll(d, 0o700)
	return d
}

type ModuleVue struct {
	ID     string       `json:"id"`
	Nom    string       `json:"nom"`
	Pages  []PageModule `json:"pages"`
	Resume ResumeModule `json:"resume"`
}

// apiHub: GET /api/hub, every module with its menu entries and its home tile.
func (s *Serveur) apiHub(w http.ResponseWriter, r *http.Request) {
	v := []ModuleVue{}
	for _, m := range modulesHub() {
		p := m.Pages()
		if p == nil {
			p = []PageModule{}
		}
		v = append(v, ModuleVue{ID: m.ID(), Nom: m.Nom(), Pages: p, Resume: m.Resume(s)})
	}
	s.ecrireJSON(w, map[string]any{"modules": v})
}
