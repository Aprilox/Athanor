package main

// The e-mail module: SMTP accounts (their password in the vault), a test send and the sending settings of
// the deployed apps. It lives in its own files only (email_*.go, static/email.js): the proof that a module
// joins the hub without touching the others (.athanor/tools/diff_modules.sh).

import "net/http"

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
func (moduleEmail) Elements(*Serveur) []ElementHub { return nil }
func (moduleEmail) Resume(*Serveur) ResumeModule {
	liste, _ := lireComptesEmail()
	r := ResumeModule{Stats: []StatModule{{Cle: "Email accounts", Valeur: len(liste), Page: "#email"}}, Etat: "inactif"}
	if len(liste) > 0 {
		r.Etat = "ok"
	}
	return r
}
func (moduleEmail) Tutoriel() []EtapeTutoriel { return nil }

func (moduleEmail) Routes(mux *http.ServeMux, s *Serveur) {
	mux.HandleFunc("GET /api/email/comptes", s.garde(false, s.apiEmailComptes))
}

func (s *Serveur) apiEmailComptes(w http.ResponseWriter, r *http.Request) {
	liste, err := lireComptesEmail()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, "The email accounts cannot be read")
		return
	}
	s.ecrireJSON(w, map[string]any{"comptes": liste})
}
