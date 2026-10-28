package main

// The master's role handed to a node (2.46): the master chooses a live node of its team; the node gets the
// team (its key, its computers' hashed tokens, its orders) in the answer to its next beat, copies the team's
// workshop from the master, opens its own listener and tunnel, then tells the old master its address
// (POST /equipe/passation). The old master then becomes a node of it (a token of its own made for that), and
// for two minutes answers the others' beats with the new master's address (409 nouveau_maitre); a node that
// missed it finds the address in the new master's presence on GitHub. The old master's store of the
// workshop is frozen from the start (nothing written to it is lost: the nodes keep their changes and give
// them to the new master); a handover not finished within 5 minutes is cancelled on both sides.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var dureePassation = 5 * time.Minute

// passationDirecte: what the new master gets from the old one.
type passationDirecte struct {
	Equipe      string                `json:"equipe"`
	Cle         string                `json:"cle"`
	Noeuds      map[string]noeudConnu `json:"noeuds"`
	AtelierLien bool                  `json:"atelier_lien,omitempty"`
	Ordres      []ordreDirect         `json:"ordres,omitempty"`
	Ancien      string                `json:"ancien"` // the old master's name
}

type passationEnCours struct {
	cible, nom string
	paquet     passationDirecte
	jeton      string // the old master's own token at the new master
	debut      time.Time
}

// VuePassation: a handover, as the page shows it.
type VuePassation struct {
	Nom    string `json:"nom"`
	Depuis int64  `json:"depuis"`
	Etat   string `json:"etat"` // en_cours (the master hands over), relais (this computer takes over), erreur
	Erreur string `json:"erreur,omitempty"`
}

var relaisDirects = map[string]bool{} // projects this computer is taking over (directMu)

// transmettreMaitre: the master hands the team over to one of its live nodes.
func (s *Serveur) transmettreMaitre(p Projet, id string) error {
	e := s.lireDirect(p)
	directMu.Lock()
	h := hubs[p.Nom]
	directMu.Unlock()
	if e.Role != "maitre" || h == nil {
		return errors.New("Only the master hands the team over.")
	}
	n, ok := e.Noeuds[id]
	if !ok {
		return errors.New("This computer is not in the team.")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if v, vu := h.vus[id]; !vu || time.Since(v.quand) > enLigneDirect {
		return errors.New("This computer is not live: it must be on, Athanor open, to take the team over.")
	}
	if h.passation != nil {
		return errors.New("The team is already being handed over.")
	}
	moi := s.presenceDirecte(p)
	jeton := aleatoire(32)
	noeuds := map[string]noeudConnu{}
	for k, v := range e.Noeuds {
		if k != id {
			noeuds[k] = v
		}
	}
	noeuds[moi.ID] = noeudConnu{Nom: moi.Nom, Jeton: hacherJeton(jeton), Ajoute: time.Now().UTC().Format(time.RFC3339)}
	if e.AtelierLien {
		// nothing more written to this store: the nodes keep what they change and give it to the new master
		if err := os.MkdirAll(s.magasinProjet(p).dossier, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(s.magasinProjet(p).dossier, "gele"), nil, 0o600); err != nil {
			return err
		}
	}
	h.erreurPassation = ""
	h.passation = &passationEnCours{cible: id, nom: n.Nom, jeton: jeton, debut: time.Now(),
		paquet: passationDirecte{Equipe: e.Equipe, Cle: e.Cle, Noeuds: noeuds, AtelierLien: e.AtelierLien, Ordres: e.Ordres, Ancien: moi.Nom}}
	return nil
}

// verifierPassation: a handover not finished in time is cancelled (the store open again).
func (s *Serveur) verifierPassation(p Projet) {
	directMu.Lock()
	h := hubs[p.Nom]
	directMu.Unlock()
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.passation != nil && time.Since(h.passation.debut) > dureePassation {
		h.erreurPassation = "The other computer did not take the team over: this computer stays the master (try again)."
		h.passation = nil
		_ = os.Remove(filepath.Join(s.magasinProjet(p).dossier, "gele"))
	}
}

// hubPassation: POST /equipe/passation {adresse}: the new master is ready; this computer becomes its node.
func (s *Serveur) hubPassation(h *hubEquipe, w http.ResponseWriter, r *http.Request) {
	id := s.noeudAppel(h, r)
	var c struct {
		Adresse string `json:"adresse"`
	}
	if json.NewDecoder(r.Body).Decode(&c) != nil || !reAdresseDirecte.MatchString(c.Adresse) {
		repondreDirect(w, 400, map[string]string{"erreur": "bad request"})
		return
	}
	h.mu.Lock()
	if h.nouveau != nil && h.nouveau.adresse == c.Adresse { // said again (its first answer was lost)
		h.mu.Unlock()
		repondreDirect(w, 200, map[string]bool{"ok": true})
		return
	}
	pa := h.passation
	if id == "" || pa == nil || pa.cible != id {
		h.mu.Unlock()
		repondreDirect(w, 410, map[string]string{"erreur": "No handover to this computer (cancelled?)."})
		return
	}
	e := s.lireDirect(h.projet)
	nouveau := equipeDirecte{Role: "noeud", Equipe: e.Equipe, Cle: e.Cle, Adresse: c.Adresse, Jeton: pa.jeton, Maitre: pa.nom,
		AtelierLien: e.AtelierLien, AccepteOrdres: true, Depuis: time.Now().Unix(), DernierOrdre: dernierOrdre(e.Ordres)}
	if err := s.ecrireDirect(h.projet, nouveau); err != nil {
		h.mu.Unlock()
		repondreDirect(w, 500, map[string]string{"erreur": err.Error()})
		return
	}
	h.nouveau, h.passation = &nouveauMaitre{adresse: c.Adresse, nom: pa.nom, noeuds: e.Noeuds}, nil
	h.mu.Unlock()
	directMu.Lock()
	delete(clients, h.projet.Nom)
	directMu.Unlock()
	notifier(h.projet.Nom, texteRelais(s.langue(), "donne", pa.nom))
	repondreDirect(w, 200, map[string]bool{"ok": true})
	go func() { // the others find the new master here for a while, then on GitHub
		time.Sleep(attenteFinAncienMaitre)
		s.arreterHub(h)
	}()
}

var attenteFinAncienMaitre = 2 * time.Minute

func dernierOrdre(l []ordreDirect) int64 {
	if n := len(l); n > 0 {
		return l[n-1].Seq
	}
	return 0
}

type nouveauMaitre struct {
	adresse, nom string
	noeuds       map[string]noeudConnu // who may still ask this old master where the team went
}

// prendreRelais: this node becomes the team's master (the old master's package in hand).
func (s *Serveur) prendreRelais(p Projet, avant equipeDirecte, paquet passationDirecte) {
	directMu.Lock()
	if relaisDirects[p.Nom] {
		directMu.Unlock()
		return
	}
	relaisDirects[p.Nom] = true
	if c := clients[p.Nom]; c != nil {
		c.relais, c.erreur = time.Now(), ""
	}
	directMu.Unlock()
	defer func() {
		directMu.Lock()
		delete(relaisDirects, p.Nom)
		directMu.Unlock()
	}()
	echec := func(err error) {
		s.arreterMaitre(p.Nom)
		_ = s.ecrireDirect(p, avant)
		directMu.Lock()
		_ = err
		clients[p.Nom] = &clientDirect{erreur: "The handover did not finish: this computer stays a node.", echecs: 1}
		directMu.Unlock()
	}
	// the team's workshop, from the old master (its store is frozen: nothing is written to it meanwhile)
	if paquet.AtelierLien {
		if err := s.copierMagasin(p, magasinHTTP{avant.Adresse, avant.Jeton}); err != nil {
			echec(err)
			return
		}
	}
	e := equipeDirecte{Role: "maitre", Equipe: paquet.Equipe, Cle: paquet.Cle, Noeuds: paquet.Noeuds, AtelierLien: paquet.AtelierLien, Ordres: paquet.Ordres}
	if err := s.ecrireDirect(p, e); err != nil {
		echec(err)
		return
	}
	if err := s.demarrerMaitre(p); err != nil {
		echec(err)
		return
	}
	var adresse string
	for fin := time.Now().Add(3 * time.Minute); time.Now().Before(fin); time.Sleep(500 * time.Millisecond) {
		if v := s.vueDirecte(p); v != nil && v.Etat == "pret" && v.Adresse != "" {
			adresse = v.Adresse
			break
		}
	}
	if adresse == "" {
		echec(errors.New("its tunnel did not open"))
		return
	}
	for fin := time.Now().Add(dureePassation); ; {
		code, err := appelMaitre(avant.Adresse, avant.Jeton, "/equipe/passation", map[string]string{"adresse": adresse}, &map[string]bool{})
		if err == nil {
			break
		}
		if code == 410 || time.Now().After(fin) {
			echec(err)
			return
		}
		time.Sleep(attenteRelais)
	}
	directMu.Lock()
	delete(clients, p.Nom)
	directMu.Unlock()
	notifier(p.Nom, texteRelais(s.langue(), "pris", paquet.Ancien))
}

var attenteRelais = 10 * time.Second

// copierMagasin: the team's workshop, as another store has it, becomes this computer's store (the one it
// had is kept aside).
func (s *Serveur) copierMagasin(p Projet, src magasinAtelier) error {
	v, err := src.dernier()
	if err != nil {
		return err
	}
	var shas []string
	vus := map[string]bool{}
	for _, sha := range v.Fichiers {
		if !vus[sha] {
			shas, vus[sha] = append(shas, sha), true
		}
	}
	objets, err := src.objets(shas)
	if err != nil {
		return err
	}
	m := s.magasinProjet(p)
	if _, err := os.Stat(m.dossier); err == nil {
		_ = os.Rename(m.dossier, m.dossier+".avant-"+time.Now().Format("20060102-150405"))
	}
	return m.importer(v, objets)
}

// importer: an empty store takes a version as it is (its number kept: the team's workshop goes on from it).
func (m magasinLocal) importer(v ManifesteAtelier, objets map[string][]byte) error {
	for rel, sha := range v.Fichiers {
		if !cheminAtelierValide(rel) || !reShaAtelier.MatchString(sha) {
			return errors.New("bad path in the workshop: " + tronquerRunes(rel, 80))
		}
		c, ok := objets[sha]
		if !ok || shaAtelier(c) != sha {
			return errors.New("a file of the team's workshop is missing")
		}
		f := filepath.Join(m.dossier, "objets", sha)
		if err := ecrireAtomique(f, c, 0o600); err != nil {
			return err
		}
	}
	if v.Version <= 0 {
		return nil
	}
	nom := filepath.Join(m.dossier, "versions", fmt.Sprintf("%012d", v.Version))
	if err := os.MkdirAll(filepath.Dir(nom), 0o700); err != nil {
		return err
	}
	_ = os.WriteFile(nom+".claim", nil, 0o600)
	b, _ := json.MarshalIndent(v, "", " ")
	return ecrireAtomique(nom+".json", b, 0o600)
}

func texteRelais(langue, sens, nom string) string {
	t := map[string]map[string]string{
		"donne": {"en": "%s is the team's master now: this computer is one of its nodes", "fr": "%s est désormais le maître de l'équipe : cet ordinateur est l'un de ses nœuds",
			"es": "%s es ahora el maestro del equipo: este ordenador es uno de sus nodos", "de": "%s ist jetzt der Master des Teams: dieser Computer ist einer seiner Knoten",
			"it": "%s è ora il master del team: questo computer è uno dei suoi nodi", "pt": "%s é agora o mestre da equipa: este computador é um dos seus nós"},
		"pris": {"en": "This computer is the team's master now (in place of %s)", "fr": "Cet ordinateur est désormais le maître de l'équipe (à la place de %s)",
			"es": "Este ordenador es ahora el maestro del equipo (en lugar de %s)", "de": "Dieser Computer ist jetzt der Master des Teams (anstelle von %s)",
			"it": "Questo computer è ora il master del team (al posto di %s)", "pt": "Este computador é agora o mestre da equipa (no lugar de %s)"},
	}[sens]
	f, ok := t[langue]
	if !ok {
		f = t["en"]
	}
	return fmt.Sprintf(f, nom)
}
