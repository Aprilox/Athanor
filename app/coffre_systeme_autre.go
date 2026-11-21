//go:build !windows && !darwin

package main

import (
	"encoding/hex"
	"errors"
	"os/exec"
)

// secretService keeps the code key in the Secret Service (GNOME Keyring, KWallet) through `secret-tool`.
type secretService struct{}

// enveloppeSystemeLocale returns the keystore of this system, or nil when there is none.
func enveloppeSystemeLocale() enveloppeSysteme {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil
	}
	return secretService{}
}

func (secretService) Proteger(donnees []byte) ([]byte, error) {
	compte := nouveauCompteCoffre()
	// secret-tool reads the secret on stdin: the key never appears in the process list.
	if _, err := outilCoffre(hex.EncodeToString(donnees), "secret-tool", "store", "--label=Athanor vault", "service", serviceCoffre, "compte", compte); err != nil {
		return nil, err
	}
	if k, err := (secretService{}).Ouvrir([]byte(compte)); err != nil || string(k) != string(donnees) {
		return nil, errors.New("the Secret Service did not keep the vault key")
	}
	return []byte(compte), nil
}

func (secretService) Ouvrir(protege []byte) ([]byte, error) {
	compte := string(protege)
	if !reCompteCoffre.MatchString(compte) {
		return nil, errors.New("bad Secret Service reference")
	}
	out, err := outilCoffre("", "secret-tool", "lookup", "service", serviceCoffre, "compte", compte)
	if err != nil {
		return nil, err
	}
	return cleDuCompte(out)
}
