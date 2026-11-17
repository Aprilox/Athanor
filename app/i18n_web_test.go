package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// clesJournalMission: the keys of a mission's journal (entreeJournal(id, t, niveau, "key", …) in mission_*.go) and
// the words the live page gives t() through its tables (relay steps, journal parameters).
func clesJournalMission(t *testing.T) []string {
	fichiers, _ := filepath.Glob("mission_*.go")
	re := regexp.MustCompile(`"(?:info|attention|erreur)",\s*("(?:[^"\\]|\\.)*")`)
	var cles []string
	for _, f := range fichiers {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if c, err := strconv.Unquote(m[1]); err == nil {
				cles = append(cles, c)
			}
		}
	}
	if len(cles) < 25 {
		t.Fatalf("%d journal keys in mission_*.go", len(cles))
	}
	b, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"MOTS_JOURNAL"} {
		i := strings.Index(string(b), "\nconst "+table+" = ")
		j := strings.Index(string(b)[i+1:], ";\n")
		if i < 0 || j < 0 {
			t.Fatalf("no %s in app.js", table)
		}
		for _, m := range regexp.MustCompile(`"((?:[^"\\]|\\.)*)"(?:\s*[,\]}])`).FindAllStringSubmatch(string(b)[i:i+1+j], -1) {
			if m[1] != "" {
				cles = append(cles, cleJS(m[1]))
			}
		}
	}
	return append(cles, "prod checked: ", "no prod on the card: dev checked on ")
}

// clesWeb: the keys the web interface translates: every t("…") of app.js (traduireNav's labels for
// index.html included) and every text of the GUIDE (ids and "#page" links left out).
func clesWeb(t *testing.T) []string {
	b, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	email, err := os.ReadFile("static/email.js") // the e-mail module page (M201)
	if err != nil {
		t.Fatal(err)
	}
	js := strings.ReplaceAll(string(b)+"\n"+string(email), "\r\n", "\n")
	// clesTUI also gives the terminal's own few keys (its pages, its form help): translated all the same
	cles := clesTUI(js)
	vu := map[string]bool{}
	for _, c := range cles {
		vu[c] = true
	}
	i := strings.Index(js, "\nconst GUIDE = [")
	j := strings.Index(js[i+1:], "\n];")
	if i < 0 || j < 0 {
		t.Fatal("no GUIDE in app.js")
	}
	guide := js[i : i+1+j]
	n := 0
	for _, m := range regexp.MustCompile(`(id: )?"((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(guide, -1) {
		cle := cleJS(m[2])
		if m[1] != "" || strings.HasPrefix(cle, "#") {
			continue
		}
		n++
		if !vu[cle] {
			vu[cle] = true
			cles = append(cles, cle)
		}
	}
	if n < 80 || !vu["Home"] || !vu["Start a project"] {
		t.Fatalf("keys not found: %d texts in the GUIDE, %d keys", n, len(cles))
	}
	return cles
}

// cleJS: a string literal's content as the text it stands for, like i18n.js's keys read as JSON.
func cleJS(s string) string {
	if v, err := strconv.Unquote(`"` + s + `"`); err == nil {
		return v
	}
	return strings.NewReplacer(`\"`, `"`, `\n`, "\n", `\\`, `\`).Replace(s)
}

func TestWebTraduction(t *testing.T) {
	cles := clesWeb(t)
	for _, e := range []error{errCoffreVerrouille, errCoffreSansMdp, errCoffreMdp, errCoffreInconnu, errCoffreRetraitMdp,
		errCoffreTrousseau, errCoffreFormat, errCoffreAbime, errCoffreIllisible, errServeurAdresse, errServeurPort,
		errServeurUtilisateur, errServeurEmpreinte, errServeurMode, errServeurPasCle, errServeurPhrase, errServeurInjoignable,
		errServeurCleRefusee, errEmpreinteChangee, errServeurCleCode, errServeurMdpRefuse, errServeurCleImport, errServeurPhraseVide,
		errServeurPhraseFaux, errServeurSansCle, errCoupureMotDePasse} { // the vault's errors reach the toast (B14)
		cles = append(cles, e.Error())
	}
	cles = append(cles, errTacheArretee.Error(), errTacheRevoquee.Error(), prefixeVerifEchouee) // a task's end (B64)
	cles = append(cles, "This key opens the server {0}: it stays in the infra scope", "This key opens the server {0}: remove that server first")
	cles = append(cles, errEnvURL.Error(), errEnvURLIP.Error(), errEnvURLIDN.Error(), errEnvURLPort.Error(), errEnvProtect.Error(), errEnvPolitique.Error(), errEnvProdAuto.Error(), errEnvFourn.Error(), errEnvAuthHTTP.Error(),
		errEnvNom.Error(), errEnvRelais.Error(), errAccesEnv.Error()) // the Environments panel (M112)
	cles = append(cles, errFournisseursFichier.Error(), errFournisseurCle.Error(), errFournisseurPortee.Error(),
		"This Cloudflare token has expired: create a new one and replace it in the Vault.",
		"This Cloudflare token is disabled: enable it again on Cloudflare or replace it.",
		"This Cloudflare token sees no zone: give it access to your domain's zone.") // the providers (M114)
	cles = append(cles, errCleDeploiementNonLie.Error(), errCleDeploiementGH) // the deployment key (M119)
	cles = append(cles, "The mission waits for the vault to be unlocked")     // the bell after a restart (M170)
	cles = append(cles, errDossierAbsolu.Error(), errDossierPoints.Error(), errDossierAbsent.Error(), errDossierLien.Error(), errDossierSecret.Error(),
		errDossierAthanor.Error(), errDossierSysteme.Error(), errDossiersTrop.Error(), errPieceAccueilTropGrosse.Error(),
		"This type of file is not accepted: images, PDF and text files only.") // the home conversation's folders and files (M196)
	cles = append(cles, errSFTPAbsent, errSFTPExiste, errSFTPInconnu, errSFTPRefuse, errSFTPDossier, errSFTPChemin, errSFTPMemeNom, errSFTPTropGros, errSFTPLien) // the files panel (M231c)
	cles = append(cles, clesMoteur(t)...)                                                                                                                         // the engine's messages (B79)
	cles = append(cles, clesJournalMission(t)...)                                                                                                                 // a mission's journal (M171)
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		if len(dico) < 1000 {
			t.Fatalf("%s: %d keys", l, len(dico))
		}
		for _, cle := range cles {
			if _, ok := dico[cle]; !ok {
				t.Errorf("%s: no translation of %q", l, cle)
			}
		}
	}
}

// TestInterfaceSansEmoji: labels, buttons and empty states carry no emoji (owner's note of 2026-11-14 19h22);
// typographic signs (✓ ✕ ⚠ ☰ …) stay.
func TestInterfaceSansEmoji(t *testing.T) {
	re := regexp.MustCompile("[\U0001F300-\U0001FAFF⚙⛔✨⏱⏳⏭]")
	for _, f := range []string{"static/app.js", "static/index.html", "static/i18n.js"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(string(b), "\n") {
			if m := re.FindAllString(l, 5); m != nil {
				t.Errorf("%s:%d: emoji %q", f, i+1, m)
			}
		}
	}
}
