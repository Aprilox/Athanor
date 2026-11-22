package main

// The terminal interface: the keys, and what they do (the same steps and questions as the app's buttons).

import (
	"strings"
	"time"
)

func (a *appTUI) allerPage(p int) {
	a.page = (p + nbPages) % nbPages
	a.derniere["distance"], a.derniere["hub"] = time.Time{}, time.Time{}
}

// touche: a key (or a paste: nothing to paste into outside a form).
func (a *appTUI) touche(ev evenementTUI) {
	k := ev.touche()
	if k == "ctrl+c" && a.form == nil && a.dialogue == nil {
		a.demanderQuitter()
		return
	}
	switch {
	case a.dialogue != nil: // over a form too (Discard what was typed?)
		a.toucheDialogue(ev)
		return
	case a.form != nil:
		a.toucheFormulaire(ev)
		return
	case a.aide:
		a.aide = false
		return
	case ev.genre == evCollage:
		return
	}
	if l := a.lettrePage(k); l != "" {
		a.touchePage(ev, l)
		return
	}
	switch k {
	case "q", "f10":
		a.demanderQuitter()
	case "?", "f1", "h":
		a.aide = true
	case "1", "H", "echap":
		a.allerPage(pAccueil)
	case "2":
		a.allerPage(pDistance)
	case "tab", "stab", "maj+tab":
		a.allerPage(a.page + 1)
	case "w":
		a.ouvrirFenetre()
	case "!":
		a.arretUrgenceTUI()
	default:
		a.touchePage(ev, k)
	}
}

// lettrePage: the letters Remote gives a meaning of its own (o, v, r, d; m, c, t: the address), small or capital.
func (a *appTUI) lettrePage(k string) string {
	if len([]rune(k)) != 1 || a.page != pDistance || a.dist == nil {
		return ""
	}
	if l := strings.ToLower(k); strings.Contains("ovrdmct", l) {
		return l
	}
	return ""
}

// touchePage: the keys of the page shown.
func (a *appTUI) touchePage(ev evenementTUI, k string) {
	if a.page != pDistance {
		return
	}
	a.toucheDistance(k, func(d *int, sens int) {
		switch k {
		case "haut", "k":
			*d += sens
		case "bas", "j":
			*d -= sens
		case "pgprec":
			*d += 10 * sens
		case "pgsuiv":
			*d -= 10 * sens
		case "debut":
			*d = 1 << 30 * sens
		case "fin":
			*d = -(1 << 30) * sens
		}
	})
}

// ----------------------------------------------------------------------------- dialogs and forms

func (a *appTUI) demander(titre, texte string, choix []choixTUI, fn func(id string)) {
	sel := 0
	a.dialogue = &dialogueTUI{titre: titre, texte: texte, choix: choix, sel: sel, fn: fn}
}

func (a *appTUI) choisirDialogue() {
	d := a.dialogue
	a.dialogue = nil
	if d != nil && d.fn != nil && d.sel >= 0 && d.sel < len(d.choix) {
		d.fn(d.choix[d.sel].id)
	}
}

func (a *appTUI) toucheDialogue(ev evenementTUI) {
	d := a.dialogue
	switch ev.touche() {
	case "echap", "ctrl+c":
		a.dialogue = nil
		if d.fn != nil {
			d.fn("")
		}
	case "haut", "gauche", "stab", "k":
		d.sel = (d.sel - 1 + len(d.choix)) % len(d.choix)
	case "bas", "droite", "tab", "j":
		d.sel = (d.sel + 1) % len(d.choix)
	case "entree", " ":
		a.choisirDialogue()
	default:
		// the first letter of a choice, or its number
		if ev.nom == "" && ev.r >= '1' && ev.r <= '9' && int(ev.r-'1') < len(d.choix) {
			d.sel = int(ev.r - '1')
			a.choisirDialogue()
		}
	}
}

// toucheFormulaire: the form's keys; Tab goes through its fields, then its two buttons (Save, Cancel). A
// terminal may keep Ctrl+S for itself (VS Code's saves the file): F2 and Alt+S save as well.
func (a *appTUI) toucheFormulaire(ev evenementTUI) {
	f := a.form
	nb := len(f.champs) + 2
	k := ev.touche()
	switch k {
	case "echap", "ctrl+c":
		a.abandonnerFormulaire()
		return
	case "tab":
		f.sel = (f.sel + 1) % nb
		return
	case "stab", "maj+tab":
		f.sel = (f.sel - 1 + nb) % nb
		return
	case "ctrl+s", "f2", "alt+s":
		a.validerFormulaire()
		return
	}
	// on a button
	if f.sel >= len(f.champs) {
		switch k {
		case "entree", " ":
			if f.sel == len(f.champs) {
				a.validerFormulaire()
			} else {
				a.abandonnerFormulaire()
			}
		case "gauche", "droite":
			f.sel = len(f.champs) + (f.sel-len(f.champs)+1)%2
		case "haut":
			f.sel = len(f.champs) - 1
		}
		return
	}
	c := f.champs[f.sel]
	switch k {
	case "entree":
		if c.h > 1 {
			c.s.inserer("\n")
		} else if f.sel < len(f.champs)-1 {
			f.sel++
		} else {
			a.validerFormulaire()
		}
	case "alt+entree", "ctrl+j":
		if c.h > 1 {
			c.s.inserer("\n")
		} else {
			a.validerFormulaire()
		}
	default:
		c.s.touche(ev)
	}
}

// abandonnerFormulaire: Escape or Cancel; what was typed is never thrown away without asking.
func (a *appTUI) abandonnerFormulaire() {
	f := a.form
	if f == nil {
		return
	}
	rempli := false // something typed: a field changed since the form opened (a setting's value is there from the start)
	for i, c := range f.champs {
		if c.s.masque && strings.TrimSpace(c.s.texte()) == "" {
			continue
		}
		if i >= len(f.debut) || strings.TrimSpace(c.s.texte()) != strings.TrimSpace(f.debut[i]) {
			rempli = true
		}
	}
	if !rempli {
		a.form = nil
		return
	}
	a.demander(a.t("Discard what you typed?"), a.t("The text of this form is lost if you close it now."),
		[]choixTUI{{id: "", texte: a.t("Keep editing")}, {id: "jeter", texte: a.t("Discard"), danger: true}}, func(id string) {
			if id == "jeter" {
				a.form = nil
			}
		})
}

func (a *appTUI) validerFormulaire() {
	f := a.form
	a.form = nil
	f.erreur = ""
	a.formValide = f
	vals := make([]string, len(f.champs))
	for i, c := range f.champs {
		vals[i] = c.s.texte()
	}
	f.fn(vals)
}

// formulaire: fields (label, lines; a field of 1 line is one line), and what to do with what is typed.
func (a *appTUI) formulaire(titre, aide string, champs []champTUI, fn func([]string)) {
	for i := range champs {
		if champs[i].s == nil {
			champs[i].s = &saisieT{}
		}
		champs[i].s.multi = champs[i].h > 1
	}
	debut := make([]string, len(champs))
	for i := range champs {
		debut[i] = champs[i].s.texte()
	}
	a.form = &formulaireTUI{titre: titre, aide: aide, champs: champs, fn: fn, debut: debut}
}

const aideFormulaireLigne = "Tab: next field or button · Enter: save · Esc: cancel"
const aideDeverrouiller = "Tab: next field or button · Enter: unlock · Esc: cancel"

// ----------------------------------------------------------------------------- the pilot

// ----------------------------------------------------------------------------- projects

// ----------------------------------------------------------------------------- requests

// ----------------------------------------------------------------------------- chat

// ----------------------------------------------------------------------------- setup & health

// ----------------------------------------------------------------------------- the rest

// demanderMotDePasse: the application is locked (Settings › Lock): its password first.
func (a *appTUI) demanderMotDePasse() {
	if a.form != nil || a.verrouDemande {
		return
	}
	// only while the password is sent: Escape closes the form and the next refresh asks again
	a.formulaire(a.t("Athanor is locked"), a.t(aideDeverrouiller), []champTUI{{lib: a.t("Password"), h: 1, s: &saisieT{masque: true}}}, func(v []string) {
		a.verrouDemande = true
		c := *a.c
		go func() {
			var r struct {
				ChangerMdp bool `json:"changer_mdp"`
			}
			err := c.appel("POST", "deverrouiller", map[string]string{"mot_de_passe": v[0]}, &r)
			a.majs <- func(a *appTUI) {
				a.verrouDemande = false
				if err != nil {
					a.annoncer(a.texteErreur(err), true)
				} else if r.ChangerMdp { // a 2.x password: the window asks for a new one as soon as it opens
					a.annoncer(a.t("Your password is shorter than 12 characters: open Athanor's window to choose a new one."), true)
				}
				a.derniere["etat"] = time.Time{}
			}
		}()
	})
	a.form.bouton = a.t("Unlock")
}

// ouvrirFenetre: Athanor's window as well (a computer with a screen).
func (a *appTUI) ouvrirFenetre() { a.agir("POST", "fenetre", nil, nil) }

func (a *appTUI) demanderQuitter() {
	if a.proprio { // Athanor could not go to the background: it stops with this terminal
		a.demander(a.t("Quit"), a.t("The pilots keep running. Athanor itself stops with this terminal: start it again (here or with its window) to follow them."),
			[]choixTUI{{id: "oui", texte: a.t("Quit")}, {id: "", texte: a.t("Cancel")}}, func(id string) {
				if id == "oui" {
					a.quitter = true
				}
			})
		return
	}
	// Athanor runs on without this terminal (closing it, or the SSH session, leaves it running too)
	a.demander(a.t("Quit"), a.t("Leave Athanor running in the background? The pilots, missions and remote access go on, and ./athanor opens this screen again. Stopped completely, remote access closes and nothing watches your projects until you start it again (the pilots keep running)."),
		[]choixTUI{{id: "fond", texte: a.t("Leave it running")}, {id: "arret", texte: a.t("Stop Athanor completely"), danger: true}, {id: "", texte: a.t("Cancel")}}, func(id string) {
			switch id {
			case "fond":
				a.quitter = true
			case "arret":
				a.agir("POST", "quitter", nil, func(map[string]any) { a.quitter = true })
			}
		})
}

// arretUrgenceTUI: ! on every page: the web's emergency stop, after the same confirmation.
func (a *appTUI) arretUrgenceTUI() {
	t := a.t
	a.demander(t("Emergency stop"), t("Emergency stop? Every pilot stops now (the current cycle is cut), every hub session is revoked, the pending approvals are refused, the “never ask again” rules are removed and the vault is locked. To resume: unlock Athanor, then Start each project."),
		[]choixTUI{{id: "oui", texte: t("Emergency stop"), danger: true}, {id: "", texte: t("Cancel")}}, func(id string) {
			if id == "oui" {
				a.agir("POST", "hub/urgence", map[string]any{}, func(map[string]any) { a.derniere["approbations"] = time.Time{} })
			}
		})
}
