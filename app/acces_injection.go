package main

// Injection of the grants (stage 3): the application, which holds the vault keys, rewrites the project's
// .athanor/secrets.env (where the pilot already reads its values) with exactly the code-scope entries
// granted for the project's current environment. Infra entries are never written there.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const enteteSecrets = "# Values Athanor gives Claude (vault grants and For you answers; sourced by its commands, never committed)."

// EtatInjection: what the injection did, or would do, for a project (names only, never a value).
type EtatInjection struct {
	Etat    string   `json:"etat"`    // ok | verrouille | indisponible | erreur
	Donnees []string `json:"donnees"` // variables written from the vault
	Repli   []string `json:"repli"`   // lines of the old file whose value is not in the vault, kept
	// B21: granted but not written, the value holding several lines (the file has one value per line)
	Ignorees []string `json:"ignorees"`
}

// ligneSecret: one NAME='value' line of secrets.env; nom "" is another line (a comment, export K="v")
// written by someone else, kept as it is in valeur (B18).
type ligneSecret struct{ nom, valeur string }

// estCommentaireAutrui (B18): a comment written by someone else (not one of Athanor's headers, both
// "# Values "), kept as it is.
func estCommentaireAutrui(l string) bool {
	return strings.HasPrefix(strings.TrimSpace(l), "#") && !strings.HasPrefix(l, "# Values ")
}

// ecarterLignes (B18): the other lines of a secrets.env about to be rewritten (LEGACY=v, export K="v"),
// never sourced again since they could run a command, are added to secrets.env.ancien instead of lost.
func ecarterLignes(p Projet, avant []byte) error {
	ancien, err := lireAtelier(p, "secrets.env.ancien", tailleTachesMax)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	ajout := ""
	for _, l := range strings.Split(string(avant), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" || estCommentaireAutrui(l) || strings.HasPrefix(l, "# Values ") || reLigneSecret.MatchString(l) ||
			slices.Contains(strings.Split(string(ancien)+ajout, "\n"), l) {
			continue
		}
		ajout += l + "\n"
	}
	if ajout == "" {
		return nil
	}
	if len(ancien) == 0 {
		ancien = []byte("# Lines of secrets.env not in the NAME='value' format, set aside by Athanor (never sourced).\n")
	}
	return ecrireAtelier(p, "", "secrets.env.ancien", append(ancien, ajout...))
}

func lireLignesSecrets(b []byte) []ligneSecret {
	var l []ligneSecret
	for _, x := range strings.Split(string(b), "\n") {
		x = strings.TrimRight(x, "\r")
		if m := reLigneSecret.FindStringSubmatch(x); m != nil {
			l = append(l, ligneSecret{m[1], strings.ReplaceAll(x[len(m[1])+2:len(x)-1], `'\''`, "'")})
		} else if estCommentaireAutrui(x) {
			l = append(l, ligneSecret{"", x})
		}
	}
	return l
}

func (s *Serveur) projetNomme(nom string) (Projet, bool) {
	return s.projetParNom(nom)
}

// planInjection computes the new secrets.env lines; ecrire is false when nothing must be written (same
// lines, or a state other than ok). garder: values to keep in the file although no longer in the vault
// (Undo the import). Called with verrouReponses held.
func (s *Serveur) planInjection(p Projet, garder map[string]string) (lignes []ligneSecret, etat EtatInjection, ecrire bool) {
	etat = EtatInjection{Etat: "indisponible", Donnees: []string{}, Repli: []string{}, Ignorees: []string{}}
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
			if err == nil {
				etat.Ignorees = append(etat.Ignorees, a.Variable) // B21: said on the panel
			}
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
		if l.nom == "" {
			lignes = append(lignes, l)
			continue
		}
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
	return s.injecterSansVerrou(p, garder)
}

// injecterSansVerrou: injecterAcces for a caller that already holds verrouReponses (For you answers).
func (s *Serveur) injecterSansVerrou(p Projet, garder map[string]string) (EtatInjection, error) {
	lignes, etat, ecrire := s.planInjection(p, garder)
	if !ecrire {
		return etat, nil
	}
	if err := ecrireLignesSecrets(p, lignes); err != nil {
		etat.Etat = "erreur"
		return etat, err
	}
	return etat, nil
}

// ecrireLignesSecrets writes the project's secrets.env (and its workers' copies) from its lines.
func ecrireLignesSecrets(p Projet, lignes []ligneSecret) error {
	texte := []string{enteteSecrets}
	for _, l := range lignes {
		if l.nom == "" {
			texte = append(texte, l.valeur)
		} else {
			texte = append(texte, l.nom+"="+citerShell(l.valeur))
		}
	}
	contenu := []byte(strings.Join(texte, "\n") + "\n")
	if avant, err := lireAtelier(p, "secrets.env", tailleTachesMax); err == nil {
		if err := ecarterLignes(p, avant); err != nil {
			return err // never a line lost
		}
	}
	if err := ecrireAtelier(p, "", "secrets.env", contenu); err != nil {
		return err
	}
	copiesOuvriers(p, contenu)
	return nil
}

// viderAccesUrgence (B51): at the emergency stop, the values granted from the vault leave every project's
// secrets.env (by name: no key needed); the resume or the unlock writes them again. Returns the number of
// files emptied.
func (s *Serveur) viderAccesUrgence(projets []Projet) int {
	if s.acces == nil || s.accesErr != nil {
		return 0
	}
	verrouReponses.Lock()
	defer verrouReponses.Unlock()
	n := 0
	for _, p := range projets {
		pa := s.acces.Projet(p.Nom)
		accordees := map[string]bool{}
		for _, a := range pa.Acces {
			if a.Env == pa.Env {
				accordees[a.Variable] = true
			}
		}
		avant, err := lireAtelier(p, "secrets.env", tailleTachesMax)
		if err != nil || len(accordees) == 0 {
			continue
		}
		anciennes := lireLignesSecrets(avant)
		garde := slices.DeleteFunc(slices.Clone(anciennes), func(l ligneSecret) bool { return accordees[l.nom] })
		if len(garde) != len(anciennes) && ecrireLignesSecrets(p, garde) == nil {
			n++
		}
	}
	return n
}

// copiesOuvriers (A6): the copy of secrets.env each parallel worker got at its start (work/<id>/.athanor)
// follows the project's file, so a removed grant leaves them too.
func copiesOuvriers(p Projet, contenu []byte) {
	ouvriers, _ := os.ReadDir(filepath.Join(p.Atelier(), "work"))
	for _, o := range ouvriers {
		f := filepath.Join(p.Atelier(), "work", o.Name(), ".athanor", "secrets.env")
		if o.IsDir() && fichierRegulierPilote(f) && !estLien(f) {
			if os.WriteFile(f, contenu, 0o600) == nil {
				_ = restreindreFichier(f)
			}
		}
	}
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
