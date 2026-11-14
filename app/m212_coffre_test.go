package main

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// C11: an old copy of the file and the old password no longer open today's secrets; .ancien is removed.
func TestM212CoffreAncienFichier(t *testing.T) {
	c, chemin := coffreTest(t, nil)
	if err := c.Deverrouiller("ancien-mdp"); err != nil {
		t.Fatal(err)
	}
	ajouter(t, c, "Avant", "secret", "openai", "", "temoin-avant")
	copie, _ := os.ReadFile(chemin)
	if err := c.ChangerMotDePasse("ancien-mdp", "nouveau-mdp"); err != nil {
		t.Fatal(err)
	}
	e := ajouter(t, c, "Apres", "secret", "cloudflare", "", "temoin-apres")
	actuel, _ := os.ReadFile(chemin)
	// the attacker puts the old key envelopes in front of the current entries
	var vieux, neuf fichierCoffre
	_ = json.Unmarshal(copie, &vieux)
	_ = json.Unmarshal(actuel, &neuf)
	neuf.KDF, neuf.Cles = vieux.KDF, vieux.Cles
	b, _ := json.Marshal(neuf)
	if err := os.WriteFile(chemin, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c = rouvrir(t, chemin, nil)
	if err := c.Deverrouiller("ancien-mdp"); err != nil {
		t.Fatal(err)
	}
	if v, err := c.Valeur(e.ID); err == nil {
		t.Fatalf("old envelopes + old password opened a current secret: %q", v)
	}
	if err := os.WriteFile(chemin+".ancien", copie, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(chemin, actuel, 0o600)
	c = rouvrir(t, chemin, nil)
	if err := c.Deverrouiller("nouveau-mdp"); err != nil {
		t.Fatal(err)
	}
	ajouter(t, c, "Encore", "secret", "jeton", "", "x")
	if _, err := os.Stat(chemin + ".ancien"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("coffre.json.ancien kept after a save")
	}
}

// C12: two entry names swapped in the file no longer open; values sealed the old way are resealed.
func TestM212CoffreNomsDansAAD(t *testing.T) {
	c, chemin := coffreTest(t, nil)
	if err := c.Deverrouiller("mdp"); err != nil {
		t.Fatal(err)
	}
	a := ajouter(t, c, "STRIPE", "secret", "stripe", "", "temoin-stripe")
	b := ajouter(t, c, "OPENAI", "secret", "openai", "", "temoin-github")
	// an entry sealed the old way (before C12), as an old vault holds it
	i := c.indice(b.ID)
	k, _ := c.cle("code")
	c.f.Entrees[i].Valeur = sceller(k, []byte("temoin-github"), aadEntreeV1(c.f.Entrees[i]))
	if err := c.enregistrer(); err != nil {
		t.Fatal(err)
	}
	c = rouvrir(t, chemin, nil)
	if err := c.Deverrouiller("mdp"); err != nil {
		t.Fatal(err)
	}
	valeurAttendue(t, c, b.ID, "temoin-github") // migrated on opening
	var f fichierCoffre
	brut, _ := os.ReadFile(chemin)
	_ = json.Unmarshal(brut, &f)
	for _, e := range f.Entrees {
		if _, err := desceller(c.cleCode, e.Valeur, aadEntree(e)); err != nil {
			t.Fatalf("%s not resealed with its name in the AAD", e.Nom)
		}
	}
	// swap the names and types of the two entries in the file
	f.Entrees[0].Nom, f.Entrees[1].Nom = f.Entrees[1].Nom, f.Entrees[0].Nom
	f.Entrees[0].Type, f.Entrees[1].Type = f.Entrees[1].Type, f.Entrees[0].Type
	brut, _ = json.Marshal(f)
	_ = os.WriteFile(chemin, brut, 0o600)
	c = rouvrir(t, chemin, nil)
	if err := c.Deverrouiller("mdp"); err != nil {
		t.Fatal(err)
	}
	if v, err := c.Valeur(a.ID); err == nil {
		t.Fatalf("a renamed entry still opens: %q", v)
	}
	// a rename through Athanor reseals the value
	c = rouvrir(t, chemin, nil)
	_ = os.WriteFile(chemin, func() []byte {
		f.Entrees[0].Nom, f.Entrees[1].Nom = f.Entrees[1].Nom, f.Entrees[0].Nom
		f.Entrees[0].Type, f.Entrees[1].Type = f.Entrees[1].Type, f.Entrees[0].Type
		b, _ := json.Marshal(f)
		return b
	}(), 0o600)
	c = rouvrir(t, chemin, nil)
	if err := c.Deverrouiller("mdp"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ModifierMeta(a.ID, "STRIPE_LIVE", "stripe", "n", "", nil); err != nil {
		t.Fatal(err)
	}
	valeurAttendue(t, c, a.ID, "temoin-stripe")
}
