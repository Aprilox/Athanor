package main

// GET/POST /api/acces: a project's grants. Answers carry the entries' metadata (name, type, family,
// scope), never a value.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
)

// chargerAcces loads hub/acces.json at start-up (an unreadable file is reported and left untouched).
func (s *Serveur) chargerAcces() {
	s.acces, s.accesErr = ouvrirAcces(cheminAcces())
	if s.accesErr != nil {
		fmt.Fprintln(os.Stderr, "access:", s.accesErr)
	}
}

func (s *Serveur) nomsProjets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := make([]string, 0, len(s.config.Projets))
	for _, p := range s.config.Projets {
		n = append(n, p.Nom)
	}
	return n
}

// synchroniserAcces: grants follow the vault and the projects (see Acces.Synchroniser).
func (s *Serveur) synchroniserAcces() error {
	if s.acces == nil || s.coffre == nil {
		return nil
	}
	return s.acces.Synchroniser(s.coffre.Liste(), s.nomsProjets())
}

// accesPret: the grants and the vault are loaded and the project exists, or false once refused.
func (s *Serveur) accesPret(w http.ResponseWriter, projet string) bool {
	if !s.coffrePret(w) {
		return false
	}
	if s.acces == nil || s.accesErr != nil {
		s.erreur(w, http.StatusInternalServerError, "The access file cannot be read")
		return false
	}
	for _, n := range s.nomsProjets() {
		if n == projet {
			if err := s.synchroniserAcces(); err != nil {
				s.erreur(w, http.StatusInternalServerError, err.Error())
				return false
			}
			return true
		}
	}
	s.erreur(w, http.StatusNotFound, "Unknown project")
	return false
}

// AccesVu: a grant as the page sees it, with its entry's metadata.
type AccesVu struct {
	AccesProjet
	Nom     string `json:"nom"`
	Famille string `json:"famille"`
	Type    string `json:"type"`
	Portee  string `json:"portee"`
}

func (s *Serveur) etatAcces(projet string) map[string]any {
	p := s.acces.Projet(projet)
	entrees := map[string]EntreeCoffre{}
	for _, e := range s.coffre.Liste() {
		entrees[e.ID] = e
	}
	vus := []AccesVu{}
	for _, a := range p.Acces {
		if e, ok := entrees[a.Entree]; ok {
			vus = append(vus, AccesVu{AccesProjet: a, Nom: e.Nom, Famille: e.Famille, Type: e.Type, Portee: e.Portee})
		}
	}
	pr, _ := s.projetNomme(projet)
	return map[string]any{"projet": projet, "env": p.Env, "envs": envsProjet, "forcer_infra_natif": p.ForcerInfraNatif, "acces": vus,
		"injection": s.etatInjection(pr)}
}

// apiAcces: GET /api/acces?projet=NAME.
func (s *Serveur) apiAcces(w http.ResponseWriter, r *http.Request) {
	projet := r.URL.Query().Get("projet")
	if !s.accesPret(w, projet) {
		return
	}
	s.ecrireJSON(w, s.etatAcces(projet))
}

// apiAccesModifier: POST /api/acces {projet, action: ajouter|retirer|renommer|env, entree, env, variable}.
func (s *Serveur) apiAccesModifier(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Projet, Action, Entree, Env, Variable string
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, string(errCoffreRequete))
		return
	}
	if !s.accesPret(w, c.Projet) {
		return
	}
	var err error
	switch c.Action {
	case "ajouter":
		var e EntreeCoffre
		for _, x := range s.coffre.Liste() {
			if x.ID == c.Entree {
				e = x
			}
		}
		if e.ID == "" {
			err = errCoffreInconnu
			break
		}
		_, err = s.acces.Ajouter(c.Projet, e, c.Env, c.Variable, "")
	case "retirer":
		err = s.acces.Retirer(c.Projet, c.Entree, c.Env)
	case "renommer":
		err = s.acces.RenommerVariable(c.Projet, c.Entree, c.Env, c.Variable)
	case "env":
		err = s.acces.ChangerEnv(c.Projet, c.Env)
	default:
		err = errCoffreRequete
	}
	switch {
	case errors.Is(err, errAccesDoublon), errors.Is(err, errAccesDeja):
		s.erreur(w, http.StatusConflict, err.Error())
	case errors.Is(err, errAccesInconnu):
		s.erreur(w, http.StatusNotFound, err.Error())
	case err != nil:
		s.erreurCoffre(w, err)
	default:
		if p, ok := s.projetNomme(c.Projet); ok {
			if _, err := s.injecterAcces(p, nil); err != nil {
				fmt.Fprintln(os.Stderr, "access injection:", p.Nom+":", err)
			}
		}
		s.ecrireJSON(w, s.etatAcces(c.Projet))
	}
}
