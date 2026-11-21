package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Note 01h54: a vault sealed with password A next to a config without password (reset or replaced during
// tests). Before, setting password B sent "" as the old password to the vault, which answered a bare
// "Wrong password". Now: the cause is said, the old password takes the vault back, or an empty vault after
// an explicit choice (the old file kept aside, still sealed); nothing is deleted without that choice.
func TestCoffreAncienMotDePasse(t *testing.T) {
	s := serveurCoffreTest(t)
	const temoin = "temoin-ancien-mdp-7731"
	var corps strings.Builder
	appel := func(json string) (int, string) {
		w := appelCoffre(t, s.apiMotDePasse, "", json, nil)
		corps.WriteString(w.Body.String())
		return w.Code, w.Body.String()
	}
	oublierConfig := func() { // what the owner saw: the config lost its password, the vault kept it
		s.mu.Lock()
		s.config.MotDePasseHash, s.config.Sel, s.session = "", "", ""
		s.mu.Unlock()
		s.coffre.Verrouiller()
	}

	// 1. a vault without password: the first password is simply set
	if s.coffreAncienMdp() {
		t.Fatal("a new vault reported as sealed by an old password")
	}
	if code, b := appel(`{"nouveau":"alpha-1-phrase"}`); code != 200 {
		t.Fatal("first password:", code, b)
	}
	n, err := s.coffre.Ajouter(EntreeCoffre{Nom: "SSH_PROD", Famille: "secret", Type: "ssh"}, temoin)
	if err != nil || n.Portee != "infra" {
		t.Fatal("infra entry:", err, n.Portee)
	}

	// 2. sealed vault, config without password, the old password known
	oublierConfig()
	if !s.coffreAncienMdp() {
		t.Fatal("the old password is not reported")
	}
	if code, b := appel(`{"nouveau":"beta-2-phrase-x"}`); code != 409 || !strings.Contains(b, "protected by an old password") {
		t.Fatal("no old password given:", code, b)
	}
	if code, b := appel(`{"ancien":"pas-le-bon-mdp","nouveau":"beta-2-phrase-x"}`); code != 401 || !strings.Contains(b, "Wrong old password of the vault") {
		t.Fatal("wrong old password:", code, b)
	}
	if code, b := appel(`{"ancien":"alpha-1-phrase","nouveau":"court"}`); code != 400 || !strings.Contains(b, "At least 12 characters") {
		t.Fatal("short password:", code, b)
	}
	if code, b := appel(`{"vider_coffre":true,"nouveau":""}`); code != 400 {
		t.Fatal("empty vault without a new password:", code, b)
	}
	if s.config.MotDePasseHash != "" || len(s.coffre.Liste()) != 1 {
		t.Fatal("a refusal changed the config or the vault")
	}
	if code, b := appel(`{"ancien":"alpha-1-phrase","nouveau":"beta-2-phrase-x"}`); code != 200 {
		t.Fatal("old password given:", code, b)
	}
	if err := lisible(s.coffre, n.ID, temoin); err != nil || s.config.MotDePasseHash == "" {
		t.Fatal("vault not taken back:", err)
	}

	// 3. the old password forgotten: an empty vault only on the explicit choice, the old file kept aside
	oublierConfig()
	if code, b := appel(`{"ancien":"oublie-depuis","nouveau":"gamma-3-phrase"}`); code != 401 || len(s.coffre.Liste()) != 1 {
		t.Fatal("vault touched without the choice:", code, b)
	}
	if code, b := appel(`{"vider_coffre":true,"nouveau":"gamma-3-phrase"}`); code != 200 {
		t.Fatal("empty vault:", code, b)
	}
	if len(s.coffre.Liste()) != 0 || s.coffreAncienMdp() == false || s.config.MotDePasseHash == "" {
		t.Fatal("vault not emptied, or not sealed by the new password")
	}
	copies, _ := filepath.Glob(s.coffre.chemin + ".ancien-*")
	if len(copies) != 1 {
		t.Fatal("old vault file not kept aside:", copies)
	}
	t.Cleanup(func() { _ = os.Remove(copies[0]) })
	if b, _ := os.ReadFile(copies[0]); len(b) == 0 || strings.Contains(string(b), temoin) {
		t.Fatal("old vault file empty or in clear")
	}
	if code, b := appel(`{"ancien":"gamma-3-phrase","nouveau":"delta-4-phrase"}`); code != 200 {
		t.Fatal("the new password does not work:", code, b)
	}

	if strings.Contains(corps.String(), temoin) || strings.Contains(journalAuditTexte(t), temoin) {
		t.Fatal("the value leaked into a response or the audit log")
	}
	if !strings.Contains(journalAuditTexte(t), "coffre vidé") {
		t.Fatal("the empty vault is not in the audit log")
	}
}
