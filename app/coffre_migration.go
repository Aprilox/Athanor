package main

// Migration of the secrets kept before the vault (V§10.7): a copy, never a move, of each project's
// .athanor/secrets.env, of extensions/secrets.json and of the secret keys of the kit's .env into code-scope
// entries marked with their source. The fingerprint makes a new run create nothing; hub/coffre-migration.json
// lists what was created, so "Undo the import" removes exactly that. The old files are only read.

import (
	"errors"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type etatMigration struct {
	Entrees    []string   `json:"entrees"`    // the ids it created
	Empreintes []string   `json:"empreintes"` // the values already imported: an entry the user deleted is not brought back
	Annulee    bool       `json:"annulee,omitempty"`
	AnnuleeLe  *time.Time `json:"annulee_le,omitempty"`
	Maj        time.Time  `json:"maj"`
}

type secretAncien struct{ nom, valeur, typ, source, fichier string }

var migrationMu sync.Mutex

// shortcut: off under `go test`, so the servers of the other tests never import their fixtures into a
// vault; the migration test switches it on.
var migrationAuto = !testing.Testing()

func cheminMigration(c *Coffre) string {
	return filepath.Join(filepath.Dir(c.chemin), "coffre-migration.json")
}

func lireEtatMigration(chemin string) etatMigration {
	var e etatMigration
	lireJSONFichier(chemin, &e)
	return e
}

func (e *etatMigration) enregistrer(chemin string) error {
	e.Maj = time.Now().UTC()
	return ecrireJSONFichier(chemin, e, 0o600)
}

// secretsAnciens: the secrets of the old files, placeholders and oversized values left out.
func (s *Serveur) secretsAnciens() []secretAncien {
	s.mu.Lock()
	projets := slices.Clone(s.config.Projets)
	s.mu.Unlock()
	kit := s.kitCourant()
	var tous []secretAncien
	for _, p := range projets {
		b, err := lireAtelier(p, "secrets.env", tailleTachesMax)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimRight(l, "\r")
			if m := reLigneSecret.FindStringSubmatch(l); m != nil { // NAME='value', ' written '\''
				v := strings.ReplaceAll(l[len(m[1])+2:len(l)-1], `'\''`, "'")
				tous = append(tous, secretAncien{nom: m[1], valeur: v, source: "projet:" + p.Nom, fichier: p.Nom + "/.athanor/secrets.env"})
			}
		}
	}
	sec := lireSecrets(kit)
	for _, id := range slices.Sorted(maps.Keys(sec)) {
		for _, nom := range slices.Sorted(maps.Keys(sec[id])) {
			tous = append(tous, secretAncien{nom: nom, valeur: sec[id][nom], source: "mcp:" + id, fichier: "extensions/secrets.json (" + id + ")"})
		}
	}
	for _, l := range LireEnv(kit.Env()) {
		if l.Cle != "" && estNomSecret(l.Cle) {
			tous = append(tous, secretAncien{nom: l.Cle, valeur: sansGuillemets(l.Valeur), source: ".env", fichier: ".env"})
		}
	}
	var out []secretAncien
	for _, a := range tous {
		p := proposerNomValeur(a.nom, a.valeur) // the type recognised as by Paste; nil = a placeholder
		if p == nil || a.valeur == "" || len(a.valeur) > maxValeurCoffre || utf8.RuneCountInString(a.nom) > maxNomCoffre {
			continue
		}
		a.typ = p.Type
		if p.Famille == "variable" { // given as a secret, even under an ordinary name
			a.typ = "autre"
		}
		out = append(out, a)
	}
	return out
}

// migrerSecrets copies the old secrets into the vault and returns how many entries it created. auto: at
// start-up or at the unlock, never after an undo; otherwise "Import again", which clears the undo.
func (s *Serveur) migrerSecrets(auto bool) (int, error) {
	if s.coffre == nil || s.coffreErr != nil || (auto && !migrationAuto) {
		return 0, nil
	}
	migrationMu.Lock()
	defer migrationMu.Unlock()
	chemin := cheminMigration(s.coffre)
	etat := lireEtatMigration(chemin)
	if auto && etat.Annulee {
		return 0, nil
	}
	anciens := s.secretsAnciens()
	if len(anciens) > 0 {
		if err := s.coffre.OuvrirCode(); err != nil {
			return 0, err // closed (no keystore here): the unlock runs it again
		}
	}
	if !auto && etat.Annulee {
		etat.Annulee, etat.AnnuleeLe = false, nil
		if err := etat.enregistrer(chemin); err != nil {
			return 0, err
		}
	}
	connues := map[string]bool{}
	for _, e := range s.coffre.Liste() {
		connues[e.Empreinte] = true
	}
	for _, f := range etat.Empreintes {
		connues[f] = true
	}
	d := dicoTUI(s.langue())
	cree := 0
	for _, a := range anciens {
		f := s.coffre.Empreinte(a.valeur)
		if f == "" || connues[f] {
			continue
		}
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: a.nom, Famille: "secret", Type: a.typ, Portee: "code",
			Source: a.source, Note: traduireTUI(d, "Imported from {0}", a.fichier)}, a.valeur)
		if errors.As(err, new(erreurEntree)) {
			continue
		}
		if err != nil {
			return cree, err
		}
		connues[f] = true
		etat.Entrees, etat.Empreintes = append(etat.Entrees, e.ID), append(etat.Empreintes, f)
		if err := etat.enregistrer(chemin); err != nil { // after each one: an interrupted run still undoes what it made
			return cree, err
		}
		cree++
	}
	return cree, nil
}

// annulerMigration removes the entries the migration created (those the user already deleted are
// skipped) and marks it undone: nothing is imported again until "Import again".
func (s *Serveur) annulerMigration() (int, error) {
	if s.coffre == nil || s.coffreErr != nil {
		return 0, errors.New("The vault file cannot be read")
	}
	migrationMu.Lock()
	defer migrationMu.Unlock()
	chemin := cheminMigration(s.coffre)
	etat := lireEtatMigration(chemin)
	n := 0
	for len(etat.Entrees) > 0 {
		if err := s.coffre.Supprimer(etat.Entrees[0]); err != nil && !errors.Is(err, errCoffreInconnu) {
			_ = etat.enregistrer(chemin)
			return n, err
		} else if err == nil {
			n++
		}
		etat.Entrees = etat.Entrees[1:]
	}
	maintenant := time.Now().UTC()
	etat = etatMigration{Annulee: true, AnnuleeLe: &maintenant}
	return n, etat.enregistrer(chemin)
}

// resumeMigration: for the Vault page, the imported entries still in the vault and whether it was undone.
func (s *Serveur) resumeMigration() map[string]any {
	etat := lireEtatMigration(cheminMigration(s.coffre))
	ids := map[string]bool{}
	for _, e := range s.coffre.Liste() {
		ids[e.ID] = true
	}
	n := 0
	for _, id := range etat.Entrees {
		if ids[id] {
			n++
		}
	}
	return map[string]any{"importees": n, "annulee": etat.Annulee}
}

// apiCoffreMigration: POST /api/coffre/migration, "Import again".
func (s *Serveur) apiCoffreMigration(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	n, err := s.migrerSecrets(false)
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	s.ecrireJSON(w, map[string]int{"importees": n})
}

// apiCoffreMigrationAnnuler: POST /api/coffre/migration/annuler, "Undo the import".
func (s *Serveur) apiCoffreMigrationAnnuler(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	n, err := s.annulerMigration()
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	s.ecrireJSON(w, map[string]int{"retirees": n})
}
