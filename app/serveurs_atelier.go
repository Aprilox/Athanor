package main

// The workshop of a server (M96, ARCHITECTURE.md §Garde-fous): modules/servers/<id>/RUNBOOK.md, made when the
// server is added (its access without any secret, its roles, what runs according to its sheet, then the
// History, Backups and Going back sections). A Servers agent reads it with runbook_lire and adds to it with
// runbook_ajouter (masked, bounded, dated, logged); Athanor itself adds the changes that ran and their
// backups. No agent writes any other file of Athanor: the SFTP tools only use the server's files/ folder.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	maxEntreeRunbook = 4000      // characters of one runbook_ajouter
	maxRunbook       = 256 << 10 // an agent's entry is refused beyond (Athanor's own short lines are still added)
)

// sectionsRunbook: the sections an entry goes to (runbook_ajouter's section), with their heading.
var sectionsRunbook = map[string]string{"historique": "History", "sauvegardes": "Backups", "revenir": "Going back"}

// verrouRunbook: a runbook is read and rewritten under it, so that two entries never lose one another.
var verrouRunbook sync.Mutex

func fichierRunbook(id string) string {
	return filepath.Join(dossierModule("servers"), id, "RUNBOOK.md")
}

// markdownRunbook: the runbook as it is made (f nil: no sheet yet).
func markdownRunbook(srv ServeurInventaire, f *FicheServeur, quand time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Runbook of server %s\n\nMade by Athanor on %s. Kept by Athanor and by the Servers agents (runbook_ajouter): read it before working on this server. "+
		"Never write a secret here; Athanor masks the vault's values.\n\n## Access\n\n", srv.Nom, quand.UTC().Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&b, "- **Address**: `%s@%s` (SSH)\n", srv.Utilisateur, adresseSSH(srv.Hote, srv.Port))
	if srv.EmpreinteHote != "" {
		fmt.Fprintf(&b, "- **Host fingerprint**: `%s`\n", srv.EmpreinteHote)
	}
	b.WriteString("- **Key**: in Athanor's vault; its value never leaves it. Agents reach the server only through Athanor's Servers tools, by its id `" + srv.ID + "`.\n")
	b.WriteString("\n## Role\n\n")
	if len(srv.Roles) == 0 {
		b.WriteString("- no role yet\n")
	}
	for _, r := range srv.Roles {
		fmt.Fprintf(&b, "- %s\n", map[string]string{"dev": "dev: tests and previews", "prod": "prod: a live site, every change needs the owner's accord"}[r])
	}
	if f == nil {
		b.WriteString("\n## What runs\n\n- its sheet was not read yet: see serveur_fiche\n")
	} else {
		fmt.Fprintf(&b, "\n## What runs (sheet of %s; serveur_fiche gives the current one)\n\n", f.Date.UTC().Format("2006-01-02 15:04 UTC"))
		fmt.Fprintf(&b, "- **System**: %s\n", premierNonVide(f.Systeme.Nom, "unknown"))
		fmt.Fprintf(&b, "- **Services**: %s\n", premierNonVide(strings.Join(f.Services, ", "), "none found"))
		var l []string
		for _, c := range f.Conteneurs {
			l = append(l, c.Nom+" ("+c.Image+")")
		}
		fmt.Fprintf(&b, "- **Containers**: %s\n", premierNonVide(strings.Join(l, ", "), "none"))
		l = nil
		for _, p := range f.Ports {
			l = append(l, fmt.Sprintf("%d/%s", p.Port, p.Proto))
		}
		fmt.Fprintf(&b, "- **Listening ports**: %s\n", premierNonVide(strings.Join(slices.Compact(l), ", "), "none found"))
	}
	b.WriteString("\n## History\n\n## Backups\n\n## Going back\n\n" +
		"- A file Athanor writes keeps its previous version next to it, in `<file>.athanor-prev`.\n" +
		"- The paths a destructive step names are archived first in `" + dossierSauvegardes + "/<plan>-<step>.tar.gz` (see Backups); restoring one is a step of a plan the owner accepts.\n" +
		"- A firewall or SSH change that is not confirmed is undone by the server itself when its delay ends.\n")
	return b.String()
}

func premierNonVide(s, sinon string) string {
	if s == "" {
		return sinon
	}
	return s
}

// assurerRunbook: the runbook of server id, made from its inventory entry and its sheet when it is missing.
// Called under verrouRunbook.
func assurerRunbook(id string) (string, error) {
	if !idServeurValide.MatchString(id) {
		return "", errServeurInconnu
	}
	b, err := os.ReadFile(fichierRunbook(id))
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return string(b), err
	}
	l, err := lireServeurs()
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(l, func(v ServeurInventaire) bool { return v.ID == id })
	if i < 0 {
		return "", errServeurInconnu
	}
	var f *FicheServeur
	if j, err := os.ReadFile(filepath.Join(dossierModule("servers"), id, "fiche.json")); err == nil {
		var v FicheServeur
		if json.Unmarshal(j, &v) == nil {
			f = &v
		}
	}
	t := markdownRunbook(l[i], f, time.Now())
	if err := os.MkdirAll(filepath.Dir(fichierRunbook(id)), 0o700); err != nil {
		return "", err
	}
	return t, ecrireAtomique(fichierRunbook(id), []byte(t), 0o600)
}

// ajouterRunbook adds a dated entry of de at the end of a section (its continuation lines indented, so that
// no text makes a heading of its own); limite > 0: refused when the runbook would grow beyond it.
func ajouterRunbook(id, section, de, texte string, limite int) error {
	titre, ok := sectionsRunbook[section]
	if !ok {
		return errors.New("section must be historique, sauvegardes or revenir")
	}
	verrouRunbook.Lock()
	defer verrouRunbook.Unlock()
	t, err := assurerRunbook(id)
	if err != nil {
		return err
	}
	texte = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !caractereInvisible(r) {
			return r
		}
		return -1
	}, strings.ReplaceAll(texte, "\r\n", "\n"))
	ligne := "- " + time.Now().UTC().Format("2006-01-02 15:04 UTC") + " · " + de + ": " + strings.ReplaceAll(strings.TrimSpace(texte), "\n", "\n  ") + "\n"
	if limite > 0 && len(t)+len(ligne) > limite {
		return fmt.Errorf("the runbook is full (%d KB): Athanor still adds the changes that run and their backups", limite>>10)
	}
	tete := "\n## " + titre + "\n"
	i := strings.Index(t, tete)
	if i < 0 { // a section lost (edited by hand): made again at the end
		t = strings.TrimRight(t, "\n") + "\n" + tete
		i = strings.Index(t, tete)
	}
	debut, fin := i+len(tete), len(t)
	if j := strings.Index(t[debut:], "\n## "); j >= 0 {
		fin = debut + j + 1 // the next heading's line
	}
	corps := "\n"
	if c := strings.Trim(t[debut:fin], "\n"); c != "" {
		corps = "\n" + c + "\n"
	}
	t = t[:debut] + corps + ligne + "\n" + t[fin:]
	return ecrireAtomique(fichierRunbook(id), []byte(strings.TrimRight(t, "\n")+"\n"), 0o600)
}

// noterRunbook: a change or destructive call that ran for an agent goes to History, its backups to Backups.
func (s *Serveur) noterRunbook(id string, e Etape, r ResultatOutil, err error) {
	ou := "under the owner's rule for this server"
	if e.Plan != "" {
		ou = fmt.Sprintf("plan %s, step %d", e.Plan, e.Num)
	}
	quoi := fmt.Sprintf("%s (%s, %s) %s → ", e.Outil, e.Classe, ou, couper(s.masquerSecrets(string(e.Arguments)), 300))
	if err != nil {
		quoi += "error: " + couper(s.masquerSecrets(err.Error()), 300)
	} else {
		quoi += fmt.Sprintf("code %d", r.Code)
	}
	if e := ajouterRunbook(id, "historique", "Athanor", quoi, 0); e != nil {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "serveur:" + id, Action: "serveurs.runbook", Resultat: "non écrit : " + couper(e.Error(), auditMaxOutil)})
		return
	}
	for _, b := range r.sauvegardes {
		_ = ajouterRunbook(id, "sauvegardes", "Athanor", couper(s.masquerSecrets(b), 500)+" ("+ou+")", 0)
	}
}

func init() {
	srv := schemaTexte("The server's id, from serveurs_liste")
	outilsHub["runbook_lire"] = OutilHub{Description: "Read the server's RUNBOOK.md: its access, role, what runs, the history of the changes, the backups and how to go back. Read it before working on the server.",
		Schema: schemaObjet(map[string]any{"serveur": srv}, "serveur"), Faire: outilRunbookLire}
	outilsHub["runbook_ajouter"] = OutilHub{Description: "Add a dated entry to a section of the server's RUNBOOK.md (what you did and found, a backup, how to undo a change). Never a secret.",
		Schema: schemaObjet(map[string]any{"serveur": srv,
			"section": map[string]any{"type": "string", "description": "historique: what was done or found; sauvegardes: a backup; revenir: how to undo a change", "enum": []string{"historique", "sauvegardes", "revenir"}},
			"texte":   schemaTexte(fmt.Sprintf("The entry, %d characters at most", maxEntreeRunbook))}, "serveur", "section", "texte"),
		Faire: outilRunbookAjouter}
}

func outilRunbookLire(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct {
		Serveur string `json:"serveur"`
	}
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	if err := s.serveurPerimetre(x, "runbook_lire", a.Serveur); err != nil {
		return "", err
	}
	verrouRunbook.Lock()
	t, err := assurerRunbook(a.Serveur)
	verrouRunbook.Unlock()
	if err != nil {
		return "", err
	}
	// agents' entries and names read from the server are in it: data too
	return enDonnees("server-runbook", fmt.Sprintf(` server="%s"`, a.Serveur), strings.TrimSuffix(s.masquerSecrets(t), "\n")), nil
}

func outilRunbookAjouter(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct {
		Serveur string `json:"serveur"`
		Section string `json:"section"`
		Texte   string `json:"texte"`
	}
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	if err := s.serveurPerimetre(x, "runbook_ajouter", a.Serveur); err != nil {
		return "", err
	}
	texte, err := texteRequis("texte", a.Texte, maxEntreeRunbook)
	if err == nil {
		texte = s.masquerSecrets(texte)
		err = ajouterRunbook(a.Serveur, a.Section, "agent", texte, maxRunbook)
	}
	res := "ajouté à " + a.Section + " : " + couper(texte, auditMaxOutil)
	if err != nil {
		res = "refusé : " + couper(s.masquerSecrets(err.Error()), auditMaxOutil)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "serveur:" + a.Serveur, Projet: x.Projet, Mission: x.Mission,
		Agent: x.Agent, Action: "serveurs.runbook_ajouter", Resultat: res})
	if err != nil {
		return "", err
	}
	return enJSON(map[string]any{"ok": true, "section": a.Section}), nil
}
