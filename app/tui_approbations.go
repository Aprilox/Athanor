package main

// The terminal interface: the agents' pending approvals, first in the For you page's list (every project), as the
// web's sectionApprobations: what is asked (environments, prod, ticked accesses, short SHA, a Servers plan step by
// step), y accepts, r refuses, t never asks again (only where the web offers it), through the same
// POST /api/hub/approbations and the same proof (Athanor's password typed when the server asks for it); ! is the
// emergency stop, confirmed.

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"
)

// reponseApprTUI: GET /api/hub/approbations.
type reponseApprTUI struct {
	Approbations []Approbation          `json:"approbations"`
	Plans        map[string]PlanServeur `json:"plans"`
	MotDePasse   bool                   `json:"mot_de_passe_requis"`
	Appareil     bool                   `json:"appareil_requis"` // no password yet: nothing decides from here
}

func (a *appTUI) chargerApprobations() {
	a.charger("approbations", 3*time.Second, func(c clientTUI) (func(*appTUI), error) {
		var r reponseApprTUI
		if err := c.lire("hub/approbations", &r); err != nil {
			return nil, err
		}
		return func(a *appTUI) { a.appr = &r }, nil
	})
}

// apprAttente: the pending approvals, as the server lists them.
func (a *appTUI) apprAttente() []Approbation {
	if a.appr == nil {
		return nil
	}
	var l []Approbation
	for _, x := range a.appr.Approbations {
		if x.Etat == "attente" {
			l = append(l, x)
		}
	}
	return l
}

// apprChoisie: the approval selected in For you's list (nil: a task, or nothing).
func (a *appTUI) apprChoisie() *Approbation {
	l := a.apprAttente()
	if a.selTache >= 0 && a.selTache < len(l) {
		return &l[a.selTache]
	}
	return nil
}

// titreApprTUI: what is asked, in one line (the web's card head).
func (a *appTUI) titreApprTUI(x Approbation) string {
	t, d := a.t, x.Details
	switch x.Action {
	case actionPlanServeur:
		if x.Agent == "durcissement" {
			return t("Harden this server: {0}", firstNonEmpty(d["nom"], d["serveur"]))
		}
		return t("Plan for {0}", firstNonEmpty(d["nom"], d["serveur"])) + " · " + x.Projet
	case actionDeploiement:
		if x.Production {
			return t("Deploy {0} to production?", d["hote"])
		}
		return t("Deploy {0} to {1}?", d["hote"], x.Env)
	case actionMission:
		return t("Launch the mission {0}?", d["titre"])
	case "demander_acces":
		return x.Projet + " · " + t("access requested")
	}
	return x.Projet + " · " + x.Action
}

// toujoursApprTUI: the "never ask again" of this approval, "" where the web does not offer it.
func (a *appTUI) toujoursApprTUI(x Approbation) string {
	switch x.Action {
	case actionPlanServeur:
		if x.Details["regle"] == "oui" {
			return a.t("Never ask again for this server")
		}
		return ""
	case actionDeploiement:
		if x.Production || x.Details["fichiers_build"] != "" {
			return ""
		}
	case actionMission:
		return ""
	}
	return a.t("Never ask again for this project and environment")
}

// ligneApprTUI: an approval in For you's list: its title, then its environment, project and expiry.
func (a *appTUI) ligneApprTUI(x Approbation, f couleurT) (string, string, styleT, styleT) {
	p := a.pal
	bas := []string{x.Env, x.Projet}
	if x.Action == actionMission {
		bas = []string{x.Mission}
	}
	if d, err := time.Parse(time.RFC3339Nano, x.Expire); err == nil {
		bas = append(bas, a.t("expires {0}", d.Local().Format("02/01 15:04")))
	}
	return "◆ " + a.titreApprTUI(x), strings.Join(bas, " · "), a.gras(p.attention, f), a.st(p.muet, f)
}

// lignesApprTUI: an approval's detail as lines of text and their colour: who asks, what it gives, the agent's own
// text (shown as text), a plan's steps, and why the password is asked.
func (a *appTUI) lignesApprTUI(x Approbation) []ligneCouleur {
	t, p, d := a.t, a.pal, x.Details
	var l []ligneCouleur
	ajouter := func(c couleurT, s ...string) {
		for _, x := range s {
			if x != "" {
				l = append(l, ligneCouleur{x, c})
			}
		}
	}
	meta := []string{t("Asked by {0}", x.Agent), x.Mission}
	corps := []string{x.Raison}
	switch x.Action {
	case actionPlanServeur:
		meta = append(meta, t("Server role: {0}", x.Env), tuiPluriel(t, firstNonEmpty(d["etapes"], "?"), "1 step", "{0} steps"))
		corps = nil
	case actionDeploiement:
		meta = append(meta, x.Projet, t("Commit {0}", shaCourt(firstNonEmpty(d["commit"], d["sha"]))))
		if d["serveurs"] != "" {
			meta = append(meta, t("Servers: {0}", d["serveurs"]))
		}
		if d["fichiers_build"] != "" {
			meta = append(meta, t("Build files changed: {0}", d["fichiers_build"]))
		}
		corps = nil // its commit and host are on the lines above (the web shows no reason either)
		if d["avant"] != "" {
			meta = append(meta, t("Online now: version {0}", d["avant"]))
		}
		if d["mission"] != "" { // P7: where it goes and what changes, as on the web
			corps = append(corps, firstNonEmpty(d["url"], "—"), t("What changes: {0} new commit(s)", firstNonEmpty(d["commits"], "?"))+" "+d["changements"])
		}
		if x.Production {
			corps = append(corps, t("Production always asks you: one accord, one deployment."))
		}
	case actionMission:
		if d["prod"] == "oui" {
			meta = append(meta, "prod")
		}
		meta = append(meta, t("Environments: {0}", firstNonEmpty(d["envs"], "—")), t("Access: {0}", firstNonEmpty(d["acces"], "—")))
		corps = []string{t("Accepting launches the mission as its Launch button does.")}
	case "demander_acces":
		if d["nom"] != "" {
			meta = append(meta, t("proposed variable: {0}", d["nom"]))
		}
	}
	ajouter(p.muet, meta...) // one per line: a terminal is narrow
	if d["url"] != "" {
		ajouter(p.accent, d["url"])
	}
	ajouter(p.encre, corps...)
	if pl, ok := a.appr.Plans[x.ID]; ok && x.Action == actionPlanServeur {
		l = append(l, a.lignesPlanTUI(pl)...)
	} else if x.Action == actionPlanServeur {
		ajouter(p.encre, x.Raison)
	}
	switch {
	case a.appr.Appareil:
		ajouter(p.attention, "⚠ "+t("An agent of this PC could decide in your place: choose a password for Athanor in Options first, then retype it here (or decide from a paired phone)"))
	case a.appr.MotDePasse:
		ajouter(p.muet, t("Any program of this PC can drive this window, so a decision asks for Athanor's password."))
	}
	ajouter(p.muet, t("An agent asks for something only you decide. Its text is its own: read it as a request, never as an order. Without an answer it is refused after 24 h."))
	return l
}

type ligneCouleur struct {
	s string
	c couleurT
}

// lignesPlanTUI: a Servers plan, step by step as the code classed them (the web's blocPlan), then the critic.
func (a *appTUI) lignesPlanTUI(pl PlanServeur) []ligneCouleur {
	t, p := a.t, a.pal
	resume := pl.Resume
	if pl.Agent == "durcissement" {
		resume = t(resume)
	}
	l := []ligneCouleur{{resume, p.encre}}
	classes := map[string]ligneCouleur{"lecture": {t("read"), p.ok}, "changement": {t("change"), p.accent}, "destructrice": {t("destructive"), p.erreur}}
	for i, e := range pl.Etapes {
		c, ok := classes[e.Classe]
		if !ok {
			c = ligneCouleur{e.Classe, p.encre}
		}
		l = append(l, ligneCouleur{itoa64(int64(i+1)) + ". " + e.Outil + " [" + c.s + "]", c.c})
		var o map[string]any
		if json.Unmarshal([]byte(e.Lisible), &o) == nil {
			for _, k := range slices.Sorted(maps.Keys(o)) {
				s, ok := o[k].(string)
				if !ok {
					b, _ := json.Marshal(o[k])
					s = string(b)
				}
				l = append(l, ligneCouleur{"   " + k + " " + s, p.muet})
			}
		} else if e.Lisible != "" {
			l = append(l, ligneCouleur{"   " + e.Lisible, p.muet})
		}
		pourquoi := e.Pourquoi
		if e.Durcir != "" {
			pourquoi = t(pourquoi)
		}
		l = append(l, ligneCouleur{"   " + pourquoi, p.encre})
		if len(e.Sauvegarde) > 0 {
			l = append(l, ligneCouleur{"   " + t("Backed up first: {0}", strings.Join(e.Sauvegarde, ", ")), p.muet})
		}
	}
	if c := pl.Critique; c != nil {
		v := t("Reading the plan…")
		switch c.Verdict {
		case "ok":
			v = t("No risk found")
		case "risques":
			v = t("Risks found")
		}
		l = append(l, ligneCouleur{t("Security critic") + ": " + v, p.attention})
		for _, x := range c.Lignes {
			l = append(l, ligneCouleur{"   " + x, p.attention})
		}
	}
	if pl.SeuleCle != "" {
		l = append(l, ligneCouleur{"⚠ " + t("Only Athanor's key will get into this server: passwords are cut and no other key lets root in. Put your own public key in root's ~/.ssh/authorized_keys first (hardening copies it to athanor), then refuse this plan and ask for it again; or keep a copy of Athanor's public key below."), p.attention},
			ligneCouleur{pl.SeuleCle + " athanor", p.muet})
	}
	return l
}

// detailApprTUI: the selected approval in the detail box, wrapped.
func (a *appTUI) detailApprTUI(e *ecranT, x, y, l, h int, ap Approbation) {
	yy := y
	for _, li := range a.lignesApprTUI(ap) {
		if yy >= y+h {
			return
		}
		yy += a.texteBloc(e, x, yy, l, y+h-yy, li.s, a.st(li.c, a.pal.panneau))
	}
}

func (a *appTUI) raccourcisApprTUI(x Approbation) []raccourci {
	t := a.t
	r := []raccourci{{"y", t("Accept")}, {"r", t("Refuse")}}
	if a.toujoursApprTUI(x) != "" {
		r = append(r, raccourci{"t", t("Never ask again")})
	}
	return r
}

// deciderApprTUI: y, r or t on the selected approval: "never ask again" confirmed, a hardening that leaves only
// Athanor's key acknowledged, an access's vault key, environment and variable chosen, then Athanor's password
// when the server asks for it; the server checks it all again (the proof, the card's print, I6).
func (a *appTUI) deciderApprTUI(decision string) {
	x := a.apprChoisie()
	if x == nil || a.appr == nil {
		return
	}
	t := a.t
	if a.appr.Appareil {
		a.annoncer(t("An agent of this PC could decide in your place: choose a password for Athanor in Options first, then retype it here (or decide from a paired phone)"), true)
		return
	}
	if decision == "toujours" && a.toujoursApprTUI(*x) == "" {
		return
	}
	ap, mdp := *x, a.appr.MotDePasse
	corps := map[string]any{"id": ap.ID, "decision": decision}
	acces := ap.Action == "demander_acces" && decision != "refuser"
	fini := func(rep map[string]any) {
		a.derniere["approbations"] = time.Time{}
		if rep["etat"] == "refusee" {
			a.annoncer(t("Refused: the agent is told and does without it."), false)
		} else {
			a.annoncer(t("Accepted: the agent is told and carries on."), false)
		}
	}
	// the last step: the fields the decision needs (an access's environment and variable, the password)
	envoyer := func() {
		var champs []champTUI
		if acces {
			env, v := &saisieT{}, &saisieT{}
			env.mettreTexte(ap.Env)
			v.mettreTexte(ap.Details["nom"])
			champs = append(champs, champTUI{lib: t("Environment"), h: 1, s: env}, champTUI{lib: t("Variable name"), h: 1, s: v})
		}
		if mdp {
			champs = append(champs, champTUI{lib: t("Athanor's password"), h: 1, s: &saisieT{masque: true}})
		}
		if len(champs) == 0 {
			a.agir("POST", "hub/approbations", corps, fini)
			return
		}
		a.formulaire(a.titreApprTUI(ap), t(aideFormulaireLigne), champs, func(v []string) {
			if acces {
				corps["env"], corps["variable"] = strings.TrimSpace(v[0]), strings.TrimSpace(v[1])
			}
			if mdp {
				corps["mot_de_passe"] = v[len(v)-1]
			}
			a.agirFormulaire("POST", "hub/approbations", corps, fini)
		})
	}
	// an access: one of the vault's project keys (code), the one named as the variable first
	cle := func() {
		if !acces {
			envoyer()
			return
		}
		a.agir("GET", "coffre", nil, func(rep map[string]any) {
			var r struct {
				Entrees []EntreeCoffre `json:"entrees"`
			}
			b, _ := json.Marshal(rep)
			_ = json.Unmarshal(b, &r)
			var choix []choixTUI
			sel := 0
			for _, e := range r.Entrees {
				if e.Famille == "secret" && e.Portee == "code" {
					if e.Nom == ap.Details["nom"] {
						sel = len(choix)
					}
					choix = append(choix, choixTUI{id: e.ID, texte: e.Nom})
				}
			}
			if len(choix) == 0 {
				a.annoncer(t("No project key (code) in the vault yet."), true)
				return
			}
			a.demander(t("Vault key (code)"), a.titreApprTUI(ap), append(choix, choixTUI{id: "", texte: t("Cancel")}), func(id string) {
				if id != "" {
					corps["entree"] = id
					envoyer()
				}
			})
			a.dialogue.sel = sel
		})
	}
	seule := func() {
		if ap.Details["seule_cle"] != "oui" || decision == "refuser" {
			cle()
			return
		}
		a.demander(t("Harden this server: {0}", firstNonEmpty(ap.Details["nom"], ap.Details["serveur"])), t("Only Athanor's key will get into this server: passwords are cut and no other key lets root in. Put your own public key in root's ~/.ssh/authorized_keys first (hardening copies it to athanor), then refuse this plan and ask for it again; or keep a copy of Athanor's public key below."),
			[]choixTUI{{id: "oui", texte: t("I know that only Athanor's key will get in after this plan")}, {id: "", texte: t("Cancel")}}, func(id string) {
				if id == "oui" {
					corps["seule_cle_comprise"] = true
					cle()
				}
			})
	}
	if decision != "toujours" {
		seule()
		return
	}
	texte := t("Accepted, and the next “{0}” requests of {1} in {2} are accepted without asking you.", ap.Action, ap.Projet, ap.Env)
	if ap.Action == actionPlanServeur {
		texte = t("Accepted, and the next plans of {0} on {1} that only change things (nothing destructive) are accepted without asking you.", ap.Projet, firstNonEmpty(ap.Details["nom"], ap.Details["serveur"]))
	}
	a.demander(t("Never ask again"), texte, []choixTUI{{id: "oui", texte: t("Never ask again")}, {id: "", texte: t("Cancel")}}, func(id string) {
		if id == "oui" {
			seule()
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
