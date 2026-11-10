package main

// Injection of the grants (stage 3): the application, which holds the vault keys, rewrites the project's
// .athanor/secrets.env (where the pilot already reads its values) with exactly the code-scope entries
// granted for the project's current environment. Infra entries are never written there.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

const enteteSecrets = "# Values Athanor gives Claude (vault grants and For you answers; sourced by its commands, never committed)."

// EtatInjection: what the injection did, or would do, for a project (names only, never a value).
type EtatInjection struct {
	Etat    string   `json:"etat"`    // ok | verrouille | indisponible | erreur
	Donnees []string `json:"donnees"` // variables written from the vault
	Repli   []string `json:"repli"`   // lines of the old file whose value is not in the vault, kept
}

// ligneSecret: one NAME='value' line of secrets.env.
type ligneSecret struct{ nom, valeur string }

func lireLignesSecrets(b []byte) []ligneSecret {
	var l []ligneSecret
	for _, x := range strings.Split(string(b), "\n") {
		x = strings.TrimRight(x, "\r")
		if m := reLigneSecret.FindStringSubmatch(x); m != nil {
			l = append(l, ligneSecret{m[1], strings.ReplaceAll(x[len(m[1])+2:len(x)-1], `'\''`, "'")})
		}
	}
	return l
}

func (s *Serveur) projetNomme(nom string) (Projet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.config.Projets {
		if p.Nom == nom {
			return p, true
		}
	}
	return Projet{}, false
}

// planInjection computes the new secrets.env lines; ecrire is false when nothing must be written (same
// lines, or a state other than ok). garder: values to keep in the file although no longer in the vault
// (Undo the import). Called with verrouReponses held.
func (s *Serveur) planInjection(p Projet, garder map[string]string) (lignes []ligneSecret, etat EtatInjection, ecrire bool) {
	etat = EtatInjection{Etat: "indisponible", Donnees: []string{}, Repli: []string{}}
	if s.coffre == nil || s.coffreErr != nil || s.acces == nil || s.accesErr != nil {
		return nil, etat, false
	}
	entrees := map[string]EntreeCoffre{}
	valeursVariables := map[string]bool{} // a variable is compared by its value: no key needed
	for _, e := range s.coffre.Liste() {
		entrees[e.ID] = e
		if e.Famille == "variable" {
			valeursVariables[e.Valeur] = true
		}
	}
	empreintes := true
	if len(entrees) > 0 {
		if err := s.coffre.OuvrirCode(); errors.Is(err, errCoffreSansMdp) {
			empreintes = false // no code key at all: the vault holds variables only
		} else if err != nil {
			etat.Etat = "verrouille" // the file stays as it is
			return nil, etat, false
		}
	}
	donnees := map[string]string{}
	pa := s.acces.Projet(p.Nom)
	for _, a := range pa.Acces {
		e, ok := entrees[a.Entree]
		if !ok || a.Env != pa.Env || e.Portee != "code" {
			continue // infra: kept by Athanor for its own actions, never given to the pilot
		}
		v, err := s.coffre.Valeur(e.ID)
		if errors.Is(err, errCoffreVerrouille) || errors.Is(err, errCoffreSansMdp) {
			etat.Etat = "verrouille"
			return nil, etat, false
		}
		// shortcut: a multi-line value would break the line format of secrets.env; skipped until the
		// file gets another format
		if err != nil || strings.ContainsAny(v, "\r\n\x00") {
			continue
		}
		donnees[a.Variable] = v
	}
	connues := map[string]bool{}
	for _, f := range s.coffre.EmpreintesConnues() {
		connues[f] = true
	}
	dansCoffre := func(v string) bool {
		return valeursVariables[v] || (empreintes && connues[s.coffre.Empreinte(v)])
	}
	avant, err := lireAtelier(p, "secrets.env", tailleTachesMax)
	if err != nil && !os.IsNotExist(err) {
		etat.Etat = "erreur"
		return nil, etat, false
	}
	anciennes := lireLignesSecrets(avant)
	pris := map[string]bool{}
	for _, l := range anciennes { // decision 7: the old file is the fallback for what the vault lacks
		if _, ok := donnees[l.nom]; ok || pris[l.nom] || dansCoffre(l.valeur) {
			continue // replaced by its grant, or a vault value not granted here: it goes
		}
		lignes = append(lignes, l)
		pris[l.nom] = true
		etat.Repli = append(etat.Repli, l.nom)
	}
	for _, n := range slices.Sorted(maps.Keys(garder)) {
		if _, ok := donnees[n]; !ok && !pris[n] && nomSecretValide(n) && !strings.ContainsAny(garder[n], "\r\n\x00") {
			lignes = append(lignes, ligneSecret{n, garder[n]})
			pris[n] = true
			etat.Repli = append(etat.Repli, n)
		}
	}
	for _, n := range slices.Sorted(maps.Keys(donnees)) {
		lignes = append(lignes, ligneSecret{n, donnees[n]})
		etat.Donnees = append(etat.Donnees, n)
	}
	etat.Etat = "ok"
	slices.Sort(etat.Repli)
	return lignes, etat, !slices.Equal(lignes, anciennes)
}

// injecterAcces rewrites the project's secrets.env from its grants (see planInjection); the state says
// what was done. Never a value in an error or a log.
func (s *Serveur) injecterAcces(p Projet, garder map[string]string) (EtatInjection, error) {
	_ = s.synchroniserAcces() // automatic grants of freshly imported entries first
	verrouReponses.Lock()
	defer verrouReponses.Unlock()
	lignes, etat, ecrire := s.planInjection(p, garder)
	if !ecrire {
		return etat, nil
	}
	texte := []string{enteteSecrets}
	for _, l := range lignes {
		texte = append(texte, l.nom+"="+citerShell(l.valeur))
	}
	if err := ecrireAtelier(p, "", "secrets.env", []byte(strings.Join(texte, "\n")+"\n")); err != nil {
		etat.Etat = "erreur"
		return etat, err
	}
	return etat, nil
}

// etatInjection: what the next injection would give, for GET /api/acces (nothing is written).
func (s *Serveur) etatInjection(p Projet) EtatInjection {
	verrouReponses.RLock()
	defer verrouReponses.RUnlock()
	_, etat, _ := s.planInjection(p, nil)
	return etat
}

// injecterTous: after a change of the vault, every project's file follows (a granted value changed, an
// entry gone, a value now in the vault and not granted). garder: project → values to keep.
func (s *Serveur) injecterTous(garder map[string]map[string]string) {
	s.mu.Lock()
	projets := slices.Clone(s.config.Projets)
	s.mu.Unlock()
	for _, p := range projets {
		if _, err := s.injecterAcces(p, garder[p.Nom]); err != nil {
			fmt.Fprintln(os.Stderr, "access injection:", p.Nom+":", err) // the error of a file write, no value
		}
	}
}
