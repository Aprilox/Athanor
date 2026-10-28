package main

// athanor github pcs | pcs-activer | pcs-machine (pcs.go): what the application asks, on the pilot's side
// (WSL, Linux, macOS, native Windows), where the GitHub key lives.
//   pcs PROJECT [--pilote 0|1] [--publier]   this computer's presence published (--publier), the others' read:
//                                            one JSON line
//   pcs-activer PROJECT 0|1                  several computers on this project (the shared workshop with it)
//   pcs-machine [--nom N] [--capacites C] [--mots "gpu, network"]   this computer, for the others

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// reLienDirect: an encrypted address (base64url), the only thing --lien writes.
var reLienDirect = regexp.MustCompile(`^[A-Za-z0-9_-]{16,1000}$`)

// VuePCs: what athanor github pcs prints.
type VuePCs struct {
	Lie     bool         `json:"lie"`
	Actif   bool         `json:"actif"`
	Atelier bool         `json:"atelier"` // the workshop shared on GitHub (the switch turns it on: the app asks first)
	Depot   string       `json:"depot,omitempty"`
	Moi     *PresencePC  `json:"moi,omitempty"`
	PCs     []PresencePC `json:"pcs"`
	Erreur  string       `json:"erreur,omitempty"`
	Lu      string       `json:"lu,omitempty"` // when GitHub was read (RFC 3339)
	Machine MachinePC    `json:"machine"`
	Rejoint bool         `json:"rejoint,omitempty"` // (the switch) the project has just started from the others' work
	Notes   []string     `json:"notes,omitempty"`   // the last notes between the computers (the application reads them)
}

func ecrireJSONGH(w io.Writer, v any) {
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
}

func pcsGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	v := VuePCs{PCs: []PresencePC{}, Machine: lireMachinePC()}
	if !ok {
		ecrireJSONGH(w, v)
		return 2
	}
	pilote, publier, lien := "", false, "-"
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--pilote":
			if i+1 < len(args) {
				pilote = args[i+1]
				i++
			}
		case "--publier":
			publier = true
		case "--lien": // the master's direct address, encrypted (the application): published with the presence
			if i+1 < len(args) {
				lien = args[i+1]
				i++
			}
		}
	}
	d := dossierGH(projet)
	r, lie := lireReglagesGH(d)
	v.Lie, v.Actif, v.Depot, v.Atelier = lie, lie && r.Equipe, r.Depot, r.Atelier
	if !v.Actif {
		ecrireJSONGH(w, v)
		return 0
	}
	verrou := verrouGH(d, 60*time.Second) // a push or a milestone taken meanwhile: one at a time
	if verrou != nil {
		defer verrou.Close()
	}
	switch {
	case lien == "aucun":
		_ = os.Remove(d + "/lien-direct")
	case lien != "-" && reLienDirect.MatchString(lien):
		_ = ecrireFichierPrive(d+"/lien-direct", lien+"\n")
	}
	switch pilote {
	case "1":
		_ = ecrireFichierPrive(d+"/pcs-pilote", "1\n")
	case "0":
		// (its milestone file stays: a stopped pilot's presence says no milestone anyway, and the pilot
		// itself frees it when it stops; this way a late reading of the application never frees the
		// milestone of a pilot that runs)
		_ = ecrireFichierPrive(d+"/pcs-pilote", "0\n")
	}
	d, miroir, r, err := contexteGH(projet)
	if err != nil {
		v.Erreur = err.Error()
		ecrireJSONGH(w, v)
		return 0
	}
	moi := presenceLocalePC(projet, d)
	v.Moi = &moi
	var erreurs []string
	if publier {
		if err := publierPresencePC(d, miroir, r, moi); err != nil {
			erreurs = append(erreurs, err.Error())
		}
	}
	if err := recupererPCs(d, miroir, r); err != nil {
		erreurs = append(erreurs, err.Error())
	} else {
		v.Lu = time.Now().UTC().Format(time.RFC3339)
	}
	v.PCs = lirePCs(miroir, moi.ID)
	if o := obstacleEquipePC(projet, miroir, r); o != "" {
		erreurs = append(erreurs, strings.ToUpper(o[:1])+o[1:]+".")
	}
	if v.PCs == nil {
		v.PCs = []PresencePC{}
	}
	v.Erreur = strings.Join(erreurs, " · ")
	ecrireJSONGH(w, v)
	return 0
}

func pcsActiverGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		return 2
	}
	d := dossierGH(projet)
	verrou := verrouGH(d, 60*time.Second)
	if verrou != nil {
		defer verrou.Close()
	}
	r, lie := lireReglagesGH(d)
	if !lie {
		fmt.Fprintln(w, "Not linked to GitHub.")
		return 3
	}
	actif := argGH(args, 1) == "1"
	if actif {
		// the plan, the requests and the log go through the commits: the others work from them (unless
		// the team's direct link carries them: atelier_lien.go)
		r.Atelier = !atelierParLienProjet(projet)
		r.Auto = true // each milestone pushed at once: the others build on it
		if err := ecrireExclusionAtelier(projet, r.Atelier); err != nil {
			fmt.Fprintln(w, "Cannot write the project's .git/info/exclude: "+err.Error())
			return 1
		}
		_ = ecrireAttributsPC(projet)
	} else if r.Equipe {
		// the computer leaves at once for the others, rather than in 3 minutes: its presence leaves GitHub
		// (its work in progress stays, for the computer that takes its milestones)
		if _, miroir, _, err := contexteGH(projet); err == nil {
			retirerPresencePC(d, miroir, r, lireMachinePC().ID)
		}
		removeSiExiste(d + "/pcs-jalon")
	}
	r.Equipe = actif
	if ecrireReglagesGH(d, r) != nil {
		return 1
	}
	if actif {
		if code := rejoindreEquipeGH(projet, w); code != 0 {
			r.Equipe = false
			_ = ecrireReglagesGH(d, r)
			return code
		}
	}
	fmt.Fprintln(w, "Saved.")
	return 0
}

// rejoindreEquipeGH: at the switch, a project without a commit starts from the others' work at once (its
// plan and requests show); a project whose history has nothing in common with GitHub's is refused.
func rejoindreEquipeGH(projet string, w io.Writer) int {
	d, miroir, r, err := contexteGH(projet)
	if err != nil {
		fmt.Fprintln(w, err.Error())
		return 1
	}
	if err := recupererPCs(d, miroir, r); err != nil {
		fmt.Fprintln(w, "GitHub cannot be read now: the pilot brings the other computers' work when it starts.")
		return 0
	}
	branche := firstNonEmpty(r.Branche, "main")
	distant, _ := sortieGH(gmGH(miroir, nil, "rev-parse", "-q", "--verify", refsDistantPC+branche+"^{commit}"))
	if !reShaGH.MatchString(distant) {
		return 0 // nothing on GitHub yet: this computer is the first
	}
	sha, _, raison := lireProjetGH(projet)
	if raison == "absent" {
		switch res, detail := recevoirPC(projet, d, miroir, r); res {
		case recuRejoint:
			fmt.Fprintf(w, "This computer starts from the other computers' work: %s.\n", detail)
		case recuBloque, recuErreur:
			fmt.Fprintln(w, "Not yet: "+detail+".")
		}
		return 0
	}
	if gmGH(miroir, nil, "merge-base", sha, distant).Run() != nil {
		fmt.Fprintf(w, "This project's history and the one on GitHub (%s) have nothing in common: it cannot work with the computers of that repository. Add the project to this computer from an empty folder linked to %s instead.\n", r.Depot, r.Depot)
		return 4
	}
	return 0
}

func pcsMachineGH(args []string, w io.Writer) int {
	m := lireMachinePC()
	change := false
	for i := 0; i+1 < len(args); i += 2 {
		val := strings.TrimSpace(args[i+1])
		switch args[i] {
		case "--nom":
			if val == "" || len([]rune(val)) > 60 {
				fmt.Fprintln(w, "A name of 60 characters at most.")
				return 2
			}
			m.Nom, change = val, true
		case "--capacites":
			if len([]rune(val)) > 300 {
				val = string([]rune(val)[:300])
			}
			m.Capacites, change = val, true
		case "--mots":
			m.Mots, change = motsPC(val), true
		}
	}
	if change {
		if err := ecrireMachinePC(m); err != nil {
			fmt.Fprintln(w, err.Error())
			return 1
		}
	}
	ecrireJSONGH(w, m)
	return 0
}
