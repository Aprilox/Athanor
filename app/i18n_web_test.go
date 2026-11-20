package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// clesJournalMission: the words the page of an old mission gives t() through its table (journal parameters). The
// journal's own keys were written by the missions' code (gone, M302): their translations stay in i18n.js for the
// journals already on disk.
func clesJournalMission(t *testing.T) []string {
	var cles []string
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
	guide := clesGuide(t, js)
	for _, cle := range guide {
		if !vu[cle] {
			vu[cle] = true
			cles = append(cles, cle)
		}
	}
	if len(guide) < 80 || !vu["Home"] || !vu["Start a project"] {
		t.Fatalf("keys not found: %d texts in the GUIDE, %d keys", len(guide), len(cles))
	}
	return cles
}

// clesGuide: every text of the GUIDE of app.js (ids and "#page" links left out).
func clesGuide(t *testing.T, js string) []string {
	i := strings.Index(js, "\nconst GUIDE = [")
	j := strings.Index(js[i+1:], "\n];")
	if i < 0 || j < 0 {
		t.Fatal("no GUIDE in app.js")
	}
	var cles []string
	for _, m := range regexp.MustCompile(`(id: )?"((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(js[i:i+1+j], -1) {
		if cle := cleJS(m[2]); m[1] == "" && !strings.HasPrefix(cle, "#") {
			cles = append(cles, cle)
		}
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
		errEnvNom.Error(), errEnvRelais.Error(), errAccesEnv.Error(), // the project's deployment (M112)
		"Unknown site, or this server is not one of its servers")
	cles = append(cles, errFournisseursFichier.Error(), errFournisseurCle.Error(), errFournisseurPortee.Error(),
		"This Cloudflare token has expired: create a new one and replace it in the Vault.",
		"This Cloudflare token is disabled: enable it again on Cloudflare or replace it.",
		"This Cloudflare token sees no zone: give it access to your domain's zone.") // the providers (M114)
	cles = append(cles, errCleDeploiementNonLie.Error(), errCleDeploiementGH) // the deployment key (M119)
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

// TestVocabulaireInterface (M306): a project has one deployment, with no environment to name and no provider to
// add. No key of i18n.js and no translation says those words any more; each exception is listed under its reason
// and must still be in the dictionary. Missions came back in 3.1 (a project, a server and a key linked): the word is
// allowed again.
func TestVocabulaireInterface(t *testing.T) {
	en := regexp.MustCompile(`(?i)environment|\btesting\b|\bin test\b|pre-?production|\bstaging\b|provider`)
	traduit := map[string]*regexp.Regexp{
		"fr": regexp.MustCompile(`(?i)environnement|pr[ée]-?production|fournisseur|\ben essai\b|\bessai et\b`),
		"es": regexp.MustCompile(`(?i)entorno|preproducci|proveedor`),
		"de": regexp.MustCompile(`(?i)umgebung|vorproduktion|anbieter|\bstaging\b`),
		"it": regexp.MustCompile(`(?i)\bambient[ei]\b|pre-?produzion|fornitor`),
		"pt": regexp.MustCompile(`(?i)\bambientes?\b|pr[ée]-?produ|fornecedor|provedor`),
	}
	// the same words with another meaning: a sandbox in Spanish, a desktop in Portuguese
	autreSens := regexp.MustCompile(`(?i)entorno aislado|ambientes? de trabalho`)
	permis := map[string]bool{}
	for _, cles := range [][]string{
		// the old missions of Athanor 2 stay readable and deletable: their page, their deletion, the card of an older conversation
		{"Old missions", "No old mission left.", "Delete the mission", "Delete this mission?", "Mission deleted.",
			"Athanor no longer creates nor follows missions: these ones stay here to be read or deleted. Their projects keep working on their own, each with its own deployment.",
			"The mission, its journal and its follow-up files are deleted.", "Another mission uses this project",
			"This project existed before the mission: remove it from Studio if you want to", "No local project is linked to this mission",
			"Prepare a mission",
			"This card of an older conversation is no longer launched: ask again, and the assistant fills the New mission form."},
		// the journal of an old mission, written by code that is gone and still read on its page (keys and PREFIXES of i18n.js)
		{"Mission paused: {0}", "Mission resumed: {0}", "Mission stopped: {0}. Nothing was undone", "Mission done: {0}",
			"The environment {0} stays: {1}", "The environment {0} kept its policy: {1}", "The environment {0} now deploys: {1}",
			"Athanor restarted: the mission waits for the vault to be unlocked", "Athanor restarted: the mission resumes",
			"The vault is unlocked: the mission resumes", "The milestone in progress is done: the mission is paused",
			"Stop asked by {0}: the mission pauses once the milestone in progress is done", "The remote Athanor {0} is offline: the mission waits for it",
			"The remote Athanor did not take the mission for {0}: {1}", "no environment on the card: the Studio lane finished the request"},
		// the e-mail provider (Gmail, OVH, Brevo…) of the E-mail page: the word its users know, nothing to add in Athanor
		{"Provider", "The provider fills in the server, port and security: you can still change them.",
			"Add an SMTP account (your provider's sending server): Athanor keeps its password in the vault, tests the sending and gives your deployed apps what they need to send emails.",
			"Add the SMTP account of your provider (Gmail, Outlook, OVH, Brevo…): its server, its user name and its password. The password goes straight into the vault and is never shown again.",
			// the same word in the Guide's topic about e-mail
			"Your deployed apps send e-mails (sign-up, password reset, orders) through the SMTP account of your e-mail provider. Add it once on the *E-mail* page: Athanor keeps its password in the vault, tests it and gives each project the settings it needs.",
			"At your provider, create an *app password* (Gmail, Outlook and Yahoo ask for one when two-step verification is on) or an SMTP key (Brevo, Mailgun, SendGrid…). Then *E-mail › Add an email account*: choose the provider (it fills in the server, the port and the security), give the user name, the sender and the password. The password goes straight into the vault (type *E-mail account*) and is never shown again."},
		// "testing" as what Athanor is doing or as a word to search skills with, never a place to deploy to
		{"Testing…", "testing Claude's fix ({0}/3)…", "testing it as it is…", "e.g. front-end, testing, react, documentation",
			"Nothing found: try another word, in English (e.g. testing, react, docs).",
			"A repository or a folder holding one or several skills (a SKILL.md each), e.g. https://github.com/anthropics/skills/tree/main/skills/webapp-testing"},
	} {
		for _, c := range cles {
			permis[c] = true
			if _, ok := dicoTUI("fr")[c]; !ok {
				t.Errorf("exception no longer in i18n.js, remove it: %q", c)
			}
		}
	}
	for l, re := range traduit {
		for cle, v := range dicoTUI(l) {
			if permis[cle] {
				continue
			}
			if l == "fr" && en.MatchString(cle) {
				t.Errorf("key %q", cle)
			}
			if m := re.FindString(autreSens.ReplaceAllString(v, "")); m != "" {
				t.Errorf("%s: %q in the translation of %q", l, m, cle)
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
