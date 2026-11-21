package main

// Deleting a mission: its own files go (the mission, its journal, an old one's orchestrator folder); a mission of 3.1
// still working is stopped first (its pilot, its server tasks); on request, the project it created and its folder
// (to the recycle bin). Otherwise its project keeps its deployment and access. Never a vault entry, a key or a server.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// OptionsSuppression: what goes with the mission besides its own files (unticked by default in the window).
type OptionsSuppression struct {
	Projet bool `json:"projet,omitempty"` // the project the mission created, removed from Studio, its folder to the recycle bin
	// the programs holding the project's folder the owner chose to stop first (after a refusal that named them)
	Arreter []int `json:"arreter,omitempty"`
}

// erreurBloquee: the project's folder is held by programs, named for the owner (the window offers to stop them).
type erreurBloquee struct{ l []ProgrammeBloquant }

func (e erreurBloquee) Error() string { return "programs are using the project's files" }

// ApercuSuppression: what Delete can also remove and what stays, shown in the confirmation before anything is done.
type ApercuSuppression struct {
	Projet     string     `json:"projet,omitempty"`
	Dossier    string     `json:"dossier,omitempty"`
	DossierOK  bool       `json:"dossier_ok"`            // the project and its folder can be removed
	DossierNon string     `json:"dossier_non,omitempty"` // why not (English, translated in the window)
	Arrete     bool       `json:"arrete,omitempty"`      // a mission of 3.1 whose work runs: deleting it stops its pilot and server tasks
	Depot      string     `json:"depot,omitempty"`       // the GitHub repository: a link only, never deleted
	Envs       []EnvCarte `json:"envs,omitempty"`        // what stays online on the servers
}

// projetMission: m's Dev lane's project (false: none, or gone).
func (s *Serveur) projetMission(m Mission) (Projet, bool) {
	for _, v := range m.Voies {
		if v.Module == "dev" {
			return s.projetParNom(v.Ref)
		}
	}
	return Projet{}, false
}

// faitMission: a launch event of m starts with debut (what its launch did).
func faitMission(m Mission, debut string) bool {
	return slices.ContainsFunc(m.Historique, func(e EvenementMission) bool { return e.Etat == "lancee" && strings.HasPrefix(e.Detail, debut) })
}

// projetSupprimable: m's project and why its folder cannot go with the mission ("": it can). Only a project this
// mission created, used by no other mission, whose pilot is stopped, whose folder is a real folder (no link, no
// junction) and never a drive, the personal folder, a system folder or the tool's folder.
func (s *Serveur) projetSupprimable(m Mission) (Projet, string) {
	p, ok := s.projetMission(m)
	if !ok {
		return p, "No local project is linked to this mission"
	}
	if !faitMission(m, "project created: "+p.Dossier) {
		return p, "This project existed before the mission: remove it from Studio if you want to"
	}
	for _, x := range lireMissions() {
		if x.ID != m.ID && slices.ContainsFunc(x.Voies, func(v VoieMission) bool { return v.Module == "dev" && v.Ref == p.Nom }) {
			return p, "Another mission uses this project"
		}
	}
	if p.PiloteActif() && m.Projet == "" { // M302: an old mission no longer stops anything (a mission of 3.1 is stopped first)
		return p, "Stop its pilot first"
	}
	return p, controlerDossierSupprime(p.Dossier, s.kitCourant().Racine)
}

func (s *Serveur) apercuSuppression(m Mission) ApercuSuppression {
	var a ApercuSuppression
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	a.Depot, a.Envs = c.Projet.Depot, c.Envs
	if c.Projet.Distant != "" {
		return a
	}
	p, non := s.projetSupprimable(m)
	a.Projet, a.Dossier, a.DossierNon, a.DossierOK = p.Nom, p.Dossier, non, non == ""
	a.Arrete = m.Projet == "" && tacheEnCoursID(m.Tache)
	if m.Projet != "" {
		e := s.acces.Environnements(p.Nom)[s.envJalon(p.Nom)]
		a.Arrete = p.PiloteActif()
		if e.URL != "" && e.Deploiement != nil {
			a.Envs = []EnvCarte{{URL: e.URL}}
		}
	}
	return a
}

// apiSupprimerMission: DELETE /api/missions/{id} {projet}: the old mission deleted, with its project and folder
// when asked.
func (s *Serveur) apiSupprimerMission(w http.ResponseWriter, r *http.Request) {
	var o OptionsSuppression
	if r.ContentLength != 0 && lireCorps(r, &o) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	retire, err := s.supprimerMission(r.PathValue("id"), deQui(r), o)
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.ecrireJSON(w, map[string]any{"supprimee": true, "retire": retire})
}

// supprimerMission: mission id's files removed, whatever state it was left in (nothing is stopped: it was no longer
// followed); with o.Projet, the project it created and its folder first. The list of what was removed is audited
// (names and paths only).
func (s *Serveur) supprimerMission(id, qui string, o OptionsSuppression) ([]string, error) {
	m, err := lireMission(id)
	if err != nil {
		return nil, err
	}
	retire := []string{"mission " + id}
	if m.Projet == "" && tacheEnCoursID(m.Tache) { // a servers mission still working: its task stopped first
		s.arreterTache(m.Tache)
		retire = append(retire, "task "+m.Tache+" stopped")
	}
	if p, ok := s.projetLocalChat(m.Projet); ok && m.Projet != "" && (p.PiloteActif() || tacheEnCoursID(lireRelais()[p.Nom].Premier)) {
		// a mission of 3.1 still working: deleted cleanly, its pilot and its server tasks stopped first
		if err := s.arreterProjet(p, "arreter"); err != nil {
			return nil, err
		}
		retire = append(retire, "pilot of "+p.Nom+" stopped")
	}
	if o.Projet {
		p, non := s.projetSupprimable(m)
		if non != "" {
			return nil, erreurMission{http.StatusConflict, non}
		}
		if _, err := os.Lstat(p.Dossier); err == nil {
			if noms := arreterBloquants(p.Dossier, o.Arreter); len(noms) > 0 {
				retire = append(retire, "programs stopped: "+strings.Join(noms, ", "))
			}
			// a pilot just stopped (or an indexer) releases its files a few seconds later: Windows refuses until then;
			// a program still running there (a preview server left by Claude) is named at once instead
			err := corbeilleProjet(p.Dossier)
			if l := programmesBloquants(p.Dossier); err != nil && len(o.Arreter) == 0 && slices.ContainsFunc(l, func(b ProgrammeBloquant) bool { return b.Arretable }) {
				return nil, erreurBloquee{l}
			}
			for i := 0; err != nil && i < 20; i++ {
				time.Sleep(pauseCorbeille)
				err = corbeilleProjet(p.Dossier)
			}
			if err != nil {
				return nil, erreurMission{http.StatusInternalServerError, "Cannot send the project's folder to the recycle bin: close what uses it, then delete again"}
			}
		}
		if err := s.retirerProjet(p); err != nil {
			return nil, err
		}
		s.oublierGitHub(p)
		retire = append(retire, "project "+p.Nom, "folder "+p.Dossier+" (recycle bin)")
	}
	s.revoquerJetons("mission supprimée", func(x SessionHub) bool { return x.Projet == prefixeMission+id })
	f := filepath.Join(dossierMissions(), id+".json")
	missionMu.Lock()        // never while the servers pilot reads and writes it back (mission_serveurs.go)
	err = retirerFichier(f) // Windows: the module reading it now (lireMissions at each hub request) holds it for a moment
	missionMu.Unlock()
	_ = os.Remove(f + ".ancien")
	_ = os.RemoveAll(dossierSessionMission(id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return retire, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission supprimée",
		Resultat: s.masquerSecrets(strings.Join(retire, ", "))})
	return retire, nil
}

var pauseCorbeille = 500 * time.Millisecond // between two tries of the recycle bin (10 s in all)
