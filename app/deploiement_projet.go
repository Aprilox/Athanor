package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"strings"
)

// PreparationRelais (M283): the server preparation of a project's deployment, kept in its relay record.
type PreparationRelais struct {
	Tache     string `json:"tache,omitempty"`     // its Servers task ("": the servers already served the project)
	Empreinte string `json:"empreinte,omitempty"` // the servers, the address and the deployment plan it was made for
	Erreur    string `json:"erreur,omitempty"`    // why it did not start (tried again at the next look)
}

// planPret (M249): p's framing is done, its plan holds at least one milestone.
func planPret(p Projet) bool { return len(p.LirePlan()) > 0 }

// donneesPreparation: what the preparation brief is made of: the project's deployment, or a mission's card
// (briefPreparationMission, until M302).
type donneesPreparation struct {
	Entete          string   // who asks, before the address
	URL, Protection string   // the site's address and its protection
	DNS             string   // the provider holding the host's zone, its token given ("" none)
	APIs, ARemplir  []string // names of the vault entries given, and of those the owner has still to fill
	Plan            string   // the deployment's plan, in the owner's words
	Demande         string   // the project's request
	Accord          bool     // a production is submitted to the owner before it first goes online
}

// briefPreparation (M249): what the Servers task prepares before the first deployment: the standard list adapted to
// the plan, the DNS provider, the vault entries given or still to fill (names only), the plans.
func briefPreparation(d donneesPreparation, p Projet) string {
	var b strings.Builder
	b.WriteString(d.Entete + ": " + d.URL + ", protection " + d.Protection + ".\n" +
		"Prepare these servers now, while the Studio builds the project, so that each delivery then only deploys it. " +
		"Standard list, adapted to the project's plan below (skip what it does not need): the system packages and runtime it needs; " +
		"nginx or an equivalent web server as reverse proxy; a dedicated system user; the site's folder; the TLS certificate of " +
		"the host; the firewall (SSH, HTTP and HTTPS only). Do not deploy the application itself.\n")
	if d.Accord {
		b.WriteString("If the deployment plan or the request below describes a production (real users or data, a public launch), " +
			"propose your plan and wait for the owner's accord before anything goes online for the first time.\n")
	}
	if d.DNS != "" {
		b.WriteString("DNS: the zone of " + hoteURL(d.URL) + " is at the provider " + d.DNS + ", its token is given.\n")
	}
	if len(d.APIs) > 0 {
		b.WriteString("Vault entries given (names only, never ask for a value): " + strings.Join(d.APIs, ", ") + ".\n")
	}
	if len(d.ARemplir) > 0 {
		b.WriteString("Vault entries the owner has still to fill (leave what needs them for later): " + strings.Join(d.ARemplir, ", ") + ".\n")
	}
	// both cut: a task's text holds maxTexteTache characters, the project's plan comes last
	if t := strings.TrimSpace(d.Plan); t != "" {
		b.WriteString("Deployment plan (the owner's words):\n" + couper(t, 1500) + "\n")
	}
	if t := strings.TrimSpace(d.Demande); t != "" {
		b.WriteString("Project's request (its start):\n" + couper(t, 600) + "\n")
	}
	b.WriteString("Project plan:\n")
	for _, j := range p.LirePlan() {
		b.WriteString("- " + couper(j.Titre, 160) + "\n")
	}
	return b.String()
}

// tientHote: h is in one of f's zones.
func tientHote(f Fournisseur, h string) bool {
	return h != "" && slices.ContainsFunc(f.Zones, func(z ZoneFournisseur) bool { return h == z.Nom || strings.HasSuffix(h, "."+z.Nom) })
}

// fournisseurDeploiement (M283): among l, the provider of e: the one whose token is among e's API (the one holding
// its host's zone first), else the older e.Fournisseur.
func fournisseurDeploiement(e Environnement, l []Fournisseur) (Fournisseur, bool) {
	h := hoteURL(e.URL)
	for _, choix := range []func(Fournisseur) bool{
		func(f Fournisseur) bool { return slices.Contains(e.APIs, f.Cle) && tientHote(f, h) },
		func(f Fournisseur) bool { return slices.Contains(e.APIs, f.Cle) },
		func(f Fournisseur) bool { return e.Fournisseur != "" && f.ID == e.Fournisseur },
	} {
		if i := slices.IndexFunc(l, choix); i >= 0 {
			return l[i], true
		}
	}
	return Fournisseur{}, false
}

// briefPreparationProjet: the preparation brief of p's deployment e.
func (s *Serveur) briefPreparationProjet(p Projet, e Environnement) string {
	d := donneesPreparation{Entete: "Project \"" + p.Nom + "\"", URL: e.URL, Protection: e.Protection, Plan: e.Plan,
		Accord: modeProjet(p.Nom) != "ignorer"}
	if s.coffre != nil {
		for _, x := range s.coffre.Liste() {
			if slices.Contains(e.APIs, x.ID) {
				d.APIs = append(d.APIs, x.Nom)
			}
		}
	}
	l, _ := lireFournisseurs()
	if f, ok := fournisseurDeploiement(e, l); ok && tientHote(f, hoteURL(e.URL)) {
		d.DNS = f.ID
	}
	b, _ := os.ReadFile(p.Demande())
	d.Demande = string(b)
	return briefPreparation(d, p)
}

// preparerProjet (M283): the servers of p's deployment are prepared by a Servers task as soon as its plan holds a
// milestone, once per fingerprint (servers, address, deployment plan) noted in the relay's record: a restart
// launches nothing again, a change of one of them prepares again. Nothing without a server or a moment, nor
// while a mission covers the project (its own Servers lane prepares, until M302). The first look at a project
// already deployed only learns: its servers serve it.
func (s *Serveur) preparerProjet(p Projet) {
	if s.acces == nil {
		return
	}
	e := s.acces.Environnements(p.Nom)[s.envJalon(p.Nom)]
	if len(e.Serveurs) == 0 || e.Moment == "" || !planPret(p) || missionSurProjet(p.Nom) {
		return
	}
	serveurs := slices.Sorted(slices.Values(e.Serveurs))
	h := sha256.Sum256([]byte(strings.Join(serveurs, ",") + "\x00" + e.URL + "\x00" + e.Plan))
	r, prep := lireRelais()[p.Nom], PreparationRelais{Empreinte: hex.EncodeToString(h[:12])}
	if r.Preparation != nil && r.Preparation.Empreinte == prep.Empreinte {
		return
	}
	if r.Preparation != nil || e.Deploiement == nil {
		t, err := s.lancerTache(couper(s.briefPreparationProjet(p, e), maxTexteTache-1), e.Serveurs, false, p.Nom)
		if err != nil {
			prep = PreparationRelais{Erreur: s.masquerSecrets(err.Error())}
			if r.Preparation != nil && *r.Preparation == prep {
				return // said once, tried again at each look (the vault closed, native Windows mode)
			}
		} else {
			prep.Tache = t.ID
			s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "servers", Projet: p.Nom, Env: s.envJalon(p.Nom),
				Action: "préparation du serveur", Resultat: t.ID + " (" + strings.Join(serveurs, ", ") + ")"})
		}
	}
	r.Preparation = &prep
	ecrireRelais(p.Nom, r)
}
