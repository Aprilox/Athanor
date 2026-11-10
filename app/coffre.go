package main

// The vault (hub/coffre.json): secrets encrypted with AES-256-GCM under two random data keys, variables
// in clear. The "code" key is wrapped by Athanor's password and by the system keystore (projects reopen
// their test keys alone); the "infra" key only by the password, so a process of the same user cannot
// open it through the keystore. Format: ARCHITECTURE.md §Coffre.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	errCoffreVerrouille = errors.New("The vault is locked: unlock Athanor to use this secret")
	errCoffreSansMdp    = errors.New("Choose a password for Athanor first (Settings › Password)")
	errCoffreMdp        = errors.New("Wrong password")
	errCoffreInconnu    = errors.New("Unknown vault entry")
)

// typesCoffre: the closed list of entry types, with the scope proposed for each.
var typesCoffre = map[string]string{
	"cloudflare": "infra", "ssh": "infra", "certificat": "infra", "github": "infra", "aws": "infra",
	"anthropic": "code", "openai": "code", "stripe": "code", "mot_de_passe": "code", "jeton": "code", "autre": "code",
}

// enveloppeSysteme: the system keystore (DPAPI, Keychain, Secret Service) that protects the code key.
type enveloppeSysteme interface {
	Proteger(donnees []byte) ([]byte, error)
	Ouvrir(protege []byte) ([]byte, error)
}

// serviceCoffre names the Keychain / Secret Service items; each code key gets its own account.
const serviceCoffre = "athanor-coffre"

var reCompteCoffre = regexp.MustCompile(`^code-[0-9a-f]{16}$`)

// nouveauCompteCoffre is the keystore reference stored in coffre.json instead of a blob.
func nouveauCompteCoffre() string { return "code-" + hex.EncodeToString(octetsAleatoires(8)) }

// outilCoffre runs a keystore command (security, secret-tool) with the data on stdin, never in argv,
// and a time limit so a keyring that asks to be unlocked cannot freeze Athanor.
func outilCoffre(entree string, nom string, args ...string) (string, error) {
	ctx, fin := context.WithTimeout(context.Background(), 10*time.Second)
	defer fin()
	cmd := exec.CommandContext(ctx, nom, args...)
	cmd.Stdin = strings.NewReader(entree)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", nom, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// cleDuCompte reads back a hex key printed by the keystore.
func cleDuCompte(sortie string) ([]byte, error) {
	k, err := hex.DecodeString(sortie)
	if err != nil || len(k) != 32 {
		return nil, errors.New("keystore returned no key")
	}
	return k, nil
}

type EntreeCoffre struct {
	ID        string    `json:"id"`
	Nom       string    `json:"nom"`
	Famille   string    `json:"famille"` // secret | variable
	Type      string    `json:"type"`
	Note      string    `json:"note"`
	Portee    string    `json:"portee"` // code | infra
	Valeur    string    `json:"valeur"` // secret: base64(nonce‖AES-GCM); variable: clear
	Source    string    `json:"source,omitempty"`
	Empreinte string    `json:"empreinte,omitempty"`
	Cree      time.Time `json:"cree"`
	Modifie   time.Time `json:"modifie"`
}

type fichierCoffre struct {
	Version int `json:"version"`
	KDF     struct {
		Algo string `json:"algo"`
		Iter int    `json:"iter"`
		Sel  string `json:"sel"`
	} `json:"kdf"`
	Cles struct {
		Code struct {
			Mdp     string `json:"mdp,omitempty"`
			Systeme string `json:"systeme,omitempty"`
		} `json:"code"`
		Infra struct { // never a system envelope: see the header
			Mdp string `json:"mdp,omitempty"`
		} `json:"infra"`
	} `json:"cles"`
	Entrees []EntreeCoffre `json:"entrees"`
	Maj     time.Time      `json:"maj"`
}

type Coffre struct {
	mu       sync.Mutex
	chemin   string
	systeme  enveloppeSysteme // nil: none on this system
	f        fichierCoffre
	cleCode  []byte // nil: closed
	cleInfra []byte
}

func cheminCoffre() string { return filepath.Join(dossierAthanor(), "hub", "coffre.json") }

// ouvrirCoffre reads the vault file (a missing file is an empty vault, written at the first entry);
// keys stay closed until OuvrirCode or Deverrouiller.
func ouvrirCoffre(chemin string, systeme enveloppeSysteme) (*Coffre, error) {
	c := &Coffre{chemin: chemin, systeme: systeme}
	b, err := os.ReadFile(chemin)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.f.Version = 1
		c.f.KDF.Algo, c.f.KDF.Iter, c.f.KDF.Sel = prefixeMdp, iterMdp, aleatoire(16)
		return c, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &c.f); err != nil {
		return nil, fmt.Errorf("vault file unreadable: %w", err)
	}
	if c.f.Version != 1 || c.f.KDF.Algo != prefixeMdp || c.f.KDF.Iter < 1 || c.f.KDF.Iter > 10_000_000 {
		return nil, errors.New("vault file: unknown format")
	}
	return c, nil
}

func octetsAleatoires(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// sceller: base64(nonce‖AES-256-GCM(clair)) under cle, bound to aad.
func sceller(cle, clair, aad []byte) string {
	g, err := gcm(cle)
	if err != nil {
		panic(err) // keys are always 32 bytes
	}
	nonce := octetsAleatoires(g.NonceSize())
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, clair, aad))
}

func desceller(cle []byte, s string, aad []byte) ([]byte, error) {
	g, err := gcm(cle)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) < g.NonceSize() {
		return nil, errors.New("vault: damaged value")
	}
	return g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], aad)
}

func gcm(cle []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(cle)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func aadCle(nom string) []byte        { return []byte("athanor-coffre:" + nom) }
func aadEntree(e EntreeCoffre) []byte { return []byte(e.ID + "|" + e.Famille + "|" + e.Portee) }

// kek: the key-encryption key derived from the password with the vault's own salt.
func (c *Coffre) kek(mdp string) []byte {
	sel, _ := hex.DecodeString(c.f.KDF.Sel)
	return deriverMdp(mdp, sel, c.f.KDF.Iter)
}

func (c *Coffre) aMdp() bool { return c.f.Cles.Code.Mdp != "" || c.f.Cles.Infra.Mdp != "" }

// Deverrouiller opens both keys with Athanor's password. The first call with a password creates the
// missing keys and wraps them with it (the caller has already checked the password against the config).
func (c *Coffre) Deverrouiller(mdp string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if mdp == "" {
		return errCoffreMdp
	}
	kek := c.kek(mdp)
	var code, infra []byte
	var err error
	if s := c.f.Cles.Code.Mdp; s != "" {
		if code, err = desceller(kek, s, aadCle("code")); err != nil {
			return errCoffreMdp
		}
	}
	if s := c.f.Cles.Infra.Mdp; s != "" {
		if infra, err = desceller(kek, s, aadCle("infra")); err != nil {
			return errCoffreMdp
		}
	}
	nouvelles := code == nil || infra == nil
	if code == nil { // no password envelope yet: the key opened by the keystore, else a new one
		if code = c.cleCode; code == nil {
			if c.f.Cles.Code.Systeme != "" {
				if code, err = c.ouvrirSysteme(); err != nil {
					return err
				}
			} else {
				code = octetsAleatoires(32)
			}
		}
		c.f.Cles.Code.Mdp = sceller(kek, code, aadCle("code"))
	}
	if c.f.Cles.Code.Systeme == "" && c.envelopperSysteme(code) {
		nouvelles = true
	}
	if infra == nil {
		infra = octetsAleatoires(32)
		c.f.Cles.Infra.Mdp = sceller(kek, infra, aadCle("infra"))
	}
	c.cleCode, c.cleInfra = code, infra
	if nouvelles && len(c.f.Entrees) > 0 {
		return c.enregistrer()
	}
	return nil
}

// OuvrirCode opens the code key through the system keystore (at start-up, without the password).
// No key yet and a keystore: a new code key is created for it.
func (c *Coffre) OuvrirCode() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ouvrirCode()
}

func (c *Coffre) ouvrirCode() error {
	if c.cleCode != nil {
		return nil
	}
	if c.systeme == nil {
		if c.f.Cles.Code.Mdp == "" {
			return errCoffreSansMdp
		}
		return errCoffreVerrouille
	}
	if c.f.Cles.Code.Systeme == "" {
		if c.f.Cles.Code.Mdp != "" {
			return errCoffreVerrouille // made before the keystore was there: the password reopens it
		}
		k := octetsAleatoires(32)
		if !c.envelopperSysteme(k) {
			return errCoffreSansMdp
		}
		c.cleCode = k
		return nil
	}
	k, err := c.ouvrirSysteme()
	if err != nil {
		return err
	}
	c.cleCode = k
	return nil
}

func (c *Coffre) ouvrirSysteme() ([]byte, error) {
	if c.systeme == nil {
		return nil, errCoffreVerrouille
	}
	p, err := base64.StdEncoding.DecodeString(c.f.Cles.Code.Systeme)
	if err != nil {
		return nil, errCoffreVerrouille
	}
	k, err := c.systeme.Ouvrir(p)
	if err != nil || len(k) != 32 {
		return nil, errCoffreVerrouille
	}
	return k, nil
}

// envelopperSysteme adds the keystore envelope of the code key when there is a keystore and none yet.
func (c *Coffre) envelopperSysteme(code []byte) bool {
	if c.systeme == nil {
		return false
	}
	if c.f.Cles.Code.Systeme != "" {
		return true
	}
	p, err := c.systeme.Proteger(code)
	if err != nil {
		return false
	}
	c.f.Cles.Code.Systeme = base64.StdEncoding.EncodeToString(p)
	return true
}

// Verrouiller forgets the infra key, and the code key unless the keystore can reopen it alone.
func (c *Coffre) Verrouiller() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.cleInfra)
	c.cleInfra = nil
	if c.f.Cles.Code.Systeme == "" || c.systeme == nil {
		clear(c.cleCode)
		c.cleCode = nil
	}
}

// ChangerMotDePasse rewraps the data keys only (entries untouched). nouveau "" removes the password,
// refused while an infra entry exists or when code secrets would be left without any envelope.
func (c *Coffre) ChangerMotDePasse(ancien, nouveau string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	code, infra := c.cleCode, c.cleInfra
	if c.aMdp() {
		kek := c.kek(ancien)
		var err error
		if s := c.f.Cles.Code.Mdp; s != "" {
			if code, err = desceller(kek, s, aadCle("code")); err != nil {
				return errCoffreMdp
			}
		}
		if s := c.f.Cles.Infra.Mdp; s != "" {
			if infra, err = desceller(kek, s, aadCle("infra")); err != nil {
				return errCoffreMdp
			}
		}
	}
	if nouveau == "" {
		for _, e := range c.f.Entrees {
			if e.Portee == "infra" || (e.Famille == "secret" && c.f.Cles.Code.Systeme == "") {
				return errors.New("Remove the vault entries that need the password first")
			}
		}
		c.f.Cles.Code.Mdp, c.f.Cles.Infra.Mdp = "", ""
		clear(c.cleInfra)
		c.cleCode, c.cleInfra = code, nil
		if c.f.Cles.Code.Systeme == "" {
			c.cleCode = nil
		}
		return c.enregistrer()
	}
	if code == nil && c.f.Cles.Code.Systeme != "" {
		var err error
		if code, err = c.ouvrirSysteme(); err != nil {
			return err
		}
	}
	if code == nil {
		code = octetsAleatoires(32)
	}
	if infra == nil {
		infra = octetsAleatoires(32)
	}
	c.f.KDF.Sel, c.f.KDF.Iter = aleatoire(16), max(c.f.KDF.Iter, iterMdp)
	kek := c.kek(nouveau)
	c.f.Cles.Code.Mdp = sceller(kek, code, aadCle("code"))
	c.f.Cles.Infra.Mdp = sceller(kek, infra, aadCle("infra"))
	c.envelopperSysteme(code)
	c.cleCode, c.cleInfra = code, infra
	return c.enregistrer()
}

// cle: the open data key of a scope, or why it is not available.
func (c *Coffre) cle(portee string) ([]byte, error) {
	if portee == "infra" {
		if c.cleInfra != nil {
			return c.cleInfra, nil
		}
		if c.f.Cles.Infra.Mdp == "" {
			return nil, errCoffreSansMdp
		}
		return nil, errCoffreVerrouille
	}
	if err := c.ouvrirCode(); err != nil {
		return nil, err
	}
	return c.cleCode, nil
}

// empreinte: HMAC-SHA256 under a key derived from the code key (never a bare hash of the value);
// empty while the code key is closed.
func (c *Coffre) empreinte(valeur string) string {
	if c.cleCode == nil {
		return ""
	}
	k, err := hkdf.Key(sha256.New, c.cleCode, nil, "athanor-empreinte", 32)
	if err != nil {
		return ""
	}
	m := hmac.New(sha256.New, k)
	m.Write([]byte(valeur))
	return hex.EncodeToString(m.Sum(nil))
}

func verifierEntree(e EntreeCoffre) error {
	switch {
	case strings.TrimSpace(e.Nom) == "":
		return errors.New("Give the entry a name")
	case e.Famille != "secret" && e.Famille != "variable":
		return errors.New("Unknown entry family")
	case e.Portee != "code" && e.Portee != "infra":
		return errors.New("Unknown scope")
	case e.Famille == "variable" && e.Portee == "infra":
		return errors.New("A variable cannot have the infra scope")
	case typesCoffre[e.Type] == "":
		return errors.New("Unknown entry type")
	}
	return nil
}

// poserValeur stores valeur in e (encrypted for a secret) and refreshes the fingerprint.
func (c *Coffre) poserValeur(e *EntreeCoffre, valeur string) error {
	if e.Famille == "variable" {
		e.Valeur = valeur
	} else {
		k, err := c.cle(e.Portee)
		if err != nil {
			return err
		}
		e.Valeur = sceller(k, []byte(valeur), aadEntree(*e))
	}
	e.Empreinte = c.empreinte(valeur)
	return nil
}

// Ajouter adds an entry (ID, dates and fingerprint set here) and returns it without its secret value.
func (c *Coffre) Ajouter(e EntreeCoffre, valeur string) (EntreeCoffre, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.Portee == "" {
		e.Portee = typesCoffre[e.Type]
	}
	if e.Famille == "variable" {
		e.Portee = "code"
	}
	if err := verifierEntree(e); err != nil {
		return EntreeCoffre{}, err
	}
	e.ID = aleatoire(8)
	e.Cree = time.Now().UTC()
	e.Modifie = e.Cree
	if err := c.poserValeur(&e, valeur); err != nil {
		return EntreeCoffre{}, err
	}
	c.f.Entrees = append(c.f.Entrees, e)
	if err := c.enregistrer(); err != nil {
		c.f.Entrees = c.f.Entrees[:len(c.f.Entrees)-1]
		return EntreeCoffre{}, err
	}
	return masquer(e), nil
}

func (c *Coffre) indice(id string) int {
	return slices.IndexFunc(c.f.Entrees, func(e EntreeCoffre) bool { return e.ID == id })
}

// ModifierMeta changes the name, type, note and scope; a secret changing scope is re-encrypted
// under the other key (both must be open).
func (c *Coffre) ModifierMeta(id, nom, typ, note, portee string) (EntreeCoffre, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indice(id)
	if i < 0 {
		return EntreeCoffre{}, errCoffreInconnu
	}
	ancienne := c.f.Entrees[i]
	e := ancienne
	if portee == "" {
		portee = ancienne.Portee
	}
	e.Nom, e.Type, e.Note, e.Portee = nom, typ, note, portee
	if err := verifierEntree(e); err != nil {
		return EntreeCoffre{}, err
	}
	if e.Famille == "secret" && e.Portee != ancienne.Portee {
		clair, err := c.dechiffrer(ancienne)
		if err != nil {
			return EntreeCoffre{}, err
		}
		if err := c.poserValeur(&e, clair); err != nil {
			return EntreeCoffre{}, err
		}
	}
	e.Modifie = time.Now().UTC()
	c.f.Entrees[i] = e
	if err := c.enregistrer(); err != nil {
		c.f.Entrees[i] = ancienne
		return EntreeCoffre{}, err
	}
	return masquer(e), nil
}

// RemplacerValeur sets a new value (a secret is never shown again, only replaced).
func (c *Coffre) RemplacerValeur(id, valeur string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indice(id)
	if i < 0 {
		return errCoffreInconnu
	}
	ancienne := c.f.Entrees[i]
	e := ancienne
	if err := c.poserValeur(&e, valeur); err != nil {
		return err
	}
	e.Modifie = time.Now().UTC()
	c.f.Entrees[i] = e
	if err := c.enregistrer(); err != nil {
		c.f.Entrees[i] = ancienne
		return err
	}
	return nil
}

func (c *Coffre) Supprimer(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indice(id)
	if i < 0 {
		return errCoffreInconnu
	}
	avant := slices.Clone(c.f.Entrees)
	c.f.Entrees = slices.Delete(c.f.Entrees, i, i+1)
	if err := c.enregistrer(); err != nil {
		c.f.Entrees = avant
		return err
	}
	return nil
}

func masquer(e EntreeCoffre) EntreeCoffre {
	if e.Famille == "secret" {
		e.Valeur = ""
	}
	return e
}

// Liste: every entry, secrets without their value (variables are readable even locked).
func (c *Coffre) Liste() []EntreeCoffre {
	c.mu.Lock()
	defer c.mu.Unlock()
	l := make([]EntreeCoffre, len(c.f.Entrees))
	for i, e := range c.f.Entrees {
		l[i] = masquer(e)
	}
	return l
}

// Valeur: the clear value, for Athanor's own use only (never in an API response).
func (c *Coffre) Valeur(id string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indice(id)
	if i < 0 {
		return "", errCoffreInconnu
	}
	return c.dechiffrer(c.f.Entrees[i])
}

func (c *Coffre) dechiffrer(e EntreeCoffre) (string, error) {
	if e.Famille == "variable" {
		return e.Valeur, nil
	}
	k, err := c.cle(e.Portee)
	if err != nil {
		return "", err
	}
	b, err := desceller(k, e.Valeur, aadEntree(e))
	if err != nil {
		return "", errors.New("vault: this value is damaged and cannot be read")
	}
	return string(b), nil
}

func (c *Coffre) enregistrer() error {
	c.f.Maj = time.Now().UTC()
	b, err := json.MarshalIndent(c.f, "", "  ")
	if err != nil {
		return err
	}
	return ecrireAtomique(c.chemin, b, 0o600)
}
