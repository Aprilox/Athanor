package main

// The e-mail module: SMTP accounts (their password in the vault), a test send and the sending settings of
// the deployed apps. It lives in its own files only (email_*.go, static/email.js): the proof that a module
// joins the hub without touching the others (.athanor/tools/diff_modules.sh).

import (
	"fmt"
	"net/http"
	"strings"
)

func init() {
	enregistrerModule(moduleEmail{})
	// its API: refused from a device by default, the account list (names, no value) readable
	prefixesHub = append(prefixesHub, "/api/email")
	autorisesDistance["GET /api/email/comptes"] = true
}

type moduleEmail struct{}

func (moduleEmail) ID() string  { return "email" }
func (moduleEmail) Nom() string { return "E-mail" }
func (moduleEmail) Pages() []PageModule {
	return []PageModule{{Hash: "#email", Ordre: 35, Icone: "email", Libelle: "E-mail"}}
}

// Elements: an account whose last test send failed waits for the owner (the bell) until a test passes.
func (moduleEmail) Elements(*Serveur) []ElementHub {
	liste, _ := lireComptesEmail()
	var out []ElementHub
	for _, c := range liste {
		if d := c.DernierTest; d != nil && !d.OK {
			out = append(out, ElementHub{ID: c.ID, Titre: c.Nom, Etat: "erreur", Lien: "#email",
				Attente: &AttenteElement{Raison: "The test e-mail of {0} failed: {1}", Args: []any{c.Nom, &MessageMoteur{Cle: d.Message}}, Page: "#email", Cle: d.T}})
		}
	}
	return out
}

// Resume: the tile: how many accounts, how many projects send through them, how the test sends went.
func (moduleEmail) Resume(s *Serveur) ResumeModule {
	liste, _ := lireComptesEmail()
	projets, testes, echecs := map[string]bool{}, 0, 0
	for _, c := range liste {
		if s != nil {
			for _, p := range s.acces.projetsEmail(c.Entree) {
				projets[p.Projet] = true
			}
		}
		if c.DernierTest != nil {
			testes++
			if !c.DernierTest.OK {
				echecs++
			}
		}
	}
	test := "Not tested"
	switch {
	case echecs > 0:
		test = "Failed"
	case testes > 0:
		test = "Passed"
	}
	r := ResumeModule{Stats: []StatModule{
		{Cle: "Email accounts", Valeur: len(liste), Page: "#email"},
		{Cle: "Projects sending e-mail", Valeur: len(projets), Page: "#email"},
		{Cle: "Test send", Valeur: test, Page: "#email"},
	}, Besoin: echecs, Etat: "inactif"}
	switch {
	case echecs > 0:
		r.Etat = "attention"
	case len(liste) > 0:
		r.Etat = "ok"
	}
	return r
}

// Tutoriel: the page's 4 steps, shown once at the first visit of #email (a missing button: the bubble in the middle).
func (moduleEmail) Tutoriel() []EtapeTutoriel {
	return []EtapeTutoriel{
		{Titre: "Your e-mail accounts", Texte: "Add the SMTP account of your provider (Gmail, Outlook, OVH, Brevo…): its server, its user name and its password. The password goes straight into the vault and is never shown again.", Cible: "[data-em-ajouter]"},
		{Titre: "Test the sending", Texte: "Send a test e-mail to check the account. A failure is said in plain words (password refused, wrong port…) and waits in the bell until a test passes.", Cible: "[data-em-tester]"},
		{Titre: "Your apps send e-mails", Texte: "Configure sending gives a project SMTP_HOST, SMTP_PORT, SMTP_SECURE, SMTP_USER, SMTP_FROM and SMTP_PASSWORD: your deployed app reads them to send its e-mails. Athanor asks for its password first, if it has one.", Cible: "[data-em-configurer]"},
		{Titre: "Stop at any time", Texte: "The × next to a project stops its sending; deleting the account takes its settings out of every project. This tutorial stays in the Guide, with the topic “Send e-mails from your apps”.", Cible: ".email-explication"},
	}
}

// ContexteChat: the accounts for the home conversation: their names and where they send, never a server's
// password nor its user name.
func (moduleEmail) ContexteChat(s *Serveur) string {
	liste, _ := lireComptesEmail()
	var b strings.Builder
	for _, c := range liste {
		var ou []string
		for _, p := range s.acces.projetsEmail(c.Entree) {
			ou = append(ou, p.Projet+" ("+p.Env+")")
		}
		fmt.Fprintf(&b, "- [email] e-mail account %s: ", c.Nom)
		if len(ou) == 0 {
			b.WriteString("sends for no project")
		} else {
			b.WriteString("sends for " + strings.Join(ou, ", "))
		}
		if d := c.DernierTest; d != nil && !d.OK {
			b.WriteString(", its last test failed")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (moduleEmail) Routes(mux *http.ServeMux, s *Serveur) {
	mux.HandleFunc("GET /api/email/comptes", s.garde(false, s.apiEmailComptes))
	mux.HandleFunc("POST /api/email/comptes", s.garde(false, s.apiEmailAjouter))
	mux.HandleFunc("POST /api/email/comptes/{id}", s.garde(false, s.apiEmailModifier))
	mux.HandleFunc("DELETE /api/email/comptes/{id}", s.garde(false, s.apiEmailSupprimer))
	mux.HandleFunc("POST /api/email/comptes/{id}/test", s.garde(false, s.apiEmailTester))
	mux.HandleFunc("POST /api/email/configurer", s.garde(false, s.apiEmailConfigurer))
}
