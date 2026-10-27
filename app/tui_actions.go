package main

// The terminal interface: the keys, and what they do (the same steps and questions as the app's buttons).

import (
	"errors"
	"strings"
	"time"
)

func (a *appTUI) allerPage(p int) {
	a.page = (p + nbPages) % nbPages
	// the chat: its field ready at once (what is typed goes into the message, never to a key of the pilot);
	// Escape leaves it for the page's keys
	a.chatActif = a.page == pChat
	a.rechActive = false
	a.derniere["demandes"], a.derniere["chat"], a.derniere["sante"] = time.Time{}, time.Time{}, time.Time{}
}

// touche: a key (or a paste).
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
	}
	// a field being typed in takes the keys first
	if a.page == pChat && a.chatActif {
		switch k {
		case "echap":
			a.chatActif = false
		case "entree":
			a.envoyerChat()
		case "alt+entree", "ctrl+j":
			a.saisie.inserer("\n")
		case "pgprec":
			a.defilC += 10
		case "pgsuiv":
			a.defilC -= 10
		case "tab":
			a.allerPage(a.page + 1)
		case "stab", "maj+tab":
			a.allerPage(a.page - 1)
		default:
			if ev.nom != "" && strings.HasPrefix(ev.nom, "f") && len(ev.nom) > 1 && ev.nom != "fin" {
				break // F1…F12: nothing in the field
			}
			a.saisie.touche(ev)
		}
		return
	}
	if a.page == pJournal && a.rechActive {
		switch k {
		case "echap":
			a.rechActive = false
			a.recherche.mettreTexte("")
		case "entree", "tab":
			a.rechActive = false
		default:
			a.recherche.touche(ev)
		}
		return
	}
	if ev.genre == evCollage {
		if a.page == pChat {
			a.chatActif = true
			a.saisie.inserer(ev.texte)
		}
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
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		a.allerPage(int(k[0] - '1'))
	case "0":
		a.allerPage(9)
	case "tab":
		a.allerPage(a.page + 1)
	case "stab", "maj+tab":
		a.allerPage(a.page - 1)
	case "p":
		a.choisirProjet()
	case "]", "[":
		l := a.projetsVisibles()
		for i, pv := range l {
			if pv.Nom == a.c.projet {
				d := 1
				if k == "[" {
					d = -1
				}
				a.changerProjet(l[(i+d+len(l))%len(l)].Nom)
				break
			}
		}
	case "N":
		a.nouveauProjet()
	case "s":
		a.demarrer()
	case "a":
		a.arreterApres()
	case "x":
		a.arreterMaintenant()
	case "g":
		a.recupererGitHub()
	case "w":
		a.ouvrirFenetre()
	case "M":
		a.gererProjet()
	default:
		a.touchePage(ev, k)
	}
}

// lettresPages: the letters a page gives a meaning of its own; there they come before the keys of every
// page (p is the project chooser elsewhere, the palette in Settings; a is Stop after cycle elsewhere, the
// automatic push on GitHub), typed small or capital.
var lettresPages = map[int]string{pReglages: "lpa", pGitHub: "cdlrptaug", pDistance: "ovrd"}

func (a *appTUI) lettrePage(k string) string {
	if len([]rune(k)) != 1 {
		return ""
	}
	if a.page == pGitHub && (a.gh == nil || a.vue == nil || a.vue.Projet == nil) || a.page == pDistance && a.dist == nil {
		return "" // nothing to act on yet: the keys of every page
	}
	if l := strings.ToLower(k); strings.Contains(lettresPages[a.page], l) {
		return l
	}
	return ""
}

// touchePage: the keys of the page shown.
func (a *appTUI) touchePage(ev evenementTUI, k string) {
	defiler := func(d *int, sens int) {
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
	}
	switch a.page {
	case pSuivi:
		defiler(&a.defilPlan, -1)
	case pJournal:
		switch k {
		case "o":
			a.outils = !a.outils
		case "r":
			a.resultats = !a.resultats
		case "u":
			a.sousAgents = !a.sousAgents
		case "/":
			a.rechActive = true
		case "f", "G":
			a.defilJ = 0
		case "echap":
			a.recherche.mettreTexte("")
		default:
			defiler(&a.defilJ, 1)
		}
	case pDemandes:
		switch k {
		case "n":
			a.nouvelleDemande()
		case "e":
			a.remarque()
		case "d", "suppr":
			a.supprimerDemande()
		case "alt+haut", "K":
			a.deplacerDemande(-1)
		case "alt+bas", "J":
			a.deplacerDemande(1)
		default:
			defiler(&a.selDem, -1)
			if a.dem != nil {
				a.selDem = min(max(0, a.selDem), max(0, len(a.dem.Attente)-1))
			}
		}
	case pChat:
		switch k {
		case "i", "entree":
			a.chatActif = true
		case "c":
			a.nouvelleConversation()
		case "m":
			var choix []choixTUI
			for _, m := range []string{"", "opus", "sonnet", "haiku", "fable"} {
				lib := m
				if m == "" {
					lib = a.t("account default")
				}
				if m == a.modeleChat {
					lib = "● " + lib
				}
				choix = append(choix, choixTUI{id: "=" + m, texte: lib})
			}
			a.demander(a.t("Model"), "", choix, func(id string) {
				if strings.HasPrefix(id, "=") {
					a.modeleChat = id[1:]
				}
			})
		default:
			// (typing does not start a message by itself: s, a, x… are the pilot's keys)
			defiler(&a.defilC, 1)
		}
	case pPourVous, pEquipe, pGitHub, pReglages:
		a.touchePages2(ev, k, defiler)
	case pDistance:
		a.toucheDistance(k, defiler)
	case pSante:
		switch k {
		case "v":
			a.agir("POST", "sante/verifier", map[string]string{"id": "tout"}, func(map[string]any) { a.derniere["sante"] = time.Time{} })
		case "entree":
			if v := a.verifChoisie(); v != nil {
				a.agir("POST", "sante/verifier", map[string]string{"id": v.ID}, func(map[string]any) { a.derniere["sante"] = time.Time{} })
			}
		case "r":
			a.reparer()
		case "I":
			a.installer()
		case "T":
			a.demanderJeton()
		default:
			defiler(&a.selV, -1)
		}
	}
}

func (a *appTUI) verifChoisie() *VerifVue {
	if a.selV >= 0 && a.selV < len(a.verifs) {
		return &a.verifs[a.selV]
	}
	return nil
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

const aideFormulaireMulti = "Tab: next field or button · Enter: new line · F2 (or Ctrl+S): save · Esc: cancel"
const aideFormulaireLigne = "Tab: next field or button · Enter: save · Esc: cancel"

// ----------------------------------------------------------------------------- the pilot

func (a *appTUI) demarrer() {
	v := a.vue
	if v == nil || v.Projet == nil {
		a.nouveauProjet()
		return
	}
	if v.Actif {
		a.annoncer(a.t("The pilot is already running."), false)
		return
	}
	if time.Now().Before(a.demarrage) {
		a.annoncer(a.t("Starting…"), false)
		return
	}
	// Several computers: the request comes with the others' work, the pilot brings it in
	if v.DemandeVide && (v.GitHub == nil || !v.GitHub.Equipe) {
		a.demander(a.t("Start"), a.t("The project has no request yet: Claude would wait for one. Write what you want first."),
			[]choixTUI{{id: "demande", texte: a.t("Write the request")}, {id: "", texte: a.t("Cancel")}}, func(id string) {
				if id == "demande" {
					a.allerPage(pDemandes)
					a.nouvelleDemande()
				}
			})
		return
	}
	suite := func() { a.verifierGitHubPuisDemarrer() }
	if !v.KitInstalle {
		a.demander(a.t("Before starting"), a.t("Athanor has not seen {0} installed yet. Did you install it?", a.t("the tooling (Claude Code and the system sandbox)")),
			[]choixTUI{{id: "demarrer", texte: a.t("Start anyway")}, {id: "installer", texte: a.t("Install it now")}, {id: "sante", texte: a.t("Open the check")}, {id: "", texte: a.t("Cancel")}},
			func(id string) {
				switch id {
				case "demarrer":
					suite()
				case "installer":
					a.allerPage(pSante)
					a.agir("POST", "sante/installer", map[string]string{"mode": ""}, nil)
				case "sante":
					a.allerPage(pSante)
				}
			})
		return
	}
	suite()
}

// verifierGitHubPuisDemarrer: a project behind its repository asks first (someone pushed meanwhile).
func (a *appTUI) verifierGitHubPuisDemarrer() {
	a.annoncer(a.t("Checking GitHub…"), false)
	a.demarrage = time.Now().Add(25 * time.Second)
	c := *a.c
	go func() {
		var r EtatRecuperer
		err := c.appel("POST", "github", map[string]string{"action": "verifier"}, &r)
		a.majs <- func(a *appTUI) {
			a.toast = ""
			if err != nil || r.Equipe || r.Resultat != "verifie" || r.Derriere <= 0 {
				a.lancerPilote()
				return
			}
			quand := r.Quand
			if d, ok := lireDate(r.Quand); ok {
				quand = d.Format("2006-01-02 " + a.formatHeure())
			}
			texte := a.t("GitHub has {0} commit(s) this folder does not have: someone pushed since your last work (the last one: “{1}”, by {2}, {3}). Bring them in before starting?", r.Derriere, firstNonEmpty(r.Sujet, "?"), firstNonEmpty(r.Auteur, "?"), quand)
			if r.Devant > 0 {
				texte += " " + a.t("This folder also has {0} commit(s) of its own: both are merged.", r.Devant)
			}
			texte += " " + a.t("Starting from here instead: the pushes are refused until GitHub's work is brought in.")
			a.demander(a.t("Not up to date with GitHub"), texte, []choixTUI{{id: "recuperer", texte: a.t("Bring them in, then start")}, {id: "ici", texte: a.t("Start from here")}, {id: "", texte: a.t("Cancel")}},
				func(id string) {
					switch id {
					case "ici":
						a.lancerPilote()
					case "recuperer":
						a.recupererPuis(func() { a.lancerPilote() })
					default:
						a.demarrage = time.Time{}
					}
				})
		}
	}()
}

func (a *appTUI) lancerPilote() {
	a.demarrage = time.Now().Add(25 * time.Second)
	a.agirAvec("POST", "pilote/demarrer", nil, nil, func(err error) {
		a.demarrage = time.Time{}
		a.annoncer(a.texteErreur(err), true)
	})
}

func (a *appTUI) arreterApres() {
	v := a.vue
	if v == nil || v.Projet == nil || !v.Actif {
		return
	}
	if v.ArretDemande != "" {
		a.demander(a.t("Let it go on"), a.t("The pilot will stop at the end of this cycle. Cancel this stop and let it go on?"),
			[]choixTUI{{id: "annuler", texte: a.t("Let it go on")}, {id: "", texte: a.t("Keep the stop")}}, func(id string) {
				if id == "annuler" {
					a.agir("POST", "pilote/annuler-arret", nil, nil)
				}
			})
		return
	}
	a.agir("POST", "pilote/arreter-apres-cycle", nil, nil)
}

func (a *appTUI) arreterMaintenant() {
	v := a.vue
	if v == nil || v.Projet == nil || !v.Actif {
		return
	}
	a.demander(a.t("Stop the pilot now?"), a.t("The current cycle is cut off: what Claude has not saved yet is lost, and will be done again at the next start.\n\nTo lose nothing, choose “Stop after cycle”: Claude finishes the current milestone, saves it, then the pilot stops by itself."),
		[]choixTUI{{id: "apres", texte: a.t("Stop after cycle")}, {id: "maintenant", texte: a.t("Stop now"), danger: true}, {id: "", texte: a.t("Cancel")}},
		func(id string) {
			switch id {
			case "apres":
				a.arreterApres()
			case "maintenant":
				a.agir("POST", "pilote/arreter", nil, nil)
			}
		})
}

// recupererGitHub: "Get GitHub's work" (working in turns).
func (a *appTUI) recupererGitHub() {
	if a.vue == nil || a.vue.Projet == nil {
		return
	}
	a.demander(a.t("Get GitHub's work"), a.t("Brings into this project the work GitHub has (the other's last push), as git pull would."),
		[]choixTUI{{id: "oui", texte: a.t("Get GitHub's work")}, {id: "", texte: a.t("Cancel")}}, func(id string) {
			if id == "oui" {
				a.recupererPuis(nil)
			}
		})
}

func (a *appTUI) recupererPuis(ensuite func()) {
	a.annoncer(a.t("Getting GitHub's work…"), false)
	c := *a.c
	go func() {
		var r EtatRecuperer
		err := c.appel("POST", "github", map[string]string{"action": "recuperer"}, &r)
		a.majs <- func(a *appTUI) {
			if err != nil {
				a.annoncer(a.texteErreur(err), true)
				return
			}
			textes := map[string]string{
				"a-jour":  a.t("Already up to date with GitHub."),
				"fusion":  a.t("{0} commit(s) of GitHub brought into the project.", firstNonEmpty(r.Detail, "?")),
				"rejoint": a.t("The project starts from GitHub's work ({0}).", r.Detail),
				"aucun":   a.t("GitHub's branch {0} has no commit yet: nothing to bring in.", firstNonEmpty(r.Branche, "main")),
				"conflit": a.t("GitHub's work and this folder's change the same lines: nothing was changed yet. Claude merges them at the start of the next cycle, keeping both."),
				"sale":    a.t("This folder has changes not committed: save them first (Save and push the work in progress), then get GitHub's work."),
				"pilote":  a.t("The pilot is running: stop it first (Stop after cycle), then get GitHub's work."),
			}
			msg, ok := textes[r.Resultat]
			if !ok {
				msg = a.t("GitHub's work not brought in: {0}", firstNonEmpty(r.Detail, firstNonEmpty(r.Resultat, "?")))
			}
			bon := map[string]bool{"a-jour": true, "fusion": true, "rejoint": true, "aucun": true, "conflit": true}[r.Resultat]
			a.annoncer(msg, !bon)
			a.evs, a.dernierN = nil, 0
			if bon && ensuite != nil {
				ensuite()
			}
		}
	}()
}

// ----------------------------------------------------------------------------- projects

func (a *appTUI) choisirProjet() {
	l := a.projetsVisibles()
	if len(l) == 0 {
		a.nouveauProjet()
		return
	}
	var choix []choixTUI
	sel := 0
	for i, pv := range l {
		etat := ""
		if pv.Actif {
			etat = " ●"
		}
		choix = append(choix, choixTUI{id: pv.Nom, texte: pv.Nom + etat})
		if pv.Nom == a.c.projet {
			sel = i
		}
	}
	choix = append(choix, choixTUI{id: "+", texte: "+ " + a.t("New project")})
	var archives []string
	for _, pv := range a.vue.Projets {
		if pv.Archive {
			archives = append(archives, pv.Nom)
		}
	}
	if len(archives) > 0 {
		choix = append(choix, choixTUI{id: "archives", texte: a.t("Archived projects") + " (" + itoa64(int64(len(archives))) + ")"})
	}
	a.demander(a.t("Choose a project"), "", choix, func(id string) {
		switch id {
		case "":
		case "+":
			a.nouveauProjet()
		case "archives":
			var c []choixTUI
			for _, n := range archives {
				c = append(c, choixTUI{id: n, texte: n})
			}
			c = append(c, choixTUI{id: "", texte: a.t("Cancel")})
			a.demander(a.t("Bring back"), a.t("Archived projects"), c, func(n string) {
				if n != "" {
					a.agir("POST", "projets/gestion", map[string]string{"nom": n, "action": "desarchiver"}, func(map[string]any) { a.changerProjet(n) })
				}
			})
		default:
			a.changerProjet(id)
		}
	})
	a.dialogue.sel = sel
}

func (a *appTUI) nouveauProjet() {
	dossier := &saisieT{}
	a.formulaire(a.t("New project"), a.t(aideFormulaireMulti),
		[]champTUI{{lib: a.t("Project name"), h: 1}, {lib: a.t("Development folder") + " (" + a.t("full path") + ")", h: 1, s: dossier}, {lib: a.t("What you want"), h: 8}},
		func(v []string) {
			corps := map[string]any{"nom": strings.TrimSpace(v[0]), "dossier": strings.TrimSpace(v[1]), "demande": v[2]}
			nom := strings.TrimSpace(v[0])
			a.agirFormulaire("POST", "projets", corps, func(map[string]any) {
				a.changerProjet(nom)
				a.allerPage(pSuivi)
			})
		})
}

// ----------------------------------------------------------------------------- requests

func (a *appTUI) nouvelleDemande() {
	if a.vue == nil || a.vue.Projet == nil {
		return
	}
	a.formulaire(a.t("New request"), a.t(aideFormulaireMulti),
		[]champTUI{{lib: a.t("Short title"), h: 1}, {lib: a.t("What you want"), h: 10}},
		func(v []string) {
			if strings.TrimSpace(v[1]) == "" {
				a.formValide.erreur = a.t("Missing text")
				a.form = a.formValide
				return
			}
			a.agirFormulaire("POST", "demandes", map[string]string{"titre": v[0], "texte": v[1]}, func(map[string]any) { a.derniere["demandes"] = time.Time{} })
		})
}

func (a *appTUI) remarque() {
	if a.vue == nil || a.vue.Projet == nil {
		return
	}
	a.formulaire(a.t("Note along the way"), a.t("To correct without waiting: Claude reads it at the start of the next cycle, during the current request.")+"  "+a.t(aideFormulaireMulti),
		[]champTUI{{lib: a.t("Note along the way"), h: 5}},
		func(v []string) {
			if strings.TrimSpace(v[0]) == "" {
				return
			}
			a.agir("POST", "remarque", map[string]string{"texte": v[0]}, nil)
		})
}

func (a *appTUI) supprimerDemande() {
	if a.dem == nil || a.selDem >= len(a.dem.Attente) {
		return
	}
	d := a.dem.Attente[a.selDem]
	a.demander(a.t("Delete"), a.t("Delete the request “{0}”?", firstNonEmpty(d.Nom, d.Fichier)),
		[]choixTUI{{id: "", texte: a.t("Cancel")}, {id: "oui", texte: a.t("Delete"), danger: true}}, func(id string) {
			if id == "oui" {
				a.agir("DELETE", "demandes/"+d.Fichier, nil, func(map[string]any) { a.derniere["demandes"] = time.Time{} })
			}
		})
}

func (a *appTUI) deplacerDemande(sens int) {
	if a.dem == nil {
		return
	}
	i, j := a.selDem, a.selDem+sens
	if i < 0 || j < 0 || i >= len(a.dem.Attente) || j >= len(a.dem.Attente) {
		return
	}
	a.dem.Attente[i], a.dem.Attente[j] = a.dem.Attente[j], a.dem.Attente[i]
	a.selDem = j
	var ordre []string
	for _, d := range a.dem.Attente {
		ordre = append(ordre, d.Fichier)
	}
	a.agir("POST", "demandes/ordre", map[string]any{"ordre": ordre}, func(map[string]any) { a.derniere["demandes"] = time.Time{} })
}

// ----------------------------------------------------------------------------- chat

func (a *appTUI) envoyerChat() {
	m := strings.TrimSpace(a.saisie.texte())
	if m == "" || a.envoi {
		return
	}
	a.envoi = true
	c := *a.c
	go func() {
		var rep map[string]any
		err := c.appel("POST", "chat", map[string]string{"message": m, "modele": a.modeleChat, "mode": ""}, &rep)
		a.majs <- func(a *appTUI) {
			a.envoi = false
			if err != nil {
				a.annoncer(a.texteErreur(err), true)
				return
			}
			a.saisie.mettreTexte("")
			a.defilC = 0
			a.derniere["chat"] = time.Time{}
		}
	}()
}

func (a *appTUI) nouvelleConversation() {
	a.demander(a.t("New conversation"), a.t("Start a new conversation? Claude forgets this one (it stays in the project's log)."),
		[]choixTUI{{id: "oui", texte: a.t("New conversation")}, {id: "", texte: a.t("Cancel")}}, func(id string) {
			if id == "oui" {
				a.agir("POST", "chat/effacer", nil, func(map[string]any) { a.derniere["chat"] = time.Time{} })
			}
		})
}

// ----------------------------------------------------------------------------- setup & health

func (a *appTUI) reparer() {
	v := a.verifChoisie()
	if v == nil {
		return
	}
	if v.Statut != "echec" || !v.ReparationAuto {
		if v.ID == "jeton" {
			a.demanderJeton()
			return
		}
		a.annoncer(a.t("Nothing to repair here."), false)
		return
	}
	id := v.ID
	if id == "jeton" {
		for _, x := range a.verifs {
			if x.ID == "claude" && x.Statut == "echec" {
				a.demander(a.t("Repair"), a.t("Claude Code is not installed yet: the token comes from it. Install it first (the Claude Code line, or Install everything), then get the token."),
					[]choixTUI{{id: "installer", texte: a.t("Install everything")}, {id: "", texte: a.t("Cancel")}}, func(r string) {
						if r == "installer" {
							a.installer()
						}
					})
				return
			}
		}
	}
	if v.ReparationTerminal {
		// in this terminal, in front of the user: each command shown, sudo answered here, then back
		a.demander(a.t("Repair"), a.t(v.ReparationInfo)+"\n\n"+a.t("It runs in this terminal: you see each command and answer what it asks (your password for sudo). Athanor comes back when it is done."),
			[]choixTUI{{id: "oui", texte: a.t("Repair")}, {id: "", texte: a.t("Cancel")}}, func(r string) {
				if r == "oui" {
					a.etapeDansTerminal(id, func(code int) {
						if id == "jeton" && code == 0 {
							a.demanderJeton()
						}
						a.agir("POST", "sante/verifier", map[string]string{"id": "tout"}, func(map[string]any) { a.derniere["sante"] = time.Time{} })
					})
				}
			})
		return
	}
	a.demander(a.t("Repair"), a.t(v.ReparationInfo), []choixTUI{{id: "oui", texte: a.t("Repair")}, {id: "", texte: a.t("Cancel")}}, func(r string) {
		if r == "oui" {
			a.agir("POST", "sante/reparer", map[string]string{"id": id}, func(map[string]any) { a.derniere["sante"] = time.Time{} })
		}
	})
}

func (a *appTUI) installer() {
	a.demander(a.t("Install everything"), a.t("Installs what Claude needs on this computer (Claude Code, the system sandbox, the tools).")+"\n\n"+a.t("It runs in this terminal: you see each command and answer what it asks (your password for sudo). Athanor comes back when it is done."),
		[]choixTUI{{id: "oui", texte: a.t("Install everything")}, {id: "", texte: a.t("Cancel")}}, func(id string) {
			if id == "oui" {
				a.etapeDansTerminal("installer", func(int) {
					a.agir("POST", "sante/verifier", map[string]string{"id": "tout"}, func(map[string]any) { a.derniere["sante"] = time.Time{} })
				})
			}
		})
}

// etapeDansTerminal: a repair or the installation run in this terminal (the server gives its command line),
// then ensuite with its exit code.
func (a *appTUI) etapeDansTerminal(id string, ensuite func(code int)) {
	c := *a.c
	go func() {
		var r struct {
			Titre   string   `json:"titre"`
			Ligne   []string `json:"ligne"`
			Dossier string   `json:"dossier"`
		}
		err := c.lire("sante/terminal?id="+id, &r) // (the callers give it escaped)
		a.majs <- func(a *appTUI) {
			if err == nil && len(r.Ligne) == 0 {
				err = errors.New("no command")
			}
			if err != nil {
				a.annoncer(a.texteErreur(err), true)
				return
			}
			if a.executer == nil {
				a.demander(a.t(r.Titre), a.t("Run it in a terminal:")+"\n\n"+citerLigne(r.Ligne), []choixTUI{{id: "", texte: "OK"}}, nil)
				return
			}
			code, err := a.executer(a.t(r.Titre), r.Ligne, r.Dossier)
			if err != nil {
				a.demander(a.t(r.Titre), a.texteErreur(err)+"\n\n"+a.t("Run it in a terminal:")+"\n\n"+citerLigne(r.Ligne), []choixTUI{{id: "", texte: "OK"}}, nil)
				return
			}
			if ensuite != nil {
				ensuite(code)
			}
		}
	}()
}

// demanderJeton: the token Claude Code showed (claude setup-token), into Athanor's settings (.env).
func (a *appTUI) demanderJeton() {
	a.formulaire(a.t("Token and model access"), a.t("Paste the token Claude Code showed (it starts with sk-ant-). It goes into Athanor's settings, never on the screen.")+"  "+a.t(aideFormulaireLigne),
		[]champTUI{{lib: "CLAUDE_CODE_OAUTH_TOKEN", h: 1, s: &saisieT{masque: true}}}, func(v []string) {
			j := strings.TrimSpace(v[0])
			if j == "" {
				return
			}
			a.agir("POST", "env", map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": j}, func(map[string]any) {
				a.agir("POST", "sante/verifier", map[string]string{"id": "jeton"}, func(map[string]any) { a.derniere["sante"] = time.Time{} })
			})
		})
}

// ----------------------------------------------------------------------------- the rest

// demanderMotDePasse: the application is locked (Settings › Lock): its password first.
func (a *appTUI) demanderMotDePasse() {
	if a.form != nil || a.verrouDemande {
		return
	}
	a.verrouDemande = true
	a.formulaire(a.t("Athanor is locked"), a.t(aideFormulaireLigne), []champTUI{{lib: a.t("Password"), h: 1, s: &saisieT{masque: true}}}, func(v []string) {
		c := *a.c
		go func() {
			err := c.appel("POST", "deverrouiller", map[string]string{"mot_de_passe": v[0]}, nil)
			a.majs <- func(a *appTUI) {
				a.verrouDemande = false
				if err != nil {
					a.annoncer(a.texteErreur(err), true)
				}
				a.derniere["etat"] = time.Time{}
			}
		}()
	})
}

// ouvrirFenetre: Athanor's window as well (a computer with a screen).
func (a *appTUI) ouvrirFenetre() { a.agir("POST", "fenetre", nil, nil) }

func (a *appTUI) demanderQuitter() {
	texte := a.t("The pilots keep running.")
	if a.proprio {
		texte = a.t("The pilots keep running. Athanor itself stops with this terminal: start it again (here or with its window) to follow them.")
	}
	a.demander(a.t("Quit"), texte, []choixTUI{{id: "oui", texte: a.t("Quit")}, {id: "", texte: a.t("Cancel")}}, func(id string) {
		if id == "oui" {
			a.quitter = true
		}
	})
}
