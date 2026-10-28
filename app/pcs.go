package main

// Several computers on one project (several Athanor, one GitHub repository): each one publishes what it
// is and what it does on a ref of its own of the repository (refs/athanor/pc/<id>, not a branch; a single pc.json, replaced
// at each beat), and reads the others' every minute. No tunnel between the computers: GitHub already
// carries the code, checks each computer's key (the repository's own), and works behind any network.
//
//   - a computer is online while its beat is recent (3 minutes); it says its name, what it is good at
//     (free text, and words such as gpu or network that the plan's "Needs:" use), whether its pilot runs,
//     and the milestone it builds;
//   - a pilot only takes a milestone no other online computer holds, those whose "Needs:" match its own
//     words first, and leaves to the others those that match theirs; two computers that take the same
//     one at the same moment: the earlier keeps it;
//   - between two cycles it merges what the others pushed (the shared LOG.md, BUGS.md… are joined, never
//     in conflict), and pushes its own; a real conflict is left to Claude in the next cycle.
//
// Only projects linked to the same GitHub repository work together, with the shared workshop (the plan,
// the requests, the log go through the commits).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	enLignePC = 3 * time.Minute // beats every minute: three missed and the computer is offline
	// on GitHub, the presences and the works in progress are refs of their own, never branches: hidden
	// from the list of branches, no workflow runs when they change or go, so they can be removed
	refPresencePC = "refs/athanor/pc/"
	brancheePC    = "athanor/pc/" // where 2.36 put them (branches): removed once
	refsPCs       = "refs/athanor/pcs/"
	oubliPC       = 30 * 24 * time.Hour // a computer not seen for so long: its refs leave GitHub
	refsDistantPC = "refs/athanor/distant/"
)

// MachinePC: this computer, for the others (donneesGitHub()/machine.json, one for every project).
type MachinePC struct {
	ID        string   `json:"id"`
	Nom       string   `json:"nom"`
	Capacites string   `json:"capacites,omitempty"` // "RTX 4090, 24 GB: 3D models, renders"
	Mots      []string `json:"mots,omitempty"`      // gpu, network…: what the plan's "Needs:" name
}

// PresencePC: what a computer publishes on its ref.
type PresencePC struct {
	ID         string    `json:"id"`
	Nom        string    `json:"nom"`
	Capacites  string    `json:"capacites,omitempty"`
	Mots       []string  `json:"mots,omitempty"`
	Version    string    `json:"version"`
	Maj        string    `json:"maj"` // RFC 3339, UTC
	Pilote     bool      `json:"pilote"`
	Phase      string    `json:"phase,omitempty"`
	Cycle      int64     `json:"cycle,omitempty"`
	Jalon      string    `json:"jalon,omitempty"` // the milestone it holds ("cadrage", "livraison" for the framing, the delivery)
	JalonTitre string    `json:"jalon_titre,omitempty"`
	Depuis     string    `json:"depuis,omitempty"`  // when it took it (RFC 3339, UTC)
	Pousse     string    `json:"pousse,omitempty"`  // its last commit pushed ("abc1234 M3: …")
	Refus      []RefusPC `json:"refus,omitempty"`   // programs it could not run, for which milestones (pcs_relais.go)
	Parti      bool      `json:"parti,omitempty"`   // it stopped working with the others (2.36.1, which kept its branch)
	Lien       string    `json:"lien,omitempty"`    // the master's direct address, encrypted with the team's key (equipe_directe.go)
	Chiffre    string    `json:"chiffre,omitempty"` // a private team (its workshop through the direct link): all of it, encrypted
	Prive      bool      `json:"prive,omitempty"`   // computed by the reader: it came encrypted
	EnLigne    bool      `json:"en_ligne"`          // computed by the reader
	Moi        bool      `json:"moi,omitempty"`
}

func (p PresencePC) recente(maintenant time.Time) bool {
	t, err := time.Parse(time.RFC3339, p.Maj)
	return err == nil && maintenant.Sub(t) < enLignePC && t.Sub(maintenant) < enLignePC
}

func fichierMachinePC() string { return donneesGitHub() + "/machine.json" }

var reIDMachine = regexp.MustCompile(`^[0-9a-f]{12}$`)

// lireMachinePC: this computer (an id made once; its name, the computer's, until one is chosen).
func lireMachinePC() MachinePC {
	var m MachinePC
	if b, err := os.ReadFile(fichierMachinePC()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if !reIDMachine.MatchString(m.ID) {
		m.ID = aleatoire(6)
		if m.Nom == "" {
			m.Nom, _ = os.Hostname()
		}
		_ = ecrireMachinePC(m)
	}
	if strings.TrimSpace(m.Nom) == "" {
		m.Nom, _ = os.Hostname()
	}
	return m
}

func ecrireMachinePC(m MachinePC) error {
	_ = os.MkdirAll(donneesGitHub(), 0o700)
	b, _ := json.MarshalIndent(m, "", "  ")
	t, err := os.CreateTemp(donneesGitHub(), ".machine.")
	if err != nil {
		return err
	}
	_, _ = t.Write(b)
	t.Close()
	return os.Rename(t.Name(), fichierMachinePC())
}

// motsPC: "GPU, réseau rapide; 3d" → gpu, réseau-rapide, 3d (lower case, a word per need).
func motsPC(s string) []string {
	var v []string
	vus := map[string]bool{}
	for _, m := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		m = strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(m))), "-")
		m = strings.Trim(m, ".-")
		if m != "" && len(m) <= 30 && !vus[m] && len(v) < 12 {
			vus[m] = true
			v = append(v, m)
		}
	}
	return v
}

// niveauMotPC: a word and its level ("gpu:3" → gpu, 3; "gpu" → gpu, 1): how capable a computer is at it
// (its words), or what a milestone needs (Needs:).
func niveauMotPC(m string) (string, int) {
	if i := strings.LastIndex(m, ":"); i > 0 && i == len(m)-2 && m[i+1] >= '1' && m[i+1] <= '9' {
		return m[:i], int(m[i+1] - '0')
	}
	return m, 1
}

// capacitePC: the level of a computer's words for that word (0: it does not have it).
func capacitePC(mots []string, base string) int {
	n := 0
	for _, m := range mots {
		if b, l := niveauMotPC(m); b == base && l > n {
			n = l
		}
	}
	return n
}

// ---------------------------------------------------------------------------- GitHub

// depotEssaiGH: the tests' stand-in for GitHub (a local bare repository); empty: GitHub over SSH.
var depotEssaiGH string

type distantGH struct {
	url   string
	extra []string // git -c options for that remote
}

// distantsGH: GitHub's addresses to try (its SSH port, then the HTTPS one), as the push uses them.
func distantsGH(d string, r reglagesGH) []distantGH {
	if depotEssaiGH != "" {
		return []distantGH{{depotEssaiGH, []string{"-c", "protocol.file.allow=always"}}}
	}
	var v []distantGH
	for _, essai := range []string{firstNonEmpty(os.Getenv("ATHANOR_GITHUB_SSH"), "github.com:22"), secoursSSHGH()} {
		if essai == "" {
			continue
		}
		hote, port := essai, essai
		if i := strings.LastIndex(essai, ":"); i >= 0 {
			hote, port = essai[:i], essai[i+1:]
		}
		v = append(v, distantGH{fmt.Sprintf("ssh://git@%s:%s/%s.git", hote, port, r.Depot),
			[]string{"-c", "protocol.ssh.allow=always", "-c", "core.sshCommand=" + commandeSSHGH(d, port)}})
	}
	return v
}

// gitDistantGH: git ARGS (with the remote's url where {url} stands) on the private folder, on GitHub's
// addresses in turn while the network is the reason; its output.
func gitDistantGH(d, miroir string, r reglagesGH, args ...string) (string, error) {
	var sortie string
	var err error
	for _, dist := range distantsGH(d, r) {
		a := append([]string{}, dist.extra...)
		for _, x := range args {
			if x == "{url}" {
				x = dist.url
			}
			a = append(a, x)
		}
		out, e := gmGH(miroir, nil, a...).CombinedOutput()
		sortie, err = strings.TrimRight(string(out), "\n"), e
		if err == nil || !reReseauGH.MatchString(sortie) {
			break
		}
	}
	return sortie, err
}

// contexteGH: the project's GitHub settings and its private folder, ready (the project's objects in it).
func contexteGH(projet string) (d, miroir string, r reglagesGH, err error) {
	d = dossierGH(projet)
	r, lie := lireReglagesGH(d)
	if !lie {
		return d, "", r, errors.New("not linked to GitHub")
	}
	_, objets, raison := lireProjetGH(projet)
	if raison == "detourne" {
		return d, "", r, errors.New("the project's .git folder points elsewhere")
	}
	if objets == "" {
		objets = projet + "/.git/objects"
	}
	miroir = d + "/miroir.git"
	if err := preparerMiroirGH(miroir, objets); err != nil {
		return d, "", r, err
	}
	return d, miroir, r, nil
}

// ---------------------------------------------------------------------------- presence

// etatLocalPC: what this computer does on the project (its pilot, from the application; its milestone,
// from the pilot), kept apart so that neither overwrites the other's part.
type etatLocalPC struct {
	Pilote     bool   `json:"pilote"`
	Jalon      string `json:"jalon,omitempty"`
	JalonTitre string `json:"jalon_titre,omitempty"`
	Depuis     string `json:"depuis,omitempty"`
}

func lireEtatLocalPC(d string) etatLocalPC {
	var e etatLocalPC
	if b, err := os.ReadFile(d + "/pcs-pilote"); err == nil {
		e.Pilote = strings.TrimSpace(string(b)) == "1"
	}
	if b, err := os.ReadFile(d + "/pcs-jalon"); err == nil {
		p := strings.SplitN(strings.TrimRight(string(b), "\n"), "\t", 3)
		if len(p) == 3 {
			e.Jalon, e.JalonTitre, e.Depuis = p[0], p[1], p[2]
		}
	}
	return e
}

// presenceLocalePC: this computer's presence on the project, now.
func presenceLocalePC(projet, d string) PresencePC {
	m := lireMachinePC()
	e := lireEtatLocalPC(d)
	p := PresencePC{ID: m.ID, Nom: m.Nom, Capacites: m.Capacites, Mots: m.Mots, Version: versionAthanor,
		Maj: time.Now().UTC().Format(time.RFC3339), Pilote: e.Pilote, Moi: true, Refus: lireRefusPC(d)}
	if e.Pilote {
		p.Jalon, p.JalonTitre, p.Depuis = e.Jalon, e.JalonTitre, e.Depuis
		if st, ok := (Projet{Dossier: projet}).LireEtat(); ok {
			p.Phase, p.Cycle = st.Phase, st.Cycle
		}
	}
	if b, err := os.ReadFile(d + "/dernier.json"); err == nil {
		var x struct {
			Resultat, Commit string
		}
		if json.Unmarshal(b, &x) == nil && x.Resultat == "ok" {
			p.Pousse = coupeOctets(x.Commit, 100)
		}
	}
	if b, err := os.ReadFile(d + "/lien-direct"); err == nil { // the master's encrypted address (equipe_directe.go)
		p.Lien = coupeOctets(strings.TrimSpace(string(b)), 2048)
	}
	p.EnLigne = true
	return p
}

// publierPresencePC: this computer's presence, on its ref of GitHub (a single commit, replaced each time:
// no history grows on GitHub).
func publierPresencePC(d, miroir string, r reglagesGH, p PresencePC, cle string) error {
	p.Moi, p.EnLigne, p.Prive = false, false, false
	b, _ := json.MarshalIndent(p, "", "  ")
	if cle != "" {
		// a private team: GitHub sees an id and a time only (the computer's name, what it builds, its
		// address: encrypted with the team's key)
		c, err := chiffrerDirect(cle, string(b))
		if err != nil {
			return errors.New("presence: the team's key is unreadable")
		}
		b, _ = json.MarshalIndent(PresencePC{ID: p.ID, Maj: p.Maj, Version: p.Version, Chiffre: c}, "", "  ")
	}
	executer := func(stdin []byte, env []string, args ...string) (string, error) {
		c := gmGH(miroir, env, args...)
		c.Stdin = bytes.NewReader(stdin)
		return sortieGH(c)
	}
	blob, err := executer(b, nil, "hash-object", "-w", "--stdin")
	if err != nil || !reShaGH.MatchString(blob) {
		return errors.New("presence: cannot write it")
	}
	arbre, err := executer([]byte("100644 blob "+blob+"\tpc.json\n"), nil, "mktree")
	if err != nil || !reShaGH.MatchString(arbre) {
		return errors.New("presence: cannot write it")
	}
	ident := []string{"GIT_AUTHOR_NAME=Athanor", "GIT_AUTHOR_EMAIL=athanor@users.noreply.github.com",
		"GIT_COMMITTER_NAME=Athanor", "GIT_COMMITTER_EMAIL=athanor@users.noreply.github.com"}
	titre := p.Nom + " (" + p.ID + ")"
	if cle != "" {
		titre = p.ID
	}
	commit, err := executer(nil, ident, "commit-tree", arbre, "-m", "Athanor: "+titre)
	if err != nil || !reShaGH.MatchString(commit) {
		return errors.New("presence: cannot write it")
	}
	if out, err := gitDistantGH(d, miroir, r, "push", "--porcelain", "--no-verify", "--force", "{url}", commit+":"+refPresencePC+p.ID); err != nil {
		return fmt.Errorf("presence not published: %s", derniereLigneSortie(out))
	}
	entretenirMiroirPC(miroir)
	retirerBranchesPC(d, miroir, r, p.ID)
	menagePCs(d, miroir, r, p.ID)
	return nil
}

// retirerPresencePC: this computer leaves the team: its presence leaves GitHub at once (a ref, not a
// branch: no workflow runs for it).
func retirerPresencePC(d, miroir string, r reglagesGH, id string) {
	if _, err := gitDistantGH(d, miroir, r, "push", "--porcelain", "--no-verify", "{url}", ":"+refPresencePC+id); err == nil {
		_ = gmGH(miroir, nil, "update-ref", "-d", refsPCs+id).Run()
	}
}

// retirerBranchesPC: the branches 2.36 made for this computer (athanor/pc/…, athanor/encours/…), removed
// from GitHub once, now that its refs replace them (GitHub runs the repository's "delete" workflows that
// one time, if it has any).
func retirerBranchesPC(d, miroir string, r reglagesGH, id string) {
	fait := d + "/pcs-branches-retirees"
	if _, err := os.Stat(fait); err == nil {
		return
	}
	out, err := gitDistantGH(d, miroir, r, "ls-remote", "{url}", "refs/heads/"+brancheePC+id, "refs/heads/"+brancheEnCoursPC+id)
	if err != nil {
		return
	}
	var suppr []string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) == 2 && reShaGH.MatchString(f[0]) &&
			(f[1] == "refs/heads/"+brancheePC+id || f[1] == "refs/heads/"+brancheEnCoursPC+id) {
			suppr = append(suppr, ":"+f[1])
		}
	}
	if len(suppr) > 0 {
		if _, err := gitDistantGH(d, miroir, r, append([]string{"push", "--porcelain", "--no-verify", "{url}"}, suppr...)...); err != nil {
			return
		}
	}
	_ = os.WriteFile(fait, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

// menagePCs: once a day, the refs of the computers not seen for oubliPC (their presence and their work in
// progress) leave GitHub; a computer that comes back publishes its presence again.
func menagePCs(d, miroir string, r reglagesGH, moi string) {
	marque := d + "/pcs-menage"
	if st, err := os.Stat(marque); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		return
	}
	_ = os.WriteFile(marque, nil, 0o600)
	_ = os.Chtimes(marque, time.Now(), time.Now())
	refs, err := sortieGH(gmGH(miroir, nil, "for-each-ref", "--format=%(refname)", refsPCs))
	if err != nil {
		return
	}
	var suppr, locales []string
	for _, ref := range strings.Split(refs, "\n") {
		id := strings.TrimPrefix(ref, refsPCs)
		if ref == "" || id == moi || !reIDMachine.MatchString(id) {
			continue
		}
		out, err := gmGH(miroir, nil, "cat-file", "blob", ref+":pc.json").Output()
		var p PresencePC
		if err != nil || len(out) > 64<<10 || json.Unmarshal(out, &p) != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, p.Maj)
		if err != nil || time.Since(t) < oubliPC {
			continue
		}
		suppr = append(suppr, ":"+refPresencePC+id)
		locales = append(locales, refsPCs+id)
		if gmGH(miroir, nil, "rev-parse", "-q", "--verify", refsEnCoursPC+id).Run() == nil {
			suppr = append(suppr, ":"+refDistantEnCoursPC+id)
			locales = append(locales, refsEnCoursPC+id)
		}
	}
	if len(suppr) == 0 {
		return
	}
	if _, err := gitDistantGH(d, miroir, r, append([]string{"push", "--porcelain", "--no-verify", "{url}"}, suppr...)...); err == nil {
		for _, l := range locales {
			_ = gmGH(miroir, nil, "update-ref", "-d", l).Run()
		}
	}
}

// entretenirMiroirPC: each presence (every minute, of every computer) leaves objects in the private folder;
// past a few thousand, the old ones go (never those of the last hour: a fetch or a push may be using them).
func entretenirMiroirPC(miroir string) {
	out, err := gmGH(miroir, nil, "count-objects").Output()
	if err != nil {
		return
	}
	var n int
	if _, err := fmt.Sscanf(string(out), "%d objects", &n); err != nil || n < 3000 {
		return
	}
	_ = gmGH(miroir, nil, "gc", "--quiet", "--prune=1.hour.ago").Run()
}

// recupererPCs: the others' presences and the branch's last commits, from GitHub into the private folder.
func recupererPCs(d, miroir string, r reglagesGH) error {
	branche := firstNonEmpty(r.Branche, "main")
	if out, err := gitDistantGH(d, miroir, r, "fetch", "--prune", "--no-tags", "--no-write-fetch-head", "{url}",
		"+"+refPresencePC+"*:"+refsPCs+"*", "+"+refDistantEnCoursPC+"*:"+refsEnCoursPC+"*"); err != nil {
		return fmt.Errorf("GitHub not read: %s", derniereLigneSortie(out))
	}
	// the branch may not exist yet (nothing pushed): not an error
	_, _ = gitDistantGH(d, miroir, r, "fetch", "--no-tags", "--no-write-fetch-head", "{url}",
		"+refs/heads/"+branche+":"+refsDistantPC+branche)
	return nil
}

// clePresencePC: the team's key when the project's team is private (its workshop through the direct link):
// the presences then go encrypted; "" otherwise.
func clePresencePC(projet string) string {
	if e, ok := equipeDirectePilote(priveDePilote(kitDuProgramme(), projet, estWindows)); ok {
		return e.Cle
	}
	return ""
}

// lirePCs: the presences read last (the computer itself apart), online or not; the ones online first. An
// encrypted presence is read with the team's key (cle), else left out.
func lirePCs(miroir, moi, cle string) []PresencePC {
	refs, err := sortieGH(gmGH(miroir, nil, "for-each-ref", "--format=%(refname)", refsPCs))
	if err != nil {
		return nil
	}
	maintenant := time.Now()
	var v []PresencePC
	for _, ref := range strings.Split(refs, "\n") {
		if ref == "" {
			continue
		}
		out, err := gmGH(miroir, nil, "cat-file", "blob", ref+":pc.json").Output()
		if err != nil || len(out) > 64<<10 {
			continue
		}
		var p PresencePC
		if json.Unmarshal(out, &p) != nil || !reIDMachine.MatchString(p.ID) || p.ID == moi || ref != refsPCs+p.ID {
			continue
		}
		if p.Chiffre != "" {
			t, err := dechiffrerDirect(cle, p.Chiffre)
			var x PresencePC
			if cle == "" || err != nil || json.Unmarshal([]byte(t), &x) != nil || x.ID != p.ID {
				continue // another team's, or a key this computer does not have
			}
			p = x
			p.Chiffre, p.Prive = "", true
		}
		if p.Parti {
			continue
		}
		p.Nom = coupeOctets(strings.TrimSpace(p.Nom), 60)
		p.Capacites = coupeOctets(p.Capacites, 300)
		p.JalonTitre = coupeOctets(p.JalonTitre, 200)
		p.Jalon = coupeValide(p.Jalon, 80)
		if len(p.Mots) > 12 {
			p.Mots = p.Mots[:12]
		}
		for i := range p.Mots {
			p.Mots[i] = coupeValide(p.Mots[i], 30)
		}
		if len(p.Refus) > maxRefusPC {
			p.Refus = p.Refus[:maxRefusPC]
		}
		for i := range p.Refus {
			p.Refus[i].Outil, p.Refus[i].Jalon = coupeValide(p.Refus[i].Outil, 30), coupeValide(p.Refus[i].Jalon, 80)
			p.Refus[i].Pourquoi = coupeValide(p.Refus[i].Pourquoi, 200)
		}
		p.EnLigne, p.Moi = p.recente(maintenant), false
		if !p.EnLigne {
			p.Pilote, p.Jalon, p.JalonTitre = false, "", ""
		}
		v = append(v, p)
	}
	sort.SliceStable(v, func(i, j int) bool {
		if v[i].EnLigne != v[j].EnLigne {
			return v[i].EnLigne
		}
		return v[i].Nom < v[j].Nom
	})
	return v
}

// ---------------------------------------------------------------------------- bringing the others' work

// resultat of recevoirPC
const (
	recuAJour   = "a-jour"
	recuFusion  = "fusion"
	recuConflit = "conflit"
	recuSale    = "sale"    // changes not committed: nothing merged now
	recuAucun   = "aucun"   // nothing on GitHub yet
	recuErreur  = "erreur"  // could not read GitHub
	recuRejoint = "rejoint" // the project had no commit: it starts from the others' work
	recuBloque  = "bloque"  // the project's files or history cannot take the others' work (detail: why)
)

// dossierAvantEquipePC: where the workshop files a computer had before it joined are kept (Athanor's data
// for the project, out of the project and of the commits).
func dossierAvantEquipePC(d string) string { return d + "/avant-equipe" }

// gTravailGH: git in the project's folder (its working tree), its automatic commands off.
func gTravailGH(projet string, args ...string) *exec.Cmd {
	opts := optionsSuresGit(projet, append(sansEnvGitPC(), "GIT_DIR="+projet+"/.git"))
	c := commande("git", append(append(opts, "-c", "merge.renames=true"), args...)...)
	c.Dir = projet
	c.Env = append(sansEnvGitPC(), "GIT_DIR="+projet+"/.git", "GIT_WORK_TREE="+projet, "GIT_TERMINAL_PROMPT=0")
	return c
}

// recevoirPC: what the others pushed, merged into the project (after recupererPCs). A conflict is never
// left half done: the merge is undone and .athanor/MERGE_NEEDED.md tells Claude how to make it.
func recevoirPC(projet, d, miroir string, r reglagesGH) (string, string) {
	branche := firstNonEmpty(r.Branche, "main")
	distant, _ := sortieGH(gmGH(miroir, nil, "rev-parse", "-q", "--verify", refsDistantPC+branche+"^{commit}"))
	if !reShaGH.MatchString(distant) {
		return recuAucun, ""
	}
	gd := projet + "/.git"
	local := "refs/remotes/athanor/" + branche
	if !existeGH(gd) && !estLienGH(gd) {
		// a computer that joins with a new folder: its repository, then the others' work in it
		c := commande("git", "init", "-q", "-b", branche, projet)
		c.Env = sansEnvGitPC()
		if c.Run() != nil {
			return recuErreur, "the project's git repository could not be created"
		}
		// what never goes to GitHub, before any file arrives (the pilot's state, its logs, the secrets)
		if ecrireExclusionAtelier(projet, r.Atelier) != nil {
			return recuErreur, "the project's .git/info/exclude could not be written"
		}
	}
	if _, err := gProjetGH(gd, "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--no-write-fetch-head", "--quiet", miroir,
		"+"+refsDistantPC+branche+":"+local); err != nil {
		return recuErreur, "the commits of GitHub could not be brought into the project"
	}
	if _, _, raison := lireProjetGH(projet); raison == "absent" {
		return rejoindrePC(projet, d, branche, local)
	}
	if gTravailGH(projet, "merge-base", "--is-ancestor", local, "HEAD").Run() == nil {
		return recuAJour, ""
	}
	if gTravailGH(projet, "merge-base", local, "HEAD").Run() != nil {
		// two projects that only share a name: never mixed
		return recuBloque, "this project's history and the one on GitHub (" + r.Depot + ") have nothing in common: the other computers' work is not merged into it. Start this computer from an empty folder linked to " + r.Depot + ", or stop working with other computers on it"
	}
	if !propreTravailPC(projet) {
		return recuSale, ""
	}
	avant, _ := gProjetGH(gd, "rev-parse", "HEAD")
	ident := identPC(projet)
	_ = ecrireAttributsPC(projet)
	ecartes := ecarterNonSuivisPC(projet, d, local)
	args := append(ident, "merge", "--no-edit", "-m", "Merge the other computers' work ("+branche+")", local)
	if out, err := gTravailGH(projet, args...).CombinedOutput(); err != nil && !cochesSeulesPC(projet, ident) {
		defer remettreNonSuivisPC(projet, ecartes)
		_ = gTravailGH(projet, "merge", "--abort").Run()
		if apres, _ := gProjetGH(gd, "rev-parse", "HEAD"); apres != avant || !propreTravailPC(projet) {
			// the files were clean before the merge (checked above): back to exactly where they were, even
			// when the abort could not finish (a file held open on Windows, line endings rewritten)
			_ = gTravailGH(projet, "reset", "--hard", "--quiet", avant).Run()
		}
		fichiers, _ := gTravailGH(projet, "diff", "--name-only", "HEAD..."+local).Output()
		_ = consigneFusionPC(projet, local, strings.Fields(string(fichiers)), strings.TrimSpace(string(out)), r.Equipe)
		return recuConflit, "a conflict with the other computers' work: Claude merges it at the start of the next cycle"
	}
	joindreNonSuivisPC(projet, ecartes, ident, local)
	if !r.Atelier {
		garderAtelierPC(projet, avant)
	}
	n, _ := gProjetGH(gd, "rev-list", "--count", avant+".."+local)
	_ = fichierAtelier(projet, "MERGE_NEEDED.md", true, nil)
	return recuFusion, n
}

// garderAtelierPC: the workshop not shared on GitHub (or through the team's direct link): files of
// .athanor/ the others' commits took out of git (a computer that stopped sharing it) were removed from the
// folder by the merge: they are this computer's workshop, put back as they were (now out of git).
func garderAtelierPC(projet, avant string) {
	out, err := gTravailGH(projet, "diff", "--name-only", "-z", "--diff-filter=D", "--no-renames", avant, "HEAD", "--", ".athanor/").Output()
	if err != nil {
		return
	}
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "" || !strings.HasPrefix(f, ".athanor/") || strings.Contains(f, "..") {
			continue
		}
		chemin := filepath.Join(projet, filepath.FromSlash(f))
		if _, err := os.Lstat(chemin); err == nil {
			continue
		}
		b, err := gTravailGH(projet, "cat-file", "blob", avant+":"+f).Output()
		if err != nil {
			continue
		}
		parties := strings.Split(strings.TrimPrefix(f, ".athanor/"), "/")
		if d, err := dossierAtelierSur(projet, parties[:len(parties)-1]...); err == nil {
			_ = ecrireAtomique(filepath.Join(d, parties[len(parties)-1]), b, 0o644)
		}
	}
}

// rejoindrePC: the project has no commit yet (a computer joining the team): it starts from the branch the
// others pushed. Files of the folder that GitHub also has are never overwritten, except the workshop's
// (.athanor/, what Athanor wrote when the project was added): those are kept in dossierAvantEquipePC.
func rejoindrePC(projet, d, branche, local string) (string, string) {
	liste, err := gTravailGH(projet, "ls-tree", "-r", "-z", "--name-only", local).Output()
	if err != nil {
		return recuErreur, "GitHub's files could not be listed"
	}
	atelier, obstacle := dejaLaPC(projet, liste)
	if obstacle != "" {
		return recuBloque, obstacle
	}
	dest := ""
	if len(atelier) > 0 {
		dest = filepath.Join(dossierAvantEquipePC(d), time.Now().Format("20060102-150405"))
		for _, f := range atelier {
			cible := filepath.Join(dest, filepath.FromSlash(f))
			if err := os.MkdirAll(filepath.Dir(cible), 0o700); err != nil {
				return recuErreur, err.Error()
			}
			if err := os.Rename(filepath.Join(projet, filepath.FromSlash(f)), cible); err != nil {
				return recuErreur, "a workshop file could not be put aside: " + err.Error()
			}
		}
	}
	if piloteNatifPC() {
		// the native Windows pilot keeps the project's files as git has them (core.autocrlf false, as it
		// sets it at its start): written so from the first checkout, or every file would look changed then
		_ = gTravailGH(projet, "config", "core.autocrlf", "false").Run()
		_ = gTravailGH(projet, "config", "core.fileMode", "false").Run()
	}
	if out, err := gTravailGH(projet, "checkout", "-q", "--no-track", "-B", branche, local).CombinedOutput(); err != nil {
		return recuBloque, "GitHub's files could not be written in the project: " + derniereLigneSortie(string(out))
	}
	n, _ := gProjetGH(projet+"/.git", "rev-list", "--count", "HEAD")
	detail := n + " commit(s)"
	if dest != "" {
		detail += "; the workshop files this computer had before are kept in " + dest
	}
	return recuRejoint, detail
}

// dejaLaPC: of GitHub's files (ls-tree -z), the ones the folder already has: the workshop's (put aside at
// the join), and why the others' work cannot come in (files of the project itself, never overwritten).
func dejaLaPC(projet string, liste []byte) (atelier []string, obstacle string) {
	var autres []string
	for _, f := range strings.Split(string(liste), "\x00") {
		if f == "" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(projet, filepath.FromSlash(f))); err != nil {
			continue
		}
		if strings.HasPrefix(f, ".athanor/") {
			atelier = append(atelier, f)
		} else {
			autres = append(autres, f)
		}
	}
	if len(autres) > 0 {
		sort.Strings(autres)
		n := len(autres)
		if n > 5 {
			autres = append(autres[:5], fmt.Sprintf("… (%d in all)", n))
		}
		obstacle = "the project folder already has files that GitHub's repository has too (" + strings.Join(autres, ", ") + "): Athanor does not overwrite them. Move them out of the folder, then it starts from the other computers' work"
	}
	return atelier, obstacle
}

// obstacleEquipePC: why the project cannot take the others' work now ("" when it can), from what was read.
func obstacleEquipePC(projet, miroir string, r reglagesGH) string {
	distant := refsDistantPC + firstNonEmpty(r.Branche, "main")
	if gmGH(miroir, nil, "rev-parse", "-q", "--verify", distant+"^{commit}").Run() != nil {
		return ""
	}
	sha, _, raison := lireProjetGH(projet)
	switch {
	case raison == "absent":
		liste, err := gmGH(miroir, nil, "ls-tree", "-r", "-z", "--name-only", distant).Output()
		if err != nil {
			return ""
		}
		_, obstacle := dejaLaPC(projet, liste)
		return obstacle
	case raison == "" && gmGH(miroir, nil, "merge-base", sha, distant).Run() != nil:
		return "this project's history and the one on GitHub (" + r.Depot + ") have nothing in common: the other computers' work is not merged into it"
	}
	return ""
}

// piloteNatifPC: the pilot runs on Windows itself (not in WSL): it sets the project's line endings.
var piloteNatifPC = func() bool { return runtime.GOOS == "windows" || os.Getenv("ATHANOR_NATIF") == "1" }

// sansEnvGitPC: the environment without what would point git to another repository.
func sansEnvGitPC() []string {
	var env []string
	for _, e := range os.Environ() {
		if k, _, _ := strings.Cut(e, "="); !envGitRetire[k] {
			env = append(env, e)
		}
	}
	return env
}

// reCochePC: a milestone's box in the plan ("- [x] M3 …").
var reCochePC = regexp.MustCompile(`(?m)^(\s*[-*]\s*)\[[ xX]\]`)

// cochesSeulesPC: the merge stopped on the plan alone, and only because two computers ticked milestones on
// lines next to each other (git calls that a conflict): the plan merged with every box ticked on either
// side, and the merge committed. Anything else in the conflict is left to Claude.
func cochesSeulesPC(projet string, ident []string) bool {
	const plan = ".athanor/PLAN.md"
	out, err := gTravailGH(projet, "diff", "--name-only", "--diff-filter=U").Output()
	if err != nil || strings.TrimSpace(string(out)) != plan {
		return false
	}
	nous, err1 := gTravailGH(projet, "cat-file", "blob", ":2:"+plan).Output()
	eux, err2 := gTravailGH(projet, "cat-file", "blob", ":3:"+plan).Output()
	if err1 != nil || err2 != nil {
		return false
	}
	fusion, ok := fusionnerCochesPC(string(nous), string(eux))
	if !ok || os.WriteFile(filepath.Join(projet, filepath.FromSlash(plan)), []byte(fusion), 0o644) != nil {
		return false
	}
	if gTravailGH(projet, "add", "--", plan).Run() != nil {
		return false
	}
	return gTravailGH(projet, append(ident, "commit", "-q", "--no-edit", "--no-verify")...).Run() == nil
}

// fusionnerCochesPC: two plans that differ only by their ticked boxes: one plan, each box ticked on either.
func fusionnerCochesPC(nous, eux string) (string, bool) {
	vide := func(s string) string { return reCochePC.ReplaceAllString(s, "${1}[ ]") }
	if vide(nous) != vide(eux) {
		return "", false
	}
	ln, le := strings.Split(nous, "\n"), strings.Split(eux, "\n")
	for i := range ln {
		if ln[i] != le[i] && reCochePC.MatchString(le[i]) && !strings.Contains(reCochePC.FindString(le[i]), "[ ]") {
			ln[i] = le[i] // ticked there
		}
	}
	return strings.Join(ln, "\n"), true
}

// fichiersUnionPC: the workshop's notes every computer adds to (merge=union): their lines are joined.
var fichiersUnionPC = map[string]bool{"LOG.md": true, "BUGS.md": true, "SOLUTIONS.md": true, "DECISIONS.md": true, "BLOCKERS.md": true, "TEAM_NOTES.md": true}

type ecartePC struct{ chemin, garde string }

// ecarterNonSuivisPC: workshop files this computer has but never committed (the pilot writes some at its
// start) that the others' work brings: git would refuse the merge for them. The same content is just
// removed; another one is put aside (dossierAvantEquipePC), then joined back or restored.
func ecarterNonSuivisPC(projet, d, local string) []ecartePC {
	out, err := gTravailGH(projet, "diff", "--name-only", "-z", "HEAD", local, "--", ".athanor/").Output()
	if err != nil {
		return nil
	}
	var v []ecartePC
	dest := filepath.Join(dossierAvantEquipePC(d), time.Now().Format("20060102-150405.000"))
	for _, f := range strings.Split(string(out), "\x00") {
		chemin := filepath.Join(projet, filepath.FromSlash(f))
		if f == "" || !estFichierRegulierPC(chemin) || gTravailGH(projet, "ls-files", "--error-unmatch", "--", f).Run() == nil {
			continue
		}
		leur, err := gTravailGH(projet, "cat-file", "blob", local+":"+f).Output()
		if err != nil {
			continue // not in their work (deleted there): git leaves it alone
		}
		if mien, err := os.ReadFile(chemin); err == nil && bytes.Equal(bytes.ReplaceAll(mien, []byte("\r\n"), []byte("\n")), leur) {
			_ = os.Remove(chemin)
			continue
		}
		garde := filepath.Join(dest, filepath.FromSlash(f))
		if os.MkdirAll(filepath.Dir(garde), 0o700) == nil && os.Rename(chemin, garde) == nil {
			v = append(v, ecartePC{f, garde})
		}
	}
	return v
}

// joindreNonSuivisPC: after the merge, the lines of the notes put aside that the merged notes lack, added
// to them (in the merge commit); the other files stay put aside.
func joindreNonSuivisPC(projet string, ecartes []ecartePC, ident []string, local string) {
	ajoute := false
	for _, e := range ecartes {
		if !fichiersUnionPC[filepath.Base(e.chemin)] {
			continue
		}
		mien, err1 := os.ReadFile(e.garde)
		chemin := filepath.Join(projet, filepath.FromSlash(e.chemin))
		fusion, err2 := os.ReadFile(chemin)
		if err1 != nil || err2 != nil {
			continue
		}
		deja := map[string]bool{}
		for _, l := range strings.Split(strings.ReplaceAll(string(fusion), "\r\n", "\n"), "\n") {
			deja[l] = true
		}
		var plus []string
		for _, l := range strings.Split(strings.ReplaceAll(string(mien), "\r\n", "\n"), "\n") {
			if strings.TrimSpace(l) != "" && !deja[l] {
				plus = append(plus, l)
			}
		}
		if len(plus) == 0 {
			continue
		}
		texte := string(fusion)
		if !strings.HasSuffix(texte, "\n") {
			texte += "\n"
		}
		if os.WriteFile(chemin, []byte(texte+strings.Join(plus, "\n")+"\n"), 0o644) == nil && gTravailGH(projet, "add", "--", e.chemin).Run() == nil {
			ajoute = true
		}
	}
	if ajoute {
		// the merge commit, made here and not pushed yet: these lines belong in it (never a commit of the
		// others, after a fast-forward: that one is on GitHub already)
		tete, _ := gProjetGH(projet+"/.git", "rev-parse", "HEAD")
		leur, _ := gProjetGH(projet+"/.git", "rev-parse", local)
		if tete != leur && gTravailGH(projet, "rev-parse", "-q", "--verify", "HEAD^2").Run() == nil {
			_ = gTravailGH(projet, append(ident, "commit", "-q", "--amend", "--no-edit", "--no-verify")...).Run()
		} else {
			_ = gTravailGH(projet, append(ident, "commit", "-q", "--no-verify", "-m", "Notes of this computer joined to the team's")...).Run()
		}
	}
}

// remettreNonSuivisPC: the merge did not happen: the files put aside back in their place.
func remettreNonSuivisPC(projet string, ecartes []ecartePC) {
	for _, e := range ecartes {
		chemin := filepath.Join(projet, filepath.FromSlash(e.chemin))
		if _, err := os.Lstat(chemin); err != nil {
			_ = os.MkdirAll(filepath.Dir(chemin), 0o755)
			_ = os.Rename(e.garde, chemin)
		}
	}
}

func estFichierRegulierPC(f string) bool {
	st, err := os.Lstat(f)
	return err == nil && st.Mode().IsRegular()
}

// identPC: who the merges of the pilot are made by (the project's git author, else this computer).
func identPC(projet string) []string {
	gd := projet + "/.git"
	nom, _ := gProjetGH(gd, "config", "--get", "user.name")
	email, _ := gProjetGH(gd, "config", "--get", "user.email")
	return []string{"-c", "user.name=" + firstNonEmpty(nom, firstNonEmpty(lireMachinePC().Nom, "Athanor")), "-c", "user.email=" + firstNonEmpty(email, "athanor@users.noreply.github.com")}
}

// propreTravailPC: no change to the project's tracked files (what a merge could touch).
func propreTravailPC(projet string) bool {
	out, err := gTravailGH(projet, "status", "--porcelain", "--untracked-files=no").Output()
	return err == nil && strings.TrimSpace(string(out)) == ""
}

// ecrireAttributsPC: .athanor/.gitattributes, so that what every computer adds at the end of the log
// (and of the other shared notes) is joined at a merge, never a conflict (kept on this computer only).
func ecrireAttributsPC(projet string) error {
	return fichierAtelier(projet, ".gitattributes", false, strings.NewReader("LOG.md merge=union\nBUGS.md merge=union\nSOLUTIONS.md merge=union\nDECISIONS.md merge=union\nBLOCKERS.md merge=union\nTEAM_NOTES.md merge=union\n"))
}

func consigneFusionPC(projet, local string, fichiers []string, sortie string, equipe bool) error {
	var b strings.Builder
	ref := strings.TrimPrefix(local, "--allow-unrelated-histories ")
	if ref != local {
		// the user brought in a GitHub branch with a history of its own (outil_github_recuperer.go)
		b.WriteString("# Bring GitHub's files in first\n\n")
		b.WriteString("The user chose to bring into the project the files of GitHub's branch, whose history shares nothing with this project's (typically a README, a licence, a .gitignore GitHub made when the repository was created). Some files exist on both sides with different contents; Athanor did not merge them (nothing was changed). Before anything else:\n\n")
	} else if equipe {
		b.WriteString("# Merge the other computers' work first\n\n")
		b.WriteString("Other computers of the team pushed commits that touch the same lines as this computer's. The pilot did not merge them (nothing was changed). Before anything else:\n\n")
	} else {
		// working in turns on one repository (outil_github_recuperer.go)
		b.WriteString("# Merge the work pushed to GitHub first\n\n")
		b.WriteString("Someone else working on this repository pushed commits to GitHub that touch the same lines as this folder's own commits. The user asked to bring them in; Athanor did not merge them (nothing was changed). Before anything else:\n\n")
	}
	fmt.Fprintf(&b, "1. `git merge %s`\n", local)
	b.WriteString("2. Resolve each conflict keeping both computers' work (their milestones and yours), never one side thrown away; in .athanor/PLAN.md keep every milestone ticked on either side.\n")
	b.WriteString("3. Run `bash .athanor/check.sh`, commit the merge, note it in .athanor/LOG.md, then delete this file.\n\n")
	if strings.Contains(sortie, "untracked working tree files would be overwritten") {
		fmt.Fprintf(&b, "Here git stopped before merging anything: files of this folder that were never committed are in the other computers' commits (listed below). For each one, compare it with theirs (`git show %s:<file>`), keep in theirs what this computer's version has that matters, then move this computer's copy out of the way (or delete it when it is only build output) and merge.\n\n", ref)
	}
	if len(fichiers) > 0 {
		b.WriteString("Files both sides changed: ")
		for i, f := range fichiers {
			if i == 20 {
				b.WriteString("…")
				break
			}
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("`" + f + "`")
		}
		b.WriteString("\n")
	}
	if sortie != "" {
		b.WriteString("\ngit said:\n```\n" + coupeOctets(sortie, 1500) + "\n```\n")
	}
	return fichierAtelier(projet, "MERGE_NEEDED.md", false, strings.NewReader(b.String()))
}

func ecrireFichierPrive(f, contenu string) error {
	t, err := os.CreateTemp(filepath.Dir(f), ".pcs.")
	if err != nil {
		return err
	}
	_, _ = t.WriteString(contenu)
	t.Close()
	return os.Rename(t.Name(), f)
}

func removeSiExiste(f string) { _ = os.Remove(f) }
