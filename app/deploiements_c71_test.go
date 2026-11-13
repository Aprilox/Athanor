package main

import "testing"

// TestRetourAutomatique (C76, C71): only a version served then found failing goes back by itself, in dev and
// staging under the auto policy, never in prod; Go back is not offered after a failure before the switch.
func TestRetourAutomatique(t *testing.T) {
	rate := &VerifDeploiement{OK: false}
	servie := Deploiement{Env: "dev", Etat: "echec", Bascule: true, Verification: rate}
	auto, accord := Environnement{Politique: "auto"}, Environnement{Politique: "accord"}
	for nom, c := range map[string]struct {
		e    Environnement
		d    Deploiement
		veut bool
	}{
		"dev auto":            {auto, servie, true},
		"staging auto":        {auto, Deploiement{Env: "staging", Etat: "echec", Bascule: true, Verification: rate}, true},
		"prod auto":           {auto, Deploiement{Env: "prod", Etat: "echec", Bascule: true, Verification: rate}, false},
		"dev accord":          {accord, servie, false},
		"before the switch":   {auto, Deploiement{Env: "dev", Etat: "echec", Verification: rate}, false},
		"reload failed":       {auto, Deploiement{Env: "dev", Etat: "echec", Bascule: true}, false},
		"check passed":        {auto, Deploiement{Env: "dev", Etat: "ok", Bascule: true, Verification: &VerifDeploiement{OK: true}}, false},
		"way back that fails": {auto, Deploiement{Env: "dev", Etat: "echec", Retour: true, Verification: rate}, false},
	} {
		if got := retourAutomatique(c.e, c.d); got != c.veut {
			t.Errorf("%s: retourAutomatique = %v", nom, got)
		}
	}
	for _, c := range []struct {
		d    Deploiement
		veut bool
	}{
		{Deploiement{Etat: "echec"}, true},
		{Deploiement{Etat: "echec", Bascule: true}, false},
		{Deploiement{Etat: "echec", Verification: rate}, false},
		{Deploiement{Etat: "echec", Retour: true}, false},
		{Deploiement{Etat: "ok"}, false},
	} {
		if got := avantBascule(c.d); got != c.veut {
			t.Errorf("avantBascule(%+v) = %v", c.d, got)
		}
	}
}
