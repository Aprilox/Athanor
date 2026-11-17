package main

// The owner's categories for the API of the vault (M287): a category is {id, name} in coffre.json and an entry
// names at most one by its id, in clear and outside the sealed binding (no key is needed to sort the vault).
// Removing a category removes no entry: its entries go back to "no category".

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"unicode/utf8"
)

const maxNomCategorie = 50

// sansCategorie: the filter value of the lists for the entries that have no category (never an id: not hex).
const sansCategorie = "sans"

// categorieFournisseurs: the category migrerFournisseurs makes; the owner renames or removes it as any other.
const categorieFournisseurs = "Fournisseurs"

const (
	errCategorieNom      = erreurEntree("Give the category a name")
	errCategorieNomLong  = erreurEntree("The category name is too long (50 characters at most)")
	errCategoriePrise    = erreurEntree("A category with this name already exists")
	errCategorieInconnue = erreurEntree("This category no longer exists")
)

type CategorieCoffre struct {
	ID  string `json:"id"`
	Nom string `json:"nom"`
}

func (c *Coffre) indiceCategorie(id string) int {
	return slices.IndexFunc(c.f.Categories, func(x CategorieCoffre) bool { return x.ID == id })
}

// Categories: the owner's categories, in the order they were made.
func (c *Coffre) Categories() []CategorieCoffre {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]CategorieCoffre{}, c.f.Categories...)
}

// PoserCategorie makes a category (id "") or renames one; the name is trimmed, refused empty, too long or
// already taken by another category (any case).
func (c *Coffre) PoserCategorie(id, nom string) (CategorieCoffre, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indiceCategorie(id)
	nom = strings.TrimSpace(nom)
	switch {
	case id != "" && i < 0:
		return CategorieCoffre{}, errCategorieInconnue
	case nom == "":
		return CategorieCoffre{}, errCategorieNom
	case utf8.RuneCountInString(nom) > maxNomCategorie:
		return CategorieCoffre{}, errCategorieNomLong
	case slices.ContainsFunc(c.f.Categories, func(x CategorieCoffre) bool { return x.ID != id && strings.EqualFold(x.Nom, nom) }):
		return CategorieCoffre{}, errCategoriePrise
	}
	avant := slices.Clone(c.f.Categories)
	if i < 0 {
		c.f.Categories, i = append(c.f.Categories, CategorieCoffre{ID: aleatoire(8)}), len(c.f.Categories)
	}
	c.f.Categories[i].Nom = nom
	if err := c.enregistrer(); err != nil {
		c.f.Categories = avant
		return CategorieCoffre{}, err
	}
	return c.f.Categories[i], nil
}

// SupprimerCategorie removes a category; its entries stay in the vault, without a category.
func (c *Coffre) SupprimerCategorie(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indiceCategorie(id)
	if i < 0 {
		return errCategorieInconnue
	}
	categories, entrees := slices.Clone(c.f.Categories), slices.Clone(c.f.Entrees)
	c.f.Categories = slices.Delete(c.f.Categories, i, i+1)
	for j := range c.f.Entrees {
		if c.f.Entrees[j].Categorie == id {
			c.f.Entrees[j].Categorie = ""
		}
	}
	if err := c.enregistrer(); err != nil {
		c.f.Categories, c.f.Entrees = categories, entrees
		return err
	}
	return nil
}

// Classer puts an entry in a category ("" = none); its value, dates and binding do not change.
func (c *Coffre) Classer(id, categorie string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indice(id)
	switch {
	case i < 0:
		return errCoffreInconnu
	case categorie != "" && c.indiceCategorie(categorie) < 0:
		return errCategorieInconnue
	case c.f.Entrees[i].Categorie == categorie:
		return nil
	}
	avant := c.f.Entrees[i].Categorie
	c.f.Entrees[i].Categorie = categorie
	if err := c.enregistrer(); err != nil {
		c.f.Entrees[i].Categorie = avant
		return err
	}
	return nil
}

// classerFournisseurs, once per vault (FournisseursMigres): the entries of cles that have no category go to the
// category "Fournisseurs", made when there is none of that name; without any cle, only the mark is set (and
// written with the vault's next change when the file does not exist yet).
func (c *Coffre) classerFournisseurs(cles []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f.FournisseursMigres {
		return nil
	}
	categories, entrees := slices.Clone(c.f.Categories), slices.Clone(c.f.Entrees)
	c.f.FournisseursMigres = true
	if len(cles) > 0 {
		i := slices.IndexFunc(c.f.Categories, func(x CategorieCoffre) bool { return strings.EqualFold(x.Nom, categorieFournisseurs) })
		if i < 0 {
			c.f.Categories, i = append(c.f.Categories, CategorieCoffre{ID: aleatoire(8), Nom: categorieFournisseurs}), len(c.f.Categories)
		}
		for j, e := range c.f.Entrees {
			if e.Categorie == "" && slices.Contains(cles, e.ID) {
				c.f.Entrees[j].Categorie = c.f.Categories[i].ID
			}
		}
	}
	if err := c.enregistrerSiUtile(); err != nil {
		c.f.Categories, c.f.Entrees, c.f.FournisseursMigres = categories, entrees, false
		return err
	}
	return nil
}

// migrerFournisseurs (M287), at start-up: the providers became API of the vault, so the token of each provider
// of fournisseurs.json is sorted under "Fournisseurs". That file is only read, and no id changes: what the
// projects and the old missions refer to stays as it is. An unreadable file: tried again at the next start.
func (s *Serveur) migrerFournisseurs() {
	if s.coffre == nil || s.coffreErr != nil {
		return
	}
	l, err := lireFournisseurs()
	if err != nil {
		return
	}
	cles := make([]string, 0, len(l))
	for _, f := range l {
		cles = append(cles, f.Cle)
	}
	if err := s.coffre.classerFournisseurs(cles); err != nil {
		fmt.Fprintln(os.Stderr, "vault categories:", err)
	}
}

// apiCoffreCategories: GET /api/coffre/categories, the owner's categories.
func (s *Serveur) apiCoffreCategories(w http.ResponseWriter, _ *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	s.ecrireJSON(w, map[string]any{"categories": s.coffre.Categories()})
}

// apiCoffreCategoriePoser: POST /api/coffre/categories {nom} makes one, POST /api/coffre/categories/{id} {nom}
// renames it.
func (s *Serveur) apiCoffreCategoriePoser(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	var c struct {
		Nom string `json:"nom"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if lireCorps(r, &c) != nil {
		s.erreurCoffre(w, errCoffreRequete)
		return
	}
	cat, err := s.coffre.PoserCategorie(r.PathValue("id"), c.Nom)
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	s.ecrireJSON(w, cat)
}

// apiCoffreCategorieSupprimer: DELETE /api/coffre/categories/{id}; its API go back to "no category".
func (s *Serveur) apiCoffreCategorieSupprimer(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	if err := s.coffre.SupprimerCategorie(r.PathValue("id")); err != nil {
		s.erreurCoffre(w, err)
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}
