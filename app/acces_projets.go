package main

// Project access grants (stage 3): which vault entry each project receives, for which environment and
// under which variable name. hub/acces.json holds entry ids and names, never a value. (acces.go is the
// unrelated access of windows to the interface.)

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var envsProjet = []string{"dev", "staging", "prod"}

const (
	errAccesEnv      = erreurEntree("Unknown environment (dev, staging or prod)")
	errAccesVariable = erreurEntree("Invalid variable name: capital letters, digits and _, starting with a letter, and not a name that changes how commands run")
	errAccesDoublon  = erreurEntree("This variable name is already used in this environment")
	errAccesDeja     = erreurEntree("This entry is already given to this environment")
	errAccesInconnu  = erreurEntree("Unknown access")
)

type AccesProjet struct {
	Entree   string    `json:"entree"` // vault entry id
	Env      string    `json:"env"`
	Variable string    `json:"variable"`
	Auto     string    `json:"auto,omitempty"` // "" | migration | pourvous
	Cree     time.Time `json:"cree"`
}

type ProjetAcces struct {
	Env              string        `json:"env"`
	ForcerInfraNatif bool          `json:"forcer_infra_natif"`
	Acces            []AccesProjet `json:"acces"`
}

type fichierAcces struct {
	Version   int                     `json:"version"`
	Projets   map[string]*ProjetAcces `json:"projets"`
	AutoFaits []string                `json:"auto_faits,omitempty"` // entries whose automatic grant was made (once)
}

type Acces struct {
	mu     sync.Mutex
	chemin string
	f      fichierAcces
}

func cheminAcces() string { return filepath.Join(dossierAthanor(), "hub", "acces.json") }

// ouvrirAcces reads the grants (a missing file is empty, written at the first change); an unreadable file
// is an error, never overwritten.
func ouvrirAcces(chemin string) (*Acces, error) {
	a := &Acces{chemin: chemin, f: fichierAcces{Version: 1, Projets: map[string]*ProjetAcces{}}}
	b, err := os.ReadFile(chemin)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &a.f); err != nil {
		return nil, fmt.Errorf("access file unreadable: %w", err)
	}
	if a.f.Version != 1 {
		return nil, errors.New("access file: unknown version")
	}
	if a.f.Projets == nil {
		a.f.Projets = map[string]*ProjetAcces{}
	}
	return a, nil
}

// modifier applies f to a copy of the file and saves it; on any error the memory is left unchanged.
func (a *Acces) modifier(f func(*fichierAcces) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, _ := json.Marshal(a.f)
	var copie fichierAcces
	_ = json.Unmarshal(b, &copie)
	if copie.Projets == nil {
		copie.Projets = map[string]*ProjetAcces{}
	}
	if err := f(&copie); err != nil {
		return err
	}
	b, err := json.MarshalIndent(copie, "", "  ")
	if err != nil {
		return err
	}
	if err := ecrireAtomique(a.chemin, b, 0o600); err != nil {
		return err
	}
	a.f = copie
	return nil
}

// ResumeAcces: the badge next to Start, for the project's current environment. Total counts every grant
// (infra ones included); Secrets and Variables only what the pilot receives (code scope).
type ResumeAcces struct {
	Env       string   `json:"env"`
	Envs      []string `json:"envs"`
	Total     int      `json:"total"`
	Secrets   int      `json:"secrets"`
	Variables int      `json:"variables"`
	Infra     int      `json:"infra"`
	Etat      string   `json:"etat"` // the injection's state (EtatInjection.Etat)
}

func resumerAcces(p ProjetAcces, entrees []EntreeCoffre) ResumeAcces {
	r := ResumeAcces{Env: p.Env, Envs: envsProjet}
	par := map[string]EntreeCoffre{}
	for _, e := range entrees {
		par[e.ID] = e
	}
	for _, a := range p.Acces {
		e, ok := par[a.Entree]
		if !ok || a.Env != p.Env {
			continue
		}
		r.Total++
		switch {
		case e.Portee != "code":
			r.Infra++
		case e.Famille == "variable":
			r.Variables++
		default:
			r.Secrets++
		}
	}
	return r
}

// Projet: a copy of a project's grants (environment dev when it has none).
func (a *Acces) Projet(nom string) ProjetAcces {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := ProjetAcces{Env: "dev", Acces: []AccesProjet{}}
	if x := a.f.Projets[nom]; x != nil {
		p.Env, p.ForcerInfraNatif, p.Acces = x.Env, x.ForcerInfraNatif, slices.Clone(x.Acces)
	}
	if p.Acces == nil {
		p.Acces = []AccesProjet{}
	}
	return p
}

func projetDe(f *fichierAcces, nom string) *ProjetAcces {
	if f.Projets[nom] == nil {
		f.Projets[nom] = &ProjetAcces{Env: "dev", Acces: []AccesProjet{}}
	}
	return f.Projets[nom]
}

// variableParType: the proposed name when the entry's own name is not a valid shell name.
var variableParType = map[string]string{
	"cloudflare": "CLOUDFLARE_API_TOKEN", "ssh": "SSH_PRIVATE_KEY", "certificat": "TLS_CERTIFICATE",
	"github": "GITHUB_TOKEN", "anthropic": "APP_ANTHROPIC_API_KEY", "openai": "OPENAI_API_KEY",
	"stripe": "STRIPE_KEY", "aws": "AWS_SECRET_ACCESS_KEY", "mot_de_passe": "APP_PASSWORD",
	"jeton": "API_TOKEN", "autre": "APP_SECRET",
}

func variableProposee(e EntreeCoffre) string {
	if nomSecretValide(e.Nom) {
		return e.Nom
	}
	if v, ok := variableParType[e.Type]; ok && e.Famille == "secret" {
		return v
	}
	return "APP_VALUE"
}

// libre: v, or v_2, v_3… when v is already used in this environment.
func libre(p *ProjetAcces, env, v string) string {
	pris := map[string]bool{}
	for _, x := range p.Acces {
		if x.Env == env {
			pris[x.Variable] = true
		}
	}
	for i, n := 2, v; ; i++ {
		if !pris[n] {
			return n
		}
		n = fmt.Sprintf("%s_%d", v, i)
	}
}

// Ajouter grants entry e to the project for env, under variable (proposed when empty).
func (a *Acces) Ajouter(projet string, e EntreeCoffre, env, variable, auto string) (AccesProjet, error) {
	if !slices.Contains(envsProjet, env) {
		return AccesProjet{}, errAccesEnv
	}
	variable = strings.TrimSpace(variable)
	if variable != "" && !nomSecretValide(variable) {
		return AccesProjet{}, errAccesVariable
	}
	var n AccesProjet
	err := a.modifier(func(f *fichierAcces) error {
		p := projetDe(f, projet)
		for _, x := range p.Acces {
			if x.Entree == e.ID && x.Env == env {
				return errAccesDeja
			}
			if x.Variable == variable && x.Env == env {
				return errAccesDoublon
			}
		}
		if variable == "" {
			variable = libre(p, env, variableProposee(e))
		}
		n = AccesProjet{Entree: e.ID, Env: env, Variable: variable, Auto: auto, Cree: time.Now().UTC()}
		p.Acces = append(p.Acces, n)
		return nil
	})
	return n, err
}

func (a *Acces) Retirer(projet, entree, env string) error {
	return a.modifier(func(f *fichierAcces) error {
		p := f.Projets[projet]
		if p == nil {
			return errAccesInconnu
		}
		avant := len(p.Acces)
		p.Acces = slices.DeleteFunc(p.Acces, func(x AccesProjet) bool { return x.Entree == entree && x.Env == env })
		if len(p.Acces) == avant {
			return errAccesInconnu
		}
		return nil
	})
}

func (a *Acces) RenommerVariable(projet, entree, env, variable string) error {
	variable = strings.TrimSpace(variable)
	if !nomSecretValide(variable) {
		return errAccesVariable
	}
	return a.modifier(func(f *fichierAcces) error {
		p := f.Projets[projet]
		if p == nil {
			return errAccesInconnu
		}
		i := slices.IndexFunc(p.Acces, func(x AccesProjet) bool { return x.Entree == entree && x.Env == env })
		if i < 0 {
			return errAccesInconnu
		}
		for j, x := range p.Acces {
			if j != i && x.Env == env && x.Variable == variable {
				return errAccesDoublon
			}
		}
		p.Acces[i].Variable = variable
		return nil
	})
}

func (a *Acces) ChangerEnv(projet, env string) error {
	if !slices.Contains(envsProjet, env) {
		return errAccesEnv
	}
	return a.modifier(func(f *fichierAcces) error {
		projetDe(f, projet).Env = env
		return nil
	})
}

// Synchroniser follows the vault and the project list: grants of entries no longer in the vault and
// projects no longer in the list go; entries imported from a project (source projet:<name>) are granted
// to it on dev once (removing that grant does not bring it back). Nothing is written when nothing changes.
func (a *Acces) Synchroniser(entrees []EntreeCoffre, projets []string) error {
	ids := map[string]bool{}
	for _, e := range entrees {
		ids[e.ID] = true
	}
	connu := map[string]bool{}
	for _, p := range projets {
		connu[p] = true
	}
	a.mu.Lock()
	change := false
	for nom, p := range a.f.Projets {
		change = change || !connu[nom] || slices.ContainsFunc(p.Acces, func(x AccesProjet) bool { return !ids[x.Entree] })
	}
	var auto []EntreeCoffre
	for _, e := range entrees {
		if nom, ok := strings.CutPrefix(e.Source, "projet:"); ok && connu[nom] && !slices.Contains(a.f.AutoFaits, e.ID) {
			auto = append(auto, e)
		}
	}
	a.mu.Unlock()
	if !change && len(auto) == 0 {
		return nil
	}
	return a.modifier(func(f *fichierAcces) error {
		for nom, p := range f.Projets {
			if !connu[nom] {
				delete(f.Projets, nom)
				continue
			}
			p.Acces = slices.DeleteFunc(p.Acces, func(x AccesProjet) bool { return !ids[x.Entree] })
		}
		for _, e := range auto {
			nom := strings.TrimPrefix(e.Source, "projet:")
			p := projetDe(f, nom)
			if !slices.ContainsFunc(p.Acces, func(x AccesProjet) bool { return x.Entree == e.ID && x.Env == "dev" }) {
				p.Acces = append(p.Acces, AccesProjet{Entree: e.ID, Env: "dev", Variable: libre(p, "dev", variableProposee(e)),
					Auto: "migration", Cree: time.Now().UTC()})
			}
			f.AutoFaits = append(f.AutoFaits, e.ID)
		}
		return nil
	})
}

// RenommerProjet moves a project's grants to its new name.
func (a *Acces) RenommerProjet(ancien, nouveau string) error {
	return a.modifier(func(f *fichierAcces) error {
		if p := f.Projets[ancien]; p != nil {
			delete(f.Projets, ancien)
			f.Projets[nouveau] = p
		}
		return nil
	})
}

func (a *Acces) SupprimerProjet(nom string) error {
	return a.modifier(func(f *fichierAcces) error {
		delete(f.Projets, nom)
		return nil
	})
}
