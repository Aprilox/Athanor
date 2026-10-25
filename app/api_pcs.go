package main

// Several computers on a project (pcs.go), from the application: each minute, for the projects that work
// with other computers, this computer's presence is published and the others' read (athanor github pcs, on
// the pilot's side where the GitHub key is); the Overview shows them, Remote names this computer.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type cachePCs struct {
	vue     VuePCs
	quand   time.Time
	enCours bool
}

var (
	pcsMu    sync.Mutex
	pcsCache = map[string]*cachePCs{} // project name → last answer
)

// lirePCsProjet: athanor github pcs for the project (published too when publier), kept for the pages.
func (s *Serveur) lirePCsProjet(p Projet, publier bool) (VuePCs, error) {
	args := []string{"--pilote", "0"}
	if p.PiloteActif() {
		args[1] = "1"
	}
	if publier {
		args = append(args, "--publier")
	}
	out, err := s.executerGitHub(p, 60*time.Second, "pcs", args...)
	var v VuePCs
	if err == nil {
		err = json.Unmarshal([]byte(derniereLigneJSON(out)), &v)
	}
	if err != nil {
		return v, err
	}
	if v.PCs == nil {
		v.PCs = []PresencePC{}
	}
	if v.Actif {
		v.Notes = notesEquipePC(p.Atelier(), 5)
	}
	pcsMu.Lock()
	ancien := pcsCache[p.Nom]
	pcsCache[p.Nom] = &cachePCs{vue: v, quand: time.Now()}
	pcsMu.Unlock()
	s.notifierRefusPCs(p.Nom, ancien == nil, v)
	return v, nil
}

var (
	refusVusMu sync.Mutex
	refusVus   = map[string]bool{} // project · computer · program · milestone · when: already said
)

// texteRefusNotif: "{program} does not work on {computer}" / "{milestone}: the other computers try in its place".
var texteRefusNotif = map[string][2]string{
	"en": {"%s does not work on %s", "%s: the other computers try in its place."},
	"fr": {"%s ne marche pas sur %s", "%s : les autres ordinateurs essaient à sa place."},
	"es": {"%s no funciona en %s", "%s: los otros ordenadores lo intentan en su lugar."},
	"de": {"%s funktioniert nicht auf %s", "%s: Die anderen Computer versuchen es an seiner Stelle."},
	"it": {"%s non funziona su %s", "%s: gli altri computer provano al suo posto."},
	"pt": {"%s não funciona em %s", "%s: os outros computadores tentam no seu lugar."},
}

// notifierRefusPCs: a computer of the team could not run a program (pcs_relais.go): said once, on every
// computer of the team (the first reading of a project only learns what was already there).
func (s *Serveur) notifierRefusPCs(projet string, premier bool, v VuePCs) {
	var tous []PresencePC
	if v.Moi != nil {
		tous = append(tous, *v.Moi)
	}
	tous = append(tous, v.PCs...)
	l, ok := texteRefusNotif[s.langue()]
	if !ok {
		l = texteRefusNotif["en"]
	}
	refusVusMu.Lock()
	defer refusVusMu.Unlock()
	for _, p := range tous {
		for _, r := range p.Refus {
			cle := projet + "\x00" + p.ID + "\x00" + r.Outil + "\x00" + r.Jalon + "\x00" + r.Quand
			if refusVus[cle] {
				continue
			}
			refusVus[cle] = true
			if !premier {
				outil := r.Outil
				if outil != "" {
					outil = strings.ToUpper(outil[:1]) + outil[1:]
				}
				go notifier(fmt.Sprintf(l[0], outil, p.Nom)+" · "+projet, fmt.Sprintf(l[1], r.Jalon))
				s.notifierAppareils(MessagePush{Titre: fmt.Sprintf(l[0], outil, p.Nom) + " · " + projet, Texte: fmt.Sprintf(l[1], r.Jalon), URL: "/#suivi", Tag: "refus-" + r.Jalon})
			}
		}
	}
}

// derniereLigneJSON: the JSON line of a command's output (what the pilot's side may print before it).
func derniereLigneJSON(out string) string {
	lignes := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lignes) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lignes[i]); strings.HasPrefix(l, "{") {
			return l
		}
	}
	return out
}

// derniereLigneTexte: the last line a command printed (its reason, when it refuses).
func derniereLigneTexte(out string) string {
	l := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(l[len(l)-1])
}

// battementsPCs: every minute, the projects working with other computers say they are there and read
// the others (the first time, every project is read once, to know which ones do).
func (s *Serveur) battementsPCs() {
	time.Sleep(20 * time.Second) // after the application's own start
	premier := true
	for {
		if !s.kitCourant().Valide() {
			time.Sleep(time.Minute)
			continue
		}
		s.mu.Lock()
		projets := append([]Projet{}, s.config.Projets...)
		s.mu.Unlock()
		for _, p := range projets {
			if p.Archive || !p.Existe() {
				continue
			}
			pcsMu.Lock()
			c := pcsCache[p.Nom]
			pcsMu.Unlock()
			if c == nil && !premier {
				continue // not working with other computers (its switch refreshes it)
			}
			if c != nil && !c.vue.Actif {
				continue
			}
			_, _ = s.lirePCsProjet(p, c != nil && c.vue.Actif)
		}
		premier = false
		time.Sleep(time.Minute)
	}
}

// apiPCs: GET the project's computers (read again when the last reading is over a minute old).
func (s *Serveur) apiPCs(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	pcsMu.Lock()
	c := pcsCache[p.Nom]
	pcsMu.Unlock()
	if c != nil && time.Since(c.quand) < 70*time.Second {
		s.ecrireJSON(w, c.vue)
		return
	}
	v, err := s.lirePCsProjet(p, c != nil && c.vue.Actif)
	if err != nil {
		if c != nil {
			s.ecrireJSON(w, c.vue)
			return
		}
		s.erreur(w, http.StatusBadGateway, err.Error())
		return
	}
	s.ecrireJSON(w, v)
}

// apiPCsActiver: POST {actif}: the project works with other computers (same GitHub repository), or no longer.
func (s *Serveur) apiPCsActiver(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	var c struct {
		Actif bool `json:"actif"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	val := "0"
	if c.Actif {
		val = "1"
	}
	out, err := s.executerGitHub(p, 90*time.Second, "pcs-activer", val)
	if err != nil {
		s.erreur(w, http.StatusBadGateway, firstNonEmpty(derniereLigneTexte(out), err.Error()))
		return
	}
	v, err := s.lirePCsProjet(p, c.Actif)
	if err != nil {
		s.erreur(w, http.StatusBadGateway, err.Error())
		return
	}
	v.Rejoint = strings.Contains(out, "starts from the other computers' work")
	s.ecrireJSON(w, v)
}

// apiPCsMachine: GET this computer for the others; POST {nom, capacites, mots} changes it.
func (s *Serveur) apiPCsMachine(w http.ResponseWriter, r *http.Request) {
	args := []string{"github", "pcs-machine"}
	if r.Method == http.MethodPost {
		var c struct {
			Nom       string `json:"nom"`
			Capacites string `json:"capacites"`
			Mots      string `json:"mots"`
		}
		if err := lireCorps(r, &c); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(c.Nom) == "" {
			s.erreur(w, http.StatusBadRequest, "A name")
			return
		}
		args = append(args, "--nom", c.Nom, "--capacites", c.Capacites, "--mots", c.Mots)
	}
	out, err := executerKitArgs(s.kitCourant(), 45*time.Second, args...)
	var m MachinePC
	if err == nil {
		err = json.Unmarshal([]byte(derniereLigneJSON(out)), &m)
	}
	if err != nil {
		s.erreur(w, http.StatusBadGateway, firstNonEmpty(strings.TrimSpace(out), err.Error()))
		return
	}
	if r.Method == http.MethodPost { // the projects working with others say the new name at once
		pcsMu.Lock()
		for k := range pcsCache {
			pcsCache[k].quand = time.Time{}
		}
		pcsMu.Unlock()
	}
	s.ecrireJSON(w, m)
}
