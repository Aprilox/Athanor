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
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	errCoffreVerrouille = errors.New("The vault is locked: unlock Athanor to use this secret")
	errCoffreSansMdp    = errors.New("Choose a password for Athanor first (Settings › Password)")
	errCoffreMdp        = errors.New("Wrong password")
	errCoffreInconnu    = errors.New("Unknown vault entry")
	errCoffreRetraitMdp = errors.New("The vault holds secrets that only open with this password: delete them first")
	errCoffreTrousseau  = errors.New("The system keystore returned no key")
	errCoffreFormat     = errors.New("The vault file has an unknown format")
	errCoffreAbime      = errors.New("A vault value is damaged")
	errCoffreIllisible  = errors.New("This vault value is damaged and cannot be read")
	// note 01h54: a config without password next to a vault still wrapped by one (config reset or replaced)
	errCoffreAncienMdp  = errors.New("This vault is protected by an old password: type that old password to take it back, or start from an empty vault")
	errCoffreAncienFaux = errors.New("Wrong old password of the vault")
	errCoffreFichier    = errors.New("The vault file cannot be read: it was left as it is (details in the log)")
)

// typesCoffre: the closed list of entry types, with the scope proposed for each.
var typesCoffre = map[string]string{
	"cloudflare": "infra", "ssh": "infra", "certificat": "infra", "github": "infra", "aws": "infra",
	"anthropic": "code", "openai": "code", "stripe": "code", "mot_de_passe": "code", "jeton": "code", "smtp": "code", "autre": "code",
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
	cmd := commandeContexte(ctx, nom, args...)
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
		return nil, errCoffreTrousseau
	}
	return k, nil
}

type EntreeCoffre struct {
	ID      string `json:"id"`
	Nom     string `json:"nom"`
	Famille string `json:"famille"` // secret | variable
	Type    string `json:"type"`
	Note    string `json:"note"`
	Portee  string `json:"portee"` // code | infra
	Valeur  string `json:"valeur"` // secret: base64(nonce‖AES-GCM); variable: clear
	// the values that go with a secret (an SMTP account's server, port, sender…): clear, each delivered beside it under
	// its own variable name, and bound to the sealed value (aadEntree): a field changed in the file no longer opens it
	Champs    []ChampCoffre `json:"champs,omitempty"`
	Source    string        `json:"source,omitempty"`
	Empreinte string        `json:"empreinte,omitempty"`
	// M287: id of the owner's category ("" = none); clear and outside aadEntree, so it changes without any key
	Categorie string `json:"categorie,omitempty"`
	// M225: the production value when it differs from Valeur (then the testing value), sealed under aadProd;
	// older versions ignore these fields and use Valeur everywhere
	Prod          string    `json:"prod,omitempty"`
	EmpreinteProd string    `json:"empreinte_prod,omitempty"`
	DeuxValeurs   bool      `json:"deux_valeurs,omitempty"` // set by masquer only, never on disk
	Cree          time.Time `json:"cree"`
	Modifie       time.Time `json:"modifie"`
}

// ChampCoffre: one value that goes with a secret, and the variable name a project receives it under.
type ChampCoffre struct {
	Nom    string `json:"nom"`
	Valeur string `json:"valeur"`
}

const (
	maxChampsCoffre      = 10
	maxValeurChampCoffre = 500
)

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
	// M287 (coffre_categories.go): the owner's categories, and the mark of migrerFournisseurs, run once
	Categories         []CategorieCoffre `json:"categories,omitempty"`
	FournisseursMigres bool              `json:"fournisseurs_migres,omitempty"`
	Maj                time.Time         `json:"maj"`
}

type Coffre struct {
	mu       sync.Mutex
	chemin   string
	systeme  enveloppeSysteme // nil: none on this system
	f        fichierCoffre
	cleCode  []byte // nil: closed
	cleInfra []byte
	urgence  string // emergency stop (RFC 3339): the keystore no longer reopens the code key
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
		return nil, errCoffreFormat
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
		return nil, errCoffreAbime
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

func aadCle(nom string) []byte { return []byte("athanor-coffre:" + nom) }

// aadEntree binds a sealed value to its entry, name and type included (C12: two names swapped in the
// file no longer open); aadEntreeV1 is the old binding, resealed by migrerAAD once the key is open.
func aadEntree(e EntreeCoffre) []byte {
	a := fmt.Appendf(nil, "v2|%s|%s|%s|%q|%q", e.ID, e.Famille, e.Portee, e.Nom, e.Type)
	for _, c := range e.Champs { // none: the binding of every entry made before the fields
		a = fmt.Appendf(a, "|%q=%q", c.Nom, c.Valeur)
	}
	return a
}
func aadProd(e EntreeCoffre) []byte     { return append(aadEntree(e), "|prod"...) }
func aadEntreeV1(e EntreeCoffre) []byte { return []byte(e.ID + "|" + e.Famille + "|" + e.Portee) }

// migrerAAD reseals under aadEntree the secrets still sealed under aadEntreeV1 whose key is open.
func (c *Coffre) migrerAAD() {
	change := false
	for i, e := range c.f.Entrees {
		k := c.cleCode
		if e.Portee == "infra" {
			k = c.cleInfra
		}
		if e.Famille != "secret" || k == nil {
			continue
		}
		if _, err := desceller(k, e.Valeur, aadEntree(e)); err == nil {
			continue
		}
		if b, err := desceller(k, e.Valeur, aadEntreeV1(e)); err == nil {
			c.f.Entrees[i].Valeur, change = sceller(k, b, aadEntree(e)), true
			clear(b)
		}
	}
	if change {
		_ = c.enregistrer() // a failed write keeps the resealed values in memory for the next save
	}
}

// RepartirVide moves the vault file aside (vault.json.ancien-<date>, still sealed: its old password opens it)
// and leaves an empty vault; only called after the user confirmed that its keys leave the vault.
func (c *Coffre) RepartirVide() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copie := ""
	if _, err := os.Stat(c.chemin); err == nil {
		copie = c.chemin + ".ancien-" + time.Now().Format("20060102-150405")
		if err := os.Rename(c.chemin, copie); err != nil {
			return "", err
		}
	}
	clear(c.cleCode)
	clear(c.cleInfra)
	c.cleCode, c.cleInfra = nil, nil
	c.f = fichierCoffre{Version: 1, FournisseursMigres: c.f.FournisseursMigres} // the migration stays done
	c.f.KDF.Algo, c.f.KDF.Iter, c.f.KDF.Sel = prefixeMdp, iterMdp, aleatoire(16)
	return copie, nil
}

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
	c.cleCode, c.cleInfra, c.urgence = code, infra, ""
	if nouvelles && len(c.f.Entrees) > 0 {
		if err := c.enregistrer(); err != nil {
			return err
		}
	}
	c.migrerAAD()
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
	if c.urgence != "" {
		return errCoffreVerrouille
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
	c.migrerAAD()
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

// Fermer (emergency stop) forgets both keys; only the password (Deverrouiller) or Rouvrir opens them again.
func (c *Coffre) Fermer() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.cleInfra)
	clear(c.cleCode)
	c.cleInfra, c.cleCode = nil, nil
	c.urgence = time.Now().UTC().Format(time.RFC3339)
}

// Rouvrir lets the keystore open the code key again after an emergency stop (Resume, no password set).
func (c *Coffre) Rouvrir() {
	c.mu.Lock()
	c.urgence = ""
	c.mu.Unlock()
}

// Urgence: when the emergency stop closed the vault, "" once reopened.
func (c *Coffre) Urgence() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.urgence
}

// ChangerMotDePasse rewraps the data keys only (entries untouched). nouveau "" removes the password,
// refused while an infra entry exists or when code secrets would be left without any envelope.
func (c *Coffre) ChangerMotDePasse(ancien, nouveau string) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	kdf, cles, cleCode, cleInfra, entrees := c.f.KDF, c.f.Cles, c.cleCode, c.cleInfra, slices.Clone(c.f.Entrees)
	defer func() {
		if err != nil { // file not written: the next save must not write envelopes the config does not match
			c.f.KDF, c.f.Cles, c.cleCode, c.cleInfra, c.f.Entrees = kdf, cles, cleCode, cleInfra, entrees
		}
	}()
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
				return errCoffreRetraitMdp
			}
		}
		c.f.Cles.Code.Mdp, c.f.Cles.Infra.Mdp = "", ""
		c.cleCode, c.cleInfra = code, nil
		if c.f.Cles.Code.Systeme == "" {
			c.cleCode = nil
		}
		if err := c.enregistrerSiUtile(); err != nil {
			return err
		}
		clear(cleInfra)
		return nil
	}
	if code == nil && c.f.Cles.Code.Systeme != "" {
		var err error
		if code, err = c.ouvrirSysteme(); err != nil {
			return err
		}
	}
	// C11: new data keys, every entry resealed, so an old copy of the file and the old password no longer
	// open today's secrets (the keystore envelope of the code key is redone with it).
	c.cleCode, c.cleInfra = code, infra
	clairs := make([]string, len(c.f.Entrees))
	for i, e := range c.f.Entrees {
		if clairs[i], err = c.dechiffrer(e); err != nil {
			return err
		}
	}
	c.cleCode, c.cleInfra = octetsAleatoires(32), octetsAleatoires(32)
	c.f.Entrees = slices.Clone(c.f.Entrees)
	for i := range c.f.Entrees {
		if err = c.poserValeur(&c.f.Entrees[i], clairs[i]); err != nil {
			return err
		}
	}
	c.f.KDF.Sel, c.f.KDF.Iter = aleatoire(16), max(c.f.KDF.Iter, iterMdp)
	kek := c.kek(nouveau)
	c.f.Cles.Code.Mdp = sceller(kek, c.cleCode, aadCle("code"))
	c.f.Cles.Infra.Mdp = sceller(kek, c.cleInfra, aadCle("infra"))
	c.f.Cles.Code.Systeme = ""
	c.envelopperSysteme(c.cleCode)
	return c.enregistrerSiUtile()
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

// Empreinte: the fingerprint of a value under the code key ("" while it is closed).
func (c *Coffre) Empreinte(valeur string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.empreinte(valeur)
}

// erreurEntree: a refusal of the input (a fixed i18n key, never the input itself).
type erreurEntree string

func (e erreurEntree) Error() string { return string(e) }

func verifierEntree(e EntreeCoffre) error {
	switch {
	case strings.TrimSpace(e.Nom) == "":
		return erreurEntree("Give the entry a name")
	case e.Famille != "secret" && e.Famille != "variable":
		return erreurEntree("Unknown entry family")
	case e.Portee != "code" && e.Portee != "infra":
		return erreurEntree("Unknown scope")
	case e.Famille == "variable" && e.Portee == "infra":
		return erreurEntree("A variable cannot have the infra scope")
	case typesCoffre[e.Type] == "":
		return erreurEntree("Unknown entry type")
	case len(e.Champs) > 0 && e.Famille != "secret":
		return erreurEntree("Only a secret has fields")
	case len(e.Champs) > maxChampsCoffre:
		return erreurEntree("Too many fields (10 at most)")
	}
	vus := map[string]bool{}
	for _, c := range e.Champs {
		switch {
		case !nomSecretValide(c.Nom) || vus[c.Nom]:
			return erreurEntree("Invalid field name: capital letters, digits and _, starting with a letter, each name once")
		case utf8.RuneCountInString(c.Valeur) > maxValeurChampCoffre || strings.ContainsAny(c.Valeur, "\r\n\x00"):
			return erreurEntree("A field holds one line of 500 characters at most")
		}
		vus[c.Nom] = true
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
	e.Empreinte = ""
	if e.Portee != "infra" { // the HMAC key comes from the code key: no offline guess at an infra value with it
		e.Empreinte = c.empreinte(valeur)
	}
	return nil
}

// poserProd stores the production value of e like poserValeur ("" removes it: one value for every environment).
func (c *Coffre) poserProd(e *EntreeCoffre, valeur string) error {
	e.Prod, e.EmpreinteProd = "", ""
	if valeur == "" {
		return nil
	}
	if e.Famille == "variable" {
		e.Prod = valeur
	} else {
		k, err := c.cle(e.Portee)
		if err != nil {
			return err
		}
		e.Prod = sceller(k, []byte(valeur), aadProd(*e))
	}
	if e.Portee != "infra" {
		e.EmpreinteProd = c.empreinte(valeur)
	}
	return nil
}

// EmpreintesValeurs: the fingerprints of every value of the vault, production values included.
func (c *Coffre) EmpreintesValeurs() map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := map[string]bool{}
	for _, e := range c.f.Entrees {
		for _, f := range []string{e.Empreinte, e.EmpreinteProd} {
			if f != "" {
				m[f] = true
			}
		}
	}
	return m
}

// EmpreintesConnues: id → fingerprint of the entries, for the import only (Liste never sends them).
func (c *Coffre) EmpreintesConnues() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := map[string]string{}
	for _, e := range c.f.Entrees {
		if e.Empreinte != "" {
			m[e.ID] = e.Empreinte
		}
	}
	return m
}

// porteeEntree: the entry's scope, or its default (a variable: code; a secret: its type's).
func porteeEntree(e EntreeCoffre) string {
	switch {
	case e.Portee != "":
		return e.Portee
	case e.Famille == "variable":
		return "code"
	}
	return typesCoffre[e.Type]
}

// Ajouter adds an entry (ID, dates and fingerprint set here) and returns it without its secret value.
func (c *Coffre) Ajouter(e EntreeCoffre, valeur string) (EntreeCoffre, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e.Portee, e.Champs = porteeEntree(e), slices.Clone(e.Champs) // never the caller's array: the fields are bound to the value
	if err := verifierEntree(e); err != nil {
		return EntreeCoffre{}, err
	}
	if e.Categorie != "" && c.indiceCategorie(e.Categorie) < 0 {
		return EntreeCoffre{}, errCategorieInconnue
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

// ModifierMeta changes the name, type, note and scope, the fields when champs is not nil, and the value when
// valeur is not nil, all or nothing (C16); a secret whose binding changes (scope, name, type, fields) is sealed
// again, under the other key for a new scope (both must be open).
func (c *Coffre) ModifierMeta(id, nom, typ, note, portee string, champs *[]ChampCoffre, valeur *string) (EntreeCoffre, error) {
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
	if champs != nil {
		e.Champs = slices.Clone(*champs)
	}
	lie := !slices.Equal(aadEntree(e), aadEntree(ancienne)) // the sealed values follow their new binding
	if err := verifierEntree(e); err != nil {
		return EntreeCoffre{}, err
	}
	if valeur != nil {
		if err := c.poserValeur(&e, *valeur); err != nil {
			return EntreeCoffre{}, err
		}
	} else if e.Famille == "secret" && lie {
		clair, err := c.dechiffrer(ancienne)
		if err != nil {
			return EntreeCoffre{}, err
		}
		if err := c.poserValeur(&e, clair); err != nil {
			return EntreeCoffre{}, err
		}
	}
	if e.Famille == "secret" && ancienne.Prod != "" && lie {
		clair, err := c.dechiffrerProd(ancienne)
		if err != nil {
			return EntreeCoffre{}, err
		}
		if err := c.poserProd(&e, clair); err != nil {
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

// PoserValeurProd sets the production value of an entry ("" removes it: Valeur serves every environment).
func (c *Coffre) PoserValeurProd(id, valeur string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indice(id)
	if i < 0 {
		return errCoffreInconnu
	}
	ancienne := c.f.Entrees[i]
	e := ancienne
	if err := c.poserProd(&e, valeur); err != nil {
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
	e.Empreinte, e.EmpreinteProd, e.DeuxValeurs, e.Champs = "", "", e.Prod != "", slices.Clone(e.Champs)
	if e.Famille == "secret" {
		e.Valeur, e.Prod = "", ""
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

// ValeurPour: the clear value given to the environment env (the production value for prod when there is one).
func (c *Coffre) ValeurPour(id, env string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indice(id)
	if i < 0 {
		return "", errCoffreInconnu
	}
	if e := c.f.Entrees[i]; env == "prod" && e.Prod != "" {
		return c.dechiffrerProd(e)
	}
	return c.dechiffrer(c.f.Entrees[i])
}

// ValeurProd: the clear production value ("" when the entry has one value only), for Athanor's own use.
func (c *Coffre) ValeurProd(id string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.indice(id)
	if i < 0 {
		return "", errCoffreInconnu
	}
	if c.f.Entrees[i].Prod == "" {
		return "", nil
	}
	return c.dechiffrerProd(c.f.Entrees[i])
}

func (c *Coffre) dechiffrerProd(e EntreeCoffre) (string, error) {
	if e.Famille == "variable" {
		return e.Prod, nil
	}
	k, err := c.cle(e.Portee)
	if err != nil {
		return "", err
	}
	b, err := desceller(k, e.Prod, aadProd(e))
	if err != nil {
		return "", errCoffreIllisible
	}
	return string(b), nil
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
		return "", errCoffreIllisible
	}
	return string(b), nil
}

// enregistrerSiUtile writes the file only once it exists or holds an entry (no empty vault on disk).
func (c *Coffre) enregistrerSiUtile() error {
	if _, err := os.Stat(c.chemin); len(c.f.Entrees) == 0 && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return c.enregistrer()
}

func (c *Coffre) enregistrer() error {
	c.f.Maj = time.Now().UTC()
	b, err := json.MarshalIndent(c.f, "", "  ")
	if err != nil {
		return err
	}
	if err := ecrireAtomique(c.chemin, b, 0o600); err != nil {
		return err
	}
	_ = os.Remove(c.chemin + ".ancien") // C11: the Windows fallback of ecrireAtomique keeps the old file aside
	return nil
}
