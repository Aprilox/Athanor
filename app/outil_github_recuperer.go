package main

// athanor github recuperer PROJECT [--verifier]: working on one repository in turns (each one on their own
// computer, one after the other, without the switch of Several computers): before taking over, the work
// GitHub has (the other's last push) is brought into the project, as git pull would, with the project's
// automatic commands off.
//
//   - --verifier: GitHub read, nothing changed: how many commits GitHub has that the project has not
//     (derriere) and the other way round (devant), and GitHub's last one. The app asks before starting a
//     pilot that is behind. sans_lien: GitHub's branch has a history of its own, nothing in common with the
//     project's (typically the README or licence GitHub adds when it creates the repository).
//   - --integrer: such a branch's files brought into the project all the same (a merge of the two
//     histories, asked by the user; never for several computers, whose projects must share one): the
//     same as below otherwise. To replace GitHub's branch instead: athanor github pousser --remplacer=SHA.
//   - otherwise, with no pilot running and the files committed: GitHub's commits merged in (a fast-forward
//     when the project has nothing of its own since). Changes on both sides that git cannot merge by itself
//     are never left half done: the merge is undone and .athanor/MERGE_NEEDED.md tells Claude to make it at
//     the start of its next cycle (recevoirPC, the same as several computers').
//
// The last line printed is a JSON object, for the app.

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// EtatRecuperer: what recuperer says (its last line).
type EtatRecuperer struct {
	Resultat string `json:"resultat"`         // a-jour, fusion, conflit, sale, rejoint, bloque, erreur, aucun, pilote, verifie
	Detail   string `json:"detail,omitempty"` // fusion: the commits brought in; bloque, erreur: why
	Derriere int    `json:"derriere"`         // --verifier: GitHub's commits the project has not
	Devant   int    `json:"devant"`           // --verifier: the project's commits GitHub has not
	Sujet    string `json:"sujet,omitempty"`  // GitHub's last commit: its subject, author and date
	Auteur   string `json:"auteur,omitempty"`
	Quand    string `json:"quand,omitempty"`
	Branche  string `json:"branche,omitempty"`
	Depot    string `json:"depot,omitempty"`
	Equipe   bool   `json:"equipe,omitempty"`    // several computers: the pilot brings the others' work in by itself
	SansLien bool   `json:"sans_lien,omitempty"` // GitHub's branch shares nothing with the project's history
	Sha      string `json:"sha,omitempty"`       // GitHub's last commit (what replacing it expects)
}

func recupererGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	verifier, integrer := argGH(args, 1) == "--verifier", argGH(args, 1) == "--integrer"
	fin := func(e EtatRecuperer, code int) int {
		b, _ := json.Marshal(e)
		fmt.Fprintln(w, string(b))
		return code
	}
	if !ok {
		return fin(EtatRecuperer{Resultat: recuErreur, Detail: "no project"}, 2)
	}
	d, miroir, r, err := contexteGH(projet)
	if err != nil {
		return fin(EtatRecuperer{Resultat: recuErreur, Detail: err.Error()}, 1)
	}
	branche := firstNonEmpty(r.Branche, "main")
	e := EtatRecuperer{Branche: branche, Depot: r.Depot, Equipe: r.Equipe}
	if !verifier && (Projet{Dossier: projet}).PiloteActif() {
		e.Resultat = "pilote"
		return fin(e, 1)
	}
	v := verrouGH(d, 120*time.Second) // never during a push
	if v != nil {
		defer v.Close()
	}
	// the branch alone (GitHub may not have it yet: nothing pushed)
	if out, err := gitDistantGH(d, miroir, r, "fetch", "--no-tags", "--no-write-fetch-head", "{url}",
		"+refs/heads/"+branche+":"+refsDistantPC+branche); err != nil {
		if strings.Contains(out, "couldn't find remote ref") || strings.Contains(out, "could not find remote ref") {
			e.Resultat = recuAucun
			return fin(e, 0)
		}
		e.Resultat, e.Detail = recuErreur, "GitHub not read: "+derniereLigneSortie(out)
		return fin(e, 1)
	}
	distant, _ := sortieGH(gmGH(miroir, nil, "rev-parse", "-q", "--verify", refsDistantPC+branche+"^{commit}"))
	if !reShaGH.MatchString(distant) {
		e.Resultat = recuAucun
		return fin(e, 0)
	}
	e.Sha = distant
	if verifier {
		e.Resultat = "verifie"
		if l, err := sortieGH(gmGH(miroir, nil, "log", "-1", "--format=%s%x00%an%x00%cI", distant)); err == nil {
			if p := strings.SplitN(l, "\x00", 3); len(p) == 3 {
				e.Sujet, e.Auteur, e.Quand = coupeValide(p[0], 200), coupeValide(p[1], 80), p[2]
			}
		}
		tete, _ := gProjetGH(projet+"/.git", "rev-parse", "-q", "--verify", "HEAD^{commit}")
		if !reShaGH.MatchString(tete) {
			e.Derriere = 1 // nothing here yet: all of GitHub's work is to bring in
			if n, err := sortieGH(gmGH(miroir, nil, "rev-list", "--count", distant)); err == nil {
				e.Derriere, _ = strconv.Atoi(n)
			}
			return fin(e, 0)
		}
		// counted in the private folder, which sees the project's objects
		if n, err := sortieGH(gmGH(miroir, nil, "rev-list", "--count", tete+".."+distant)); err == nil {
			e.Derriere, _ = strconv.Atoi(n)
		}
		if n, err := sortieGH(gmGH(miroir, nil, "rev-list", "--count", distant+".."+tete)); err == nil {
			e.Devant, _ = strconv.Atoi(n)
		}
		e.SansLien = gmGH(miroir, nil, "merge-base", tete, distant).Run() != nil
		return fin(e, 0)
	}
	if integrer {
		if r.Equipe {
			e.Resultat, e.Detail = recuBloque, "several computers work on this project: their histories must be the same one, never two merged"
			return fin(e, 1)
		}
		res, detail := integrerPC(projet, d, miroir, r)
		e.Resultat, e.Detail = res, detail
		if res == recuErreur || res == recuBloque {
			return fin(e, 1)
		}
		return fin(e, 0)
	}
	res, detail := recevoirPC(projet, d, miroir, r)
	e.Resultat, e.Detail = res, detail
	if res == recuErreur || res == recuBloque {
		if res == recuBloque && strings.Contains(detail, "nothing in common") {
			e.SansLien = true
		}
		return fin(e, 1)
	}
	return fin(e, 0)
}

// integrerPC: GitHub's branch, with a history of its own, merged into the project (both histories kept:
// the project's files and GitHub's). A file both have differently: nothing is changed, Claude merges them at
// the start of its next cycle (.athanor/MERGE_NEEDED.md). A branch that does share the project's history:
// recevoirPC, as always.
func integrerPC(projet, d, miroir string, r reglagesGH) (string, string) {
	branche := firstNonEmpty(r.Branche, "main")
	gd := projet + "/.git"
	local := "refs/remotes/athanor/" + branche
	if _, _, raison := lireProjetGH(projet); raison != "" {
		return recevoirPC(projet, d, miroir, r)
	}
	if _, err := gProjetGH(gd, "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--no-write-fetch-head", "--quiet", miroir,
		"+"+refsDistantPC+branche+":"+local); err != nil {
		return recuErreur, "the commits of GitHub could not be brought into the project"
	}
	if gTravailGH(projet, "merge-base", local, "HEAD").Run() == nil {
		return recevoirPC(projet, d, miroir, r)
	}
	if !propreTravailPC(projet) {
		return recuSale, ""
	}
	avant, _ := gProjetGH(gd, "rev-parse", "HEAD")
	ident := identPC(projet)
	args := append(ident, "merge", "--allow-unrelated-histories", "--no-edit", "-m", "Bring GitHub's files in ("+branche+")", local)
	if out, err := gTravailGH(projet, args...).CombinedOutput(); err != nil {
		_ = gTravailGH(projet, "merge", "--abort").Run()
		if apres, _ := gProjetGH(gd, "rev-parse", "HEAD"); apres != avant || !propreTravailPC(projet) {
			_ = gTravailGH(projet, "reset", "--hard", "--quiet", avant).Run()
		}
		fichiers, _ := gTravailGH(projet, "diff", "--name-only", "HEAD", local).Output()
		_ = consigneFusionPC(projet, "--allow-unrelated-histories "+local, strings.Fields(string(fichiers)), strings.TrimSpace(string(out)), false)
		return recuConflit, "GitHub's files and the project's differ in the same files: Claude merges them at the start of the next cycle"
	}
	n, _ := gProjetGH(gd, "rev-list", "--count", avant+".."+local)
	_ = fichierAtelier(projet, "MERGE_NEEDED.md", true, nil)
	return recuFusion, n
}
