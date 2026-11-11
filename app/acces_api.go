package main

// GET/POST /api/acces: a project's grants. Answers carry the entries' metadata (name, type, family,
// scope), never a value.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
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
	n := make([]string, 0, len(s.config.Projets)+1)
	for _, p := range s.projetsHub() {
		n = append(n, p.Nom)
	}
	return n
}

// synchroniserAcces: grants follow the vault and the projects (see Acces.Synchroniser).
func (s *Serveur) synchroniserAcces() error {
	if s.acces == nil || s.coffre == nil {
		return nil
	}
	// Test the vault's temporary projects keep their grants while it runs (outil_sante_coffre.go)
	return s.acces.Synchroniser(s.coffre.Liste(), append(s.nomsProjets(), projetTestCoffre, voisinTestCoffre))
}

// accesPret: the grants and the vault are loaded and the project exists, or false once refused.
func (s *Serveur) accesPret(w http.ResponseWriter, projet string) bool {
	if code, err := s.accesPretErr(projet); err != nil {
		s.erreur(w, code, err.Error())
		return false
	}
	return true
}

func (s *Serveur) accesPretErr(projet string) (int, error) {
	switch {
	case s.coffreErr != nil || s.coffre == nil:
		return http.StatusInternalServerError, errors.New("The vault file cannot be read")
	case s.acces == nil || s.accesErr != nil:
		return http.StatusInternalServerError, errors.New("The access file cannot be read")
	case !slices.Contains(s.nomsProjets(), projet):
		return http.StatusNotFound, errors.New("Unknown project")
	}
	if err := s.synchroniserAcces(); err != nil {
		return http.StatusInternalServerError, err
	}
	return 0, nil
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
	inj := s.etatInjection(pr)
	r := resumerAcces(p, s.coffre.Liste())
	r.Etat = inj.Etat
	return map[string]any{"projet": projet, "env": p.Env, "envs": envsProjet, "forcer_infra_natif": p.ForcerInfraNatif, "natif": modeNatif.Load(), "acces": vus,
		"injection": inj, "resume": r}
}

// resumeAccesProjet: the badge of GET /api/etat, nil without a readable vault and access file.
func (s *Serveur) resumeAccesProjet(p Projet) *ResumeAcces {
	if s.coffre == nil || s.coffreErr != nil || s.acces == nil || s.accesErr != nil {
		return nil
	}
	r := resumerAcces(s.acces.Projet(p.Nom), s.coffre.Liste())
	r.Etat = s.etatInjection(p).Etat
	return &r
}

// apiAcces: GET /api/acces?projet=NAME.
func (s *Serveur) apiAcces(w http.ResponseWriter, r *http.Request) {
	projet := r.URL.Query().Get("projet")
	if !s.accesPret(w, projet) {
		return
	}
	s.ecrireJSON(w, s.etatAcces(projet))
}

// apiAccesModifier: POST /api/acces {projet, action: ajouter|retirer|renommer|env|forcer, entree, env,
// variable, forcer, confirme}.
func (s *Serveur) apiAccesModifier(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Projet, Action, Entree, Env, Variable string
		Forcer, Confirme                      bool
		MotDePasse                            string `json:"mot_de_passe"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, string(errCoffreRequete))
		return
	}
	if !s.accesPret(w, c.Projet) {
		return
	}
	if c.Action == "ajouter" || c.Action == "forcer" && c.Forcer { // C25: giving a key, not taking one back
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: c.Projet, Action: "accès refusé: " + c.Action, Resultat: err.Error()})
			s.erreur(w, code, err.Error())
			return
		}
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
		forcer := s.acces.Projet(c.Projet).ForcerInfraNatif
		if e.Portee == "infra" && modeNatif.Load() && !forcer {
			err = errAccesInfraNatif
			break
		}
		var a AccesProjet
		if a, err = s.acces.Ajouter(c.Projet, e, c.Env, c.Variable, ""); err == nil {
			action := "acces_accorde"
			if e.Portee == "infra" && modeNatif.Load() {
				action = "acces_accorde_force" // an infra entry linked in native mode, the project forced
			}
			s.auditAcces(deQui(r), c.Projet, action, e, a.Variable, a.Env)
		}
	case "forcer":
		if c.Forcer && !c.Confirme {
			err = errAccesConfirmer
			break
		}
		if err = s.acces.ForcerInfra(c.Projet, c.Forcer); err == nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: c.Projet, Action: "forcer_infra_natif",
				Resultat: fmt.Sprint(c.Forcer)})
		}
	case "retirer":
		if err = s.acces.Retirer(c.Projet, c.Entree, c.Env); err == nil {
			s.auditAcces(deQui(r), c.Projet, "acces_retire", s.entreeCoffre(c.Entree), "", c.Env)
		}
	case "renommer":
		err = s.acces.RenommerVariable(c.Projet, c.Entree, c.Env, c.Variable)
	case "env":
		err = s.acces.ChangerEnv(c.Projet, c.Env)
	default:
		err = errCoffreRequete
	}
	switch {
	case errors.Is(err, errAccesDoublon), errors.Is(err, errAccesDeja), errors.Is(err, errAccesInfraNatif):
		s.erreur(w, http.StatusConflict, err.Error())
	case errors.Is(err, errAccesConfirmer):
		s.erreur(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errAccesInconnu):
		s.erreur(w, http.StatusNotFound, err.Error())
	case err != nil:
		s.erreurCoffre(w, err)
	default:
		var inj EtatInjection
		var errInj error
		if p, ok := s.projetNomme(c.Projet); ok {
			if inj, errInj = s.injecterAcces(p, nil); errInj != nil {
				fmt.Fprintln(os.Stderr, "access injection:", p.Nom+":", errInj)
			}
		}
		etat := s.etatAcces(c.Projet)
		if errInj != nil { // the plan says ok, the file was not written: a revoked value may still be in it (M43 A5)
			r := etat["resume"].(ResumeAcces)
			r.Etat = inj.Etat
			etat["injection"], etat["resume"] = inj, r
		}
		s.ecrireJSON(w, etat)
	}
}
