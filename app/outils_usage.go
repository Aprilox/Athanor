package main

// The kit's tools/ is shared by every project: a JDK, Gradle's downloads, a game's clients… It only grows. After each
// cycle the pilot notes which of its folders the cycle's commands used (directly, or through a script of the project
// they ran), in tools/.usage.json. A folder of 1 GB or more that no project used for 30 days, and that no check of a
// listed project names, is offered for removal in Setup & health; it goes only on the user's click, and is
// downloaded again the day a project needs it (tools/INSTALLS.md says how).

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const ageOutilInutilise = 30 * 24 * time.Hour

var tailleOutilInutilise int64 = 1 << 30 // shortened by the tests

// outilsProteges: the folders of tools/ never offered (the common bins, the caches, a database's data…).
var outilsProteges = map[string]bool{"bin": true, "node": true, "venv": true, "cache": true, "windows": true, "mcp": true,
	"pgdata": true, "home": true, "lib": true, "libs": true, "libroot": true, "pylib": true, "debs": true}

var (
	reRefOutil    = regexp.MustCompile(`(?:\$\{?TOOLS\}?|[/\\]tools)[/\\]((?:windows[/\\])?[A-Za-z0-9][A-Za-z0-9._+-]*)`)
	reScriptCmd   = regexp.MustCompile(`[A-Za-z0-9_./-]+\.(?:sh|py|ps1|bat|cmd|js|mjs)\b`)
	reMotGradle   = regexp.MustCompile(`(?i)\bgradlew?\b`)
	reNomOutilSur = regexp.MustCompile(`^(?:windows/)?[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

// usageOutils: a folder of tools/ ("gradle", "windows/jdk17") → project → its last use (unix); the project ""
// is when Athanor first saw the folder (the 30 days of a folder installed before 3.1.7 start then).
type usageOutils map[string]map[string]int64

// shortcut: two pilots ending a cycle at the same second may lose one's note; harmless, it comes back at its next cycle.
var usageMu sync.Mutex

func lireUsageOutils(tools string) usageOutils {
	u := usageOutils{}
	if b, err := lireBorne(filepath.Join(tools, ".usage.json"), 4<<20); err == nil {
		_ = json.Unmarshal(b, &u)
	}
	return u
}

func ecrireUsageOutils(tools string, u usageOutils) {
	if b, err := json.Marshal(u); err == nil && estDossier(tools) {
		_ = publierFichier(filepath.Join(tools, ".usage.json"), b)
	}
}

// outilsDansTexte: the folders of tools/ a command or a script names (Gradle and Playwright through their own variables).
func outilsDansTexte(s string, vus map[string]bool) {
	for _, m := range reRefOutil.FindAllStringSubmatch(s, -1) {
		vus[strings.ReplaceAll(m[1], `\`, "/")] = true
	}
	if reMotGradle.MatchString(s) {
		vus["gradle"] = true
	}
	if strings.Contains(strings.ToLower(s), "playwright") {
		vus["ms-playwright"] = true
	}
}

// noterUsageOutils: the folders of tools/ the cycle's commands used, noted for the project.
func noterUsageOutils(tools, projet string, cmds []commandeCycle) {
	vus, lus := map[string]bool{}, map[string]bool{}
	for _, c := range cmds {
		outilsDansTexte(c.cmd, vus)
		for _, s := range reScriptCmd.FindAllString(c.cmd, -1) {
			if lus[s] || len(lus) >= 40 {
				continue
			}
			lus[s] = true
			f := s
			if !filepath.IsAbs(f) {
				f = filepath.Join(projet, f)
			}
			if st, err := os.Lstat(f); err == nil && st.Mode().IsRegular() {
				if b, err := lireBorne(f, 256<<10); err == nil {
					outilsDansTexte(string(b), vus)
				}
			}
		}
	}
	if len(vus) == 0 || !estDossier(tools) {
		return
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	u := lireUsageOutils(tools)
	maintenant := time.Now().Unix()
	for n := range vus {
		if estDossier(filepath.Join(tools, filepath.FromSlash(n))) {
			if u[n] == nil {
				u[n] = map[string]int64{}
			}
			u[n][projet] = maintenant
		}
	}
	ecrireUsageOutils(tools, u)
}

// OutilInutilise: a folder of tools/ offered for removal.
type OutilInutilise struct {
	Nom    string `json:"nom"`
	Taille int64  `json:"taille"`
	Depuis int64  `json:"depuis"` // its last known use (or when Athanor first saw it), unix
}

func nomsOutils(tools string) []string {
	var noms []string
	for _, base := range []string{"", "windows"} {
		es, _ := os.ReadDir(filepath.Join(tools, base))
		for _, e := range es {
			n := strings.TrimPrefix(base+"/"+e.Name(), "/")
			if e.IsDir() && e.Type()&fs.ModeSymlink == 0 && !strings.HasPrefix(e.Name(), ".") && !outilsProteges[n] {
				noms = append(noms, n)
			}
		}
	}
	sort.Strings(noms)
	return noms
}

func tailleDossier(d string) int64 {
	var t int64
	_ = filepath.WalkDir(d, func(_ string, e fs.DirEntry, err error) error {
		if err == nil && e.Type().IsRegular() {
			if i, err := e.Info(); err == nil {
				t += i.Size()
			}
		}
		return nil
	})
	return t
}

// outilsInutilises: the folders of tools/ of 1 GB or more no project used for 30 days and no check of a listed
// project names.
func outilsInutilises(tools string, projets []Projet) []OutilInutilise {
	usageMu.Lock()
	u := lireUsageOutils(tools)
	noms := nomsOutils(tools)
	maintenant, change := time.Now().Unix(), false
	for _, n := range noms {
		if u[n] == nil {
			u[n] = map[string]int64{"": maintenant}
			change = true
		}
	}
	if change {
		ecrireUsageOutils(tools, u)
	}
	usageMu.Unlock()
	gardes := map[string]bool{}
	for _, p := range projets {
		if b, err := lireBorne(filepath.Join(p.Dossier, ".athanor", "check.sh"), 256<<10); err == nil {
			outilsDansTexte(string(b), gardes)
		}
	}
	seuil := time.Now().Add(-ageOutilInutilise).Unix()
	out := []OutilInutilise{}
	for _, n := range noms {
		var dernier int64
		for _, q := range u[n] {
			dernier = max(dernier, q)
		}
		if gardes[n] || dernier > seuil {
			continue
		}
		if t := tailleDossier(filepath.Join(tools, filepath.FromSlash(n))); t >= tailleOutilInutilise {
			out = append(out, OutilInutilise{Nom: n, Taille: t, Depuis: dernier})
		}
	}
	return out
}

// the list, worked out again at most once an hour (a walk of tens of gigabytes)
var cacheInutilises = struct {
	sync.Mutex
	quand time.Time
	l     []OutilInutilise
}{}

func (s *Serveur) apiOutilsInutilises(w http.ResponseWriter, r *http.Request) {
	cacheInutilises.Lock()
	defer cacheInutilises.Unlock()
	if time.Since(cacheInutilises.quand) > time.Hour {
		s.mu.Lock()
		projets := append([]Projet(nil), s.config.Projets...)
		s.mu.Unlock()
		cacheInutilises.l, cacheInutilises.quand = outilsInutilises(filepath.Join(s.kitCourant().Racine, "tools"), projets), time.Now()
	}
	s.ecrireJSON(w, map[string]any{"dossier": filepath.Join(s.kitCourant().Racine, "tools"), "outils": cacheInutilises.l})
}

// apiSupprimerOutil: one of the offered folders removed, never while a pilot runs (it may be using it right now).
func (s *Serveur) apiSupprimerOutil(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom string `json:"nom"`
	}
	if err := lireCorps(r, &c); err != nil || !reNomOutilSur.MatchString(c.Nom) || strings.Contains(c.Nom, "..") {
		s.erreur(w, http.StatusBadRequest, "Unknown tool.")
		return
	}
	k := s.kitCourant()
	s.mu.Lock()
	projets := append([]Projet(nil), s.config.Projets...)
	s.mu.Unlock()
	for _, p := range projets {
		if piloteVivant(k.Racine, p.Dossier) > 0 {
			s.erreur(w, http.StatusConflict, "Stop the running pilots first: one of them may be using it.")
			return
		}
	}
	tools := filepath.Join(k.Racine, "tools")
	offert := false
	for _, o := range outilsInutilises(tools, projets) {
		offert = offert || o.Nom == c.Nom
	}
	if !offert {
		s.erreur(w, http.StatusConflict, "This tool was used again or is no longer offered.")
		return
	}
	if err := os.RemoveAll(filepath.Join(tools, filepath.FromSlash(c.Nom))); err != nil {
		s.erreur(w, http.StatusInternalServerError, "Cannot remove it: "+err.Error())
		return
	}
	usageMu.Lock()
	u := lireUsageOutils(tools)
	delete(u, c.Nom)
	ecrireUsageOutils(tools, u)
	usageMu.Unlock()
	cacheInutilises.Lock()
	cacheInutilises.quand = time.Time{}
	cacheInutilises.Unlock()
	s.ecrireJSON(w, map[string]bool{"ok": true})
}
