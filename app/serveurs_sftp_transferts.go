package main

// SFTP page, the transfers: copies between this computer and the server run in Athanor's Go on the terminal's
// connection, never through the page, so that switching tabs or reloading cuts nothing. One queue per server:
// at most transfertsParallelesMax at a time, progress and speed, Annuler, automatic resumption after a network
// error (from the bytes already written), and a name already taken is never overwritten without an answer
// (remplacer, garder les deux, passer, optionally for all). The assistant's own transfers (sftp_envoyer,
// sftp_recuperer, through its guardrails) are listed in the same queue, "par l'assistant". Same routes as the
// files panel: behind garde, refused from a paired device (routeShellDistance), never a tool of the assistant;
// Athanor's data folder is never sent nor written (protegeLocal); the audit names paths and sizes only.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
)

const (
	transfertsParallelesMax = 3
	transfertsGardes        = 100    // finished transfers kept in the list
	elementsTransfertMax    = 100000 // files and folders in one transfer
	sourcesTransfertMax     = 1000
	tamponTransfert         = 1 << 20 // large writes: pkg/sftp sends them as concurrent requests
	reprisesTransfertMax    = 3
	errTransfertSens        = "Choose a direction: envoi or telechargement"
	errTransfertDossier     = "The destination must be an existing folder"
	errTransfertInconnu     = "This transfer no longer exists"
	errTransfertAction      = "This action does not apply to this transfer now"
	errTransfertTrop        = "Too many files in one transfer: send the folder in parts"
	errTransfertType        = "A file and a folder cannot replace each other: rename one first"
	errTransfertCoupe       = "The connection to the server closed: open the terminal again, then Reprendre"
)

// pauseRepriseTransfert: the wait before an automatic resumption, times the attempt (shorter in the tests).
var pauseRepriseTransfert = time.Second

// fauteTransfert: a test's fault injected while bytes are written (nil outside the tests).
var fauteTransfert func(tr *transfertSFTP) error

// etatTransfert: what the page shows of one transfer.
type etatTransfert struct {
	ID            string `json:"id"`
	Sens          string `json:"sens"` // envoi (this computer -> server) or telechargement
	Par           string `json:"par"`  // vous or assistant
	Nom           string `json:"nom"`
	Source        string `json:"source"`
	Cible         string `json:"cible"` // the destination's full path, once known
	Dossier       bool   `json:"dossier"`
	Total         int64  `json:"total"`
	Fait          int64  `json:"fait"`
	Vitesse       int64  `json:"vitesse"` // bytes per second
	Fichiers      int    `json:"fichiers"`
	FichiersTotal int    `json:"fichiers_total"`
	Etat          string `json:"etat"`              // attente, en_cours, conflit, termine, passe, annule, erreur
	Conflit       string `json:"conflit,omitempty"` // the name already taken, while etat is conflit
	Erreur        string `json:"erreur,omitempty"`
	Reprises      int    `json:"reprises,omitempty"`
	Debut         int64  `json:"debut"` // unix seconds
}

type transfertSFTP struct {
	etatTransfert
	serveur    string
	dossierDst string // the destination folder asked
	t          *sessionTerminal
	annuler    context.CancelFunc
	decision   chan string
	choix      string // the conflict's answer, kept for Reprendre
	part       string // the partial file being written (kept after an error, for Reprendre)
	faitAvant  int64  // bytes of the files already finished
	mesure     time.Time
	faitMesure int64
}

type fileTransferts struct {
	liste     []*transfertSFTP
	sem       chan struct{}
	politique string // "appliquer à tous": the answer to the next conflicts while transfers remain
}

var transferts = struct {
	sync.Mutex
	m map[string]*fileTransferts
}{m: map[string]*fileTransferts{}}

// fileDe: server id's queue; transferts is locked.
func fileDe(serveur string) *fileTransferts {
	f := transferts.m[serveur]
	if f == nil {
		f = &fileTransferts{sem: make(chan struct{}, transfertsParallelesMax)}
		transferts.m[serveur] = f
	}
	return f
}

func actif(etat string) bool { return etat == "attente" || etat == "en_cours" || etat == "conflit" }

// ajouterTransfert: tr joins its server's queue; the oldest finished ones leave it beyond transfertsGardes.
func ajouterTransfert(tr *transfertSFTP) *fileTransferts {
	transferts.Lock()
	defer transferts.Unlock()
	f := fileDe(tr.serveur)
	f.liste = append(f.liste, tr)
	for i := 0; len(f.liste) > transfertsGardes && i < len(f.liste); {
		if actif(f.liste[i].Etat) {
			i++
			continue
		}
		f.liste = slices.Delete(f.liste, i, i+1)
	}
	return f
}

func listeTransferts(serveur string) []etatTransfert {
	transferts.Lock()
	defer transferts.Unlock()
	l := []etatTransfert{}
	if f := transferts.m[serveur]; f != nil {
		for _, tr := range f.liste {
			l = append(l, tr.etatTransfert)
		}
	}
	return l
}

func (tr *transfertSFTP) avancer(n int64) {
	transferts.Lock()
	defer transferts.Unlock()
	tr.Fait += n
	if d := time.Since(tr.mesure); d >= 500*time.Millisecond {
		tr.Vitesse = int64(float64(tr.Fait-tr.faitMesure) / d.Seconds())
		tr.mesure, tr.faitMesure = time.Now(), tr.Fait
	}
}

// fichierT, coteT: one side of a copy, this computer's disk or the server's SFTP, with the same few operations.
type fichierT interface {
	io.ReadWriteSeeker
	io.Closer
}

type coteT interface {
	Lstat(p string) (fs.FileInfo, error)
	Open(p string) (fichierT, error)
	Ecrire(p string, tronquer bool) (fichierT, error)
	Mkdir(p string) error
	Remove(p string) error
	Chmod(p string, m fs.FileMode) error
	Remplacer(part, cible string) error
	Lister(p string) ([]fs.FileInfo, error)
	Join(p ...string) string
}

type coteLocal struct{}

func (coteLocal) Lstat(p string) (fs.FileInfo, error) { return os.Lstat(p) }
func (coteLocal) Open(p string) (fichierT, error)     { return os.Open(p) }
func (coteLocal) Ecrire(p string, tronquer bool) (fichierT, error) {
	fl := os.O_WRONLY | os.O_CREATE
	if tronquer {
		fl |= os.O_TRUNC
	}
	return os.OpenFile(p, fl, 0o600)
}
func (coteLocal) Mkdir(p string) error                { return os.Mkdir(p, 0o755) }
func (coteLocal) Remove(p string) error               { return os.Remove(p) }
func (coteLocal) Chmod(p string, m fs.FileMode) error { return os.Chmod(p, m) }
func (coteLocal) Remplacer(part, cible string) error  { return os.Rename(part, cible) }
func (coteLocal) Join(p ...string) string             { return filepath.Join(p...) }
func (coteLocal) Lister(p string) ([]fs.FileInfo, error) {
	l, err := os.ReadDir(p)
	infos := make([]fs.FileInfo, 0, len(l))
	for _, e := range l {
		if fi, e := e.Info(); e == nil {
			infos = append(infos, fi)
		}
	}
	return infos, err
}

type coteServeur struct{ c *sftp.Client }

func (s coteServeur) Lstat(p string) (fs.FileInfo, error) { return s.c.Lstat(p) }
func (s coteServeur) Open(p string) (fichierT, error)     { return s.c.Open(p) }
func (s coteServeur) Ecrire(p string, tronquer bool) (fichierT, error) {
	fl := os.O_WRONLY | os.O_CREATE
	if tronquer {
		fl |= os.O_TRUNC
	}
	return s.c.OpenFile(p, fl)
}
func (s coteServeur) Mkdir(p string) error                   { return s.c.Mkdir(p) }
func (s coteServeur) Remove(p string) error                  { return s.c.Remove(p) }
func (s coteServeur) Chmod(p string, m fs.FileMode) error    { return s.c.Chmod(p, m) }
func (s coteServeur) Remplacer(part, cible string) error     { return remplacerSFTP(s.c, part, cible) }
func (s coteServeur) Lister(p string) ([]fs.FileInfo, error) { return s.c.ReadDir(p) }
func (coteServeur) Join(p ...string) string                  { return path.Join(p...) }

// cotes: the source and destination sides of tr, on its terminal's current SFTP channel.
func (tr *transfertSFTP) cotes() (src, dst coteT, err error) {
	c, err := tr.t.clientSFTP()
	if err != nil {
		return nil, nil, err
	}
	if tr.Sens == "envoi" {
		return coteLocal{}, coteServeur{c}, nil
	}
	return coteServeur{c}, coteLocal{}, nil
}

// renouvelerSFTP: the next clientSFTP opens a new channel on the same connection (after a channel error).
func (t *sessionTerminal) renouvelerSFTP() {
	t.muSFTP.Lock()
	defer t.muSFTP.Unlock()
	if t.sftp != nil {
		t.sftp.Close()
		t.sftp = nil
	}
}

type elementT struct {
	rel     string // slash-separated, "" for the source itself
	dossier bool
	taille  int64
	mode    fs.FileMode
}

// parcourir: the source and, for a folder, everything inside it; links and special files are skipped (a link
// could lead out of the folder chosen, Athanor's data folder included).
func parcourir(c coteT, racine string, fi fs.FileInfo) ([]elementT, error) {
	l := []elementT{{dossier: fi.IsDir(), taille: fi.Size(), mode: fi.Mode()}}
	if !fi.IsDir() {
		return l, nil
	}
	l[0].taille = 0
	var rec func(rel string) error
	rec = func(rel string) error {
		infos, err := c.Lister(c.Join(racine, rel))
		if err != nil {
			return err
		}
		for _, x := range infos {
			if !x.IsDir() && !x.Mode().IsRegular() {
				continue
			}
			e := elementT{rel: path.Join(rel, x.Name()), dossier: x.IsDir(), mode: x.Mode()}
			if !e.dossier {
				e.taille = x.Size()
			}
			if l = append(l, e); len(l) > elementsTransfertMax {
				return errors.New(errTransfertTrop)
			}
			if e.dossier {
				if err := rec(e.rel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return l, rec("")
}

// nomLibre: "nom (1).ext", "nom (2).ext"…: the first name not taken in dossier (garder les deux).
func nomLibre(c coteT, dossier, nom string) string {
	ext := path.Ext(nom)
	if ext == nom { // ".bashrc"
		ext = ""
	}
	base := strings.TrimSuffix(nom, ext)
	for i := 1; i < 1000; i++ {
		n := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := c.Lstat(c.Join(dossier, n)); errors.Is(err, fs.ErrNotExist) {
			return n
		}
	}
	return fmt.Sprintf("%s (%s)%s", base, aleatoire(4), ext)
}

// apiSFTPTransferts: GET /api/serveurs/{id}/sftp/{session}/transferts: the server's transfers, the owner's and
// the assistant's, oldest first.
func (s *Serveur) apiSFTPTransferts(w http.ResponseWriter, r *http.Request) {
	if !memeOrigine(r) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return
	}
	if t, raison := terminalDe(r); t == nil {
		s.terminalFerme(w, raison)
		return
	}
	s.ecrireJSON(w, map[string]any{"transferts": listeTransferts(r.PathValue("id"))})
}

// apiSFTPTransferer: POST /api/serveurs/{id}/sftp/{session}/transferts {sens, sources, dossier}: one transfer per
// source (a file or a folder), queued. envoi: sources on this computer, dossier on the server; telechargement:
// the other way round.
func (s *Serveur) apiSFTPTransferer(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Sens    string   `json:"sens"`
		Sources []string `json:"sources"`
		Dossier string   `json:"dossier"`
	}
	if lireCorps(r, &d) != nil || len(d.Sources) == 0 || len(d.Sources) > sourcesTransfertMax {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	t, c, ok := s.sftpDe(w, r)
	if !ok {
		return
	}
	var src, dst coteT
	var cheminSrc, cheminDst func(string) string
	switch d.Sens {
	case "envoi":
		src, dst, cheminSrc, cheminDst = coteLocal{}, coteServeur{c}, cheminLocal, cheminSFTP
	case "telechargement":
		src, dst, cheminSrc, cheminDst = coteServeur{c}, coteLocal{}, cheminSFTP, cheminLocal
	default:
		s.erreur(w, http.StatusBadRequest, errTransfertSens)
		return
	}
	proteges := dossiersProteges()
	dossier := cheminDst(d.Dossier)
	if dossier == "" {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin)
		return
	}
	if d.Sens == "telechargement" && dansProtege(dossier, proteges) {
		s.erreur(w, http.StatusForbidden, errLocalProtege)
		return
	}
	if fi, err := dst.Lstat(dossier); err != nil || !fi.IsDir() {
		s.erreur(w, http.StatusBadRequest, errTransfertDossier)
		return
	}
	sources := make([]string, 0, len(d.Sources))
	for _, x := range d.Sources {
		p := cheminSrc(x)
		if p == "" || p == "/" || p == filepath.VolumeName(p)+string(filepath.Separator) {
			s.erreur(w, http.StatusBadRequest, errSFTPChemin)
			return
		}
		if d.Sens == "envoi" && protegeLocal(p, proteges) {
			s.erreur(w, http.StatusForbidden, errLocalProtege)
			return
		}
		if _, err := src.Lstat(p); err != nil {
			if d.Sens == "envoi" {
				s.erreurLocale(w, err)
			} else {
				s.erreurSFTP(w, err)
			}
			return
		}
		sources = append(sources, p)
	}
	ids := []string{}
	for _, p := range sources {
		tr := &transfertSFTP{serveur: t.serveur, dossierDst: dossier, t: t, decision: make(chan string, 1)}
		nom := filepath.Base(p)
		if d.Sens == "telechargement" {
			nom = path.Base(p)
		}
		tr.etatTransfert = etatTransfert{ID: aleatoire(8), Sens: d.Sens, Par: "vous", Nom: nom, Source: p, Etat: "attente", Debut: time.Now().Unix()}
		ids = append(ids, tr.ID)
		s.lancerTransfert(tr, ajouterTransfert(tr))
	}
	s.ecrireJSON(w, map[string]any{"transferts": ids})
}

// apiSFTPTransfert: POST /api/serveurs/{id}/sftp/{session}/transferts/{transfert} {action, tous}: annuler; the
// conflict's answer remplacer, garder or passer (tous: also for the next conflicts of the queue); reprendre a
// transfer stopped by an error (on this session's connection, from the bytes already written); retirer a
// finished one from the list.
func (s *Serveur) apiSFTPTransfert(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Action string `json:"action"`
		Tous   bool   `json:"tous"`
	}
	if lireCorps(r, &a) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !memeOrigine(r) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return
	}
	t, raison := terminalDe(r)
	if t == nil {
		s.terminalFerme(w, raison)
		return
	}
	transferts.Lock()
	f := fileDe(t.serveur)
	i := slices.IndexFunc(f.liste, func(x *transfertSFTP) bool { return x.ID == r.PathValue("transfert") })
	if i < 0 {
		transferts.Unlock()
		s.erreur(w, http.StatusNotFound, errTransfertInconnu)
		return
	}
	tr := f.liste[i]
	ok := true
	relancer := false
	switch a.Action {
	case "annuler":
		ok = actif(tr.Etat) && tr.annuler != nil
		if ok {
			tr.annuler()
		}
	case "remplacer", "garder", "passer":
		ok = tr.Etat == "conflit"
		if ok {
			select { // a second click finds the answer already given
			case tr.decision <- a.Action:
			default:
			}
			if a.Tous {
				f.politique = a.Action
				for _, x := range f.liste {
					if x != tr && x.Etat == "conflit" && len(x.decision) == 0 {
						x.decision <- a.Action
					}
				}
			}
		}
	case "reprendre":
		ok = tr.Etat == "erreur" && tr.Par == "vous"
		if ok {
			tr.t, tr.Etat, tr.Erreur, relancer = t, "attente", "", true
		}
	case "retirer":
		ok = !actif(tr.Etat)
		if ok {
			f.liste = slices.Delete(f.liste, i, i+1)
		}
	default:
		ok = false
	}
	transferts.Unlock()
	if !ok {
		s.erreur(w, http.StatusConflict, errTransfertAction)
		return
	}
	if relancer {
		s.lancerTransfert(tr, f)
	}
	s.ecrireJSON(w, map[string]any{"ok": true})
}

// lancerTransfert: tr runs in the background, cancelled by Annuler or by its terminal closing.
func (s *Serveur) lancerTransfert(tr *transfertSFTP, f *fileTransferts) {
	ctx, annuler := context.WithCancel(context.Background())
	transferts.Lock()
	tr.annuler = annuler
	t := tr.t
	transferts.Unlock()
	go func() {
		defer annuler()
		var coupe atomic.Bool
		go func() {
			select {
			case <-t.fini:
				coupe.Store(true)
				annuler()
			case <-ctx.Done():
			}
		}()
		err := tr.executer(ctx, f)
		annule := ctx.Err() != nil
		transferts.Lock()
		switch {
		case annule && coupe.Load():
			tr.Etat, tr.Erreur = "erreur", errTransfertCoupe
		case annule:
			tr.Etat = "annule"
		case err != nil:
			tr.Etat, tr.Erreur = "erreur", err.Error()
		case tr.Etat != "passe":
			tr.Etat = "termine"
		}
		tr.Vitesse, tr.Conflit = 0, ""
		etat, part, sens := tr.Etat, tr.part, tr.Sens
		res := fmt.Sprintf("%s -> %s (%d bytes, %d files): %s", tr.Source, tr.Cible, tr.Fait, tr.Fichiers, etat)
		if !slices.ContainsFunc(f.liste, func(x *transfertSFTP) bool { return actif(x.Etat) }) {
			f.politique = ""
		}
		transferts.Unlock()
		if etat == "annule" && part != "" { // a cancelled transfer leaves nothing half written
			if _, dst, e := tr.cotes(); e == nil {
				_ = dst.Remove(part)
			}
		}
		action := "sftp.upload"
		if sens == "telechargement" {
			action = "sftp.download"
		}
		if err != nil && etat == "erreur" {
			res += ": " + couper(err.Error(), 200)
		}
		t.auditSFTP(action, res)
	}()
}

// executer: waits for a place in the queue, settles a name already taken, then copies tr's files.
func (tr *transfertSFTP) executer(ctx context.Context, f *fileTransferts) error {
	prendre := func() error {
		select {
		case f.sem <- struct{}{}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := prendre(); err != nil {
		return err
	}
	pris := true
	defer func() {
		if pris {
			<-f.sem
		}
	}()
	transferts.Lock()
	tr.Etat, tr.mesure, tr.faitMesure = "en_cours", time.Now(), tr.Fait
	transferts.Unlock()
	src, dst, err := tr.cotes()
	if err != nil {
		return errors.New(errSFTPAbsent)
	}
	fi, err := src.Lstat(tr.Source)
	if err != nil {
		return err
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		return errors.New(errSFTPLien)
	}
	nom := tr.Nom
	cible := dst.Join(tr.dossierDst, nom)
	if tr.Cible != "" { // Reprendre: the destination settled the first time, partly written by this transfer
		cible = tr.Cible
	} else if avant, err := dst.Lstat(cible); err == nil {
		transferts.Lock()
		choix := cmp.Or(tr.choix, f.politique)
		if choix == "" {
			tr.Etat, tr.Conflit = "conflit", nom
		}
		transferts.Unlock()
		if choix == "" { // the place goes to another transfer while the owner answers
			<-f.sem
			pris = false
			select {
			case choix = <-tr.decision:
			case <-ctx.Done():
				return ctx.Err()
			}
			if err := prendre(); err != nil {
				return err
			}
			pris = true
		}
		transferts.Lock()
		tr.choix, tr.Etat, tr.Conflit = choix, "en_cours", ""
		transferts.Unlock()
		switch {
		case choix == "passer":
			transferts.Lock()
			tr.Etat = "passe"
			transferts.Unlock()
			return nil
		case choix == "garder":
			cible = dst.Join(tr.dossierDst, nomLibre(dst, tr.dossierDst, nom))
		case avant.IsDir() != fi.IsDir() || avant.Mode()&fs.ModeSymlink != 0:
			return errors.New(errTransfertType)
		}
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	elements, err := parcourir(src, tr.Source, fi)
	if err != nil {
		return err
	}
	var total int64
	nb := 0
	for _, e := range elements {
		total += e.taille
		if !e.dossier {
			nb++
		}
	}
	transferts.Lock()
	tr.Cible, tr.Dossier, tr.Total, tr.FichiersTotal = cible, fi.IsDir(), total, nb
	tr.Fait, tr.faitAvant, tr.Fichiers = 0, 0, 0
	transferts.Unlock()
	for _, e := range elements {
		de, vers := tr.Source, cible
		if e.rel != "" {
			de, vers = src.Join(tr.Source, e.rel), dst.Join(cible, e.rel)
		}
		if e.dossier {
			if err := dst.Mkdir(vers); err != nil {
				if st, e2 := dst.Lstat(vers); e2 != nil || !st.IsDir() {
					return err
				}
			}
			continue
		}
		if err := tr.copier(ctx, de, vers, e); err != nil {
			return err
		}
		transferts.Lock()
		tr.faitAvant += e.taille
		tr.Fait, tr.part = tr.faitAvant, ""
		tr.Fichiers++
		transferts.Unlock()
	}
	return nil
}

// copier: one file through "<vers>.athanor-part" then put in place, so that a cut copy never leaves half a file
// under the real name; resumed up to reprisesTransfertMax times from the bytes already in the part file (a new
// SFTP channel each time); an existing file replaced keeps its mode, a new one takes the source's.
func (tr *transfertSFTP) copier(ctx context.Context, de, vers string, e elementT) error {
	part := vers + ".athanor-part"
	transferts.Lock()
	reprise := tr.part == part // Reprendre after an error: the part file is this one's
	tr.part = part
	transferts.Unlock()
	for essai := 0; ; essai++ {
		err := tr.copierUneFois(ctx, de, part, e.taille, reprise || essai > 0)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if essai >= reprisesTransfertMax || errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
			return err
		}
		transferts.Lock()
		tr.Reprises++
		transferts.Unlock()
		tr.t.renouvelerSFTP()
		select {
		case <-time.After(pauseRepriseTransfert * time.Duration(essai+1)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	_, dst, err := tr.cotes()
	if err != nil {
		return err
	}
	mode := e.mode.Perm()
	if avant, err := dst.Lstat(vers); err == nil {
		mode = avant.Mode().Perm()
	}
	if err := dst.Chmod(part, mode); err != nil && tr.Sens == "envoi" {
		return err
	}
	return dst.Remplacer(part, vers)
}

// copierUneFois: de (taille bytes when listed) into part, from the end of part when reprendre (else from the start).
func (tr *transfertSFTP) copierUneFois(ctx context.Context, de, part string, taille int64, reprendre bool) error {
	src, dst, err := tr.cotes()
	if err != nil {
		return err
	}
	in, err := src.Open(de)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dst.Ecrire(part, !reprendre)
	if err != nil {
		return err
	}
	debut, err := out.Seek(0, io.SeekEnd)
	if err == nil {
		_, err = in.Seek(debut, io.SeekStart)
	}
	if err != nil {
		out.Close()
		return err
	}
	transferts.Lock()
	tr.Fait = tr.faitAvant + debut
	transferts.Unlock()
	// LimitReader: no read asks past the size listed (some SFTP servers fail a read across the end); it also
	// hides ReaderFrom and WriterTo so that every block goes through the counter
	_, err = io.CopyBuffer(compteurTransfert{ctx, out, tr}, io.LimitReader(in, max(taille-debut, 0)), make([]byte, tamponTransfert))
	if e := out.Close(); err == nil {
		err = e
	}
	return err
}

type compteurTransfert struct {
	ctx context.Context
	w   io.Writer
	tr  *transfertSFTP
}

func (c compteurTransfert) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if fauteTransfert != nil {
		if err := fauteTransfert(c.tr); err != nil {
			return 0, err
		}
	}
	n, err := c.w.Write(p)
	c.tr.avancer(int64(n))
	return n, err
}

// noterTransfertAssistant: the assistant's sftp_envoyer or sftp_recuperer call shown in its server's queue
// ("par l'assistant"); the returned function records its end.
func noterTransfertAssistant(serveur, outil string, args []byte) func(ResultatOutil, error) {
	var a argsOutilServeur
	if lireArguments(args, &a) != nil {
		return func(ResultatOutil, error) {}
	}
	tr := &transfertSFTP{serveur: serveur}
	tr.etatTransfert = etatTransfert{ID: aleatoire(8), Sens: "envoi", Par: "assistant", Nom: path.Base(a.Distant),
		Source: "fichiers/" + filepath.ToSlash(a.Local), Cible: a.Distant, Etat: "en_cours", FichiersTotal: 1, Debut: time.Now().Unix()}
	if outil == "sftp_recuperer" {
		tr.Sens, tr.Source, tr.Cible = "telechargement", a.Distant, "fichiers/"+filepath.ToSlash(a.Local)
	}
	ajouterTransfert(tr)
	return func(r ResultatOutil, err error) {
		var n int64
		fmt.Sscanf(r.Sortie, "%d bytes", &n)
		transferts.Lock()
		defer transferts.Unlock()
		tr.Etat, tr.Fait, tr.Total = "termine", n, n
		if err != nil || r.Code != 0 {
			tr.Etat, tr.Erreur = "erreur", "The assistant's transfer failed: see its answer in the chat"
		} else {
			tr.Fichiers = 1
		}
	}
}
