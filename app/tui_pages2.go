package main

// The terminal interface: For you (what only the user can do), Team (who works on the cycle), GitHub (this
// computer's connection and the project's repository), Settings (the language, the palette, Athanor's
// settings), and managing a project (rename, archive, remove from the list).

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// chargerPages2: the data of the page shown.
func (a *appTUI) chargerPages2() {
	switch a.page {
	case pPourVous:
		a.charger("pourvous", 3*time.Second, func(c clientTUI) (func(*appTUI), error) {
			var r struct {
				Taches []TacheUtilVue `json:"taches"`
			}
			if err := c.lire("pourvous", &r); err != nil {
				return nil, err
			}
			return func(a *appTUI) { a.taches = r.Taches }, nil
		})
	case pEquipe:
		a.charger("agents", 2*time.Second, func(c clientTUI) (func(*appTUI), error) {
			var r struct {
				Agents []Agent `json:"agents"`
			}
			if err := c.lire("agents", &r); err != nil {
				return nil, err
			}
			return func(a *appTUI) { a.agents = r.Agents }, nil
		})
	case pGitHub:
		a.charger("github", 6*time.Second, func(c clientTUI) (func(*appTUI), error) {
			var r struct {
				GitHub     GitHubVue `json:"github"`
				NomPropose string    `json:"nom_propose"`
			}
			if err := c.lire("github", &r); err != nil {
				return nil, err
			}
			return func(a *appTUI) { a.gh, a.nomPropose = &r.GitHub, r.NomPropose }, nil
		})
	case pReglages:
		a.charger("env", 5*time.Second, func(c clientTUI) (func(*appTUI), error) {
			var r []LigneEnv
			if err := c.lire("env", &r); err != nil {
				return nil, err
			}
			return func(a *appTUI) { a.env = r }, nil
		})
	}
}

func (a *appTUI) raccourcisPages2() []raccourci {
	t := a.t
	switch a.page {
	case pPourVous:
		return []raccourci{{"Enter", t("Done")}, {"k", t("Skip: find another way")}}
	case pGitHub:
		if a.gh == nil {
			return nil
		}
		if !a.gh.ConnexionPC.Connecte {
			return []raccourci{{"c", t("Connect to GitHub")}}
		}
		if !a.gh.Lie {
			return []raccourci{{"l", t("Use one of my repositories")}, {"n", t("Create a private repository")}, {"D", t("Disconnect")}}
		}
		return []raccourci{{"P", t("Push now")}, {"g", t("Get GitHub's work")}, {"t", t("Test the connection")}, {"A", t("Automatic push")}, {"u", t("Unlink")}}
	case pReglages:
		return []raccourci{{"Enter", t("Edit")}, {"L", t("Language")}, {"P", t("Palette")}, {"A", t("Accent")}}
	}
	return nil
}

func (a *appTUI) touchePages2(ev evenementTUI, k string, defiler func(*int, int)) {
	switch a.page {
	case pPourVous:
		switch k {
		case "entree":
			a.repondreTache(false)
		case "k":
			a.repondreTache(true)
		default:
			defiler(&a.selTache, -1)
		}
	case pEquipe:
		defiler(&a.selAgent, -1)
	case pGitHub:
		a.toucheGitHub(k)
	case pReglages:
		switch k {
		case "entree":
			a.modifierReglage()
		case "L":
			a.choisirPreference("langue")
		case "P":
			a.choisirPreference("theme")
		case "A":
			a.choisirPreference("accent")
		default:
			defiler(&a.selReglage, -1)
		}
	}
}

// liste: rows of a list in a box, the selected one kept in view; each row a title and a line under it.
func (a *appTUI) liste(e *ecranT, ix, iy, il, ih int, n int, sel *int, ligne func(i int, f couleurT) (string, string, styleT, styleT)) {
	p := a.pal
	if n == 0 {
		return
	}
	*sel = min(max(0, *sel), n-1)
	vis := max(1, ih/2)
	debut := 0
	if *sel >= vis {
		debut = *sel - vis + 1
	}
	for i := 0; i < vis && debut+i < n; i++ {
		k := debut + i
		f := p.panneau
		if k == *sel {
			f = p.ligne
			e.rect(ix-1, iy+i*2, il+2, 2, a.st(p.encre, f))
		}
		t1, t2, s1, s2 := ligne(k, f)
		e.texte(ix, iy+i*2, il, t1, s1)
		e.texte(ix, iy+i*2+1, il, t2, s2)
		a.zone(ix-1, iy+i*2, il+2, 2, func() { *sel = k })
	}
}

// texteBloc: a text wrapped in a box, from line y; the lines used.
func (a *appTUI) texteBloc(e *ecranT, x, y, l, hmax int, t string, st styleT) int {
	n := 0
	for _, li := range envelopper(t, l) {
		if n >= hmax {
			break
		}
		e.texte(x, y+n, l, li, st)
		n++
	}
	return n
}

// ----------------------------------------------------------------------------- For you

func (a *appTUI) etatTacheLib(t TacheUtilVue) (string, couleurT) {
	p := a.pal
	switch t.Etat {
	case "pour_vous":
		return a.t("For you"), p.attention
	case "a_verifier":
		return a.t("Claude checks it"), p.accent
	case "a_contourner":
		return a.t("Claude finds another way"), p.muet
	}
	return a.t("Done"), p.ok
}

func (a *appTUI) pagePourVous(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	lg := l
	if l >= 110 {
		lg = l * 2 / 5
	}
	hl := h
	if l < 110 {
		hl = max(6, h/2) // a narrow terminal: the list above, the selected task below
	}
	ix, iy, il, ih := a.boite(e, x, y, lg, hl, a.t("For you"), true)
	if a.vue == nil || a.vue.Projet == nil {
		return
	}
	if len(a.taches) == 0 {
		a.texteBloc(e, ix, iy, il, ih, a.t("Nothing for you: what Claude cannot do itself (an account, a payment, a key…) shows up here, with the exact steps."), a.st(p.muet, fond))
		return
	}
	a.liste(e, ix, iy, il, ih, len(a.taches), &a.selTache, func(i int, f couleurT) (string, string, styleT, styleT) {
		t := a.taches[i]
		lib, c := a.etatTacheLib(t)
		return "● " + t.Titre, lib, a.gras(p.encre, f), a.st(c, f)
	})
	a.zoneMolette(x, y, lg, hl, func(s int) { a.selTache += s })
	t := a.taches[min(a.selTache, len(a.taches)-1)]
	var dx, dy, dw, dh int
	if l >= 110 {
		dx, dy, dw, dh = a.boite(e, x+lg, y, l-lg, h, t.Titre, false)
	} else {
		if h-hl < 4 {
			return
		}
		dx, dy, dw, dh = a.boite(e, x, y+hl, l, h-hl, t.Titre, false)
	}
	yy := dy
	yy += a.texteBloc(e, dx, yy, dw, dh-(yy-dy), t.Details, a.st(p.encre, fond))
	if t.Attendu != "" {
		yy++
		yy += a.texteBloc(e, dx, yy, dw, dh-(yy-dy), a.t("What Claude needs back")+": "+t.Attendu, a.st(p.accent, fond))
	}
	if len(t.Secrets) > 0 {
		var l []string
		for _, s := range t.Secrets {
			m := "○ "
			if t.Donnes[s] {
				m = "✔ "
			}
			l = append(l, m+s)
		}
		yy++
		yy += a.texteBloc(e, dx, yy, dw, dh-(yy-dy), strings.Join(l, "   "), a.st(p.muet, fond))
	}
	if t.Note != "" {
		yy++
		yy += a.texteBloc(e, dx, yy, dw, dh-(yy-dy), "Claude: "+t.Note, a.st(p.attention, fond))
	}
	if n := len(t.Reponses); n > 0 {
		r := t.Reponses[n-1]
		yy++
		a.texteBloc(e, dx, yy, dw, dh-(yy-dy), a.t("You")+": "+firstNonEmpty(r.Texte, r.Action), a.st(p.violet, fond))
	}
}

// repondreTache: done (an answer, the values asked for) or skip (Claude finds another way).
func (a *appTUI) repondreTache(passer bool) {
	if len(a.taches) == 0 {
		return
	}
	t := a.taches[min(a.selTache, len(a.taches)-1)]
	if t.Etat == "close" {
		return
	}
	champs := []champTUI{{lib: a.t("Your answer"), h: 5}}
	var noms []string
	if !passer {
		for _, s := range t.Secrets {
			if !t.Donnes[s] {
				noms = append(noms, s)
				champs = append(champs, champTUI{lib: s, h: 1, s: &saisieT{masque: true}})
			}
		}
	}
	titre := a.t("Done") + ": " + t.Titre
	action := "fait"
	if passer {
		titre, action = a.t("Skip: find another way")+": "+t.Titre, "passer"
	}
	id := t.ID
	a.formulaire(titre, a.t(aideFormulaireMulti), champs, func(v []string) {
		corps := map[string]any{"id": id, "action": action, "texte": v[0]}
		if len(noms) > 0 {
			sec := map[string]string{}
			for i, n := range noms {
				if s := strings.TrimSpace(v[i+1]); s != "" {
					sec[n] = s
				}
			}
			corps["secrets"] = sec
		}
		a.agirFormulaire("POST", "pourvous", corps, func(map[string]any) { a.derniere["pourvous"] = time.Time{} })
	})
}

// ----------------------------------------------------------------------------- Team

func (a *appTUI) pageEquipe(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t("Team"), false)
	if len(a.agents) == 0 {
		a.texteBloc(e, ix, iy, il, ih, a.t("Who works on this cycle and who called whom shows here while a cycle runs."), a.st(p.muet, fond))
		return
	}
	// the lead and the workers, each followed by the sub-agents it called
	enfants := map[string][]Agent{}
	var racines []Agent
	for _, ag := range a.agents {
		if ag.Parent != "" {
			enfants[ag.Parent] = append(enfants[ag.Parent], ag)
		} else {
			racines = append(racines, ag)
		}
	}
	type ligneEq struct {
		ag  Agent
		ind string
	}
	var lignes []ligneEq
	for _, r := range racines {
		lignes = append(lignes, ligneEq{r, ""})
		for _, c := range enfants[r.ID] {
			lignes = append(lignes, ligneEq{c, "  ↳ "})
		}
	}
	a.liste(e, ix, iy, il, ih, len(lignes), &a.selAgent, func(i int, f couleurT) (string, string, styleT, styleT) {
		ag := lignes[i].ag
		statut, c := a.t("finished"), p.muet
		switch ag.Statut {
		case "travaille":
			statut, c = a.t("working"), p.ok
		case "attend":
			statut, c = a.t("waiting"), p.attention
		}
		nom := ag.Nom
		if ag.Genre == "chef" {
			nom = a.t("Project lead")
		} else if ag.Genre == "ouvrier" {
			nom = a.t("Worker {0}", ag.Jalon)
		}
		chiffres := []string{statut}
		if ag.Tours > 0 {
			chiffres = append(chiffres, a.t("{0} turns", ag.Tours))
		}
		if ag.Modele != "" {
			chiffres = append(chiffres, ag.Modele)
		}
		detail := firstNonEmpty(ag.Bulle, firstNonEmpty(ag.Activite, ag.Mission))
		return lignes[i].ind + "● " + nom + "  " + strings.Join(chiffres, " · "), strings.Repeat(" ", len([]rune(lignes[i].ind))+2) + detail, a.gras(c, f), a.st(p.muet, f)
	})
	a.zoneMolette(x, y, l, h, func(s int) { a.selAgent += s })
}

// ----------------------------------------------------------------------------- GitHub

func (a *appTUI) pageGitHub(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	ix, iy, il, ih := a.boite(e, x, y, l, h, "GitHub", true)
	if a.vue == nil || a.vue.Projet == nil {
		return
	}
	if a.gh == nil {
		e.texte(ix, iy, il, a.t("Updating…"), a.st(p.muet, fond))
		return
	}
	g := a.gh
	var lignes []segTUI
	ajouter := func(t string, st styleT) {
		for _, li := range envelopper(t, il) {
			lignes = append(lignes, segTUI{li, st})
		}
	}
	ajouter(a.t("This computer"), a.gras(p.encre, fond))
	if g.ConnexionPC.Connecte {
		ajouter("✔ "+a.t("Connected as {0}", g.ConnexionPC.Login), a.st(p.ok, fond))
	} else {
		ajouter("○ "+a.t("Connect this computer to your GitHub account once: then every project is linked to one of your repositories, or one you are a collaborator of, without signing in again."), a.st(p.attention, fond))
		ajouter("c · "+a.t("Connect to GitHub")+" — "+a.t("in this terminal: GitHub shows a code and an address, open it on any device."), a.st(p.accent, fond))
	}
	lignes = append(lignes, segTUI{"", styleT{}})
	ajouter(a.t("This project"), a.gras(p.encre, fond))
	if !g.Lie {
		ajouter("○ "+a.t("Not linked to GitHub yet."), a.st(p.muet, fond))
		if g.ConnexionPC.Connecte {
			ajouter("l · "+a.t("Use one of my repositories")+"   n · "+a.t("Create a private repository"), a.st(p.accent, fond))
		}
	} else {
		ajouter("✔ "+g.Depot+"  ("+a.t("Branch")+": "+firstNonEmpty(g.Branche, "main")+")", a.st(p.ok, fond))
		auto := a.t("off")
		if g.Auto {
			auto = a.t("on")
		}
		ajouter(a.t("Automatic push")+": "+auto+"   ·   "+a.t("Share the workshop (.athanor/) on GitHub")+": "+map[bool]string{true: a.t("on"), false: a.t("off")}[g.Atelier], a.st(p.muet, fond))
		if len(g.Dernier) > 0 && string(g.Dernier) != "null" {
			var d map[string]any
			if json.Unmarshal(g.Dernier, &d) == nil {
				var parts []string
				for _, k := range []string{"quand", "resultat", "commit", "message"} {
					if v, ok := d[k]; ok && fmt.Sprint(v) != "" {
						parts = append(parts, fmt.Sprint(v))
					}
				}
				ajouter(a.t("Last push")+": "+strings.Join(parts, " · "), a.st(p.muet, fond))
			}
		}
		if !g.Connexion && !g.CleCompte {
			ajouter("ℹ "+a.t("This project still pushes with a key of its own (an older version): Use my GitHub connection moves it to the connection.")+" (C)", a.st(p.attention, fond))
		}
	}
	for i, s := range lignes {
		if i < ih {
			e.texte(ix, iy+i, il, s.t, s.s)
		}
	}
}

func (a *appTUI) toucheGitHub(k string) {
	g := a.gh
	if g == nil || a.vue == nil || a.vue.Projet == nil {
		return
	}
	rafraichir := func(map[string]any) { a.derniere["github"] = time.Time{} }
	switch k {
	case "c":
		a.etapeDansTerminal("github-connecter", func(int) { a.derniere["github"] = time.Time{} })
	case "D":
		if g.ConnexionPC.Connecte {
			a.demander(a.t("Disconnect"), a.t("Disconnect this computer from GitHub? The projects that use this connection are no longer linked (link them again later). The key stays listed in GitHub's Settings › SSH and GPG keys, where you can remove it."),
				[]choixTUI{{id: "", texte: a.t("Cancel")}, {id: "oui", texte: a.t("Disconnect"), danger: true}}, func(id string) {
					if id == "oui" {
						a.agir("POST", "github/connexion", map[string]string{"action": "deconnecter"}, rafraichir)
					}
				})
		}
	case "l":
		if !g.ConnexionPC.Connecte {
			a.annoncer(a.t("Connect this computer to GitHub first (step 1)."), true)
			return
		}
		a.formulaire(a.t("Use one of my repositories"), a.t(aideFormulaireLigne),
			[]champTUI{{lib: a.t("Repository (OWNER/NAME)"), h: 1}, {lib: a.t("Branch"), h: 1, s: &saisieT{t: []rune("main"), pos: 4}}},
			func(v []string) {
				a.agirFormulaire("POST", "github", map[string]any{"action": "lier", "mode": "existant", "valeur": strings.TrimSpace(v[0]), "branche": strings.TrimSpace(v[1]), "auto": true, "workflows_ok": true}, rafraichir)
			})
	case "n":
		if !g.ConnexionPC.Connecte {
			a.annoncer(a.t("Connect this computer to GitHub first (step 1)."), true)
			return
		}
		nom := &saisieT{}
		nom.mettreTexte(a.nomPropose)
		a.formulaire(a.t("Create a private repository"), a.t(aideFormulaireLigne),
			[]champTUI{{lib: a.t("Repository name"), h: 1, s: nom}, {lib: a.t("Branch"), h: 1, s: &saisieT{t: []rune("main"), pos: 4}}},
			func(v []string) {
				q := "github-creer&nom=" + url.QueryEscape(strings.TrimSpace(v[0])) + "&branche=" + url.QueryEscape(strings.TrimSpace(v[1]))
				a.etapeDansTerminal(q, func(int) { a.derniere["github"] = time.Time{} })
			})
	case "P":
		if g.Lie {
			a.annoncer(a.t("Pushing…"), false)
			a.agir("POST", "github", map[string]any{"action": "pousser"}, func(map[string]any) {
				a.annoncer(a.t("Pushed to GitHub."), false)
				a.derniere["github"] = time.Time{}
			})
		}
	case "t":
		if g.Lie {
			a.agir("POST", "github", map[string]any{"action": "tester"}, rafraichir)
		}
	case "A": // (a is Stop after cycle everywhere)
		if g.Lie {
			a.agir("POST", "github", map[string]any{"action": "auto", "auto": !g.Auto}, rafraichir)
		}
	case "C":
		if g.Lie && !g.Connexion && g.ConnexionPC.Connecte {
			a.agir("POST", "github", map[string]any{"action": "connexion"}, rafraichir)
		}
	case "u":
		if g.Lie {
			a.demander(a.t("Unlink"), a.t("Unlink this project from {0}? Nothing is deleted on GitHub; the pushes stop.", g.Depot),
				[]choixTUI{{id: "", texte: a.t("Cancel")}, {id: "oui", texte: a.t("Unlink"), danger: true}}, func(id string) {
					if id == "oui" {
						a.agir("POST", "github", map[string]any{"action": "delier"}, rafraichir)
					}
				})
		}
	case "g":
		a.recupererGitHub()
	}
}

// ----------------------------------------------------------------------------- Settings

func (a *appTUI) pageReglages(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	lg := l
	if l >= 110 {
		lg = l * 3 / 5
	}
	ix, iy, il, ih := a.boite(e, x, y, lg, h, a.t("Settings"), true)
	// the interface first (L, P, A), then Athanor's settings (.env)
	v := a.vue
	noms := map[string]string{"ardoise": "Slate", "minuit": "Midnight", "ocean": "Ocean", "clair": "Light", "sable": "Sand",
		"sarcelle": "Teal", "bleu": "Blue", "violet": "Violet", "ambre": "Amber", "rose": "Rose"}
	pref := fmt.Sprintf("%s: %s   ·   %s: %s   ·   %s: %s", a.t("Language"), nomLangue(v.Langue), a.t("Palette"), a.t(noms[firstNonEmpty(v.Theme, "ardoise")]), a.t("Accent"), a.t(noms[firstNonEmpty(v.Accent, "sarcelle")]))
	e.texte(ix, iy, il, pref, a.st(p.encre, fond))
	if len(a.env) == 0 {
		e.texte(ix, iy+2, il, a.t("Updating…"), a.st(p.muet, fond))
		return
	}
	a.liste(e, ix, iy+2, il, ih-2, len(a.env), &a.selReglage, func(i int, f couleurT) (string, string, styleT, styleT) {
		r := a.env[i]
		val := r.Valeur
		if val == "" {
			val = "—"
		}
		return r.Cle + " = " + val, a.t(r.Aide), a.gras(p.encre, f), a.st(p.muet, f)
	})
	a.zoneMolette(x, y, lg, h, func(s int) { a.selReglage += s })
	if l < 110 || a.selReglage >= len(a.env) {
		return
	}
	r := a.env[a.selReglage]
	jx, jy, jl, jh := a.boite(e, x+lg, y, l-lg, h, r.Cle, false)
	yy := jy + a.texteBloc(e, jx, jy, jl, jh, a.t(r.Aide), a.st(p.encre, fond))
	if r.Secret {
		a.texteBloc(e, jx, yy+1, jl, jh-(yy-jy)-1, a.t("A secret: never shown; Enter replaces it."), a.st(p.muet, fond))
	}
}

func nomLangue(c string) string {
	return map[string]string{"en": "English", "fr": "Français", "es": "Español", "de": "Deutsch", "it": "Italiano", "pt": "Português"}[firstNonEmpty(c, "en")]
}

func (a *appTUI) modifierReglage() {
	if a.selReglage >= len(a.env) {
		return
	}
	r := a.env[a.selReglage]
	s := &saisieT{masque: r.Secret}
	if !r.Secret {
		s.mettreTexte(r.Valeur)
	}
	cle := r.Cle
	a.formulaire(cle, a.t(r.Aide)+"  "+a.t(aideFormulaireLigne), []champTUI{{lib: cle, h: 1, s: s}}, func(v []string) {
		if r.Secret && strings.TrimSpace(v[0]) == "" {
			return // a secret left empty: unchanged
		}
		a.agirFormulaire("POST", "env", map[string]string{cle: strings.TrimSpace(v[0])}, func(map[string]any) { a.derniere["env"] = time.Time{} })
	})
}

// choisirPreference: the language, the palette or the accent, as in Settings › Options.
func (a *appTUI) choisirPreference(quoi string) {
	var choix []choixTUI
	titre := ""
	switch quoi {
	case "langue":
		titre = a.t("Language")
		for _, c := range []string{"en", "fr", "es", "de", "it", "pt"} {
			choix = append(choix, choixTUI{id: c, texte: nomLangue(c)})
		}
	case "theme":
		titre = a.t("Palette")
		for _, c := range [][2]string{{"ardoise", "Slate"}, {"minuit", "Midnight"}, {"ocean", "Ocean"}, {"clair", "Light"}, {"sable", "Sand"}} {
			choix = append(choix, choixTUI{id: c[0], texte: a.t(c[1])})
		}
	case "accent":
		titre = a.t("Accent")
		for _, c := range [][2]string{{"sarcelle", "Teal"}, {"bleu", "Blue"}, {"violet", "Violet"}, {"ambre", "Amber"}, {"rose", "Rose"}} {
			choix = append(choix, choixTUI{id: c[0], texte: a.t(c[1])})
		}
	}
	choix = append(choix, choixTUI{id: "", texte: a.t("Cancel")})
	a.demander(titre, "", choix, func(id string) {
		if id == "" {
			return
		}
		a.agir("POST", "preferences", map[string]string{quoi: id}, nil)
	})
}

// ----------------------------------------------------------------------------- managing a project

func (a *appTUI) gererProjet() {
	if a.vue == nil || a.vue.Projet == nil {
		return
	}
	nom := a.vue.Projet.Nom
	a.demander(a.t("Manage")+": "+nom, a.vue.Projet.Dossier, []choixTUI{
		{id: "renommer", texte: a.t("Rename")}, {id: "archiver", texte: a.t("Archive")}, {id: "liste", texte: a.t("Remove from the list"), danger: true}, {id: "", texte: a.t("Cancel")}},
		func(id string) {
			switch id {
			case "renommer":
				s := &saisieT{}
				s.mettreTexte(nom)
				a.formulaire(a.t("Rename"), a.t(aideFormulaireLigne), []champTUI{{lib: a.t("Project name"), h: 1, s: s}}, func(v []string) {
					nouveau := strings.TrimSpace(v[0])
					a.agirFormulaire("POST", "projets/gestion", map[string]string{"nom": nom, "action": "renommer", "valeur": nouveau}, func(map[string]any) { a.changerProjet(nouveau) })
				})
			case "archiver":
				a.demander(a.t("Archive"), a.t("Archive {0}? It leaves the list; its folder and its workshop stay as they are, and it can come back from the archived projects.", nom),
					[]choixTUI{{id: "", texte: a.t("Cancel")}, {id: "oui", texte: a.t("Archive")}}, func(r string) {
						if r == "oui" {
							a.agir("POST", "projets/gestion", map[string]string{"nom": nom, "action": "archiver"}, func(map[string]any) { a.changerProjet("") })
						}
					})
			case "liste":
				a.demander(a.t("Remove from the list"), a.t("Remove {0} from Athanor's list? Its folder and its files stay on the disk.", nom),
					[]choixTUI{{id: "", texte: a.t("Cancel")}, {id: "oui", texte: a.t("Remove from the list"), danger: true}}, func(r string) {
						if r == "oui" {
							a.agir("POST", "projets/gestion", map[string]string{"nom": nom, "action": "supprimer", "mode": "liste"}, func(map[string]any) { a.changerProjet("") })
						}
					})
			}
		})
}

var _ = sort.Strings
