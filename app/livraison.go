package main

// A delivered project (FINAL_REPORT.md with STATUS: DONE) reopened by what arrived after its delivery:
// a request in the queue (prochaineDemande), but also a note or an edit in REQUEST.md, milestones added to
// the plan and not ticked, tasks of For you answered. When the pilot ends on a delivery, it keeps what it
// delivered against (done/.livraison.json, and a copy of REQUEST.md): the next start compares with it, so
// the same delivery is never reopened twice for the same thing (a Claude that leaves a line unticked does
// not run in a loop). A delivery made before this record existed is compared by the files' dates.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type empreinteLivraison struct {
	Demande  string `json:"demande"`  // sha256 of REQUEST.md
	Plan     string `json:"plan"`     // sha256 of PLAN.md's unticked lines ("" when none)
	Reponses string `json:"reponses"` // the answered tasks of For you not handled yet ("" when none)
	Le       string `json:"le"`
}

// NouveauLivraison: what arrived since the delivery.
type NouveauLivraison struct {
	Demande  bool     // REQUEST.md changed: a note, an edit
	Ajouts   []string // its lines added since the delivery (when the delivered copy is known)
	Plan     int      // unticked milestones added to the plan
	Reponses int      // tasks of For you answered
	Le       string   // the delivery's date (YYYY-MM-DD HH:MM)
}

func (n NouveauLivraison) Vide() bool { return !n.Demande && n.Plan == 0 && n.Reponses == 0 }

func fichierLivraison(etat string) string { return filepath.Join(etat, "done", ".livraison.json") }
func copieDemandeLivree(etat string) string {
	return filepath.Join(etat, "done", ".livraison-REQUEST.md")
}

func hacherTexte(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// lignesNonCochees: PLAN.md's unticked milestones, as written.
func lignesNonCochees(plan string) []string {
	var l []string
	for _, x := range strings.Split(strings.ReplaceAll(plan, "\r", ""), "\n") {
		if t := strings.TrimSpace(x); strings.HasPrefix(t, "- [ ]") {
			l = append(l, t)
		}
	}
	return l
}

// reponsesEnAttente: the answered tasks not handled yet, "id:answers" sorted.
func reponsesEnAttente(projet string) []string {
	f := lireTachesUtil(projet)
	r := lireReponsesUtil(projet)
	var l []string
	for _, t := range f.taches {
		if t.statut == "ok" || t.statut == "resolu" {
			continue
		}
		if n := int64(len(r[t.id])); n > t.tour {
			l = append(l, t.id+":"+itoa64(n))
		}
	}
	sort.Strings(l)
	return l
}

func lireTexteLivraison(f string) string {
	b, _ := os.ReadFile(f)
	return strings.ReplaceAll(string(b), "\r", "")
}

func empreinteActuelle(projet string) empreinteLivraison {
	etat := filepath.Join(projet, ".athanor")
	return empreinteLivraison{
		Demande:  hacherTexte(lireTexteLivraison(filepath.Join(etat, "REQUEST.md"))),
		Plan:     hacherTexte(strings.Join(lignesNonCochees(lireTexteLivraison(filepath.Join(etat, "PLAN.md"))), "\n")),
		Reponses: hacherTexte(strings.Join(reponsesEnAttente(projet), "\n")),
		Le:       time.Now().Format("2006-01-02 15:04"),
	}
}

// noterLivraison: what this delivery was made against (the pilot ends on it).
func noterLivraison(projet string) {
	etat := filepath.Join(projet, ".athanor")
	if os.MkdirAll(filepath.Join(etat, "done"), 0o755) != nil {
		return
	}
	b, _ := json.Marshal(empreinteActuelle(projet))
	_ = ecrireAtomique(fichierLivraison(etat), b, 0o644)
	_ = ecrireAtomique(copieDemandeLivree(etat), []byte(lireTexteLivraison(filepath.Join(etat, "REQUEST.md"))), 0o644)
}

// lignesAjoutees: the lines of nouveau absent from ancien (blank lines and headings left out).
func lignesAjoutees(ancien, nouveau string, max int) []string {
	vu := map[string]bool{}
	for _, l := range strings.Split(ancien, "\n") {
		vu[strings.TrimSpace(l)] = true
	}
	var a []string
	for _, l := range strings.Split(nouveau, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || vu[t] || strings.HasPrefix(t, "#") {
			continue
		}
		vu[t] = true
		if r := []rune(t); len(r) > 400 {
			t = string(r[:400]) + "…"
		}
		a = append(a, t)
		if len(a) == max {
			break
		}
	}
	return a
}

// nouveauDepuisLivraison: what arrived since the project was delivered (nothing when it is not delivered).
func nouveauDepuisLivraison(projet string) NouveauLivraison {
	var n NouveauLivraison
	etat := filepath.Join(projet, ".athanor")
	fr := filepath.Join(etat, "FINAL_REPORT.md")
	b, err := os.ReadFile(fr)
	if err != nil || !reStatusDonePilote.Match(b) {
		return n
	}
	act := empreinteActuelle(projet)
	nonCochees := len(lignesNonCochees(lireTexteLivraison(filepath.Join(etat, "PLAN.md"))))
	nbReponses := len(reponsesEnAttente(projet))
	var e empreinteLivraison
	if x, err := os.ReadFile(fichierLivraison(etat)); err == nil && json.Unmarshal(x, &e) == nil && e.Demande != "" {
		n.Le = e.Le
		n.Demande = act.Demande != e.Demande
		if n.Demande {
			if c, err := os.ReadFile(copieDemandeLivree(etat)); err == nil {
				n.Ajouts = lignesAjoutees(strings.ReplaceAll(string(c), "\r", ""), lireTexteLivraison(filepath.Join(etat, "REQUEST.md")), 30)
			}
		}
		if act.Plan != e.Plan {
			n.Plan = nonCochees
		}
		if act.Reponses != e.Reponses {
			n.Reponses = nbReponses
		}
		return n
	}
	// delivered before the record existed: the files' dates (a minute's margin: a merge writes them together)
	st, err := os.Stat(fr)
	if err != nil {
		return n
	}
	livre := st.ModTime()
	n.Le = livre.Format("2006-01-02 15:04")
	apres := func(f string) bool {
		s, err := os.Stat(filepath.Join(etat, f))
		return err == nil && s.ModTime().After(livre.Add(time.Minute))
	}
	n.Demande = apres("REQUEST.md")
	if apres("PLAN.md") {
		n.Plan = nonCochees
	}
	n.Reponses = nbReponses
	return n
}

// raisonsReouverture: the reasons, in English (the pilot's log and the final report).
func (n NouveauLivraison) raisons() []string {
	var r []string
	if n.Demande {
		r = append(r, "REQUEST.md changed (a note or an edit)")
	}
	if n.Plan > 0 {
		r = append(r, itoa64(int64(n.Plan))+" unticked milestone(s) in PLAN.md")
	}
	if n.Reponses > 0 {
		r = append(r, itoa64(int64(n.Reponses))+" answered task(s) of For you")
	}
	return r
}

// maxReouvertures: the reopenings of one run of the pilot.
const maxReouvertures = 10

// rouvrirApresLivraison: the delivered project reopened by what arrived since (the final report says
// REOPENED, and a note or an edit of REQUEST.md becomes a milestone of the plan); false when nothing came.
func (p *pilote) rouvrirApresLivraison() bool {
	n := nouveauDepuisLivraison(p.projet)
	if n.Vide() {
		return false
	}
	raisons := strings.Join(n.raisons(), ", ")
	// never in a loop: what reopened it is taken in now (the next delivery compares with what comes after),
	// and one run of the pilot reopens a delivery a few times at most
	noterLivraison(p.projet)
	if p.reouvertures++; p.reouvertures > maxReouvertures {
		p.log("Delivered project: reopened " + itoa64(maxReouvertures) + " times by this run already; " + raisons + " waits for the next start.")
		return false
	}
	fr := filepath.Join(p.etat, "FINAL_REPORT.md")
	if b, err := os.ReadFile(fr); err == nil {
		_ = os.WriteFile(fr, reStatusDonePilote.ReplaceAll(b, []byte("STATUS: REOPENED (since the delivery: "+raisons+")")), 0o644)
	}
	plan := filepath.Join(p.etat, "PLAN.md")
	if _, err := os.Stat(plan); err == nil && n.Demande {
		if pf, err := os.OpenFile(plan, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			var m strings.Builder
			m.WriteString("\n- [ ] Resume: what changed in .athanor/REQUEST.md since the delivery of " + n.Le + "\n")
			m.WriteString("  - Re-read .athanor/REQUEST.md (its notes dated after the delivery and any part edited since) and do what changed, as verifiable milestones appended to this plan, the last one being a new delivery (.athanor/FINAL_REPORT.md with STATUS: DONE).\n")
			for _, a := range n.Ajouts {
				m.WriteString("  - New in REQUEST.md: " + a + "\n")
			}
			_, _ = pf.WriteString(m.String())
			pf.Close()
		}
	}
	p.log("Delivered project reopened: " + raisons + ".")
	return true
}
