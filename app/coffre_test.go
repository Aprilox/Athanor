package main

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fauxSysteme stands for DPAPI/Keychain: a key that lives only in this process.
type fauxSysteme struct{ cle []byte }

func nouveauFauxSysteme() *fauxSysteme { return &fauxSysteme{octetsAleatoires(32)} }
func (s *fauxSysteme) Proteger(d []byte) ([]byte, error) {
	return []byte(sceller(s.cle, d, []byte("systeme"))), nil
}
func (s *fauxSysteme) Ouvrir(p []byte) ([]byte, error) {
	return desceller(s.cle, string(p), []byte("systeme"))
}

func coffreTest(t *testing.T, sys enveloppeSysteme) (*Coffre, string) {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "hub", "coffre.json")
	c, err := ouvrirCoffre(chemin, sys)
	if err != nil {
		t.Fatal(err)
	}
	return c, chemin
}

func rouvrir(t *testing.T, chemin string, sys enveloppeSysteme) *Coffre {
	t.Helper()
	c, err := ouvrirCoffre(chemin, sys)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ajouter(t *testing.T, c *Coffre, nom, famille, typ, portee, valeur string) EntreeCoffre {
	t.Helper()
	e, err := c.Ajouter(EntreeCoffre{Nom: nom, Famille: famille, Type: typ, Portee: portee, Note: "note " + nom}, valeur)
	if err != nil {
		t.Fatalf("Ajouter %s: %v", nom, err)
	}
	if famille == "secret" && e.Valeur != "" {
		t.Fatalf("Ajouter returned the secret value of %s", nom)
	}
	return e
}

func valeurAttendue(t *testing.T, c *Coffre, id, attendu string) {
	t.Helper()
	if v, err := c.Valeur(id); err != nil || v != attendu {
		t.Fatalf("Valeur(%s) = %q, %v; want %q", id, v, err, attendu)
	}
}

func TestCoffreAllerRetour(t *testing.T) {
	sys := nouveauFauxSysteme()
	c, chemin := coffreTest(t, sys)
	if err := c.Deverrouiller("témoin-mdp"); err != nil {
		t.Fatal(err)
	}
	code := ajouter(t, c, "Stripe test", "secret", "stripe", "", "temoin-code-5512")
	infra := ajouter(t, c, "DNS", "secret", "cloudflare", "", "temoin-infra-8840")
	vari := ajouter(t, c, "Domaine", "variable", "autre", "", "exemple.fr")
	if code.Portee != "code" || infra.Portee != "infra" || vari.Portee != "code" {
		t.Fatalf("proposed scopes: %s %s %s", code.Portee, infra.Portee, vari.Portee)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(chemin); st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", st.Mode().Perm())
		}
	}
	var f fichierCoffre
	b, _ := os.ReadFile(chemin)
	if err := json.Unmarshal(b, &f); err != nil || f.Cles.Code.Mdp == "" || f.Cles.Code.Systeme == "" || f.Cles.Infra.Mdp == "" {
		t.Fatalf("envelopes: %v %+v", err, f.Cles)
	}
	if strings.Count(string(b), `"systeme"`) != 1 {
		t.Fatal("the infra key must have no system envelope")
	}

	// Restart: the code key reopens by the keystore alone, the infra key waits for the password.
	c = rouvrir(t, chemin, sys)
	for _, e := range c.Liste() {
		if e.Famille == "secret" && e.Valeur != "" {
			t.Fatalf("Liste shows the value of %s", e.Nom)
		}
	}
	valeurAttendue(t, c, vari.ID, "exemple.fr")
	if err := c.OuvrirCode(); err != nil {
		t.Fatal(err)
	}
	valeurAttendue(t, c, code.ID, "temoin-code-5512")
	if _, err := c.Valeur(infra.ID); !errors.Is(err, errCoffreVerrouille) {
		t.Fatalf("infra without password: %v", err)
	}
	if err := c.Deverrouiller("témoin-mdp"); err != nil {
		t.Fatal(err)
	}
	valeurAttendue(t, c, infra.ID, "temoin-infra-8840")

	// Lock: infra closed, code still open (keystore), variables readable.
	c.Verrouiller()
	if _, err := c.Valeur(infra.ID); !errors.Is(err, errCoffreVerrouille) {
		t.Fatalf("infra after lock: %v", err)
	}
	if _, err := c.Ajouter(EntreeCoffre{Nom: "SSH", Famille: "secret", Type: "ssh"}, "x"); !errors.Is(err, errCoffreVerrouille) {
		t.Fatalf("infra entry while locked: %v", err)
	}
	valeurAttendue(t, c, code.ID, "temoin-code-5512")
	valeurAttendue(t, c, vari.ID, "exemple.fr")

	// Edit, replace, delete.
	if err := c.RemplacerValeur(code.ID, "temoin-code-6000"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ModifierMeta(code.ID, "Stripe", "stripe", "clé de test", "infra"); !errors.Is(err, errCoffreVerrouille) {
		t.Fatalf("scope change to a closed key: %v", err)
	}
	if err := c.Deverrouiller("témoin-mdp"); err != nil {
		t.Fatal(err)
	}
	if e, err := c.ModifierMeta(code.ID, "Stripe", "stripe", "clé de test", "infra"); err != nil || e.Portee != "infra" || e.Valeur != "" {
		t.Fatalf("ModifierMeta: %+v %v", e, err)
	}
	c = rouvrir(t, chemin, sys)
	if err := c.Deverrouiller("témoin-mdp"); err != nil {
		t.Fatal(err)
	}
	valeurAttendue(t, c, code.ID, "temoin-code-6000")
	if err := c.Supprimer(vari.ID); err != nil || len(c.Liste()) != 2 {
		t.Fatalf("Supprimer: %v", err)
	}
	if _, err := c.Valeur(vari.ID); !errors.Is(err, errCoffreInconnu) {
		t.Fatalf("deleted entry: %v", err)
	}
}

func TestCoffreRefus(t *testing.T) {
	// No password and no keystore: variables only.
	c, _ := coffreTest(t, nil)
	if _, err := c.Ajouter(EntreeCoffre{Nom: "k", Famille: "secret", Type: "jeton"}, "v"); !errors.Is(err, errCoffreSansMdp) {
		t.Fatalf("code secret without any envelope: %v", err)
	}
	ajouter(t, c, "Région", "variable", "autre", "", "eu-west-3")
	for _, e := range []EntreeCoffre{
		{Nom: "", Famille: "variable", Type: "autre"},
		{Nom: "x", Famille: "autre", Type: "autre"},
		{Nom: "x", Famille: "variable", Type: "inconnu"},
		{Nom: "x", Famille: "secret", Type: "jeton", Portee: "partout"},
	} {
		if _, err := c.Ajouter(e, "v"); err == nil {
			t.Fatalf("accepted %+v", e)
		}
	}

	// Keystore only: code secrets work, the first infra entry asks for a password.
	c, chemin := coffreTest(t, nouveauFauxSysteme())
	ajouter(t, c, "OpenAI", "secret", "openai", "", "v")
	if _, err := c.Ajouter(EntreeCoffre{Nom: "SSH", Famille: "secret", Type: "ssh"}, "v"); !errors.Is(err, errCoffreSansMdp) {
		t.Fatalf("infra without password: %v", err)
	}

	// Wrong password, and no removal of the password while an infra entry exists.
	if err := c.ChangerMotDePasse("", "témoin-mdp"); err != nil {
		t.Fatal(err)
	}
	ajouter(t, c, "SSH", "secret", "ssh", "", "v")
	c = rouvrir(t, chemin, nil)
	if err := c.Deverrouiller("faux"); !errors.Is(err, errCoffreMdp) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := c.ChangerMotDePasse("faux", "autre"); !errors.Is(err, errCoffreMdp) {
		t.Fatalf("change with a wrong password: %v", err)
	}
	if err := c.ChangerMotDePasse("témoin-mdp", ""); err == nil {
		t.Fatal("password removed with an infra entry")
	}
}

func TestCoffreChangerMdpDeuxFois(t *testing.T) {
	c, chemin := coffreTest(t, nil)
	if err := c.Deverrouiller("un"); err != nil {
		t.Fatal(err)
	}
	a := ajouter(t, c, "A", "secret", "anthropic", "", "temoin-a-1")
	b := ajouter(t, c, "B", "secret", "aws", "", "temoin-b-2")
	avant, _ := os.ReadFile(chemin)
	if err := c.ChangerMotDePasse("un", "deux"); err != nil {
		t.Fatal(err)
	}
	if err := c.ChangerMotDePasse("deux", "trois"); err != nil {
		t.Fatal(err)
	}
	var fa, fb fichierCoffre
	apres, _ := os.ReadFile(chemin)
	_ = json.Unmarshal(avant, &fa)
	_ = json.Unmarshal(apres, &fb)
	for i := range fa.Entrees { // only the envelopes are rewritten
		if fa.Entrees[i].Valeur != fb.Entrees[i].Valeur {
			t.Fatal("an entry was re-encrypted")
		}
	}
	c = rouvrir(t, chemin, nil)
	for _, m := range []string{"un", "deux"} {
		if err := c.Deverrouiller(m); !errors.Is(err, errCoffreMdp) {
			t.Fatalf("old password %q: %v", m, err)
		}
	}
	if err := c.Deverrouiller("trois"); err != nil {
		t.Fatal(err)
	}
	valeurAttendue(t, c, a.ID, "temoin-a-1")
	valeurAttendue(t, c, b.ID, "temoin-b-2")
}

// alterer rewrites the vault file through f, then reopens and unlocks it.
func alterer(t *testing.T, chemin string, f func(*fichierCoffre)) *Coffre {
	t.Helper()
	var fc fichierCoffre
	b, _ := os.ReadFile(chemin)
	if err := json.Unmarshal(b, &fc); err != nil {
		t.Fatal(err)
	}
	f(&fc)
	b, _ = json.Marshal(fc)
	if err := os.WriteFile(chemin, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c := rouvrir(t, chemin, nil)
	if err := c.Deverrouiller("mdp"); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCoffreAltere(t *testing.T) {
	c, chemin := coffreTest(t, nil)
	if err := c.Deverrouiller("mdp"); err != nil {
		t.Fatal(err)
	}
	a := ajouter(t, c, "A", "secret", "jeton", "", "temoin-a")
	b := ajouter(t, c, "B", "secret", "jeton", "", "temoin-b")
	original, _ := os.ReadFile(chemin)

	// Values swapped between two entries: the AAD (id|family|scope) no longer matches.
	c = alterer(t, chemin, func(f *fichierCoffre) {
		f.Entrees[0].Valeur, f.Entrees[1].Valeur = f.Entrees[1].Valeur, f.Entrees[0].Valeur
	})
	for _, id := range []string{a.ID, b.ID} {
		if _, err := c.Valeur(id); err == nil {
			t.Fatal("swapped value accepted")
		}
	}
	// One byte of the ciphertext flipped.
	_ = os.WriteFile(chemin, original, 0o600)
	c = alterer(t, chemin, func(f *fichierCoffre) {
		v, _ := base64.StdEncoding.DecodeString(f.Entrees[0].Valeur)
		v[len(v)-1] ^= 1
		f.Entrees[0].Valeur = base64.StdEncoding.EncodeToString(v)
	})
	if _, err := c.Valeur(a.ID); err == nil {
		t.Fatal("altered value accepted")
	}
	valeurAttendue(t, c, b.ID, "temoin-b")
	// Family rewritten in the file: the clear value never comes out.
	_ = os.WriteFile(chemin, original, 0o600)
	c = alterer(t, chemin, func(f *fichierCoffre) { f.Entrees[1].Famille = "variable" })
	if v, _ := c.Valeur(b.ID); v == "temoin-b" {
		t.Fatal("family change gave the clear value")
	}
	// Altered key envelope: the password no longer opens it.
	_ = os.WriteFile(chemin, original, 0o600)
	var f fichierCoffre
	_ = json.Unmarshal(original, &f)
	k, _ := base64.StdEncoding.DecodeString(f.Cles.Infra.Mdp)
	k[20] ^= 1
	f.Cles.Infra.Mdp = base64.StdEncoding.EncodeToString(k)
	nb, _ := json.Marshal(f)
	_ = os.WriteFile(chemin, nb, 0o600)
	if err := rouvrir(t, chemin, nil).Deverrouiller("mdp"); err == nil {
		t.Fatal("altered envelope accepted")
	}
}

// TestCoffreCopieIlisible: a copy of coffre.json and of the config gives away no secret value in any
// common encoding or fast hash.
func TestCoffreCopieIlisible(t *testing.T) {
	c, chemin := coffreTest(t, nouveauFauxSysteme())
	if err := c.Deverrouiller("témoin-mdp-coffre"); err != nil {
		t.Fatal(err)
	}
	temoins := []string{"temoin-coffre-code-3141", "temoin-coffre-infra-2718"}
	e1 := ajouter(t, c, "Code", "secret", "stripe", "", temoins[0])
	ajouter(t, c, "Infra", "secret", "github", "", temoins[1])
	e3 := ajouter(t, c, "Même valeur", "secret", "jeton", "", temoins[0])
	if e1.Empreinte != "" || e3.Empreinte != "" {
		t.Fatal("a fingerprint left the vault")
	}
	if f := c.EmpreintesConnues(); len(f) != 1 {
		t.Fatalf("same value, same key: one fingerprint for both code entries and none for infra (migration), got %d", len(f))
	}
	config, _ := json.Marshal(Config{MotDePasseHash: hacherMotDePasse("témoin-mdp-coffre")})
	copie, _ := os.ReadFile(chemin)
	copie = append(copie, config...)
	for _, v := range temoins {
		s1, s256, m5 := sha1.Sum([]byte(v)), sha256.Sum256([]byte(v)), md5.Sum([]byte(v))
		for _, forme := range []string{
			v, base64.StdEncoding.EncodeToString([]byte(v)), base64.RawStdEncoding.EncodeToString([]byte(v)),
			base64.URLEncoding.EncodeToString([]byte(v)), hex.EncodeToString([]byte(v)),
			hex.EncodeToString(s1[:]), hex.EncodeToString(s256[:]), hex.EncodeToString(m5[:]),
		} {
			if bytes.Contains(bytes.ToLower(copie), []byte(strings.ToLower(forme))) {
				t.Fatalf("the copy contains %q", forme)
			}
		}
	}
	if bytes.Contains(copie, []byte("témoin-mdp-coffre")) {
		t.Fatal("the copy contains the password")
	}
}
