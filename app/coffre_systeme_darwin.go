package main

import (
	"errors"
	"fmt"
	"os/exec"
)

// trousseauMac keeps the code key in the login Keychain through the `security` tool.
type trousseauMac struct{}

// enveloppeSystemeLocale returns the keystore of this system, or nil when there is none.
func enveloppeSystemeLocale() enveloppeSysteme {
	if _, err := exec.LookPath("security"); err != nil {
		return nil
	}
	return trousseauMac{}
}

func (trousseauMac) Proteger(donnees []byte) ([]byte, error) {
	compte := nouveauCompteCoffre()
	// `security -i` reads the command on stdin: the key never appears in the process list.
	ligne := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %x\n", serviceCoffre, compte, donnees)
	if _, err := outilCoffre(ligne, "security", "-i"); err != nil {
		return nil, err
	}
	// Interactive mode exits 0 even when the command failed: read it back.
	if k, err := (trousseauMac{}).Ouvrir([]byte(compte)); err != nil || string(k) != string(donnees) {
		return nil, errors.New("the Keychain did not keep the vault key")
	}
	return []byte(compte), nil
}

func (trousseauMac) Ouvrir(protege []byte) ([]byte, error) {
	compte := string(protege)
	if !reCompteCoffre.MatchString(compte) {
		return nil, errors.New("bad Keychain reference")
	}
	out, err := outilCoffre("", "security", "find-generic-password", "-s", serviceCoffre, "-a", compte, "-w")
	if err != nil {
		return nil, err
	}
	return cleDuCompte(out)
}
