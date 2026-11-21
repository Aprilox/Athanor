package main

// The card of an old mission (3.0.0 and before): only read now, to show and remove the missions already on disk.

import (
	"net/url"
	"strings"
)

type BriefCarte struct {
	Brief string      `json:"brief"`
	Modes *ModesCarte `json:"modes,omitempty"` // Dev, a new project only: its working modes (as Manage › Working mode)
}

type EnvCarte struct {
	Nom        string   `json:"nom"` // free (nomEnvValide): dev, site…
	URL        string   `json:"url"`
	Protection string   `json:"protection"` // aucune | auth_proxy | access
	Serveurs   []string `json:"serveurs"`   // inventory ids
	// worked out by calculerCarte (C81): the policy the launch writes, and the existing environment when the
	// mission changes it; Proprietaire: the owner gave this environment in Modify (never the agent)
	Politique    string    `json:"politique,omitempty"`
	Avant        *EnvCarte `json:"avant,omitempty"`
	Proprietaire bool      `json:"proprietaire,omitempty"`
	Plancher     string    `json:"plancher,omitempty"` // R5: "accord" when the existing environment already asked; no policy change goes under it
	// M247: the orchestrator sets it when the request speaks of production (an accord for every risky action);
	// also true for the old name prod or a server with the old prod role (validerEnvironnement); never lowered
	Production bool `json:"production,omitempty"`
}

// AccesCarte: a vault entry the mission uses, by name; ID, Type and Portee come from the vault, ARemplir says
// the entry does not exist yet (the owner fills it, the agent never creates one); Decoche is the owner's.
type AccesCarte struct {
	Entree   string `json:"entree"`
	Pour     string `json:"pour"` // the project, "project/env" or a server id of the card
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Portee   string `json:"portee,omitempty"`
	ARemplir bool   `json:"a_remplir,omitempty"`
	Decoche  bool   `json:"decoche,omitempty"`
}

// ApprobationPrevue: Type mise_en_prod | plan_serveurs | deploiement | durcissement | deverrouiller_coffre.
type ApprobationPrevue struct {
	Type  string `json:"type"`
	Cible string `json:"cible,omitempty"`
}

type ToucheCarte struct {
	Serveurs []string `json:"serveurs"` // readable names
	DNS      []string `json:"dns"`
	Zones    []string `json:"zones"`
	Fichiers []string `json:"fichiers"`
	Dossier  string   `json:"dossier,omitempty"` // the project's folder (a new one: where it will be created)
}

// EstimationCarte: a range; Mesure false = no stats.tsv on this PC (25 min a cycle, cost unknown).
type EstimationCarte struct {
	Jalons     int     `json:"jalons"`
	MinutesMin int     `json:"minutes_min"`
	MinutesMax int     `json:"minutes_max"`
	CoutMin    float64 `json:"cout_min,omitempty"` // list price in USD, the measure of the quota stats.tsv gives
	CoutMax    float64 `json:"cout_max,omitempty"`
	Mesure     bool    `json:"mesure"`
}

type CarteMission struct {
	Titre         string       `json:"titre"`
	Projet        ProjetCarte  `json:"projet"`
	Dev           BriefCarte   `json:"dev"`
	Envs          []EnvCarte   `json:"envs"`
	Serveurs      BriefCarte   `json:"serveurs"`
	Acces         []AccesCarte `json:"acces"`
	JalonsEstimes int          `json:"jalons_estimes"`
	// M271: the owner's permission mode for the Servers lane (modesAutorisation, "" = Settings › Permissions), never the agent's
	Permissions string `json:"permissions,omitempty"`
	// worked out by calculerCarte: whatever the agent sends here is replaced
	Approbations          []ApprobationPrevue `json:"approbations"`
	Touche                ToucheCarte         `json:"touche"`
	Estimation            EstimationCarte     `json:"estimation"`
	AvertissementServeurs string              `json:"avertissement_serveurs,omitempty"`
	AvertissementInfra    string              `json:"avertissement_infra,omitempty"` // C80: an infra entry ticked in native mode, the project not forced (launching forces it)
}

// hoteURL: the host name of a URL already checked by validerURLEnv, "" without one.
func hoteURL(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.ToLower(p.Hostname())
}
