package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// RelaisJalon (M136): the dev deployment of a project's HEAD after a milestone delivered, one per milestone.
// Recorded before it starts, so that a restart neither repeats nor loses it; its state once started is the
// deployment history's (Etat here only says where the relay itself is).
type RelaisJalon struct {
	Jalon  string `json:"jalon"`
	Commit string `json:"commit"`
	Date   string `json:"date"`             // when the milestone was seen done (RFC 3339)
	Etat   string `json:"etat"`             // attente (not pushed yet) | lance | echec (not started)
	ID     string `json:"id,omitempty"`     // the deployment, once started
	Erreur string `json:"erreur,omitempty"` // why it did not start
}

// attentePushRelais: how long a milestone's commit may wait to be on GitHub (git source) before the relay tries anyway.
const attentePushRelais = 30 * time.Minute

var relaisMu sync.Mutex

func fichierRelais() string { return filepath.Join(dossierModule("hub"), "relais_jalons.json") }

func lireRelais() map[string]RelaisJalon {
	m := map[string]RelaisJalon{}
	lireJSONFichier(fichierRelais(), &m)
	return m
}

func ecrireRelais(projet string, r RelaisJalon) {
	relaisMu.Lock()
	defer relaisMu.Unlock()
	m := lireRelais()
	m[projet] = r
	_ = ecrireJSONFichier(fichierRelais(), m, 0o600)
}

// relaisActif: the project's dev environment asks to be deployed after each milestone.
func (s *Serveur) relaisActif(projet string) bool {
	return s.acces != nil && s.acces.Environnements(projet)["dev"].DeployerAuJalon
}

// noterJalonRelais: a milestone just done → its HEAD waits to be deployed to dev (never twice the same milestone).
func (s *Serveur) noterJalonRelais(p Projet, jalon string, quand time.Time) {
	if !s.relaisActif(p.Nom) || lireRelais()[p.Nom].Jalon == jalon {
		return
	}
	r := RelaisJalon{Jalon: jalon, Date: quand.UTC().Format(time.RFC3339), Etat: "attente"}
	if sha, err := gitSortie(p.Dossier, "rev-parse", "HEAD"); err == nil && reCommitComplet.MatchString(sha) {
		r.Commit = sha
	} else {
		r.Etat, r.Erreur = "echec", "the project has no commit to deploy"
	}
	ecrireRelais(p.Nom, r)
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "hub", Projet: p.Nom, Env: "dev", Action: "relais après jalon", Resultat: jalon + " → " + r.Commit})
}

// avancerRelais: the waiting relay of p started once its commit can be fetched (git source: on a remote
// branch, or after attentePushRelais); returns a channel closed when that deployment ends (nil: none started).
func (s *Serveur) avancerRelais(p Projet, maintenant time.Time) <-chan struct{} {
	r, ok := lireRelais()[p.Nom]
	if !ok || r.Etat != "attente" {
		return nil
	}
	e := s.acces.Environnements(p.Nom)["dev"]
	if !e.DeployerAuJalon {
		return nil
	}
	if e.Deploiement != nil && e.Deploiement.Source == "git" {
		depuis, _ := time.Parse(time.RFC3339, r.Date)
		if sortieGit(p.Dossier, "branch", "-r", "--contains", r.Commit) == "" && maintenant.Sub(depuis) < attentePushRelais {
			return nil // the pilot pushes after the milestone's commit
		}
	}
	d, suite, err := s.commencerDeploiement(p.Nom, "dev", r.Commit, OptionsDeploiement{Auteur: "relais"})
	if err != nil {
		r.Etat, r.Erreur = "echec", s.masquerSecrets(err.Error())
		ecrireRelais(p.Nom, r)
		s.notifierRelais(p.Nom, false, r.Jalon)
		return nil
	}
	r.Etat, r.ID = "lance", d.ID
	ecrireRelais(p.Nom, r)
	fini := make(chan struct{})
	go func() {
		defer close(fini)
		d, _ := suite(context.Background()) // its result is in the history
		s.notifierRelais(p.Nom, d.Etat == "ok", r.Jalon)
	}()
	return fini
}

func (s *Serveur) notifierRelais(projet string, ok bool, jalon string) {
	cle := "relais_echec"
	if ok {
		cle = "relais_ok"
	}
	s.notifierAppareils(MessagePush{Titre: s.textePush(cle) + " · " + projet, Texte: jalon, URL: "/#suivi", Tag: "relais-" + projet})
}

// etatRelais: the project's last relay with the state of its deployment (nil: relay off or nothing relayed yet).
func (s *Serveur) etatRelais(projet string) *RelaisJalon {
	if !s.relaisActif(projet) {
		return nil
	}
	r, ok := lireRelais()[projet]
	if !ok {
		return nil
	}
	if r.Etat == "lance" {
		h := historiqueDeploiements(projet, "dev")
		if i := slices.IndexFunc(h, func(d Deploiement) bool { return d.ID == r.ID }); i >= 0 {
			r.Etat, r.Erreur = h[i].Etat, h[i].Erreur // en_cours | ok | echec | revenu
			if h[i].Verification != nil && !h[i].Verification.OK && r.Erreur == "" {
				r.Erreur = "the external check failed"
			}
		}
	}
	return &r
}

// alertesRelais: the bell rings for a relay that failed (not started, or its deployment failed).
func (s *Serveur) alertesRelais() []AlerteHub {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	out := []AlerteHub{}
	for _, p := range projets {
		if p.Archive {
			continue
		}
		v := s.etatRelais(p.Nom)
		if v == nil || v.Etat != "echec" {
			continue
		}
		h := sha256.Sum256([]byte("relais\x00" + p.Nom + "\x00" + v.Jalon + "\x00" + v.Commit))
		empreinte := hex.EncodeToString(h[:12])
		out = append(out, AlerteHub{Empreinte: empreinte, Module: "dev", Element: p.Nom, Titre: p.Nom,
			Raison: "Dev deployment after the milestone failed ({0}): {1}", Args: []any{v.Jalon, v.Erreur}, Actions: []ActionAlerte{
				{ID: "voir", Libelle: "View", Page: "acces", Projet: p.Nom}, ignorerOuRouvrir(empreinte, true)}})
	}
	return out
}
