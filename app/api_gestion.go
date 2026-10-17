package main

// Managing a project: rename it, move or rename its folder, point it to another folder (the plan and
// the requests follow, the milestones are checked again), archive it, delete it (from the list, its
// workshop, or the whole folder to the recycle bin). Nothing is done while its pilot runs, and a pilot
// cannot start while an action is under way.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// verrouPlan: one read-modify-write of a PLAN.md at a time (folder check, editing from the app).
var verrouPlan sync.Mutex

// ecrireSur writes a workshop file without following a link placed there, through a temporary file
// renamed at the end (a reader never sees it half written).
func ecrireSur(chemin string, donnees []byte) error {
	if fi, err := os.Lstat(chemin); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symbolic link", filepath.Base(chemin))
	}
	t, err := os.CreateTemp(filepath.Dir(chemin), "."+filepath.Base(chemin)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = t.Write(donnees)
	if e := t.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(t.Name(), chemin)
	}
	if err != nil {
		os.Remove(t.Name())
	}
	return err
}

// ----------------------------------------------------------------------------- quick check of the milestones

// etatVerif (.athanor/verification.json): the milestones the quick check unticked because their listed
// files were missing, keyed by their line in the plan, so they tick again by themselves once the files
// are back. Cleared when the plan is edited and when the pilot starts (Claude then decides).
type etatVerif struct {
	Decoches map[string]string `json:"decoches"`
	Quand    string            `json:"quand"`
}

func (p Projet) fichierVerif() string { return filepath.Join(p.Atelier(), "verification.json") }

func (p Projet) lireVerif() etatVerif {
	var e etatVerif
	lireJSONFichier(p.fichierVerif(), &e)
	if e.Decoches == nil {
		e.Decoches = map[string]string{}
	}
	return e
}

// AVerifier: milestones are waiting for their files (the application checks them again by itself).
func (p Projet) AVerifier() bool { return len(p.lireVerif().Decoches) > 0 }

// oublierVerif: the plan was edited by hand: what the quick check unticked is the user's business now.
func (p Projet) oublierVerif() { os.Remove(p.fichierVerif()) }

// dossierVide: nothing in the folder but hidden entries (.athanor, .git, .claude…). An unreadable folder
// is an error, never "empty" (a network or cloud drive hiccup must not untick everything).
func dossierVide(d string) (bool, error) {
	entrees, err := os.ReadDir(d)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	for _, e := range entrees {
		if !strings.HasPrefix(e.Name(), ".") {
			return false, nil
		}
	}
	return true, nil
}

var (
	reParentheses   = regexp.MustCompile(`\([^)]*\)`)
	reCheminFichier = regexp.MustCompile(`^[\p{L}\p{N}_./\-]+$`)
	reExtension     = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]{0,7}$`) // a real extension: ".gd", ".tscn", not ".2"
)

// fichiersDuJalon: the paths of "Files: a, b/c.gd" (dependency plans), only what clearly is a file.
func fichiersDuJalon(j Jalon) []string {
	var l []string
	texte := reParentheses.ReplaceAllString(j.Fichiers, " ")
	for _, t := range strings.FieldsFunc(texte, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		t = strings.ReplaceAll(strings.Trim(t, "`'\""), `\`, "/")
		t = strings.TrimRight(t, ".:")
		if t == "" || strings.HasPrefix(t, "-") || strings.HasPrefix(t, ".athanor") || strings.HasPrefix(t, "/") ||
			strings.Contains(t, "..") || !reCheminFichier.MatchString(t) {
			continue
		}
		if strings.Contains(t, "/") || reExtension.MatchString(t) {
			l = append(l, filepath.FromSlash(t))
		}
	}
	return l
}

// fichierManquant: the first listed file that does not exist; an error other than "does not exist"
// stops the check.
func fichierManquant(dossier string, fichiers []string) (string, error) {
	for _, f := range fichiers {
		if _, err := os.Stat(filepath.Join(dossier, f)); err != nil {
			if os.IsNotExist(err) {
				return f, nil
			}
			return "", err
		}
	}
	return "", nil
}

var reCase = regexp.MustCompile(`^- \[( |x|X)\] `)

// VerifierJalons compares the ticked milestones with what is in the folder: in an empty folder nothing
// is done; a milestone whose listed files are missing is not done. Those unticked for missing files are
// ticked again once the files are back; the rest is for Claude at the next start (REVERIFY).
// Never while the pilot runs: Claude is working on that plan.
func (p Projet) VerifierJalons() (decoches, recoches []string, err error) {
	if p.PiloteActif() {
		return nil, nil, errors.New("Stop its pilot first")
	}
	verrouPlan.Lock()
	defer verrouPlan.Unlock()
	b, err := os.ReadFile(p.Plan())
	if err != nil {
		return nil, nil, nil // no plan yet: nothing to check
	}
	vide, err := dossierVide(p.Dossier)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read the folder: %v", err)
	}
	ev := p.lireVerif()
	lignes := strings.Split(string(b), "\n")
	for i, l := range lignes {
		m := reCase.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		cle := strings.TrimRight(l[6:], "\r") // the milestone's line identifies it (IDs can be renumbered)
		j := decouperJalon(strings.TrimSpace(cle))
		nom := j.ID
		if nom == "" {
			nom = j.Titre
		}
		fichiers := fichiersDuJalon(j)
		manquant, err := fichierManquant(p.Dossier, fichiers)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read the folder: %v", err)
		}
		if m[1] != " " { // ticked
			raison := ""
			if vide {
				raison = "the folder is empty"
			} else if manquant != "" {
				raison = "missing: " + filepath.ToSlash(manquant)
			}
			if raison != "" {
				lignes[i] = "- [ ] " + l[6:]
				if len(fichiers) > 0 { // only those can come back by themselves: the others are for Claude
					ev.Decoches[cle] = raison
				}
				decoches = append(decoches, nom+" ("+raison+")")
			}
		} else if _, ok := ev.Decoches[cle]; ok && !vide && manquant == "" && len(fichiers) > 0 {
			lignes[i] = "- [x] " + l[6:]
			delete(ev.Decoches, cle)
			recoches = append(recoches, nom)
		}
	}
	if len(decoches) == 0 && len(recoches) == 0 {
		return nil, nil, nil
	}
	if err := ecrirePlan(p, strings.Join(lignes, "\n")); err != nil {
		return nil, nil, err
	}
	ev.Quand = time.Now().Format("2006-01-02 15:04")
	if len(ev.Decoches) == 0 {
		p.oublierVerif()
	} else if b, err := json.MarshalIndent(ev, "", "  "); err == nil {
		_ = ecrireSur(p.fichierVerif(), b)
	}
	var note strings.Builder
	fmt.Fprintf(&note, "\n## %s: folder check (Athanor)\n\n", ev.Quand)
	if len(decoches) > 0 {
		fmt.Fprintf(&note, "- Unticked: %s.\n", strings.Join(decoches, "; "))
	}
	if len(recoches) > 0 {
		fmt.Fprintf(&note, "- Ticked again (their files are back): %s.\n", strings.Join(recoches, ", "))
	}
	ajouterFichier(filepath.Join(p.Atelier(), "LOG.md"), note.String())
	if len(decoches) > 0 {
		rouvrirLivraison(p, "milestones to redo after a folder change")
	}
	return decoches, recoches, nil
}

func ajouterFichier(chemin, texte string) {
	if fi, err := os.Lstat(chemin); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return
	}
	f, err := os.OpenFile(chemin, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(texte)
}

var reLivreLigne = regexp.MustCompile(`STATUS: *DONE[^*\n]*`)

// rouvrirLivraison: a delivered project with milestones to redo is not delivered any more (the pilot
// stops when FINAL_REPORT.md says STATUS: DONE).
func rouvrirLivraison(p Projet, raison string) {
	f := filepath.Join(p.Atelier(), "FINAL_REPORT.md")
	b, err := os.ReadFile(f)
	if err != nil || !reLivreLigne.Match(b) {
		return
	}
	_ = ecrireSur(f, reLivreLigne.ReplaceAll(b, []byte("STATUS: REOPENED ("+raison+")")))
}

// demanderReverification: Claude checks every milestone against the folder at the next start.
func demanderReverification(p Projet, pourquoi string) {
	_ = os.MkdirAll(p.Atelier(), 0o755)
	_ = ecrireSur(filepath.Join(p.Atelier(), "REVERIFY"), []byte(time.Now().Format("2006-01-02 15:04")+": "+pourquoi+"\n"))
	rouvrirLivraison(p, "the project's folder changed: milestones to check again")
}

// ----------------------------------------------------------------------------- copying a workshop

// copierAtelier copies the workshop (plan, requests, log, statistics…) into another folder, without
// its run-time logs, screenshots, the parallel workers' checkouts or the pilot's files. The copy is
// made aside and renamed at the end: a failed copy leaves nothing behind.
func copierAtelier(de, vers string) error {
	sauter := map[string]bool{"logs": true, "screenshots": true, ".verif": true, "pont": true, "work": true,
		"pilot.pid": true, "auto": true, "STOP": true, ".plan.tmp": true}
	if err := os.MkdirAll(filepath.Dir(vers), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(vers), ".athanor-copie-*")
	if err != nil {
		return err
	}
	err = filepath.WalkDir(de, func(chemin string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(de, chemin)
		if rel == "." {
			return nil
		}
		if sauter[strings.Split(filepath.ToSlash(rel), "/")[0]] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		cible := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(cible, 0o755)
		}
		if !d.Type().IsRegular() { // no links: a workshop never needs them
			return nil
		}
		src, err := os.Open(chemin)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.OpenFile(cible, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		if e := dst.Close(); err == nil {
			err = e
		}
		return err
	})
	if err == nil {
		err = os.Rename(tmp, vers)
	}
	if err != nil {
		os.RemoveAll(tmp)
	}
	return err
}

// ----------------------------------------------------------------------------- the API

type GestionVue struct {
	Projet     ProjetVue         `json:"projet"`
	Archive    bool              `json:"archive"`
	Vide       bool              `json:"vide"`
	Decoches   map[string]string `json:"decoches,omitempty"`
	Reverifier bool              `json:"reverifier"`
}

func (s *Serveur) apiGestionVoir(w http.ResponseWriter, r *http.Request) {
	nom := r.URL.Query().Get("nom")
	s.mu.Lock()
	var p Projet
	trouve := false
	for _, x := range s.config.Projets {
		if x.Nom == nom {
			p, trouve = x, true
		}
	}
	s.mu.Unlock()
	if !trouve {
		s.erreur(w, http.StatusNotFound, "Unknown project")
		return
	}
	_, errR := os.Stat(filepath.Join(p.Atelier(), "REVERIFY"))
	vide, _ := dossierVide(p.Dossier)
	dec := map[string]string{}
	for l, raison := range p.lireVerif().Decoches {
		j := decouperJalon(strings.TrimSpace(l))
		n := j.ID
		if n == "" {
			n = j.Titre
		}
		dec[n] = raison
	}
	s.ecrireJSON(w, GestionVue{
		Projet:     ProjetVue{Nom: p.Nom, Dossier: p.Dossier, Existe: p.Existe(), Actif: p.PiloteActif()},
		Archive:    p.Archive,
		Vide:       vide,
		Decoches:   dec,
		Reverifier: errR == nil,
	})
}

// dansDossier: d is c, or is inside c (compared without case, as Windows does).
func dansDossier(c, d string) bool {
	norm := func(p string) string {
		return strings.TrimRight(strings.ToLower(filepath.Clean(p)), `\/`) + string(filepath.Separator)
	}
	return strings.HasPrefix(norm(d), norm(c))
}

// conflitProjets: a folder that is, is inside, or contains the folder of another project (archived
// ones included): two projects must never share files, or deleting one would take the other along.
func (s *Serveur) conflitProjets(dossier, sauf string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, y := range s.config.Projets {
		if y.Nom != sauf && (dansDossier(y.Dossier, dossier) || dansDossier(dossier, y.Dossier)) {
			return errors.New("This folder is, contains or is inside the folder of another project.")
		}
	}
	return nil
}

// cheminCible validates a folder typed by the user (absolute, not a system or personal folder, not
// inside Athanor or the workshop, not shared with another project).
func (s *Serveur) cheminCible(d string, p Projet) (string, error) {
	d = strings.TrimSpace(d)
	if !filepath.IsAbs(d) {
		return "", errors.New("Give the full path of the development folder (e.g. D:\\Dev\\my-site)")
	}
	d = filepath.Clean(d)
	if cle := dossierInterdit(d, s.kitCourant().Racine); cle != "" {
		return "", errors.New(cle)
	}
	if strings.EqualFold(d, filepath.Clean(p.Dossier)) {
		return "", errors.New("This is already the project's folder.")
	}
	if dansDossier(p.Atelier(), d) {
		return "", errors.New("The new folder cannot be inside the project's workshop (.athanor).")
	}
	if err := s.conflitProjets(d, p.Nom); err != nil {
		return "", err
	}
	return d, nil
}

// occuper reserves a project for an action (refused if another one is under way); the pilot cannot be
// started meanwhile (see demarrerPilote).
func (s *Serveur) occuper(nom string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.occupes == nil {
		s.occupes = map[string]bool{}
	}
	if s.occupes[nom] {
		return false
	}
	s.occupes[nom] = true
	return true
}

func (s *Serveur) liberer(nom string) {
	s.mu.Lock()
	delete(s.occupes, nom)
	s.mu.Unlock()
}

func (s *Serveur) estOccupe(nom string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.occupes[nom]
}

func (s *Serveur) apiGestion(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom    string `json:"nom"`
		Action string `json:"action"`
		Valeur string `json:"valeur"`
		Mode   string `json:"mode"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	var p Projet
	trouve := false
	for _, x := range s.config.Projets {
		if x.Nom == c.Nom {
			p, trouve = x, true
		}
	}
	s.mu.Unlock()
	if !trouve {
		s.erreur(w, http.StatusNotFound, "Unknown project")
		return
	}
	if !s.occuper(p.Nom) {
		s.erreur(w, http.StatusBadRequest, "Another action is under way on this project.")
		return
	}
	defer s.liberer(p.Nom)
	if p.PiloteActif() {
		s.erreur(w, http.StatusBadRequest, "Stop its pilot first")
		return
	}

	switch c.Action {
	case "renommer":
		nouveau := strings.TrimSpace(c.Valeur)
		if !reNomProjet.MatchString(nouveau) {
			s.erreur(w, http.StatusBadRequest, "Invalid project name (letters, digits, spaces, dashes, 60 characters at most)")
			return
		}
		if err := s.modifierProjet(p.Nom, func(cf *Config, x *Projet) error {
			for _, y := range cf.Projets {
				if y.Nom != p.Nom && strings.EqualFold(y.Nom, nouveau) {
					return errors.New("A project with this name or folder already exists")
				}
			}
			x.Nom = nouveau
			if cf.ProjetActif == p.Nom {
				cf.ProjetActif = nouveau
			}
			return nil
		}); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		s.mu.Lock() // saved: what the server keeps by name follows
		if j, ok := s.logs[p.Nom]; ok {
			delete(s.logs, p.Nom)
			s.logs[nouveau] = j
		}
		delete(s.dernierActif, p.Nom)
		delete(s.relancer, p.Nom)
		s.mu.Unlock()
		s.ecrireJSON(w, map[string]any{"message_key": "Project renamed.", "args": []string{}, "nom": nouveau})

	case "deplacer": // move or rename the folder itself, with everything in it
		cible, err := s.cheminCible(c.Valeur, p)
		if err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := os.Stat(cible); err == nil {
			s.erreur(w, http.StatusBadRequest, "This folder already exists: choose a new name, or use “Use another folder”.")
			return
		}
		if _, err := os.Stat(filepath.Dir(cible)); err != nil {
			s.erreur(w, http.StatusBadRequest, "The parent folder does not exist.")
			return
		}
		if err := os.Rename(p.Dossier, cible); err != nil {
			s.erreur(w, http.StatusBadRequest, "Cannot move the folder (another drive, or a file in use): copy it yourself, then use “Use another folder”.")
			return
		}
		if err := s.changerDossier(p, cible); err != nil {
			_ = os.Rename(cible, p.Dossier) // the configuration could not follow: everything goes back
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.msg(w, "Folder moved.")

	case "dossier": // point the project to another folder: the workshop follows, the milestones are checked
		cible, err := s.cheminCible(c.Valeur, p)
		if err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := os.MkdirAll(cible, 0o755); err != nil {
			s.erreur(w, http.StatusInternalServerError, "Cannot create the folder: "+err.Error())
			return
		}
		nouveau := Projet{Nom: p.Nom, Dossier: cible}
		_, errNouveau := os.Stat(nouveau.Atelier())
		_, errAncien := os.Stat(p.Atelier())
		if os.IsNotExist(errNouveau) && errAncien == nil {
			// no workshop there: this project's plan, requests and history come along
			if err := copierAtelier(p.Atelier(), nouveau.Atelier()); err != nil {
				s.erreur(w, http.StatusInternalServerError, "Cannot copy the workshop: "+err.Error())
				return
			}
		}
		if err := s.changerDossier(p, cible); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		decoches, recoches, _ := nouveau.VerifierJalons()
		demanderReverification(nouveau, "project pointed from "+p.Dossier+" to "+cible)
		s.ecrireJSON(w, map[string]any{"message_key": "Folder changed: the milestones were checked against its content; Claude checks them again at the next start.", "args": []string{}, "decoches": decoches, "recoches": recoches})

	case "verifier":
		decoches, recoches, err := p.VerifierJalons()
		if err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		if c.Mode == "claude" {
			demanderReverification(p, "check asked from the application")
		}
		s.ecrireJSON(w, map[string]any{"ok": true, "decoches": decoches, "recoches": recoches})

	case "archiver", "desarchiver":
		archive := c.Action == "archiver"
		if err := s.modifierProjet(p.Nom, func(cf *Config, x *Projet) error {
			x.Archive = archive
			if archive && cf.ProjetActif == p.Nom {
				cf.ProjetActif = ""
				for _, y := range cf.Projets {
					if !y.Archive && y.Nom != p.Nom {
						cf.ProjetActif = y.Nom
						break
					}
				}
			}
			if !archive && cf.ProjetActif == "" {
				cf.ProjetActif = p.Nom
			}
			return nil
		}); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		if archive {
			marqueAuto(p, false) // an archived project is never restarted when the application opens
			s.mu.Lock()
			delete(s.relancer, p.Nom)
			s.mu.Unlock()
			s.msg(w, "Project archived: find it again under “Archived projects”.")
		} else {
			s.msg(w, "Project brought back.")
		}

	case "supprimer":
		var cible string
		switch c.Mode {
		case "liste":
		case "atelier":
			cible = p.Atelier()
		case "dossier":
			cible = p.Dossier
			// checked again now: never a system or personal folder, never another project's files
			if cle := dossierInterdit(p.Dossier, s.kitCourant().Racine); cle != "" {
				s.erreur(w, http.StatusBadRequest, cle)
				return
			}
			if err := s.conflitProjets(p.Dossier, p.Nom); err != nil {
				s.erreur(w, http.StatusBadRequest, err.Error())
				return
			}
		default:
			s.erreur(w, http.StatusBadRequest, "Unknown action")
			return
		}
		if cible != "" {
			if _, err := os.Lstat(cible); err == nil {
				if err := mettreALaCorbeille(cible); err != nil {
					s.erreur(w, http.StatusInternalServerError, "Cannot send it to the recycle bin: "+err.Error())
					return
				}
			} else if !os.IsNotExist(err) {
				s.erreur(w, http.StatusInternalServerError, "Cannot send it to the recycle bin: "+err.Error())
				return
			}
		}
		if err := s.retirerProjet(p); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		switch c.Mode {
		case "liste":
			s.msg(w, "Project removed from the list. Its folder is intact; delete its .athanor subfolder to clean it.")
		case "atelier":
			s.msg(w, "Project removed; its workshop (.athanor) is in the recycle bin.")
		default:
			s.msg(w, "Project removed; its folder is in the recycle bin.")
		}

	default:
		s.erreur(w, http.StatusBadRequest, "Unknown action")
	}
}

// copieConfig: a copy that can be changed without touching the configuration in use.
func copieConfig(c Config) Config {
	c.Projets = append([]Projet{}, c.Projets...)
	f := map[string]string{}
	for k, v := range c.FinsVues {
		f[k] = v
	}
	c.FinsVues = f
	return c
}

// modifierProjet changes a project on a copy of the configuration, saves it, and only then uses it:
// if the save fails, nothing changed.
func (s *Serveur) modifierProjet(nom string, f func(*Config, *Projet) error) error {
	s.mu.Lock()
	cf := copieConfig(s.config)
	i := -1
	for k := range cf.Projets {
		if cf.Projets[k].Nom == nom {
			i = k
		}
	}
	if i < 0 {
		s.mu.Unlock()
		return errors.New("Unknown project")
	}
	if err := f(&cf, &cf.Projets[i]); err != nil {
		s.mu.Unlock()
		return err
	}
	if err := ecrireConfig(cf); err != nil {
		s.mu.Unlock()
		return err
	}
	s.config = cf
	s.mu.Unlock()
	s.apresChangementProjets()
	return nil
}

// apresChangementProjets: the virtual desktop and the kit scripts follow the list of folders. Never
// under s.mu (the Bureau setters take their own lock); one update at a time, so the last one wins.
func (s *Serveur) apresChangementProjets() {
	s.majProjets.Lock()
	defer s.majProjets.Unlock()
	s.mu.Lock()
	d := s.dossiersProjets()
	s.ecrireListeProjets()
	s.mu.Unlock()
	s.bureau.DefinirDossiers(d)
}

// changerDossier: the project now lives in another folder; what is keyed by its folder follows
// (end already announced, Skills & MCP chosen for it).
func (s *Serveur) changerDossier(p Projet, cible string) error {
	if err := s.modifierProjet(p.Nom, func(cf *Config, x *Projet) error {
		x.Dossier = cible
		if v, ok := cf.FinsVues[p.Dossier]; ok {
			delete(cf.FinsVues, p.Dossier)
			cf.FinsVues[cible] = v
		}
		return nil
	}); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.dernierActif, p.Nom)
	delete(s.relancer, p.Nom)
	s.mu.Unlock()
	k := s.kitCourant()
	choix := lireChoix(k)
	if c, ok := choix[p.Dossier]; ok {
		delete(choix, p.Dossier)
		choix[cible] = c
		_ = ecrireJSONFichier(filepath.Join(dossierExt(k), "projets.json"), choix, 0o644)
	}
	return nil
}

// retirerProjet removes a project from the list (its files are handled by the caller).
func (s *Serveur) retirerProjet(p Projet) error {
	s.mu.Lock()
	cf := copieConfig(s.config)
	garde := cf.Projets[:0]
	for _, x := range cf.Projets {
		if x.Nom != p.Nom {
			garde = append(garde, x)
		}
	}
	cf.Projets = garde
	if cf.ProjetActif == p.Nom {
		cf.ProjetActif = ""
	}
	delete(cf.FinsVues, p.Dossier)
	if err := ecrireConfig(cf); err != nil {
		s.mu.Unlock()
		return err
	}
	s.config = cf
	delete(s.logs, p.Nom)
	delete(s.dernierActif, p.Nom)
	delete(s.relancer, p.Nom)
	s.mu.Unlock()
	k := s.kitCourant()
	choix := lireChoix(k)
	if _, ok := choix[p.Dossier]; ok {
		delete(choix, p.Dossier)
		_ = ecrireJSONFichier(filepath.Join(dossierExt(k), "projets.json"), choix, 0o644)
	}
	s.apresChangementProjets()
	return nil
}

// verifierEnFond: while milestones wait for their files (after a folder change), the current project
// is checked again now and then, so they are ticked again as soon as the files are back.
func (s *Serveur) verifierEnFond(p Projet) {
	if p.PiloteActif() || !p.AVerifier() || s.estOccupe(p.Nom) {
		return
	}
	s.mu.Lock()
	if s.derniereVerif == nil {
		s.derniereVerif = map[string]time.Time{}
	}
	if time.Since(s.derniereVerif[p.Dossier]) < 15*time.Second {
		s.mu.Unlock()
		return
	}
	s.derniereVerif[p.Dossier] = time.Now()
	s.mu.Unlock()
	_, _, _ = p.VerifierJalons()
}
