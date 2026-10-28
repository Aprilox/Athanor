package main

// The team's workshop through the direct link (2.45): the shared files of .athanor/ (the plan, the request,
// the log, the notes… atelierPartage) go from computer to computer through the master instead of the
// commits, so a public repository never carries them; .athanor/ then stays out of git on every computer.
//
// The master keeps the team's workshop in a store of its pilot's private folder (atelier-equipe/):
//   objets/<sha256>            each file's content, once
//   versions/<000000000012>.json   {version, fichiers: {"PLAN.md": sha…}, par, quand}: one per change
// A version is taken with a file created exclusively (versions/N.claim), which works the same from the
// Windows application and from the pilot in WSL on the same disk (their locks would not see each other):
// two computers writing at the same moment, the second is told the store changed and merges again.
//
// Every pilot synchronises its workshop with the store between two cycles (synchroniserAtelier): a
// three-way merge per file against what it last had in common with the store (its base, kept in its private
// folder): the notes every computer adds to are joined (their lines), the plan's ticked boxes are joined, any
// other text merged by git merge-file; when both sides changed the same lines, the team's version stays and
// this computer's goes to .athanor/merge/, with .athanor/WORKSHOP_MERGE.md telling Claude to fold it in.
// The master's pilot uses the store itself; a node's goes through the master's tunnel (/equipe/atelier…).

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	tailleMaxFichierAtelier = 8 << 20  // a bigger file of the workshop is not shared through the link
	tailleMaxEnvoiAtelier   = 48 << 20 // one write to the store
	versionsGardeesAtelier  = 30
)

// ManifesteAtelier: one version of the team's workshop.
type ManifesteAtelier struct {
	Version  int64             `json:"version"`
	Fichiers map[string]string `json:"fichiers"` // path in .athanor/ → sha256 (hex)
	Par      string            `json:"par,omitempty"`
	Quand    string            `json:"quand,omitempty"`
}

var (
	errConflitAtelier = errors.New("the team's workshop changed meanwhile")
	errGeleAtelier    = errors.New("the team's workshop is moving to another master")
	reShaAtelier      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reVersionAtelier  = regexp.MustCompile(`^([0-9]{12})\.json$`)
)

func shaAtelier(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// cheminAtelierValide: a path of the shared workshop, as the store names it ("PLAN.md", "docs/A.md"):
// relative, clean, never out of .athanor/ nor into a part of it that is not shared.
func cheminAtelierValide(rel string) bool {
	if rel == "" || len(rel) > 300 || strings.ContainsAny(rel, "\\:\x00") || strings.HasPrefix(rel, "/") ||
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))) != rel || !utf8.ValidString(rel) {
		return false
	}
	for _, p := range strings.Split(rel, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return atelierPartageable(".athanor/" + rel)
}

// ---------------------------------------------------------------------------- the store

// magasinAtelier: where a computer's workshop is synchronised with the team's.
type magasinAtelier interface {
	dernier() (ManifesteAtelier, error)
	objets(shas []string) (map[string][]byte, error)
	// ecrire: the new version over base; errConflitAtelier when the store is no longer at base
	ecrire(base int64, fichiers map[string]string, objets map[string][]byte, par string) (int64, error)
}

// magasinLocal: the master's store, on its disk.
type magasinLocal struct{ dossier string }

func (m magasinLocal) gele() bool {
	_, err := os.Stat(filepath.Join(m.dossier, "gele"))
	return err == nil
}

func (m magasinLocal) dernier() (ManifesteAtelier, error) {
	v := ManifesteAtelier{Fichiers: map[string]string{}}
	l, err := os.ReadDir(filepath.Join(m.dossier, "versions"))
	if err != nil {
		if os.IsNotExist(err) {
			return v, nil
		}
		return v, err
	}
	for i := len(l) - 1; i >= 0; i-- { // ReadDir sorts by name: the last one first
		if !reVersionAtelier.MatchString(l[i].Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.dossier, "versions", l[i].Name()))
		if err != nil {
			return v, err
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return v, err
		}
		if v.Fichiers == nil {
			v.Fichiers = map[string]string{}
		}
		return v, nil
	}
	return v, nil
}

func (m magasinLocal) objets(shas []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, s := range shas {
		if !reShaAtelier.MatchString(s) {
			return nil, errors.New("bad object name")
		}
		b, err := os.ReadFile(filepath.Join(m.dossier, "objets", s))
		if err != nil {
			return nil, fmt.Errorf("object %s missing from the team's workshop", s[:12])
		}
		out[s] = b
	}
	return out, nil
}

func (m magasinLocal) ecrire(base int64, fichiers map[string]string, objets map[string][]byte, par string) (int64, error) {
	if m.gele() {
		return 0, errGeleAtelier
	}
	d, err := m.dernier()
	if err != nil {
		return 0, err
	}
	if d.Version != base {
		return 0, errConflitAtelier
	}
	for rel, s := range fichiers {
		if !cheminAtelierValide(rel) || !reShaAtelier.MatchString(s) {
			return 0, errors.New("bad path in the workshop: " + tronquerRunes(rel, 80))
		}
	}
	dObj := filepath.Join(m.dossier, "objets")
	if err := os.MkdirAll(dObj, 0o700); err != nil {
		return 0, err
	}
	for s, b := range objets {
		if shaAtelier(b) != s {
			return 0, errors.New("an object does not match its name")
		}
		f := filepath.Join(dObj, s)
		if _, err := os.Stat(f); err == nil {
			continue
		}
		if err := ecrireAtomique(f, b, 0o600); err != nil {
			return 0, err
		}
	}
	for _, s := range fichiers {
		if _, err := os.Stat(filepath.Join(dObj, s)); err != nil {
			return 0, errors.New("a file of the new version was not sent")
		}
	}
	dVer := filepath.Join(m.dossier, "versions")
	if err := os.MkdirAll(dVer, 0o700); err != nil {
		return 0, err
	}
	n := base + 1
	for essai := 0; ; essai++ {
		nom := filepath.Join(dVer, fmt.Sprintf("%012d", n))
		f, err := os.OpenFile(nom+".claim", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			break
		}
		if !os.IsExist(err) || essai >= 3 {
			return 0, errConflitAtelier
		}
		// a claim whose writer went away (no version after 2 minutes): the next number
		st, e2 := os.Stat(nom + ".claim")
		if _, e3 := os.Stat(nom + ".json"); e3 == nil || e2 != nil || time.Since(st.ModTime()) < 2*time.Minute {
			return 0, errConflitAtelier
		}
		n++
	}
	v := ManifesteAtelier{Version: n, Fichiers: fichiers, Par: par, Quand: time.Now().UTC().Format(time.RFC3339)}
	b, _ := json.MarshalIndent(v, "", " ")
	if err := ecrireAtomique(filepath.Join(dVer, fmt.Sprintf("%012d.json", n)), b, 0o600); err != nil {
		return 0, err
	}
	if n%10 == 0 {
		m.menage()
	}
	return n, nil
}

// menage: the oldest versions and the contents no kept version names leave the store.
func (m magasinLocal) menage() {
	dVer := filepath.Join(m.dossier, "versions")
	l, err := os.ReadDir(dVer)
	if err != nil {
		return
	}
	var versions []string
	for _, e := range l {
		if reVersionAtelier.MatchString(e.Name()) {
			versions = append(versions, e.Name())
		}
	}
	if len(versions) <= versionsGardeesAtelier {
		return
	}
	gardees := versions[len(versions)-versionsGardeesAtelier:]
	premiere := strings.TrimSuffix(gardees[0], ".json")
	for _, e := range l {
		if strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".json"), ".claim") < premiere {
			_ = os.Remove(filepath.Join(dVer, e.Name()))
		}
	}
	vivants := map[string]bool{}
	for _, g := range gardees {
		var v ManifesteAtelier
		b, err := os.ReadFile(filepath.Join(dVer, g))
		if err != nil || json.Unmarshal(b, &v) != nil {
			return // never a content removed on a version not read
		}
		for _, s := range v.Fichiers {
			vivants[s] = true
		}
	}
	objs, _ := os.ReadDir(filepath.Join(m.dossier, "objets"))
	for _, o := range objs {
		if info, err := o.Info(); err == nil && !vivants[o.Name()] && time.Since(info.ModTime()) > 10*time.Minute {
			_ = os.Remove(filepath.Join(m.dossier, "objets", o.Name()))
		}
	}
}

// magasinHTTP: the master's store, from a node, through the master's tunnel.
type magasinHTTP struct{ adresse, jeton string }

var clientAtelier = &http.Client{Timeout: 90 * time.Second}

func (m magasinHTTP) appel(chemin string, corps, v any) (int, error) {
	var b io.Reader
	methode := "GET"
	if corps != nil {
		j, _ := json.Marshal(corps)
		b, methode = bytes.NewReader(j), "POST"
	}
	r, err := http.NewRequest(methode, strings.TrimRight(m.adresse, "/")+chemin, b)
	if err != nil {
		return 0, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+m.jeton)
	rep, err := clientAtelier.Do(r)
	if err != nil {
		return 0, err
	}
	defer rep.Body.Close()
	contenu, _ := io.ReadAll(io.LimitReader(rep.Body, tailleMaxEnvoiAtelier*2))
	switch rep.StatusCode {
	case 200:
		return 200, json.Unmarshal(contenu, v)
	case 409:
		return 409, errConflitAtelier
	case 423:
		return 423, errGeleAtelier
	}
	var e struct {
		Erreur string `json:"erreur"`
	}
	_ = json.Unmarshal(contenu, &e)
	return rep.StatusCode, errors.New(firstNonEmpty(e.Erreur, rep.Status))
}

func (m magasinHTTP) dernier() (ManifesteAtelier, error) {
	var v ManifesteAtelier
	_, err := m.appel("/equipe/atelier", nil, &v)
	if v.Fichiers == nil {
		v.Fichiers = map[string]string{}
	}
	return v, err
}

func (m magasinHTTP) objets(shas []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for len(shas) > 0 {
		n := min(len(shas), 200)
		var rep struct {
			Objets map[string]string `json:"objets"`
		}
		if _, err := m.appel("/equipe/atelier/objets", map[string]any{"shas": shas[:n]}, &rep); err != nil {
			return nil, err
		}
		if len(rep.Objets) == 0 {
			return nil, errors.New("the master sent none of the workshop's files asked for")
		}
		for s, b64 := range rep.Objets {
			b, err := base64.StdEncoding.DecodeString(b64)
			if err != nil || shaAtelier(b) != s {
				return nil, errors.New("a file of the team's workshop came damaged")
			}
			out[s] = b
		}
		var reste []string // the master answers as many as fit: the others again
		for _, s := range shas {
			if _, ok := out[s]; !ok {
				reste = append(reste, s)
			}
		}
		shas = reste
	}
	return out, nil
}

func (m magasinHTTP) ecrire(base int64, fichiers map[string]string, objets map[string][]byte, par string) (int64, error) {
	o := map[string]string{}
	for s, b := range objets {
		o[s] = base64.StdEncoding.EncodeToString(b)
	}
	var rep struct {
		Version int64 `json:"version"`
	}
	_, err := m.appel("/equipe/atelier/ecrire", map[string]any{"base": base, "fichiers": fichiers, "objets": o, "par": par}, &rep)
	return rep.Version, err
}

// ---------------------------------------------------------------------------- one computer's workshop

// dossiersAtelierPartage: the folders of .athanor/ the shared workshop has files in.
var dossiersAtelierPartage = map[string]bool{"docs": true, "queue": true, "done": true, "archives": true, "agents": true, "attachments": true}

// fichiersAtelierLocaux: the shared files of this computer's workshop (never through a link, never one
// bigger than the link takes: trop lists those).
func fichiersAtelierLocaux(etat string) (map[string][]byte, []string) {
	out := map[string][]byte{}
	var trop []string
	_ = filepath.WalkDir(etat, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(etat, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			// only the folders the shared workshop has are walked (never logs/, the workers' copies…)
			if rel != "." && (!strings.Contains(rel, "/") && !dossiersAtelierPartage[rel] || strings.Count(rel, "/") == 1 && strings.HasPrefix(rel, "attachments/") && rel != "attachments/request") {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() || !cheminAtelierValide(rel) {
			return nil
		}
		if info, err := e.Info(); err == nil && info.Size() > tailleMaxFichierAtelier {
			trop = append(trop, rel)
			return nil
		}
		if b, err := lireOctets(p, tailleMaxFichierAtelier); err == nil {
			out[rel] = b
		}
		return nil
	})
	sort.Strings(trop)
	return out, trop
}

// baseAtelier: what this computer last had in common with one store (its private folder).
type baseAtelier struct {
	dossier  string
	Fichiers map[string]string `json:"fichiers"`
	Existe   bool              `json:"-"`
}

func lireBaseAtelier(dossier string) baseAtelier {
	b := baseAtelier{dossier: dossier, Fichiers: map[string]string{}}
	if x, err := os.ReadFile(filepath.Join(dossier, "base.json")); err == nil && json.Unmarshal(x, &b) == nil {
		b.Existe = true
		if b.Fichiers == nil {
			b.Fichiers = map[string]string{}
		}
	}
	return b
}

func (b baseAtelier) contenu(s string) ([]byte, bool) {
	if !reShaAtelier.MatchString(s) {
		return nil, false
	}
	x, err := os.ReadFile(filepath.Join(b.dossier, "objets", s))
	return x, err == nil
}

// ecrireBase: the new common version, with the contents a next merge needs (the others go).
func ecrireBase(dossier string, fichiers map[string]string, contenus map[string][]byte) error {
	dObj := filepath.Join(dossier, "objets")
	if err := os.MkdirAll(dObj, 0o700); err != nil {
		return err
	}
	vivants := map[string]bool{}
	for _, s := range fichiers {
		vivants[s] = true
		f := filepath.Join(dObj, s)
		if _, err := os.Stat(f); err == nil {
			continue
		}
		if c, ok := contenus[s]; ok {
			if err := ecrireAtomique(f, c, 0o600); err != nil {
				return err
			}
		}
	}
	b, _ := json.MarshalIndent(map[string]any{"fichiers": fichiers}, "", " ")
	if err := ecrireAtomique(filepath.Join(dossier, "base.json"), b, 0o600); err != nil {
		return err
	}
	l, _ := os.ReadDir(dObj)
	for _, e := range l {
		if !vivants[e.Name()] {
			_ = os.Remove(filepath.Join(dObj, e.Name()))
		}
	}
	return nil
}

// ResultatAtelier: what one synchronisation did.
type ResultatAtelier struct {
	Recus    []string // files that came from the team
	Envoyes  []string // files this computer gave the team
	Conflits []string // both changed the same lines: the team's kept, this computer's in .athanor/merge/
	Trop     []string // too big for the link (they stay on this computer)
	Version  int64
}

// fusionAtelier: merges a file both sides changed since their base: the merged content and whether this
// computer's had to be put aside (conflit). nil: the file is gone.
type fusionAtelier func(rel string, base, ici, equipe []byte) (resultat []byte, conflit bool)

// synchroniserAtelier: this computer's workshop (etat: PROJECT/.athanor) and the store's made one, its
// base for this store (dossierBase) moved to it. No base yet (a computer joining the team): the team's
// version of a file both have wins, this computer's is kept in garde (never lost); without garde, the
// two are merged as when both changed it.
func synchroniserAtelier(etat, dossierBase string, m magasinAtelier, fusion fusionAtelier, par, garde string) (ResultatAtelier, error) {
	var res ResultatAtelier
	base := lireBaseAtelier(dossierBase)
	premiere := !base.Existe
	bases := map[string]string{}
	for k, v := range base.Fichiers {
		bases[k] = v
	}
	contenusBase := map[string][]byte{}
	for essai := 0; essai < 5; essai++ {
		res = ResultatAtelier{}
		locaux, trop := fichiersAtelierLocaux(etat)
		res.Trop = trop
		equipe, err := m.dernier()
		if err != nil {
			return res, err
		}
		for rel := range equipe.Fichiers {
			if !cheminAtelierValide(rel) || !reShaAtelier.MatchString(equipe.Fichiers[rel]) {
				return res, errors.New("the team's workshop names a file it must not: " + tronquerRunes(rel, 80))
			}
		}
		shaLoc := map[string]string{}
		for rel, b := range locaux {
			shaLoc[rel] = shaAtelier(b)
		}
		chemins := map[string]bool{}
		for _, x := range []map[string]string{shaLoc, bases, equipe.Fichiers} {
			for rel := range x {
				chemins[rel] = true
			}
		}
		for _, rel := range trop {
			delete(chemins, rel) // a file too big here: left as it is on both sides
		}
		// the contents needed: the team's that changed, the bases of the files both changed
		var manque []string
		vu := map[string]bool{}
		for rel := range chemins {
			l, b, e := shaLoc[rel], bases[rel], equipe.Fichiers[rel]
			if e != "" && e != l && e != b && !vu[e] {
				if _, ok := contenusBase[e]; !ok {
					manque, vu[e] = append(manque, e), true
				}
			}
		}
		recus, err := m.objets(manque)
		if err != nil {
			return res, err
		}
		for s, c := range recus {
			contenusBase[s] = c
		}
		contenuDe := func(s string) []byte {
			if s == "" {
				return nil
			}
			if c, ok := contenusBase[s]; ok {
				return c
			}
			if c, ok := base.contenu(s); ok {
				return c
			}
			return nil
		}
		fusionne := map[string]string{}
		ecrits := map[string][]byte{} // rel → new content here (nil: removed here)
		envoi := map[string][]byte{}  // sha → content the store lacks
		var conflits []string
		liste := make([]string, 0, len(chemins))
		for rel := range chemins {
			liste = append(liste, rel)
		}
		sort.Strings(liste)
		for _, rel := range liste {
			l, b, e := shaLoc[rel], bases[rel], equipe.Fichiers[rel]
			var mc []byte
			var sm string
			switch {
			case l == e:
				sm = l
			case l == b:
				sm = e // the team changed it (or removed it)
				if e != "" {
					mc = contenuDe(e)
				}
				ecrits[rel] = mc
			case e == b:
				sm = l // this computer changed it (or removed it)
			default:
				// both changed it: a removal never wins over a change
				var c []byte
				conflit := false
				switch {
				case l == "":
					c = contenuDe(e)
				case e == "":
					c = locaux[rel]
				case premiere && garde != "" && !fichiersUnionPC[filepath.Base(rel)]:
					c = contenuDe(e)
					if garde != "" {
						_ = ecrireAtomique(filepath.Join(garde, filepath.FromSlash(rel)), locaux[rel], 0o600)
					}
				default:
					var bc []byte
					if b != "" {
						bc = contenuDe(b)
					}
					c, conflit = fusion(rel, bc, locaux[rel], contenuDe(e))
				}
				if conflit {
					conflits = append(conflits, rel)
					if err := ecrireMergeAtelier(etat, rel, locaux[rel]); err != nil {
						return res, err
					}
				}
				if c == nil {
					sm = ""
				} else {
					sm = shaAtelier(c)
				}
				if sm != l {
					ecrits[rel] = c
				}
				if sm != "" && sm != e {
					envoi[sm] = c
				}
			}
			if sm != "" {
				fusionne[rel] = sm
				if sm == l && sm != e {
					envoi[sm] = locaux[rel]
				}
			}
		}
		// this computer's files first: what the team brought is never lost if the store refuses the write
		for rel, c := range ecrits {
			if err := ecrireFichierAtelierPartage(etat, rel, c); err != nil {
				return res, err
			}
			if c != nil {
				contenusBase[shaAtelier(c)] = c
			}
			res.Recus = append(res.Recus, rel)
		}
		for rel, s := range fusionne {
			if equipe.Fichiers[rel] != s {
				res.Envoyes = append(res.Envoyes, rel)
			}
		}
		for rel := range equipe.Fichiers {
			if _, ok := fusionne[rel]; !ok {
				res.Envoyes = append(res.Envoyes, rel) // removed here
			}
		}
		sort.Strings(res.Recus)
		sort.Strings(res.Envoyes)
		res.Conflits = conflits
		version := equipe.Version
		if len(res.Envoyes) > 0 {
			taille := 0
			for _, c := range envoi {
				taille += len(c)
			}
			if taille > tailleMaxEnvoiAtelier {
				return res, fmt.Errorf("%d MB to send at once: more than the link takes (%d MB)", taille>>20, tailleMaxEnvoiAtelier>>20)
			}
			v, err := m.ecrire(equipe.Version, fusionne, envoi, par)
			if errors.Is(err, errConflitAtelier) {
				// another computer wrote meanwhile: what the store had is now in this computer's files,
				// so it is the common base of the next try
				bases = equipe.Fichiers
				for s, c := range envoi {
					contenusBase[s] = c
				}
				for rel, c := range locaux {
					if equipe.Fichiers[rel] == shaAtelier(c) {
						contenusBase[equipe.Fichiers[rel]] = c
					}
				}
				for _, s := range equipe.Fichiers {
					if _, ok := contenusBase[s]; !ok {
						if c, ok := base.contenu(s); ok {
							contenusBase[s] = c
						}
					}
				}
				premiere = false
				continue
			}
			if err != nil {
				return res, err
			}
			version = v
		}
		for _, c := range envoi {
			contenusBase[shaAtelier(c)] = c
		}
		for rel, c := range locaux {
			if fusionne[rel] == shaAtelier(c) {
				contenusBase[shaAtelier(c)] = c
			}
		}
		if err := ecrireBase(dossierBase, fusionne, contenusBase); err != nil {
			return res, err
		}
		if len(conflits) > 0 {
			_ = noterMergeAtelier(etat, conflits)
		}
		res.Version = version
		return res, nil
	}
	return res, errConflitAtelier
}

// ecrireFichierAtelierPartage: a shared file of the workshop written (or removed: nil), never through a link.
func ecrireFichierAtelierPartage(etat, rel string, c []byte) error {
	if !cheminAtelierValide(rel) {
		return errors.New("bad path in the workshop: " + rel)
	}
	parties := strings.Split(rel, "/")
	projet := filepath.Dir(etat)
	d, err := dossierAtelierSur(projet, parties[:len(parties)-1]...)
	if c == nil {
		if err != nil {
			return nil
		}
		f := filepath.Join(d, parties[len(parties)-1])
		if st, err := os.Lstat(f); err == nil && (st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0) {
			return os.Remove(f)
		}
		return nil
	}
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if strings.HasSuffix(rel, ".sh") {
		mode = 0o755
	}
	return ecrireAtomique(filepath.Join(d, parties[len(parties)-1]), c, mode)
}

// ecrireMergeAtelier: this computer's version of a file the team changed at the same lines, put aside.
func ecrireMergeAtelier(etat, rel string, c []byte) error {
	parties := strings.Split(rel, "/")
	d, err := dossierAtelierSur(filepath.Dir(etat), append([]string{"merge"}, parties[:len(parties)-1]...)...)
	if err != nil {
		return err
	}
	return ecrireAtomique(filepath.Join(d, parties[len(parties)-1]), c, 0o644)
}

// noterMergeAtelier: .athanor/WORKSHOP_MERGE.md, what Claude folds in first (kept on this computer).
func noterMergeAtelier(etat string, conflits []string) error {
	f := filepath.Join(etat, "WORKSHOP_MERGE.md")
	vus := map[string]bool{}
	if b, err := os.ReadFile(f); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "- `") {
				vus[strings.Trim(strings.TrimPrefix(l, "- "), "`")] = true
			}
		}
	}
	for _, c := range conflits {
		vus[c] = true
	}
	var l []string
	for c := range vus {
		l = append(l, c)
	}
	sort.Strings(l)
	var b strings.Builder
	b.WriteString("# Merge the team's workshop first\n\n")
	b.WriteString("Another computer of the team changed these workshop files on the same lines as this computer, between two cycles. The team's version is in place; this computer's is kept in `.athanor/merge/` under the same name. Before anything else, for each file: fold into the team's version what this computer's has that matters (keep both computers' work, never one side thrown away; in PLAN.md keep every milestone ticked on either side), then delete its copy in `.athanor/merge/`, and delete this file once they are all done.\n\n")
	for _, c := range l {
		b.WriteString("- `" + c + "`\n")
	}
	return ecrireAtomique(f, []byte(b.String()), 0o644)
}

// unionLignesAtelier: a note every computer adds to (LOG.md…): the team's, then this computer's new lines.
func unionLignesAtelier(base, ici, equipe []byte) []byte {
	norm := func(b []byte) []string {
		return strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	}
	deja := map[string]bool{}
	for _, l := range norm(base) {
		deja[l] = true
	}
	for _, l := range norm(equipe) {
		deja[l] = true
	}
	var plus []string
	for _, l := range norm(ici) {
		if !deja[l] && strings.TrimSpace(l) != "" {
			plus = append(plus, l)
			deja[l] = true
		}
	}
	if len(plus) == 0 {
		return equipe
	}
	t := strings.ReplaceAll(string(equipe), "\r\n", "\n")
	if t != "" && !strings.HasSuffix(t, "\n") {
		t += "\n"
	}
	return []byte(t + strings.Join(plus, "\n") + "\n")
}

// fusionAtelierGit: the pilot's merge: the notes joined, the plan's boxes joined, any other text through
// git merge-file; binary, or a conflict of lines: the team's, this computer's put aside.
func fusionAtelierGit(rel string, base, ici, equipe []byte) ([]byte, bool) {
	if fichiersUnionPC[filepath.Base(rel)] {
		return unionLignesAtelier(base, ici, equipe), false
	}
	if rel == "PLAN.md" {
		if f, ok := fusionnerCochesPC(string(equipe), string(ici)); ok {
			return []byte(f), false
		}
	}
	texte := func(b []byte) bool { return utf8.Valid(b) && !bytes.Contains(b, []byte{0}) && len(b) < 4<<20 }
	if !texte(ici) || !texte(equipe) || !texte(base) {
		return equipe, true
	}
	d, err := os.MkdirTemp("", "atelier-fusion-")
	if err != nil {
		return equipe, true
	}
	defer os.RemoveAll(d)
	f := func(n string, b []byte) string { p := filepath.Join(d, n); _ = os.WriteFile(p, b, 0o600); return p }
	ic, ba, eq := f("ici", ici), f("base", base), f("equipe", equipe)
	c := commande("git", "merge-file", "-p", "-L", "this computer", "-L", "base", "-L", "team", ic, ba, eq)
	c.Env = sansEnvGitPC()
	out, err := c.Output()
	if err != nil {
		return equipe, true // a conflict (exit > 0), or no git: never markers left in a file
	}
	return out, false
}

// ---------------------------------------------------------------------------- the pilot's side

// equipeDirectePilote: the direct link of the pilot's project, as the application left it in the pilot's
// private folder; ok: the team's workshop goes through it.
func equipeDirectePilote(prive string) (equipeDirecte, bool) {
	var e equipeDirecte
	b, err := os.ReadFile(filepath.Join(prive, "equipe-directe.json"))
	if err != nil || json.Unmarshal(b, &e) != nil {
		return e, false
	}
	return e, e.AtelierLien && (e.Role == "maitre" || (e.Role == "noeud" && e.Adresse != "" && e.Jeton != ""))
}

func lienAtelierPilote(prive string) bool { _, ok := equipeDirectePilote(prive); return ok }

// atelierParLienProjet: the project's workshop goes through the direct link (the GitHub commands, which
// then never put .athanor/ back in the commits).
func atelierParLienProjet(projet string) bool {
	_, ok := equipeDirectePilote(priveDePilote(kitDuProgramme(), projet, estWindows))
	return ok
}

// magasinPilote: the store the pilot synchronises with (the master's own, or the master's through its
// link). The base is the same for both: the team's store moves with its master (a handover), its files
// the same.
func magasinPilote(prive string, e equipeDirecte) magasinAtelier {
	if e.Role == "maitre" {
		return magasinLocal{filepath.Join(prive, "atelier-equipe")}
	}
	return magasinHTTP{e.Adresse, e.Jeton}
}

// atelierLien: the pilot's workshop synchronised with the team's (several computers, the workshop through
// the direct link); false when the project does not work so. Never stops the pilot: a master out of reach,
// and a node goes on through the team's copy on GitHub (encrypted); the master merges that copy each time.
func (p *pilote) atelierLien() bool {
	e, ok := equipeDirectePilote(p.prive)
	if !ok {
		return false
	}
	par := lireMachinePC().Nom
	sync := func(m magasinAtelier, base string) (ResultatAtelier, error) {
		return synchroniserAtelier(p.etat, filepath.Join(p.prive, "atelier-base", base), m, fusionAtelierGit, par,
			filepath.Join(p.prive, "atelier-avant", time.Now().Format("20060102-150405")))
	}
	dire := func(res ResultatAtelier, ou string) {
		if len(res.Recus) > 0 || len(res.Envoyes) > 0 {
			p.log(fmt.Sprintf("Team workshop (%s): %s received, %s given to the team (version %d).", ou, listeCourte(res.Recus), listeCourte(res.Envoyes), res.Version))
		}
		if len(res.Conflits) > 0 {
			p.log("Team workshop: changed here and by another computer on the same lines: " + strings.Join(res.Conflits, ", ") + " (the team's kept; Claude folds this computer's in first, .athanor/WORKSHOP_MERGE.md).")
		}
		if len(res.Trop) > 0 {
			p.logUneFois("atelier-trop:"+strings.Join(res.Trop, ","), "Team workshop: too big for the direct link, kept on this computer: "+strings.Join(res.Trop, ", ")+" (put such files in the project itself: they go through GitHub).")
		}
	}
	github := func() (ResultatAtelier, error) {
		m, err := ouvrirMagasinGitHub(p.projet, e.Cle)
		if err != nil {
			return ResultatAtelier{}, err
		}
		// (a first time: what differs is merged, or put aside for Claude, never thrown away)
		return synchroniserAtelier(p.etat, filepath.Join(p.prive, "atelier-base", "github"), m, fusionAtelierGit, par, "")
	}
	res, err := sync(magasinPilote(p.prive, e), "equipe")
	if err != nil {
		if e.Role != "noeud" || errors.Is(err, errConflitAtelier) {
			p.logUneFois("atelier-lien", "Team workshop: "+messageAtelier(err)+": this computer goes on with its own and gives it to the team at the next try.")
			return true
		}
		// the master out of reach: through the team's copy on GitHub, encrypted
		resG, errG := github()
		if errG != nil {
			p.logUneFois("atelier-lien", "Team workshop: "+messageAtelier(err)+", and GitHub cannot take it either ("+tronquerRunes(errG.Error(), 120)+"): this computer goes on with its own and gives it to the team at the next try.")
			return true
		}
		p.logUneFois("atelier-lien", "Team workshop: "+messageAtelier(err)+": through the team's copy on GitHub (encrypted) meanwhile.")
		dire(resG, "GitHub, encrypted")
		return true
	}
	p.oublierLogUneFois("atelier-lien")
	dire(res, "direct link")
	if e.Role == "maitre" {
		// the copy on GitHub: what the nodes wrote there while this computer was out of reach comes in, and
		// the team's workshop goes there for the next time
		resG, errG := github()
		if errG != nil {
			p.logUneFois("atelier-github", "Team workshop: its encrypted copy on GitHub is not up to date ("+tronquerRunes(errG.Error(), 160)+").")
		} else {
			p.oublierLogUneFois("atelier-github")
			if len(resG.Recus) > 0 {
				p.log("Team workshop: what the other computers wrote on GitHub while the direct link was down: " + listeCourte(resG.Recus) + ".")
				if res2, err := sync(magasinPilote(p.prive, e), "equipe"); err == nil {
					dire(res2, "direct link")
				}
			}
		}
	}
	return true
}

func messageAtelier(err error) string {
	switch {
	case errors.Is(err, errGeleAtelier):
		return "the team's workshop is moving to another master"
	case errors.Is(err, errConflitAtelier):
		return "other computers kept writing it"
	}
	return "the master cannot be reached (" + tronquerRunes(err.Error(), 160) + ")"
}

func listeCourte(l []string) string {
	switch {
	case len(l) == 0:
		return "nothing"
	case len(l) <= 4:
		return strings.Join(l, ", ")
	}
	return strings.Join(l[:3], ", ") + " and " + strconv.Itoa(len(l)-3) + " more"
}
