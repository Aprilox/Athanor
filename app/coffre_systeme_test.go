package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// oublierCompte removes a Keychain / Secret Service item made by a test (DPAPI stores nothing).
func oublierCompte(compte string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("security", "delete-generic-password", "-s", serviceCoffre, "-a", compte).Run()
	case "windows":
	default:
		exec.Command("secret-tool", "clear", "service", serviceCoffre, "compte", compte).Run()
	}
}

// The real keystore of this system: DPAPI on Windows, skipped elsewhere when the tool is missing or locked.
func TestCoffreSystemeReel(t *testing.T) {
	if runtime.GOOS != "windows" && os.Getenv("ATHANOR_TEST_TROUSSEAU_REEL") != "1" {
		t.Skip("real Keychain / Secret Service skipped: it may ask the owner (ATHANOR_TEST_TROUSSEAU_REEL=1 to run it)")
	}
	s := enveloppeSystemeLocale()
	if s == nil {
		if runtime.GOOS == "windows" {
			t.Fatal("no DPAPI envelope on Windows")
		}
		t.Skip("no system keystore tool here: the code key is wrapped by the password only")
	}
	cle := octetsAleatoires(32)
	p, err := s.Proteger(cle)
	if err != nil {
		if runtime.GOOS == "windows" {
			t.Fatal(err)
		}
		t.Skipf("system keystore unavailable: %v", err)
	}
	if runtime.GOOS != "windows" {
		defer oublierCompte(string(p))
	}
	if bytes.Contains(p, cle) || bytes.Contains(p, []byte(base64.StdEncoding.EncodeToString(cle))) {
		t.Fatal("the protected form holds the key in clear")
	}
	k, err := s.Ouvrir(p)
	if err != nil || !bytes.Equal(k, cle) {
		t.Fatalf("round trip: %v", err)
	}
	altere := append([]byte(nil), p...)
	altere[len(altere)/2] ^= 0x55
	if k, err := s.Ouvrir(altere); err == nil && bytes.Equal(k, cle) {
		t.Fatal("altered envelope opened")
	}
	if _, err := s.Ouvrir(nil); err == nil {
		t.Fatal("empty envelope opened")
	}

	// Through the vault: a code secret is readable after a restart without the password.
	c, chemin := coffreTest(t, s)
	e := ajouter(t, c, "OpenAI", "secret", "openai", "", "témoin-systeme")
	if runtime.GOOS != "windows" {
		ref, _ := base64.StdEncoding.DecodeString(c.f.Cles.Code.Systeme)
		defer oublierCompte(string(ref))
	}
	c = rouvrir(t, chemin, s)
	if err := c.OuvrirCode(); err != nil {
		t.Fatal(err)
	}
	valeurAttendue(t, c, e.ID, "témoin-systeme")
}

// espion records every key handed to the system keystore.
type espion struct {
	*fauxSysteme
	vus [][]byte
}

func (s *espion) Proteger(d []byte) ([]byte, error) {
	s.vus = append(s.vus, append([]byte(nil), d...))
	return s.fauxSysteme.Proteger(d)
}

func TestCoffreInfraJamaisSysteme(t *testing.T) {
	sys := &espion{fauxSysteme: nouveauFauxSysteme()}
	c, chemin := coffreTest(t, sys)
	ajouter(t, c, "OpenAI", "secret", "openai", "", "v1")
	if err := c.ChangerMotDePasse("", "témoin-mdp"); err != nil {
		t.Fatal(err)
	}
	ajouter(t, c, "SSH", "secret", "ssh", "", "v2")
	if err := c.ChangerMotDePasse("témoin-mdp", "témoin-mdp-2"); err != nil {
		t.Fatal(err)
	}
	c.Verrouiller()
	c = rouvrir(t, chemin, sys)
	if err := c.OuvrirCode(); err != nil {
		t.Fatal(err)
	}
	if err := c.Deverrouiller("témoin-mdp-2"); err != nil {
		t.Fatal(err)
	}
	ajouter(t, c, "Cloudflare", "secret", "cloudflare", "", "v3")
	if c.cleInfra == nil || len(sys.vus) == 0 {
		t.Fatalf("scenario did not run: infra %v, envelopes %d", c.cleInfra != nil, len(sys.vus))
	}
	for _, k := range sys.vus {
		if bytes.Equal(k, c.cleInfra) || !bytes.Equal(k, c.cleCode) {
			t.Fatal("the system keystore received another key than the code key")
		}
	}
}
