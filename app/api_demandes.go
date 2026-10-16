package main

// Requests of a project: the current request, the queue, the notes, the full file.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type DemandeFaite struct {
	Fichier string `json:"fichier"`
	Date    string `json:"date"`
	Titre   string `json:"titre"`
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
		for _, e := range entrees {
			if !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			n := strings.TrimSuffix(e.Name(), ".md")
			date, titre := "", n
			if len(n) > 14 && n[8] == '-' && n[13] == '-' {
				date = n[:4] + "-" + n[4:6] + "-" + n[6:8] + " " + n[9:11] + "h" + n[11:13]
				titre = n[14:]
			}
			faites = append(faites, DemandeFaite{e.Name(), date, titre})
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
	s.ecrireJSON(w, map[string]any{"message_key": "Queued: {0}", "args": []string{f}, "ou": "attente"})
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
	if err := os.Remove(filepath.Join(p.FileAttente(), f)); err != nil {
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
