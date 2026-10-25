package main

import (
	"bytes"
	"testing"
)

// TestLireJSONFinal: what a command prints last is read whole, even when its message holds braces
// ("Connected: Athanor can push to {0}, branch {1}." showed as "{1}.","args":[…]} in the app).
func TestLireJSONFinal(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("Welcome to WSL\nsome {noise} first\n")
	reponseTest(&b, true, "Connected: Athanor can push to {0}, branch {1}.", "Aprilox/APOGEE", "main")
	var r struct {
		Ok     bool     `json:"ok"`
		Modele string   `json:"modele"`
		Args   []string `json:"args"`
	}
	if err := lireJSONFinal(b.String(), &r); err != nil || !r.Ok || r.Modele != "Connected: Athanor can push to {0}, branch {1}." || len(r.Args) != 2 || r.Args[0] != "Aprilox/APOGEE" {
		t.Fatalf("%v %+v", err, r)
	}
	var e EtatRecuperer
	if err := lireJSONFinal(`{"resultat":"verifie","derriere":2,"sujet":"M4: the {boss} fight"}`+"\n", &e); err != nil || e.Sujet != "M4: the {boss} fight" || e.Derriere != 2 {
		t.Fatalf("%v %+v", err, e)
	}
}
