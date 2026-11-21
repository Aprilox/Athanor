package main

import (
	"strings"
	"testing"
)

// TestBesoinProprietaire: the server agent works on like the dev pilot (another way when a step fails) and says
// what only the owner can do on one OWNER NEEDED line, which goes to their For you page.
func TestBesoinProprietaire(t *testing.T) {
	for rep, ok := range map[string]string{
		"Fait : le site répond.": "",
		"Le site est prêt.\nOWNER NEEDED: Add an A record for demo.example.fr at your registrar.": "Add an A record for demo.example.fr at your registrar.",
		"**OWNER NEEDED:** pay the plan.\n":                       "pay the plan.",
		"- owner needed: give the Cloudflare token of example.fr": "give the Cloudflare token of example.fr",
	} {
		if b := besoinProprietaire(rep); b != ok {
			t.Errorf("%q: %q, want %q", rep, b, ok)
		}
	}
	for _, c := range []string{consigneChefOps("x", []string{"vps"}), consigneApplication("p1", false)} {
		if !strings.Contains(c, "OWNER NEEDED:") {
			t.Errorf("an ops instruction without OWNER NEEDED: %q", c)
		}
	}
	if c := consigneApplication("p1", false); !strings.Contains(c, "call proposer_plan again with a corrected plan") {
		t.Errorf("a failed step ends the task instead of another way: %q", c)
	}
}

// TestCriteresVerifies: the verifier answers criterion by criterion; each one is read back with whether it holds.
func TestCriteresVerifies(t *testing.T) {
	l := criteresVerifies([]string{"[ok] nginx listens on 5050: ss shows it", "[echec] https://demo.example.fr answers 200: 502", "the backup is old", "[OK] the cron runs"})
	if len(l) != 3 || !l[0].OK || l[1].OK || !l[2].OK || l[1].Texte != "https://demo.example.fr answers 200: 502" {
		t.Errorf("criteria: %+v", l)
	}
	if c := consigneVerificateur("x", nil); !strings.Contains(c, "criteres") || !strings.Contains(c, "[ok]") {
		t.Errorf("the verifier does not check the criteria: %q", c)
	}
	if c := consigneChefOps("x", []string{"vps"}); !strings.Contains(c, "criteres") {
		t.Errorf("the ops lead does not write criteria: %q", c)
	}
	if c := consigneCorrectionServeurs(VerificationTache{Lignes: []string{"[echec] <b>x</b>"}}); !strings.Contains(c, "&lt;b>") || !strings.Contains(c, "OWNER NEEDED:") {
		t.Errorf("the correction: %q", c)
	}
}
