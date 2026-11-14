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

// Elements: an account whose last test send failed waits for the owner (the bell) until a test passes.
func (moduleEmail) Elements(*Serveur) []ElementHub {
	liste, _ := lireComptesEmail()
	var out []ElementHub
	for _, c := range liste {
		if d := c.DernierTest; d != nil && !d.OK {
			out = append(out, ElementHub{ID: c.ID, Titre: c.Nom, Etat: "erreur", Lien: "#email",
				Attente: &AttenteElement{Raison: "The test e-mail of {0} failed: {1}", Args: []any{c.Nom, d.Message}, Page: "#email", Cle: d.T}})
		}
	}
	return out
}
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
	mux.HandleFunc("POST /api/email/comptes", s.garde(false, s.apiEmailAjouter))
	mux.HandleFunc("POST /api/email/comptes/{id}", s.garde(false, s.apiEmailModifier))
	mux.HandleFunc("DELETE /api/email/comptes/{id}", s.garde(false, s.apiEmailSupprimer))
	mux.HandleFunc("POST /api/email/comptes/{id}/test", s.garde(false, s.apiEmailTester))
	mux.HandleFunc("POST /api/email/configurer", s.garde(false, s.apiEmailConfigurer))
}
