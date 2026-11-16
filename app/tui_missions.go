package main

// The terminal interface: the Missions page (key E), as the web's #missions and #missions/<id>: the list (state,
// title or sentence, the relay's step), then one mission (its lanes, its relay as text, the 10 newest lines of
// its journal); p pauses, r resumes (Athanor's password), a stops (confirmed), through the web's API.

import (
	"encoding/json"
	"strings"
	"time"
)

// the web's ETATS_MISSION, RELAIS_MISSION, RANG_RELAIS, POLITIQUES_MISSION and MOTS_JOURNAL (app.js)
var (
	etatsMissionTUI = map[string]string{"preparation": "Being prepared", "proposee": "Proposed", "acceptee": "Accepted", "lancee": "Launched",
		"terminee": "Finished", "annulee": "Cancelled", "echec": "Failed", "en_pause": "Paused", "arretee": "Stopped", "arret_demande": "Stopping after the milestone"}
	relaisMissionTUI = []string{"Milestone delivered", "Dev deployment", "Verification", "Your accord", "Production"}
	rangRelaisTUI    = map[string]int{"construction": 0, "dev_deploye": 2, "dev_verifie": 3, "accord_prod": 3, "prod_deploye": 4, "termine": 5}
	politiquesTUI    = map[string]string{"prod": "Ask me for production", "tout": "Ask me for everything", "autonome": "Autonomous"}
	motsJournalTUI   = map[string]string{"fenetre": "from this computer", "emergency stop": "emergency stop", "refusee": "refused", "expiree": "expired",
		"echec": "failed", "arretee": "stopped", "accord": "after your accord", "auto": "automatically", "deploiement": "deployment",
		"mission": "launch", "plan_serveurs": "Servers plan"}
	etatsVoieTUI = map[string]string{"travaille": "Working", "termine": "Finished", "arrete": "Stopped", // Studio: the project's pilot
		"preparation": "Reading the server", "critique": "Security critic reading the plan", "attente_accord": "Waiting for your decision", // Servers: its task
		"application": "Applying the plan", "verification": "External check", "finie": "Done", "echec": "Failed", "arretee": "Stopped"}
)

func (a *appTUI) chargerMissions() {
	a.charger("missions", 2*time.Second, func(c clientTUI) (func(*appTUI), error) {
		var r struct {
			Missions []Mission `json:"missions"`
		}
		if err := c.lire("missions", &r); err != nil {
			return nil, err
		}
		return func(a *appTUI) { a.missions = r.Missions }, nil
	})
}

// missionChoisie: the mission open, else the one selected in the list (nil: none).
func (a *appTUI) missionChoisie() *Mission {
	for i := range a.missions {
		if a.missions[i].ID == a.misOuverte {
			return &a.missions[i]
		}
	}
	if a.misOuverte == "" && a.selMis >= 0 && a.selMis < len(a.missions) {
		return &a.missions[a.selMis]
	}
	return nil
}

func carteTUI(m Mission) CarteMission {
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	return c
}

func titreMissionTUI(m Mission) string {
	if t := carteTUI(m).Titre; t != "" {
		return t
	}
	return strings.Join(strings.Fields(m.Phrase), " ")
}

// etapeRelaisTUI: "Relay: Verification" for a launched or paused mission, "" otherwise (the web's etapeRelais).
func (a *appTUI) etapeRelaisTUI(m Mission) string {
	rang := 5
	if m.Relais != nil && (m.Etat == "lancee" || m.Etat == "en_pause") {
		rang = rangRelaisTUI[m.Relais.Etape]
	}
	if rang < 5 {
		return a.t("Relay: {0}", a.t(relaisMissionTUI[rang]))
	}
	return ""
}

func (a *appTUI) couleurEtatMission(etat string) couleurT {
	switch etat {
	case "lancee":
		return a.pal.ok
	case "en_pause", "proposee", "preparation", "arret_demande":
		return a.pal.attention
	case "echec", "arretee":
		return a.pal.erreur
	}
	return a.pal.muet
}

// texteJournalTUI: a journal entry in the user's language, its parameters as the web's motJournal says them.
func (a *appTUI) texteJournalTUI(e EntreeJournal) string {
	args := make([]any, len(e.Params))
	for i, x := range e.Params {
		switch {
		case strings.HasPrefix(e.Cle, "Policy changed"):
			args[i] = a.t(firstNonEmpty(politiquesTUI[x], x))
		case strings.HasPrefix(x, "appareil:"):
			args[i] = a.t("from a paired phone")
		case motsJournalTUI[x] != "":
			args[i] = a.t(motsJournalTUI[x])
		default:
			args[i] = x
			for _, p := range prefixesTUI {
				if strings.HasPrefix(x, p) {
					args[i] = a.t(x)
					break
				}
			}
		}
	}
	return a.t(e.Cle, args...)
}

// etapeEchecTUI: the relay step of the newest journal entry when it is an error (the web's etapeEchec), else -1.
func etapeEchecTUI(j []EntreeJournal) int {
	if len(j) == 0 || j[len(j)-1].Niveau != "erreur" {
		return -1
	}
	e := j[len(j)-1]
	env := ""
	if len(e.Params) > 0 {
		env = e.Params[0]
	}
	switch {
	case strings.HasPrefix(e.ID, "incident:"), strings.HasPrefix(e.ID, "prod:"):
		return 4
	case (strings.HasPrefix(e.ID, "deploi:") || strings.HasPrefix(e.ID, "verif:")) && strings.HasSuffix(e.ID, ":echec"):
		switch env {
		case "prod":
			return 4
		case "dev":
			if strings.HasPrefix(e.ID, "deploi:") {
				return 1
			}
			return 2
		}
	}
	return -1
}

// friseTUI: the relay in text, "✓ Milestone delivered  ✓ Dev deployment  ● Verification  ○ Your accord  ○ Production"
// (3 steps without a prod on the card), then the dev and prod URLs once deployed.
func (a *appTUI) friseTUI(m Mission) (string, string) {
	c := carteTUI(m)
	var dev, prod *EnvCarte
	for i := range c.Envs {
		switch c.Envs[i].Nom {
		case "dev":
			dev = &c.Envs[i]
		case "prod":
			prod = &c.Envs[i]
		}
	}
	rang, echec := rangRelaisTUI[m.Relais.Etape], etapeEchecTUI(m.Journal)
	etapes := relaisMissionTUI
	if prod == nil {
		etapes = etapes[:3]
	}
	var l []string
	for i, x := range etapes {
		s := "○"
		switch {
		case echec >= 0 && i < echec, echec < 0 && i < rang:
			s = "✓"
		case i == echec:
			s = "✗"
		case echec < 0 && i == rang:
			s = "●"
		}
		l = append(l, s+" "+a.t(x))
	}
	var urls []string
	if dev != nil && dev.URL != "" && rang >= 2 {
		urls = append(urls, a.t("Dev:")+" "+dev.URL)
	}
	if prod != nil && prod.URL != "" && rang >= 4 {
		urls = append(urls, a.t("Prod:")+" "+prod.URL)
	}
	return strings.Join(l, "  "), strings.Join(urls, "   ")
}

// texteVoieTUI: a lane's module, name, state, then Studio's milestone n/N and what the project lead does.
func (a *appTUI) texteVoieTUI(v VoieMission) (string, string, string) {
	mod, nom := a.t(moduleDev{}.Nom()), v.Ref
	if v.Module != "dev" {
		mod, nom = a.t(moduleServeurs{}.Nom()), firstNonEmpty(v.Serveur, v.Ref)
	}
	etat := a.t(firstNonEmpty(etatsVoieTUI[v.Actuel], v.Actuel))
	if v.Etat == "refusee" {
		etat = a.t("Refused: native Windows mode")
	} else if v.Actuel == "" || v.Actuel == "inconnu" {
		etat = a.t("Not found")
	}
	var plus []string
	if v.Module == "dev" && v.Total > 0 {
		if v.Titre != "" {
			plus = append(plus, a.t("Milestone {0}/{1}: {2}", min(v.Fait+1, v.Total), v.Total, v.Titre))
		} else {
			plus = append(plus, a.t("{0} / {1} milestones done", v.Fait, v.Total))
		}
	}
	if v.Activite != "" {
		plus = append(plus, a.t(v.Activite))
	} else if v.Detail != "" {
		plus = append(plus, v.Detail)
	}
	return mod + "  " + nom, etat, strings.Join(plus, " · ")
}

// ligneMissionTUI: one mission in a list (its state, title, relay step); a click opens it.
func (a *appTUI) ligneMissionTUI(e *ecranT, x, y, l, le int, m Mission, fond couleurT) {
	p := a.pal
	e.texte(x, y, le, a.t(firstNonEmpty(etatsMissionTUI[m.Etat], m.Etat)), a.gras(a.couleurEtatMission(m.Etat), fond))
	droite := a.etapeRelaisTUI(m)
	ld := 0
	if droite != "" && l-le > largeurTexte(droite)+12 {
		ld = largeurTexte(droite) + 2
		e.texte(x+l-ld+2, y, 0, droite, a.st(p.muet, fond))
	}
	e.texte(x+le, y, l-le-ld, titreMissionTUI(m), a.st(p.encre, fond))
	id := m.ID
	a.zone(x-1, y, l+2, 1, func() { a.ouvrirMission(id) })
}

func (a *appTUI) ouvrirMission(id string) {
	a.allerPage(pMissions)
	a.misOuverte = id
}

// largeurEtatsTUI: the state column, as wide as the widest state shown.
func (a *appTUI) largeurEtatsTUI(l []Mission) int {
	le := 0
	for _, m := range l {
		le = max(le, largeurTexte(a.t(firstNonEmpty(etatsMissionTUI[m.Etat], m.Etat)))+2)
	}
	return min(le, 16)
}

func (a *appTUI) pageMissions(e *ecranT, x, y, l, h int) {
	p, t := a.pal, a.t
	if m := a.missionChoisie(); m != nil && a.misOuverte != "" {
		a.detailMission(e, x, y, l, h, *m)
		return
	}
	a.misOuverte = ""
	ix, iy, il, ih := a.boite(e, x, y, l, h, t("Missions")+" ("+itoa64(int64(len(a.missions)))+")", false)
	if len(a.missions) == 0 {
		e.texte(ix, iy, il, "◇ "+t("No mission yet"), a.gras(p.encre, p.panneau))
		e.texte(ix+2, iy+1, il-2, t("One sentence in the home bar of Athanor's window, and Studio and Servers work together."), a.st(p.muet, p.panneau))
		return
	}
	a.selMis = min(max(0, a.selMis), len(a.missions)-1)
	debut := max(0, a.selMis-ih+1) // the selected line always shown
	le := a.largeurEtatsTUI(a.missions)
	for i := debut; i < len(a.missions) && i-debut < ih; i++ {
		fond := p.panneau
		if i == a.selMis {
			fond = p.ligne
			e.rect(ix-1, iy+i-debut, il+2, 1, a.st(p.encre, fond))
			e.mettre(ix-1, iy+i-debut, '▌', a.st(p.accent, fond))
		}
		a.ligneMissionTUI(e, ix, iy+i-debut, il, le, a.missions[i], fond)
	}
}

// detailMission: one mission: its title, state and policy, its sentence, its lanes, its relay, its journal.
func (a *appTUI) detailMission(e *ecranT, x, y, l, h int, m Mission) {
	p, t := a.pal, a.t
	fin := y + h
	etat := t(firstNonEmpty(etatsMissionTUI[m.Etat], m.Etat))
	n := e.texte(x+1, y, l-2, "← "+titreMissionTUI(m), a.gras(p.encre, p.fond))
	a.zone(x+1, y, 2, 1, func() { a.misOuverte = "" })
	e.texte(x+n+3, y, l-n-4, "["+etat+"]", a.gras(a.couleurEtatMission(m.Etat), p.fond))
	if pol := politiquesTUI[m.Politique]; pol != "" {
		e.texte(x+1, y+1, l-2, t("Policy: {0}", t(pol)), a.st(p.muet, p.fond))
	}
	e.texte(x+1, y+2, l-2, strings.Join(strings.Fields(m.Phrase), " "), a.st(p.muet, p.fond))
	yc := y + 3
	// the lanes: two lines each (state, then the milestone and the activity)
	hv := min(2+max(1, 2*len(m.Voies)), fin-yc)
	if hv >= 3 {
		ix, iy, il, ih := a.boite(e, x, yc, l, hv, t("Lanes"), false)
		if len(m.Voies) == 0 {
			e.texte(ix, iy, il, t("The lanes start when the mission is launched."), a.st(p.muet, p.panneau))
		}
		for i, v := range m.Voies {
			if 2*i >= ih {
				break
			}
			nom, et, plus := a.texteVoieTUI(v)
			ln := e.texte(ix, iy+2*i, il, nom, a.gras(p.encre, p.panneau))
			e.texte(ix+ln+2, iy+2*i, il-ln-2, et, a.st(p.accent, p.panneau))
			if plus != "" && 2*i+1 < ih {
				e.texte(ix+2, iy+2*i+1, il-2, plus, a.st(p.muet, p.panneau))
			}
		}
		yc += hv
	}
	if m.Relais != nil {
		frise, urls := a.friseTUI(m)
		hr := 3
		if urls != "" {
			hr = 4
		}
		if hr = min(hr, fin-yc); hr >= 3 {
			ix, iy, il, _ := a.boite(e, x, yc, l, hr, t("Relay"), false)
			e.texte(ix, iy, il, frise, a.st(p.encre, p.panneau))
			if hr == 4 {
				e.texte(ix, iy+1, il, urls, a.st(p.accent, p.panneau))
			}
			yc += hr
		}
	}
	// the journal: the 10 newest lines, the newest first; attention in amber, error in red
	if hj := min(2+max(1, min(10, len(m.Journal))), fin-yc); hj >= 3 {
		ix, iy, il, ih := a.boite(e, x, yc, l, hj, t("Journal")+" ("+itoa64(int64(len(m.Journal)))+")", false)
		if len(m.Journal) == 0 {
			e.texte(ix, iy, il, t("No event yet: the journal fills in as the lanes move on."), a.st(p.muet, p.panneau))
		}
		nj := min(10, ih, len(m.Journal))
		quand, lq := make([]string, nj), 0 // the time today, else the day and the time
		for i := range nj {
			if d, err := time.Parse(time.RFC3339Nano, m.Journal[len(m.Journal)-1-i].T); err == nil {
				d = d.Local()
				if quand[i] = d.Format("15:04"); d.Format(time.DateOnly) != time.Now().Format(time.DateOnly) {
					quand[i] = d.Format("02/01 15:04")
				}
			}
			lq = max(lq, largeurTexte(quand[i])+2)
		}
		for i := range nj {
			j := m.Journal[len(m.Journal)-1-i]
			coul := p.encre
			switch j.Niveau {
			case "attention":
				coul = p.attention
			case "erreur":
				coul = p.erreur
			}
			e.texte(ix, iy+i, lq, quand[i], a.st(p.muet, p.panneau))
			e.texte(ix+lq, iy+i, il-lq, a.texteJournalTUI(j), a.st(coul, p.panneau))
		}
	}
}

func (a *appTUI) raccourcisMissions() []raccourci {
	t := a.t
	var r []raccourci
	if a.misOuverte != "" {
		r = append(r, raccourci{"Esc", t("Back")})
	} else if len(a.missions) > 0 {
		r = append(r, raccourci{"Enter", t("Open")})
	}
	if m := a.missionChoisie(); m != nil {
		switch m.Etat {
		case "lancee":
			r = append(r, raccourci{"p", t("Pause")})
		case "en_pause", "arretee", "arret_demande": // M250: a stopped mission can be resumed too
			r = append(r, raccourci{"r", t("Resume")})
		}
		if m.Etat == "lancee" || m.Etat == "en_pause" || m.Etat == "arret_demande" {
			r = append(r, raccourci{"a", t("Stop")})
		}
	}
	return r
}

// toucheMissions: the Missions page's keys; p, r and a act on the mission open, else on the one selected.
func (a *appTUI) toucheMissions(k string) {
	m := a.missionChoisie()
	relire := func(map[string]any) { a.derniere["missions"] = time.Time{} }
	switch k {
	case "entree":
		if m != nil {
			a.misOuverte = m.ID
		}
	case "echap":
		a.misOuverte = ""
	case "haut", "k":
		a.selMis--
	case "bas", "j":
		a.selMis++
	case "p":
		if m != nil && m.Etat == "lancee" {
			a.agir("POST", "missions/"+m.ID+"/pause", nil, relire)
		}
	case "r":
		if m != nil && (m.Etat == "en_pause" || m.Etat == "arretee" || m.Etat == "arret_demande") {
			id := m.ID
			a.formulaire(a.t("Resume"), a.t("Resuming the mission: retype Athanor's password.")+"  "+a.t(aideFormulaireLigne),
				[]champTUI{{lib: a.t("Password"), h: 1, s: &saisieT{masque: true}}}, func(v []string) {
					a.agirFormulaire("POST", "missions/"+id+"/reprendre", map[string]string{"mot_de_passe": v[0]}, relire)
				})
		}
	case "a":
		if m != nil && (m.Etat == "lancee" || m.Etat == "en_pause" || m.Etat == "arret_demande") {
			id := m.ID
			a.demander(a.t("Stop the mission"), a.t("Stop the mission? Nothing will be undone: the environments and the accesses stay."),
				[]choixTUI{{id: "oui", texte: a.t("Stop the mission"), danger: true}, {id: "", texte: a.t("Keep it going")}}, func(id2 string) {
					if id2 == "oui" {
						a.agir("POST", "missions/"+id+"/arreter", nil, relire)
					}
				})
		}
	}
}
