package main

// The direct link's workshop and orders (2.45), on the application's side: the master serves the team's
// workshop (atelier_lien.go) to its nodes and gives them its orders (every pilot started, or stopped after
// its cycle); a node follows what the master decided (the workshop through the link, its orders unless its
// user said no).

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------- orders

// ordresApres: the master's orders a node has not seen yet, recent enough to follow.
func (e equipeDirecte) ordresApres(dernier int64, maintenant time.Time) []ordreDirect {
	var v []ordreDirect
	for _, o := range e.Ordres {
		if o.Seq > dernier && maintenant.Sub(time.Unix(o.Quand, 0)) < dureeOrdreDirect {
			v = append(v, o)
		}
	}
	return v
}

// ordreEquipe: the master starts (or stops after its cycle) its own pilot, then every node's.
func (s *Serveur) ordreEquipe(p Projet, action string) error {
	e := s.lireDirect(p)
	if e.Role != "maitre" {
		return errors.New("Only the master of the team starts or stops the team.")
	}
	if action != "demarrer" && action != "arreter" {
		return errors.New("unknown action")
	}
	if action == "demarrer" && e.AtelierLien {
		s.semerAtelier(p) // the nodes wait for the workshop: it is there at once
	}
	if err := piloterOrdre(s, p, action); err != nil && !errors.Is(err, errDejaFait) {
		return err
	}
	e = s.lireDirect(p)
	var seq int64
	if n := len(e.Ordres); n > 0 {
		seq = e.Ordres[n-1].Seq
	}
	e.Ordres = append(e.Ordres, ordreDirect{Seq: seq + 1, Action: action, Quand: time.Now().Unix()})
	if len(e.Ordres) > 10 {
		e.Ordres = e.Ordres[len(e.Ordres)-10:]
	}
	return s.ecrireDirect(p, e)
}

// suivreMaitre: a node, after a beat: the workshop as the master decided, its new orders followed (unless
// this computer's user said no, or the order came before it joined).
func (s *Serveur) suivreMaitre(p Projet, atelierLien bool, ordres []ordreDirect) {
	e := s.lireDirect(p)
	if e.Role != "noeud" {
		return
	}
	change := false
	if e.AtelierLien != atelierLien {
		e.AtelierLien, change = atelierLien, true
		go s.atelierHorsGit(p, atelierLien)
	}
	sort.Slice(ordres, func(i, j int) bool { return ordres[i].Seq < ordres[j].Seq })
	var suivre []ordreDirect
	for _, o := range ordres {
		if o.Seq <= e.DernierOrdre {
			continue
		}
		e.DernierOrdre, change = o.Seq, true
		if e.AccepteOrdres && o.Quand >= e.Depuis && time.Since(time.Unix(o.Quand, 0)) < dureeOrdreDirect {
			suivre = append(suivre, o)
		}
	}
	if !change {
		return
	}
	if s.ecrireDirect(p, e) != nil {
		return // never an order followed twice
	}
	if len(suivre) > 0 {
		o := suivre[len(suivre)-1] // the last one says what the master wants now
		directMu.Lock()
		if c := clients[p.Nom]; c != nil {
			c.ordre = &o
		}
		directMu.Unlock()
		go func() {
			if piloterOrdre(s, p, o.Action) == nil {
				notifier(p.Nom, texteOrdre(s.langue(), o.Action, e.Maitre))
			}
		}()
	}
}

// piloterOrdre: an order on this computer's pilot (started, or stopped after its cycle); nothing when it
// already is so. (The tests replace it.)
var piloterOrdre = func(s *Serveur, p Projet, action string) error {
	switch action {
	case "demarrer":
		if p.PiloteActif() {
			return errDejaFait
		}
		marqueAuto(p, true)
		_, err := s.demarrerPilote(p)
		return err
	case "arreter":
		if !p.PiloteActif() {
			return errDejaFait
		}
		marqueAuto(p, false)
		_, err := s.arreterApresCycle(p)
		return err
	}
	return errors.New("unknown action")
}

var errDejaFait = errors.New("already so")

func texteOrdre(langue, action, maitre string) string {
	t := map[string]map[string]string{
		"demarrer": {"en": "%s started the team: this computer's pilot runs", "fr": "%s a lancé l'équipe : le pilote de cet ordinateur tourne",
			"es": "%s lanzó el equipo: el piloto de este ordenador funciona", "de": "%s hat das Team gestartet: der Pilot dieses Computers läuft",
			"it": "%s ha avviato il team: il pilota di questo computer è in funzione", "pt": "%s lançou a equipa: o piloto deste computador está a correr"},
		"arreter": {"en": "%s stops the team: this computer's pilot stops after its cycle", "fr": "%s arrête l'équipe : le pilote de cet ordinateur s'arrête après son cycle",
			"es": "%s detiene el equipo: el piloto de este ordenador se detiene tras su ciclo", "de": "%s stoppt das Team: der Pilot dieses Computers hält nach seinem Zyklus an",
			"it": "%s ferma il team: il pilota di questo computer si ferma dopo il suo ciclo", "pt": "%s para a equipa: o piloto deste computador para após o seu ciclo"},
	}[action]
	f, ok := t[langue]
	if !ok {
		f = t["en"]
	}
	return fmt.Sprintf(f, firstNonEmpty(maitre, "?"))
}

// ---------------------------------------------------------------------------- the workshop

// changerAtelierLien: the master: the team's workshop through the link (on) or the commits again (off).
func (s *Serveur) changerAtelierLien(p Projet, on bool) error {
	e := s.lireDirect(p)
	if e.Role != "maitre" {
		return errors.New("Only the master of the team decides where its workshop goes.")
	}
	if e.AtelierLien == on {
		return nil
	}
	if err := s.atelierHorsGit(p, on); err != nil {
		return err
	}
	e = s.lireDirect(p)
	e.AtelierLien = on
	if err := s.ecrireDirect(p, e); err != nil {
		return err
	}
	if on {
		s.semerAtelier(p)
	}
	return nil
}

// atelierHorsGit: .athanor/ out of the commits (the workshop through the link), or shared on GitHub again.
func (s *Serveur) atelierHorsGit(p Projet, horsGit bool) error {
	return atelierHorsGitFn(s, p, horsGit)
}

var atelierHorsGitFn = func(s *Serveur, p Projet, horsGit bool) error {
	a := "1"
	if horsGit {
		a = "0"
	}
	_, err := s.executerGitHub(p, 2*time.Minute, "partage", a)
	return err
}

// semerAtelier: a team's store still empty: the master's workshop is its first version (the nodes then
// have the request at once, before the master's pilot runs).
func (s *Serveur) semerAtelier(p Projet) {
	m := s.magasinProjet(p)
	if v, err := m.dernier(); err != nil || v.Version > 0 {
		return
	}
	locaux, _ := fichiersAtelierLocaux(p.Atelier())
	if len(locaux) == 0 {
		return
	}
	fichiers, objets := map[string]string{}, map[string][]byte{}
	for rel, b := range locaux {
		sha := shaAtelier(b)
		fichiers[rel], objets[sha] = sha, b
	}
	_, _ = m.ecrire(0, fichiers, objets, s.presenceDirecte(p).Nom)
}

// hubAtelier: GET /equipe/atelier, the team's workshop's last version.
func (s *Serveur) hubAtelier(h *hubEquipe, w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appelAtelier(h, w, r); !ok {
		return
	}
	v, err := s.magasinProjet(h.projet).dernier()
	if err != nil {
		repondreDirect(w, 500, map[string]string{"erreur": err.Error()})
		return
	}
	repondreDirect(w, 200, v)
}

// hubAtelierObjets: POST /equipe/atelier/objets {shas}: their contents (as many as fit in one answer).
func (s *Serveur) hubAtelierObjets(h *hubEquipe, w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appelAtelier(h, w, r); !ok {
		return
	}
	var c struct {
		Shas []string `json:"shas"`
	}
	if json.NewDecoder(r.Body).Decode(&c) != nil || len(c.Shas) == 0 || len(c.Shas) > 500 {
		repondreDirect(w, 400, map[string]string{"erreur": "bad request"})
		return
	}
	out, taille := map[string]string{}, 0
	m := s.magasinProjet(h.projet)
	for _, sha := range c.Shas {
		o, err := m.objets([]string{sha})
		if err != nil {
			repondreDirect(w, 404, map[string]string{"erreur": err.Error()})
			return
		}
		if taille > 0 && taille+len(o[sha]) > 32<<20 {
			break // the rest in the next call
		}
		taille += len(o[sha])
		out[sha] = base64.StdEncoding.EncodeToString(o[sha])
	}
	repondreDirect(w, 200, map[string]any{"objets": out})
}

// hubAtelierEcrire: POST /equipe/atelier/ecrire {base, fichiers, objets}: a node's merged workshop.
func (s *Serveur) hubAtelierEcrire(h *hubEquipe, w http.ResponseWriter, r *http.Request) {
	id, ok := s.appelAtelier(h, w, r)
	if !ok {
		return
	}
	var c struct {
		Base     int64             `json:"base"`
		Fichiers map[string]string `json:"fichiers"`
		Objets   map[string]string `json:"objets"`
	}
	if json.NewDecoder(r.Body).Decode(&c) != nil || c.Fichiers == nil {
		repondreDirect(w, 400, map[string]string{"erreur": "bad request"})
		return
	}
	objets := map[string][]byte{}
	for sha, b64 := range c.Objets {
		b, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			repondreDirect(w, 400, map[string]string{"erreur": "bad object"})
			return
		}
		objets[sha] = b
	}
	nom := s.lireDirect(h.projet).Noeuds[id].Nom // who wrote it: what the master knows, not what it says
	v, err := s.magasinProjet(h.projet).ecrire(c.Base, c.Fichiers, objets, nom)
	switch {
	case errors.Is(err, errConflitAtelier):
		repondreDirect(w, 409, map[string]string{"erreur": err.Error()})
	case errors.Is(err, errGeleAtelier):
		repondreDirect(w, 423, map[string]string{"erreur": err.Error()})
	case err != nil:
		repondreDirect(w, 400, map[string]string{"erreur": err.Error()})
	default:
		repondreDirect(w, 200, map[string]int64{"version": v})
	}
}

// appelAtelier: a paired node, and a team whose workshop goes through the link.
func (s *Serveur) appelAtelier(h *hubEquipe, w http.ResponseWriter, r *http.Request) (string, bool) {
	id := s.noeudAppel(h, r)
	if id == "" {
		repondreDirect(w, 401, map[string]string{"erreur": "not paired"})
		return "", false
	}
	if !s.lireDirect(h.projet).AtelierLien {
		repondreDirect(w, 404, map[string]string{"erreur": "The team's workshop goes through GitHub, not through the link."})
		return "", false
	}
	return id, true
}
