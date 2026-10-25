package main

// The pilot's side of several computers on one project (pcs.go): before each cycle it merges what the
// others pushed, chooses a milestone no other online computer holds (its own words first), takes it, and
// tells Claude who builds what; after the cycle it pushes, merging again if the others pushed meanwhile.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	jalonCadragePC   = "cadrage"   // writing the plan: one computer only
	jalonLivraisonPC = "livraison" // the final report: one computer only
)

// equipePC: the team of the pilot's project, for one cycle.
type equipePC struct {
	projet, d, miroir string
	r                 reglagesGH
	moi               MachinePC
	pcs               []PresencePC // the others, read last
}

// ouvrirEquipePC: the project's team (nil: one computer, as always).
func ouvrirEquipePC(projet string) (*equipePC, error) {
	d := dossierGH(projet)
	r, lie := lireReglagesGH(d)
	if !lie || !r.Equipe {
		return nil, nil
	}
	d, miroir, r, err := contexteGH(projet)
	if err != nil {
		return nil, err
	}
	return &equipePC{projet: projet, d: d, miroir: miroir, r: r, moi: lireMachinePC()}, nil
}

func (e *equipePC) verrou() func() {
	v := verrouGH(e.d, 120*time.Second)
	return func() {
		if v != nil {
			v.Close()
		}
	}
}

// actifs: the other computers online whose pilot runs.
func (e *equipePC) actifs() []PresencePC {
	var v []PresencePC
	for _, p := range e.pcs {
		if p.EnLigne && p.Pilote {
			v = append(v, p)
		}
	}
	return v
}

// lire: the others' presences, and the branch's last commits, from GitHub.
func (e *equipePC) lire() error {
	err := recupererPCs(e.d, e.miroir, e.r)
	e.pcs = lirePCs(e.miroir, e.moi.ID)
	return err
}

// recevoir: GitHub read, and what the others pushed merged into the project.
func (e *equipePC) recevoir() (string, string, error) {
	libere := e.verrou()
	defer libere()
	if err := e.lire(); err != nil {
		return recuErreur, "", err
	}
	res, detail := recevoirPC(e.projet, e.d, e.miroir, e.r)
	return res, detail, nil
}

// publier: this computer's presence now (pilot running, its milestone).
func (e *equipePC) publier() error {
	_ = ecrireFichierPrive(e.d+"/pcs-pilote", "1\n")
	return publierPresencePC(e.d, e.miroir, e.r, presenceLocalePC(e.projet, e.d))
}

// prendre: the milestone taken for this computer, said on GitHub, then checked: another computer that
// took it before (or at the same second with a smaller id) keeps it, and this one lets it go.
func (e *equipePC) prendre(j Jalon) (bool, error) {
	libere := e.verrou()
	defer libere()
	local := lireEtatLocalPC(e.d)
	depuis := time.Now().UTC().Format(time.RFC3339)
	if local.Jalon == j.ID && local.Depuis != "" {
		depuis = local.Depuis // the same milestone as the previous cycle (a relay): taken since then
	}
	titre := strings.ReplaceAll(coupeOctets(j.Titre, 200), "\t", " ")
	if err := ecrireFichierPrive(e.d+"/pcs-jalon", j.ID+"\t"+titre+"\t"+depuis+"\n"); err != nil {
		return false, err
	}
	if err := e.publier(); err != nil {
		return true, err // GitHub unreachable: this computer goes on, as when it works alone
	}
	time.Sleep(attentePrisePC)
	if err := e.lire(); err != nil {
		return true, nil
	}
	for _, p := range e.actifs() {
		if p.Jalon == j.ID && (p.Depuis < depuis || (p.Depuis == depuis && p.ID < e.moi.ID)) {
			removeSiExiste(e.d + "/pcs-jalon")
			_ = e.publier()
			return false, nil
		}
	}
	return true, nil
}

// attentePrisePC: how long a computer waits after taking a milestone before it checks no other took it
// at the same moment (their beats cross in that time).
var attentePrisePC = 5 * time.Second

// lacher: this computer holds no milestone any more (the pilot stops).
func (e *equipePC) lacher() {
	libere := e.verrou()
	defer libere()
	removeSiExiste(e.d + "/pcs-jalon")
	_ = ecrireFichierPrive(e.d+"/pcs-pilote", "0\n")
	_ = publierPresencePC(e.d, e.miroir, e.r, presenceLocalePC(e.projet, e.d))
}

// choisirJalonPC: the milestone this computer builds next, or wait (the others hold everything left):
//   - the one it already holds (a relay goes on), if it is still to do;
//   - otherwise a milestone to do whose dependencies are ticked and that no other running computer holds:
//     those whose "Needs:" match this computer's words first, then those without needs, then those whose
//     needs no running computer has; those whose needs another running computer has are left to it;
//   - no plan yet: the framing; everything ticked: the delivery; each by one computer only.
func choisirJalonPC(plan []Jalon, moi MachinePC, autres []PresencePC, actuel string) (Jalon, bool, string) {
	tenus := map[string]PresencePC{}
	motsAutres := map[string]bool{}
	for _, p := range autres {
		if !(p.EnLigne && p.Pilote) {
			continue
		}
		if p.Jalon != "" {
			tenus[p.Jalon] = p
		}
		for _, m := range p.Mots {
			motsAutres[m] = true
		}
	}
	mesMots := map[string]bool{}
	for _, m := range moi.Mots {
		mesMots[m] = true
	}
	if len(plan) == 0 {
		if p, ok := tenus[jalonCadragePC]; ok {
			return Jalon{}, true, fmt.Sprintf("%s writes the plan", p.Nom)
		}
		return Jalon{ID: jalonCadragePC, Titre: "Framing"}, false, ""
	}
	plan = identifierPlanPC(plan)
	faits := map[string]bool{}
	connus := map[string]bool{}
	for _, j := range plan {
		connus[j.ID] = true
		if j.Fait {
			faits[j.ID] = true
		}
	}
	type candidat struct {
		j        Jalon
		priorite int
		rang     int
	}
	var cands []candidat
	restants := 0
	for rang, j := range plan {
		if j.Fait {
			continue
		}
		restants++
		if _, pris := tenus[j.ID]; pris {
			continue
		}
		pret := true
		for _, dep := range j.Depend {
			if connus[dep] && !faits[dep] {
				pret = false
			}
		}
		if !pret {
			continue
		}
		priorite := 1
		if len(j.Besoins) > 0 {
			priorite = 2
			laisse := false
			for _, b := range j.Besoins {
				if mesMots[b] {
					priorite = 0
					break
				}
				if motsAutres[b] {
					laisse = true
				}
			}
			if priorite != 0 && laisse {
				continue // another running computer has what it needs
			}
		}
		if j.ID == actuel {
			priorite = -1
		}
		cands = append(cands, candidat{j, priorite, rang})
	}
	if len(cands) > 0 {
		sort.SliceStable(cands, func(a, b int) bool {
			if cands[a].priorite != cands[b].priorite {
				return cands[a].priorite < cands[b].priorite
			}
			return cands[a].rang < cands[b].rang
		})
		return cands[0].j, false, ""
	}
	if restants > 0 {
		var l []string
		for _, p := range autres {
			if p.EnLigne && p.Pilote && p.Jalon != "" {
				l = append(l, p.Nom+": "+p.Jalon)
			}
		}
		sort.Strings(l)
		if len(l) == 0 {
			return Jalon{}, true, "the milestones left wait for others or need what another computer has"
		}
		return Jalon{}, true, "the other computers build " + strings.Join(l, ", ")
	}
	if p, ok := tenus[jalonLivraisonPC]; ok {
		return Jalon{}, true, fmt.Sprintf("%s writes the final report", p.Nom)
	}
	return Jalon{ID: jalonLivraisonPC, Titre: "Delivery"}, false, ""
}

// consignePCs: what Claude is told about the team, each cycle.
func consignePCs(moi MachinePC, autres []PresencePC, j Jalon) string {
	var b strings.Builder
	b.WriteString("\n\n## Several computers build this project\n\n")
	fmt.Fprintf(&b, "This computer: **%s**", moi.Nom)
	if moi.Capacites != "" {
		fmt.Fprintf(&b, " — %s", moi.Capacites)
	}
	if len(moi.Mots) > 0 {
		fmt.Fprintf(&b, " (words: %s)", strings.Join(moi.Mots, ", "))
	}
	b.WriteString(".\n")
	mots := map[string]bool{}
	for _, m := range moi.Mots {
		mots[m] = true
	}
	if len(autres) > 0 {
		b.WriteString("The other computers of the team (same GitHub repository):\n")
		for _, p := range autres {
			etat := "offline"
			if p.EnLigne && p.Pilote && p.Jalon != "" {
				etat = "building " + p.Jalon
				if p.JalonTitre != "" {
					etat += " (" + p.JalonTitre + ")"
				}
			} else if p.EnLigne && p.Pilote {
				etat = "running"
			} else if p.EnLigne {
				etat = "online, its pilot stopped"
			}
			fmt.Fprintf(&b, "- **%s**", p.Nom)
			if p.Capacites != "" {
				fmt.Fprintf(&b, " — %s", p.Capacites)
			}
			if len(p.Mots) > 0 {
				fmt.Fprintf(&b, " (words: %s)", strings.Join(p.Mots, ", "))
			}
			fmt.Fprintf(&b, ": %s.\n", etat)
			for _, m := range p.Mots {
				mots[m] = true
			}
		}
	}
	b.WriteString("\nHow you work together:\n")
	switch j.ID {
	case jalonCadragePC:
		b.WriteString("- You write the plan for the whole team: the others wait for it.\n")
	case jalonLivraisonPC:
		b.WriteString("- Every milestone is ticked: you write the final report for the whole team.\n")
	default:
		fmt.Fprintf(&b, "- The pilot gave you milestone **%s**; the other computers build theirs at the same time. Change only what yours needs; never tick, edit or redo a milestone another computer builds.\n", j.ID)
	}
	b.WriteString("- Commit as you go. Between cycles the pilot merges the others' pushed work into the project and pushes yours: never push yourself, never rewrite commits already pushed. If `.athanor/MERGE_NEEDED.md` exists, do that merge first.\n")
	ml := make([]string, 0, len(mots))
	for m := range mots {
		ml = append(ml, m)
	}
	sort.Strings(ml)
	if len(ml) > 0 {
		fmt.Fprintf(&b, "- When you write or change the plan: a milestone that needs what only some computers have gets `Needs: <word>.` in its line, with the team's words (%s); the pilot gives it to a computer that has it. ", strings.Join(ml, ", "))
	} else {
		b.WriteString("- When you write or change the plan: ")
	}
	b.WriteString("Split big work of the same kind (several variants of a model, several levels, several pages) into separate milestones with exact `Depends on:`, so that several computers build them at once.\n")
	return b.String()
}

// butJalonPC: the cycle's goal when the pilot gave a milestone.
func butJalonPC(j Jalon, autres []PresencePC, tours int, langue string) string {
	var l []string
	for _, p := range autres {
		if p.EnLigne && p.Pilote && p.Jalon != "" && p.Jalon != jalonCadragePC && p.Jalon != jalonLivraisonPC {
			l = append(l, p.Jalon)
		}
	}
	autresTxt := ""
	if len(l) > 0 {
		sort.Strings(l)
		autresTxt = " Other computers of the team build " + strings.Join(l, ", ") + " at the same time: never work on those, never tick them."
	}
	return fmt.Sprintf("/goal Milestone %s of .athanor/PLAN.md is completed and verified according to your instructions: bash .athanor/check.sh has been run and its output, showing success, is displayed in this conversation; .athanor/PLAN.md ticks this milestone; .athanor/LOG.md has a new entry; git log shows the commit.%s If .athanor/MERGE_NEEDED.md exists, that merge is done first. If .athanor/FOR_YOU_ANSWERED exists, every answered task is handled first (athanor todo ok, missing or resolved); if %s waits for a task of the user (athanor todo list), the goal is reached once athanor todo wait has run. Or stop after %d turns. %s",
		j.ID, autresTxt, j.ID, tours, langue)
}

// ---------------------------------------------------------------------------- in the pilot's loop

// avantCyclePC: several computers: the others' work merged, a milestone taken. attendre: nothing to take
// now (the message says why); the pilot waits a minute and looks again.
func (p *pilote) avantCyclePC() (attendre bool, message string) {
	p.jalonPC, p.equipePC = nil, nil
	e, err := ouvrirEquipePC(p.projet)
	if e != nil && !p.battementPC {
		p.battementPC = true
		go p.battrePC()
	}
	if e == nil {
		if err != nil {
			p.logUneFois("pcs-erreur", "Several computers: "+err.Error()+" (this computer works alone meanwhile).")
		}
		return false, ""
	}
	res, detail, err := e.recevoir()
	switch {
	case err != nil:
		p.logUneFois("pcs-reseau", "Several computers: GitHub cannot be read ("+err.Error()+"): this computer works alone until it can.")
		return false, ""
	case res == recuFusion:
		p.log(fmt.Sprintf("Several computers: %s commit(s) of the other computers merged into the project.", detail))
	case res == recuConflit:
		p.log("Several computers: " + detail + ".")
	case res == recuSale:
		p.log("Several computers: changes not committed in the project: the others' work is merged after the next commit.")
	case res == recuRejoint:
		p.log(fmt.Sprintf("Several computers: this computer joins the team: the project starts from the other computers' work (%s).", detail))
	case res == recuBloque:
		_ = e.publier()
		return true, detail
	}
	p.oublierLogUneFois("pcs-reseau")
	if res == recuFusion && p.estTermine() {
		// another computer delivered: this one ends as it would have (at the top of the loop)
		return true, "another computer delivered the project"
	}
	actuel := lireEtatLocalPC(e.d).Jalon
	var plan []Jalon
	if b, err := os.ReadFile(filepath.Join(p.etat, "PLAN.md")); err == nil && p.planPret() {
		plan = analyserPlan(string(b))
	}
	if fichierRegulierPilote(filepath.Join(p.etat, "MERGE_NEEDED.md")) {
		// the others' work not merged yet: the milestone chosen from their plan too, never from this one alone
		// (it would frame again, or build a milestone they finished)
		plan = planAvecLeurPC(plan, e)
	}
	for essai := 0; essai < 3; essai++ {
		j, att, raison := choisirJalonPC(plan, e.moi, e.pcs, actuel)
		if att {
			removeSiExiste(e.d + "/pcs-jalon") // waiting: it builds nothing
			_ = e.publier()
			return true, raison
		}
		pris, err := e.prendre(j)
		if err != nil {
			p.logUneFois("pcs-publier", "Several computers: "+err.Error())
		}
		if pris {
			p.jalonPC, p.equipePC = &j, e
			if j.ID != actuel {
				p.log(fmt.Sprintf("Several computers: this computer takes %s.", libellePC(j)))
			}
			return false, ""
		}
		p.log(fmt.Sprintf("Several computers: %s was taken by another computer at the same moment: another one.", j.ID))
		actuel = ""
	}
	return true, "the other computers took the milestones left"
}

// planAvecLeurPC: this computer's plan with the team's (as GitHub has it): their milestones added, the
// ones ticked on either side ticked.
func planAvecLeurPC(plan []Jalon, e *equipePC) []Jalon {
	b, err := gTravailGH(e.projet, "cat-file", "blob", "refs/remotes/athanor/"+firstNonEmpty(e.r.Branche, "main")+":.athanor/PLAN.md").Output()
	if err != nil {
		return plan
	}
	leur := analyserPlan(string(b))
	if len(plan) == 0 {
		return leur
	}
	faits := map[string]bool{}
	for _, j := range leur {
		if j.Fait {
			faits[j.ID] = true
		}
	}
	v := append([]Jalon{}, plan...)
	connus := map[string]bool{}
	for i := range v {
		connus[v[i].ID] = true
		if faits[v[i].ID] {
			v[i].Fait = true
		}
	}
	for _, j := range leur {
		if !connus[j.ID] {
			v = append(v, j)
		}
	}
	return v
}

// identifierPlanPC: a milestone line without an identifier ("Build the homepage") gets its title as one,
// the same on every computer, so that it is shared out (and counted: never a delivery while it is left).
func identifierPlanPC(plan []Jalon) []Jalon {
	v := append([]Jalon{}, plan...)
	for i := range v {
		if v[i].ID == "" {
			t := strings.Join(strings.Fields(v[i].Titre), " ")
			t = strings.TrimSpace(strings.TrimLeft(t, "-*0123456789.)[]xX "))
			v[i].ID = "«" + coupeValide(t, 60) + "»"
		}
	}
	return v
}

func libellePC(j Jalon) string {
	switch j.ID {
	case jalonCadragePC:
		return "the framing (the plan, for the whole team)"
	case jalonLivraisonPC:
		return "the delivery (the final report)"
	}
	return firstNonEmpty(j.Titre, j.ID)
}

// rejoindrePC: before the project's first commit, the team's work when there is one; false when the
// folder cannot take it (the project must then not get a history of its own).
func (p *pilote) rejoindrePC() bool {
	e, err := ouvrirEquipePC(p.projet)
	if e == nil {
		if err != nil {
			p.log("Several computers: " + err.Error() + ".")
		}
		return true
	}
	switch res, detail, err := e.recevoir(); {
	case err != nil:
		p.log("Several computers: GitHub cannot be read (" + err.Error() + "): the project starts on its own.")
	case res == recuRejoint:
		p.log(fmt.Sprintf("Several computers: this computer joins the team: the project starts from the other computers' work (%s).", detail))
	case res == recuBloque:
		p.log("Several computers: " + detail + ".")
		return false
	case res == recuErreur:
		p.log("Several computers: " + detail + ".")
	}
	return true
}

// pousserPC: the push of a team: the others' work merged first; pushed again once if they pushed meanwhile.
func (p *pilote) pousserPC() (int, bool) {
	if p.equipePC == nil {
		if e, _ := ouvrirEquipePC(p.projet); e != nil {
			p.equipePC = e
		} else {
			return 0, false
		}
	}
	e := p.equipePC
	if res, detail, err := e.recevoir(); err == nil && res == recuFusion {
		p.log(fmt.Sprintf("Several computers: %s commit(s) of the other computers merged before the push.", detail))
	} else if err == nil && (res == recuConflit || res == recuBloque) {
		p.log("Several computers: " + detail + ".")
	}
	var out strings.Builder
	code := pousserGH([]string{p.projet, "--auto"}, &out)
	if code == 5 && strings.Contains(out.String(), "has commits the project does not have") {
		if res, _, err := e.recevoir(); err == nil && res == recuFusion {
			out.Reset()
			code = pousserGH([]string{p.projet, "--auto"}, &out)
		}
	}
	if code != 3 {
		lignes := strings.Split(strings.TrimRight(out.String(), "\r\n"), "\n")
		p.log(tronquerRunes(lignes[len(lignes)-1], 300))
	}
	_ = e.publier() // its last push, for the others
	return code, true
}

// finPC: the pilot stops: its milestone is free for the others.
func (p *pilote) finPC() {
	p.finiPC.Store(true) // its beat stops: it no longer says its pilot runs
	if e, _ := ouvrirEquipePC(p.projet); e != nil {
		e.lacher()
	}
}

// battrePC: while the pilot runs, its presence every minute, even when Athanor's window is closed (the
// others would otherwise see it gone after 3 minutes and take its milestone).
func (p *pilote) battrePC() {
	for !p.finiPC.Load() {
		time.Sleep(battementPC)
		if p.finiPC.Load() {
			return
		}
		e, err := ouvrirEquipePC(p.projet)
		if e == nil || err != nil {
			continue
		}
		libere := e.verrou()
		_ = e.publier()
		libere()
	}
}

var battementPC = time.Minute
