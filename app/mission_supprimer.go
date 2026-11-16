package main

// Deleting a mission (note 2026-11-16 10h04): the mission, its Servers tasks, its history and its follow-up files go;
// on request, the project and its folder the mission created (to the recycle bin) and the access it gave. Never a
// vault entry, a key or a server.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// OptionsSuppression: what goes with the mission besides its own files (unticked by default in the window).
type OptionsSuppression struct {
	Projet bool `json:"projet,omitempty"` // the project the mission created, removed from Studio, its folder to the recycle bin
	Acces  bool `json:"acces,omitempty"`  // the access the mission gave to its project (the vault entries stay)
}

// ApercuSuppression: what Delete will remove, shown in the confirmation before anything is done.
type ApercuSuppression struct {
	Etat       string              `json:"etat"`
	EnCours    bool                `json:"en_cours"` // lancee or arret_demande: stopped first (immediat | jalon)
	Taches     int                 `json:"taches"`
	Historique int                 `json:"historique"`
	Projet     string              `json:"projet,omitempty"`
	Dossier    string              `json:"dossier,omitempty"`
	DossierOK  bool                `json:"dossier_ok"`            // the project and its folder can be removed
	DossierNon string              `json:"dossier_non,omitempty"` // why not (English, translated in the window)
	Depot      string              `json:"depot,omitempty"`       // the GitHub repository: a link only, never deleted
	Acces      []string            `json:"acces,omitempty"`       // "entry (env)" given by the mission
	Envs       []EnvCarte          `json:"envs,omitempty"`        // what stays online on the servers
	Differee   *OptionsSuppression `json:"differee,omitempty"`    // already asked, done once the milestone ends
}

var etatsFinis = []string{"terminee", "annulee", "echec", "arretee"}

// accesDonnes: the access m's launch gave to project nom (card entries ticked and given, still granted as "mission").
func (s *Serveur) accesDonnes(m Mission, nom string) []AccesCarte {
	var c CarteMission
	if s.acces == nil || json.Unmarshal(m.Carte, &c) != nil || !faitMission(m, "access given: ") {
		return nil
	}
	pa := s.acces.Projet(nom)
	var l []AccesCarte
	for _, a := range c.Acces {
		if a.Decoche || a.ARemplir {
			continue
		}
		env := envAcces(a.Pour, c)
		if slices.ContainsFunc(pa.Acces, func(x AccesProjet) bool { return x.Entree == a.ID && x.Env == env && x.Auto == "mission" }) {
			a.Pour = env
			l = append(l, a)
		}
	}
	return l
}

// projetSupprimable: m's project and why its folder cannot go with the mission ("": it can). Only a project this
// mission created, used by no other mission, whose folder is a real folder (no link, no junction) and never a drive,
// the personal folder, a system folder or the tool's folder.
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
	return p, controlerDossierSupprime(p.Dossier, s.kitCourant().Racine)
}

// controlerDossierSupprime: "" when d may go to the recycle bin, else why not.
func controlerDossierSupprime(d, kit string) string {
	if !filepath.IsAbs(d) {
		return "The project's folder path is not a full path"
	}
	if cle := dossierInterdit(d, kit); cle != "" {
		return cle
	}
	fi, err := os.Lstat(d)
	if errors.Is(err, os.ErrNotExist) {
		return "" // already gone: the project is still removed from Studio
	}
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return "The project's folder is a link or not a folder: it is never deleted"
	}
	reel := cheminReel(d)
	if runtime.GOOS == "windows" && !strings.EqualFold(reel, filepath.Clean(d)) || runtime.GOOS != "windows" && reel != filepath.Clean(d) {
		return "The project's folder leads elsewhere (a link or a junction): it is never deleted"
	}
	return ""
}

func (s *Serveur) apercuSuppression(m Mission) ApercuSuppression {
	a := ApercuSuppression{Etat: m.Etat, EnCours: m.Etat == "lancee" || m.Etat == "arret_demande", Historique: len(m.Historique), Differee: m.Suppression}
	a.Taches = len(tachesDeMission(m))
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	a.Depot, a.Envs = c.Projet.Depot, c.Envs
	if c.Projet.Distant != "" {
		return a
	}
	p, non := s.projetSupprimable(m)
	a.Projet, a.Dossier, a.DossierNon, a.DossierOK = p.Nom, p.Dossier, non, non == ""
	for _, x := range s.accesDonnes(m, p.Nom) {
		a.Acces = append(a.Acces, x.Entree+" ("+x.Pour+")")
	}
	return a
}

// apiApercuSuppression: GET /api/missions/{id}/suppression: what Delete removes and what it can also remove.
func (s *Serveur) apiApercuSuppression(w http.ResponseWriter, r *http.Request) {
	m, err := lireMission(r.PathValue("id"))
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.ecrireJSON(w, s.apercuSuppression(m))
}

// apiSupprimerMission: POST /api/missions/{id}/supprimer {projet, acces, arret: immediat|jalon}. A running mission
// needs arret: immediat stops it then deletes it; jalon asks the stop after the milestone, the follower deletes it then.
func (s *Serveur) apiSupprimerMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		OptionsSuppression
		Arret string `json:"arret"`
	}
	if r.ContentLength != 0 && lireCorps(r, &c) != nil || !slices.Contains([]string{"", "immediat", "jalon"}, c.Arret) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	id, qui := r.PathValue("id"), deQui(r)
	m, err := lireMission(id)
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	if c.Projet {
		if _, non := s.projetSupprimable(m); non != "" {
			s.erreur(w, http.StatusConflict, non)
			return
		}
	}
	if m.Etat == "lancee" || m.Etat == "arret_demande" {
		switch {
		case c.Arret == "":
			s.erreur(w, http.StatusConflict, "This mission is running: choose how to stop it first")
			return
		case c.Arret == "jalon":
			if m.Etat == "lancee" {
				if _, err := s.demanderArretJalon(id, qui); err != nil {
					s.erreurMissionHTTP(w, err)
					return
				}
			}
			o := c.OptionsSuppression
			m, err = modifierMission(id, func(m *Mission) error { m.Suppression = &o; return nil })
			if err != nil {
				s.erreurMissionHTTP(w, err)
				return
			}
			s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission suppression demandée après le jalon"})
			s.ecrireJSON(w, map[string]any{"differee": true, "mission": s.voiesEnDirect(m)})
			return
		}
	}
	retire, err := s.supprimerMission(id, qui, c.OptionsSuppression)
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.ecrireJSON(w, map[string]any{"supprimee": true, "retire": retire})
}

// apiViderMissions: POST /api/missions/vider: every finished, cancelled, failed or stopped mission deleted (its own
// files only: projects and folders stay).
func (s *Serveur) apiViderMissions(w http.ResponseWriter, r *http.Request) {
	n := 0
	for _, m := range lireMissions() {
		if slices.Contains(etatsFinis, m.Etat) {
			if _, err := s.supprimerMission(m.ID, deQui(r), OptionsSuppression{}); err == nil {
				n++
			}
		}
	}
	s.ecrireJSON(w, map[string]any{"supprimees": n})
}

// supprimerMission: mission id stopped if it still runs (paused or stopping included), then its orchestrator, tokens,
// pending requests, Servers tasks and files removed; with o, its project and folder, and the access it gave. The
// list of what was removed is audited (names and paths only).
func (s *Serveur) supprimerMission(id, qui string, o OptionsSuppression) ([]string, error) {
	m, err := lireMission(id)
	if err != nil {
		return nil, err
	}
	taches := tachesDeMission(m) // read before the stop: a suspended preparation leaves its lane without its task
	if m.Etat == "lancee" || m.Etat == "arret_demande" || m.Etat == "en_pause" {
		if m, err = s.arreterMission(id, qui); err != nil {
			return nil, err
		}
	}
	var p Projet
	if o.Projet { // checked again now: the stop may have taken a while
		var non string
		if p, non = s.projetSupprimable(m); non != "" {
			return nil, erreurMission{http.StatusConflict, non}
		}
	}
	retire := []string{"mission " + id}
	s.arreterOrchestrateur(id, errMissionSupprimee)
	for t := range tachesDeMission(m) {
		taches[t] = true
	}
	s.revoquerJetons("mission supprimée", func(x SessionHub) bool { return x.Projet == prefixeMission+id || x.Mission != "" && taches[x.Mission] })
	s.fermerLancementMission(id, qui, "refuser", "mission supprimée")
	if s.approbations != nil {
		l, _ := s.approbations.Liste()
		for _, x := range l {
			if x.Etat == "attente" && (x.Mission == id || x.Details["mission"] == id || x.Mission != "" && taches[x.Mission]) {
				s.refuserAccordMission(x.ID, qui, "mission supprimée")
			}
		}
	}
	for t := range taches {
		if s.arreterTache(t) {
			attendreFinTache(t)
		}
		if x, ok := tacheMission(t); ok {
			for _, srv := range x.Serveurs {
				_ = os.Remove(filepath.Join(dossierTaches(srv), t+".json"))
			}
		}
		_ = os.RemoveAll(dossierSessionTache(t))
		retire = append(retire, "Servers task "+t)
	}
	if o.Acces && s.acces != nil && p.Nom == "" {
		p, _ = s.projetMission(m)
	}
	if o.Acces && p.Nom != "" {
		n := 0
		for _, a := range s.accesDonnes(m, p.Nom) {
			if s.acces.Retirer(p.Nom, a.ID, a.Pour) == nil {
				retire, n = append(retire, "access "+a.Entree+" ("+a.Pour+")"), n+1
			}
		}
		if n > 0 && !o.Projet {
			_, _ = s.injecterAcces(p, nil)
		}
	}
	if o.Projet {
		if _, err := os.Lstat(p.Dossier); err == nil {
			// a pilot just stopped (or an indexer) releases its files a few seconds later: Windows refuses until then
			err := corbeilleMission(p.Dossier)
			for i := 0; err != nil && i < 20; i++ {
				time.Sleep(pauseCorbeille)
				err = corbeilleMission(p.Dossier)
			}
			if err != nil {
				return retire, erreurMission{http.StatusInternalServerError, "Cannot send the project's folder to the recycle bin: close what uses it, then delete again"}
			}
		}
		if err := s.retirerProjet(p); err != nil {
			return retire, err
		}
		s.oublierGitHub(p)
		retire = append(retire, "project "+p.Nom, "folder "+p.Dossier+" (recycle bin)")
	}
	verrouMissions.Lock() // a follower or a tool writing it now finds it gone (modifierMission never brings it back)
	f := filepath.Join(dossierMissions(), id+".json")
	err = os.Remove(f)
	_ = os.Remove(f + ".ancien")
	_ = os.RemoveAll(dossierSessionMission(id))
	verrouMissions.Unlock()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return retire, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission supprimée",
		Resultat: s.masquerSecrets(strings.Join(retire, ", "))})
	return retire, nil
}

// tachesDeMission: the Servers tasks m launched: its lanes', and those its journal names (suspended or ended since).
func tachesDeMission(m Mission) map[string]bool {
	l := map[string]bool{}
	for _, v := range m.Voies {
		if v.Module == "servers" && v.Ref != "" {
			l[v.Ref] = true
		}
	}
	for _, e := range m.Journal {
		for _, debut := range []string{"suspendue:", "tache:", "plan:"} {
			if r, ok := strings.CutPrefix(e.ID, debut); ok {
				if r, _, _ = strings.Cut(r, ":"); idMissionValide.MatchString(r) {
					l[r] = true
				}
			}
		}
	}
	return l
}

var corbeilleMission = mettreALaCorbeille // a test sees what goes, without the real recycle bin

var pauseCorbeille = 500 * time.Millisecond // between two tries of the recycle bin (10 s in all)

var errMissionSupprimee = errors.New("the mission was deleted by the owner")

// attendreFinTache: a stopped task's goroutine has written its last state (2 s at most), so its file is not written
// again once deleted.
func attendreFinTache(id string) {
	for i := 0; i < 100; i++ {
		tachesEnCours.Lock()
		_, ok := tachesEnCours.m[id]
		tachesEnCours.Unlock()
		if !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// suppressionApresJalon: called by the follower once a mission asked to be deleted after its milestone is paused.
func (s *Serveur) suppressionApresJalon(m Mission) bool {
	if m.Suppression == nil || m.Etat != "en_pause" {
		return false
	}
	o := *m.Suppression
	if _, err := s.supprimerMission(m.ID, "hub", o); err != nil && o.Projet { // the folder refused since: the mission still goes
		o.Projet = false
		_, err = s.supprimerMission(m.ID, "hub", o)
		return err == nil
	}
	return true
}
