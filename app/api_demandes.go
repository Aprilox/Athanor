package main

// Requests of a project: the current request, the queue, the notes, the full file.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type DemandeFaite struct {
	Fichier string `json:"fichier"`
	Date    string `json:"date"`
	Titre   string `json:"titre"`
	Livree  bool   `json:"livree"` // false: taken into the current request and its plan, not delivered yet
}

var reLivre = regexp.MustCompile(`STATUS: *DONE`)

// projetLivre: same test as the pilot's est_termine (FINAL_REPORT.md with STATUS: DONE).
func projetLivre(p Projet) bool {
	b, err := os.ReadFile(filepath.Join(p.Atelier(), "FINAL_REPORT.md"))
	return err == nil && reLivre.Match(b)
}

// done/<date>-<time>-<NN>-<slug>.md, time as HHMMSS (pilot) or HHMM (older versions), NN = queue position
var reNomFaite = regexp.MustCompile(`^(\d{8})-(\d{6}|\d{4})-(?:\d{1,3}-)?(.+)$`)

// lireDemandeFaite: date and readable title of a request moved to done/. The title is the request's
// "# " heading (written by the app); otherwise the slug of the file name.
func lireDemandeFaite(dossier, nom string) (stamp, date, titre string) {
	n := strings.TrimSuffix(nom, ".md")
	titre = n
	if m := reNomFaite.FindStringSubmatch(n); m != nil {
		heure := m[2]
		stamp = m[1] + heure + strings.Repeat("0", 6-len(heure))
		date = m[1][:4] + "-" + m[1][4:6] + "-" + m[1][6:8] + " " + heure[:2] + "h" + heure[2:4]
		titre = strings.TrimSpace(strings.ReplaceAll(m[3], "-", " "))
	}
	if f, err := os.Open(filepath.Join(dossier, nom)); err == nil {
		buf := make([]byte, 4096)
		k, _ := f.Read(buf)
		f.Close()
		for _, l := range strings.Split(string(buf[:k]), "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "# ") {
				if t := strings.TrimSpace(l[2:]); t != "" {
					titre = t
				}
				break
			}
			if l != "" {
				break // the heading, when there is one, is the first line
			}
		}
	}
	return
}

// demandeVide: nothing but headings and lines in parentheses.
func demandeVide(s string) bool {
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "(") {
			return false
		}
	}
	return true
}

func nomFichierSur(f string) bool {
	return f != "" && !strings.ContainsAny(f, `/\`) && strings.HasSuffix(f, ".md") && !strings.HasPrefix(f, ".")
}

func (s *Serveur) apiDemandes(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	b, _ := os.ReadFile(p.Demande())
	faites := []DemandeFaite{}
	if entrees, err := os.ReadDir(p.DemandesFaites()); err == nil {
		// Moved to done/ = taken into the current request (and its plan). Delivered only if the project
		// is finished now, or if a delivery happened after it was taken.
		livraison := ""
		if b, err := os.ReadFile(filepath.Join(p.DemandesFaites(), ".last-delivery")); err == nil {
			livraison = strings.TrimSpace(string(b))
		}
		fini := projetLivre(p)
		for _, e := range entrees {
			if !nomFichierSur(e.Name()) || e.IsDir() {
				continue
			}
			stamp, date, titre := lireDemandeFaite(p.DemandesFaites(), e.Name())
			livree := fini || (stamp != "" && livraison != "" && stamp < livraison)
			faites = append(faites, DemandeFaite{e.Name(), date, titre, livree})
		}
	}
	att := p.LireFileAttente()
	if att == nil {
		att = []DemandeAttente{}
	}
	s.ecrireJSON(w, map[string]any{"demande": string(b), "attente": att, "faites": faites, "plan": p.LirePlan()})
}

func (s *Serveur) apiAjouterDemande(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Titre string `json:"titre"`
		Texte string `json:"texte"`
	}
	if err := lireCorps(r, &c); err != nil || strings.TrimSpace(c.Texte) == "" {
		s.erreur(w, http.StatusBadRequest, "Missing text")
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Titre) == "" {
		c.Titre = court(c.Texte, 50)
	}
	// First request of the project: it becomes THE request.
	if b, _ := os.ReadFile(p.Demande()); demandeVide(string(b)) {
		_ = os.MkdirAll(p.Atelier(), 0o755)
		if err := os.WriteFile(p.Demande(), []byte(s.texteDemande(c.Titre, c.Texte)), 0o644); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.ecrireJSON(w, map[string]any{"message_key": "Request saved: all that is left is to start.", "ou": "demande"})
		return
	}
	texte := "# " + strings.TrimSpace(c.Titre) + "\n\n" + libelle(s.langue(), "demande_le") + " " + DateCourte() + ".\n\n" + strings.TrimSpace(c.Texte) + "\n"
	f, err := p.AjouterDemande(c.Titre, texte)
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"message_key": "Queued: {0}", "args": []string{f}, "ou": "attente", "fichier": filepath.Base(f)})
}

func (s *Serveur) apiOrdreDemandes(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Ordre []string `json:"ordre"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, f := range c.Ordre {
		if !nomFichierSur(f) {
			s.erreur(w, http.StatusBadRequest, "Invalid file name")
			return
		}
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	if err := p.ReordonnerFile(c.Ordre); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

func (s *Serveur) apiSupprimerDemande(w http.ResponseWriter, r *http.Request) {
	f := r.PathValue("fichier")
	if !nomFichierSur(f) {
		s.erreur(w, http.StatusBadRequest, "Invalid name")
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	if err := retirerFichier(filepath.Join(p.FileAttente(), f)); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

func (s *Serveur) apiRemarque(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Texte string `json:"texte"`
	}
	if err := lireCorps(r, &c); err != nil || strings.TrimSpace(c.Texte) == "" {
		s.erreur(w, http.StatusBadRequest, "Missing text")
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	if err := p.AjouterRemarque(c.Texte, libelle(s.langue(), "remarques"), libelle(s.langue(), "remarques_aide")); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Note added: Claude reads it at the start of the next cycle.")
}

func (s *Serveur) apiEcrireDemande(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Texte string `json:"texte"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	_ = os.MkdirAll(p.Atelier(), 0o755)
	if err := os.WriteFile(p.Demande(), []byte(c.Texte), 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "REQUEST.md saved.")
}

// retirerFichier: a file removed; gone already counts as removed. On Windows, a file another program has
// open for a moment (the antivirus scanning a new file, a read of the queue) cannot be removed: tried
// again for up to two seconds.
func retirerFichier(f string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Remove(f); err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if runtime.GOOS != "windows" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}
