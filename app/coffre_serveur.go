package main

import (
	"errors"
	"fmt"
	"os"
)

// systemeCoffre gives the keystore envelope of this computer (replaced by a fake one in the tests).
var systemeCoffre = enveloppeSystemeLocale

// chargerCoffre loads the vault at start-up and reopens its code key through the keystore when it has
// an envelope there; nothing is written (the file only appears with the first entry).
func (s *Serveur) chargerCoffre() {
	s.coffre, s.coffreErr = ouvrirCoffre(cheminCoffre(), systemeCoffre())
	if s.coffreErr != nil {
		fmt.Fprintln(os.Stderr, "vault:", s.coffreErr)
		return
	}
	purgerTemoinsCoffre(s.coffre) // a Test the vault cut short (the app closed during it)
	s.coffre.mu.Lock()
	aEnveloppe := s.coffre.f.Cles.Code.Systeme != ""
	s.coffre.mu.Unlock()
	if aEnveloppe {
		_ = s.coffre.OuvrirCode() // closed keystore: the password reopens it
	}
	if _, err := s.migrerSecrets(true); err != nil && !errors.Is(err, errCoffreVerrouille) && !errors.Is(err, errCoffreSansMdp) {
		fmt.Fprintln(os.Stderr, "vault import:", err)
	}
}

// deverrouillerCoffre opens the infra key once Athanor's password has been checked.
func (s *Serveur) deverrouillerCoffre(mdp string) {
	if s.coffre == nil {
		return
	}
	if err := s.coffre.Deverrouiller(mdp); err != nil {
		fmt.Fprintln(os.Stderr, "vault:", err)
		return
	}
	if _, err := s.migrerSecrets(true); err != nil {
		fmt.Fprintln(os.Stderr, "vault import:", err)
	}
	s.injecterTous(nil) // files left as they were while the code key was closed
	s.signalerInfraOuverte()
}

// fermerCoffreSiPersonne closes the infra key when neither the computer's window nor any remote
// device is unlocked any more; a window locked by its timer still holds it (note 19h03 §3). Called with s.mu
// and the remote state's lock held.
func (s *Serveur) fermerCoffreSiPersonne(d *distance) {
	if s.coffre == nil || s.session != "" || s.verrouAuto {
		return
	}
	for _, ok := range d.deverrouilles {
		if ok {
			return
		}
	}
	s.coffre.Verrouiller()
}

// finSessionsCoffre: same as fermerCoffreSiPersonne for callers holding no lock.
func (s *Serveur) finSessionsCoffre() {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	s.fermerCoffreSiPersonne(d)
}

// changerMdpCoffre rewraps the vault's keys for the new password (empty = removed) before the
// config changes, so both never disagree. Returns the HTTP status and message of a refusal.
func (s *Serveur) changerMdpCoffre(ancien, nouveau string) (int, error) {
	if s.coffreErr != nil {
		fmt.Fprintln(os.Stderr, "vault:", s.coffreErr)
		return 500, errCoffreFichier // never rewrite a vault file that could not be read
	}
	if s.coffre == nil {
		return 0, nil
	}
	err := s.coffre.ChangerMotDePasse(ancien, nouveau)
	switch {
	case err == nil:
		return 0, nil
	case errors.Is(err, errCoffreRetraitMdp):
		return 409, err
	case errors.Is(err, errCoffreMdp) && ancien == "":
		return 409, errCoffreAncienMdp
	case errors.Is(err, errCoffreMdp):
		return 401, errCoffreAncienFaux
	}
	return 500, err
}

// coffreAncienMdp: the vault's keys are wrapped by a password (with a config holding none, an old one).
func (s *Serveur) coffreAncienMdp() bool {
	if s.coffre == nil {
		return false
	}
	s.coffre.mu.Lock()
	defer s.coffre.mu.Unlock()
	return s.coffre.aMdp()
}
