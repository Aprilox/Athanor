package main

// "Fix with Claude" in the window: the fix runs on the pilot's side (athanor extensions corriger /
// corriger-skill, ext_corriger.go, ext_skill.go) and takes minutes, so it runs in the background and the
// window asks where it is (one fix at a time per extension). A server's fix goes into the library here,
// under the library's lock; a skill's waits for Apply (its diff is shown first).
//
//   POST /api/extensions/corriger {type, id}           starts it
//   GET  /api/extensions/corriger?type=…&id=…          {encours, etape, essai, resultat}
//   POST /api/extensions/correction/appliquer {id}     a skill's proposal put in place
//   POST /api/extensions/correction/annuler {type, id} back to what it was before Claude's fix
//   POST /api/extensions/tester-skill {id}             a skill's test
//   GET  /api/extensions/corrections                   the reports for everyone (ext_corrections.go)
//   POST /api/extensions/corrections/appliquer {dossier}  the catalog's fixes into a copy of Athanor's code

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type travailCorrection struct {
	Encours  bool            `json:"encours"`
	Etape    string          `json:"etape"` // "test", "claude"
	Essai    int             `json:"essai"`
	Debut    string          `json:"debut"`
	Resultat json.RawMessage `json:"resultat,omitempty"`
}

var (
	correctionsMu sync.Mutex
	corrections   = map[string]*travailCorrection{} // "mcp|id", "skill|id"
)

func (s *Serveur) apiExtCorriger(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cle := r.URL.Query().Get("type") + "|" + r.URL.Query().Get("id")
		correctionsMu.Lock()
		t := corrections[cle]
		var copie travailCorrection
		if t != nil {
			copie = *t
		}
		correctionsMu.Unlock()
		if t == nil {
			s.ecrireJSON(w, map[string]any{"encours": false})
			return
		}
		s.ecrireJSON(w, copie)
		return
	}
	var c struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) || (c.Type != "mcp" && c.Type != "skill") {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	k := s.kitCourant()
	var args []string
	if c.Type == "mcp" {
		p, ok := s.projetRequis(w, r) // a server is started from a project's folder, as the test does
		if !ok {
			return
		}
		args = []string{"extensions", "corriger", k.Racine, p.Dossier, c.ID}
	} else {
		args = []string{"extensions", "corriger-skill", k.Racine, c.ID}
	}
	cle := c.Type + "|" + c.ID
	correctionsMu.Lock()
	if t := corrections[cle]; t != nil && t.Encours {
		correctionsMu.Unlock()
		s.erreur(w, http.StatusConflict, "Claude is already fixing it")
		return
	}
	t := &travailCorrection{Encours: true, Etape: "test", Debut: time.Now().Format(time.RFC3339)}
	corrections[cle] = t
	correctionsMu.Unlock()
	go s.executerCorrection(k, cle, c.Type, c.ID, args, t)
	s.ecrireJSON(w, map[string]any{"encours": true})
}

// executerCorrection: the kit's command, its steps as they come, then its result (a server's fix kept).
func (s *Serveur) executerCorrection(k Kit, cle, genre, id string, args []string, t *travailCorrection) {
	finir := func(res []byte) {
		correctionsMu.Lock()
		t.Encours, t.Resultat = false, res
		correctionsMu.Unlock()
	}
	echec := func(m string) {
		b, _ := json.Marshal(map[string]any{"ok": false, "erreur": m})
		finir(b)
	}
	c := commandeKit(k, args...)
	cacherFenetre(c)
	c.WaitDelay = 5 * time.Second
	sortie, err := c.StdoutPipe()
	if err != nil {
		echec(err.Error())
		return
	}
	var erreurs strings.Builder
	c.Stderr = &limiteurTexte{b: &erreurs, max: 1 << 16}
	if err := c.Start(); err != nil {
		echec(err.Error())
		return
	}
	minuteur := time.AfterFunc(40*time.Minute, func() { _ = c.Process.Kill() })
	defer minuteur.Stop()
	var res []byte
	sc := bufio.NewScanner(sortie)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		l := strings.TrimSpace(nettoyerANSI(sc.Text()))
		if e, ok := strings.CutPrefix(l, "ETAPE "); ok {
			if q, n, ok := strings.Cut(e, " "); ok {
				correctionsMu.Lock()
				t.Etape = q
				t.Essai, _ = strconv.Atoi(n)
				correctionsMu.Unlock()
			}
		} else if strings.HasPrefix(l, "{") && json.Valid([]byte(l)) {
			res = []byte(l)
		}
	}
	_ = c.Wait()
	if res == nil {
		echec(court(firstNonEmpty(derniereLigneTexte(erreurs.String()), "no answer"), 300))
		return
	}
	if genre == "mcp" {
		var rc ResultatCorrection
		if json.Unmarshal(res, &rc) == nil && rc.OK && !rc.Deja && rc.Config != nil {
			extMu.Lock()
			err := appliquerCorrectionMCP(k, id, rc)
			extMu.Unlock()
			if err != nil {
				rc.OK, rc.Erreur = false, "cannot write the library: "+err.Error()
				res, _ = json.Marshal(rc)
			}
		}
	}
	finir(res)
}

type limiteurTexte struct {
	b   *strings.Builder
	max int
}

func (l *limiteurTexte) Write(p []byte) (int, error) {
	if l.b.Len() < l.max {
		l.b.Write(p)
	}
	return len(p), nil
}

func (s *Serveur) apiExtCorrectionAppliquer(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	s.kitJSON(w, 2*time.Minute, "extensions", "appliquer-skill", s.kitCourant().Racine, c.ID)
}

func (s *Serveur) apiExtCorrectionAnnuler(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	k := s.kitCourant()
	if c.Type == "skill" {
		s.kitJSON(w, 2*time.Minute, "extensions", "annuler-skill", k.Racine, c.ID)
		return
	}
	extMu.Lock()
	err := annulerCorrectionMCP(k, c.ID)
	extMu.Unlock()
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.msg(w, "The server's configuration before Claude's fix is back.")
}

func (s *Serveur) apiExtTesterSkill(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	s.kitJSON(w, 5*time.Minute, "extensions", "tester-skill", s.kitCourant().Racine, c.ID)
}

// kitJSON: a kit command whose last line is JSON, answered as it is.
func (s *Serveur) kitJSON(w http.ResponseWriter, delai time.Duration, args ...string) {
	sortie, _ := executerKitArgs(s.kitCourant(), delai, args...)
	l := strings.Split(sortie, "\n")
	for i := len(l) - 1; i >= 0; i-- {
		if x := strings.TrimSpace(l[i]); strings.HasPrefix(x, "{") && json.Valid([]byte(x)) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(x))
			return
		}
	}
	s.ecrireJSON(w, map[string]any{"ok": false, "erreur": court(sortie, 400)})
}

func (s *Serveur) apiExtCorrections(w http.ResponseWriter, r *http.Request) {
	s.ecrireJSON(w, map[string]any{"rapports": lireRapports(s.kitCourant().Racine)})
}

func (s *Serveur) apiExtCorrectionsAppliquer(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Dossier string `json:"dossier"`
	}
	if err := lireCorps(r, &c); err != nil || strings.TrimSpace(c.Dossier) == "" {
		s.erreur(w, http.StatusBadRequest, "Choose the folder of your copy of Athanor's code")
		return
	}
	sortie, err := executerKitArgs(s.kitCourant(), 2*time.Minute, "extensions", "appliquer-corrections", s.kitCourant().Racine, strings.TrimSpace(c.Dossier))
	if err != nil {
		s.erreur(w, http.StatusBadRequest, court(derniereLigneTexte(sortie), 400))
		return
	}
	s.ecrireJSON(w, map[string]any{"ok": true, "texte": sortie, "rapports": lireRapports(s.kitCourant().Racine)})
}

// apiExtModeBlender: how Claude works with Blender (blender_apercu.go), for every project.
func (s *Serveur) apiExtModeBlender(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID   string `json:"id"`
		Mode string `json:"mode"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) || (c.Mode != modeBlenderApercu && c.Mode != modeBlenderArrierePlan && c.Mode != modeBlenderDirect) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	err := ecrireModeBlender(k, c.ID, c.Mode)
	extMu.Unlock()
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.msg(w, "Saved: it applies from the next cycle.")
}
