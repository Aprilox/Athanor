package main

// The hub's audit log (decision 4): JSON Lines in hub/journal/AAAA-MM.jsonl, one line per bus message,
// MCP call, approval decision, grant, native-mode "force" and emergency stop. Each line carries its rank n
// and prev, the SHA-256 of the line before it (the first line of a month chains the last one of the month
// before); hub/journal/fin holds the rank and hash of the last line. verifierAudit finds the first line
// modified, removed or inserted. Secrets are masked before writing: a line never holds a value.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LigneAudit: one line of the audit log. ID ties a request to its answer.
type LigneAudit struct {
	T        string `json:"t"`
	N        int    `json:"n"`
	ID       string `json:"id"`
	Type     string `json:"type"` // demande | reponse | evenement
	De       string `json:"de,omitempty"`
	Vers     string `json:"vers,omitempty"`
	Mission  string `json:"mission,omitempty"`
	Projet   string `json:"projet,omitempty"`
	Env      string `json:"env,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Action   string `json:"action,omitempty"`
	Resultat string `json:"resultat,omitempty"`
	Prev     string `json:"prev"`
}

// Audit: the log in Dossier. Maintenant (nil: time.Now) and Secrets (values to mask) are for the writer.
type Audit struct {
	Dossier    string
	Maintenant func() time.Time
	Secrets    func() []string
}

// VerifAudit: the result of a check. When !OK, Fichier/Ligne (from 1) is the first line found broken.
type VerifAudit struct {
	OK      bool   `json:"ok"`
	Lignes  int    `json:"lignes"`
	Fichier string `json:"fichier,omitempty"`
	Ligne   int    `json:"ligne,omitempty"`
	Raison  string `json:"raison,omitempty"` // modifiee | retiree | inseree | illisible
}

// shortcut: one writer at a time in this process only; another process (CLI) writing at the same instant
// could fork the chain, upgrade to a file lock once a sub-command writes to the log.
var auditMu sync.Mutex

const finAudit = "fin"

var reMoisAudit = regexp.MustCompile(`^\d{4}-\d{2}\.jsonl$`)

// motifsCles: key formats masked even when the vault does not know them (unanchored prefixesJeton and a
// few more; the last one keeps the name and masks what follows "password=").
var motifsCles = []*regexp.Regexp{
	regexp.MustCompile(`(?:ghp_|gho_|ghs_|ghu_|github_pat_)[A-Za-z0-9_]{16,}`),
	regexp.MustCompile(`sk-(?:ant-)?[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(?:sk|rk)_(?:test|live)_[A-Za-z0-9]{8,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z ]*PRIVATE KEY-----|$)`),
}

var reAffectationSecrete = regexp.MustCompile(`(?i)((?:password|passwd|secret|token|api[_-]?key)\s*[=:]\s*)[^\s,;"']+`)

const masqueAudit = "***"

func dossierJournalAudit() string { return filepath.Join(dossierAthanor(), "hub", "journal") }

func hacherLigne(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// masquerTexte takes the known values (longest first, 6 characters at least: shorter ones would hide
// ordinary words) and the key formats out of s.
func masquerTexte(s string, secrets []string) string {
	for _, v := range secrets {
		if len(v) >= 6 {
			s = strings.ReplaceAll(s, v, masqueAudit)
		}
	}
	for _, re := range motifsCles {
		s = re.ReplaceAllString(s, masqueAudit)
	}
	return reAffectationSecrete.ReplaceAllString(s, "${1}"+masqueAudit)
}

// mois: the month files, oldest first.
func (a Audit) mois() ([]string, error) {
	l, err := os.ReadDir(a.Dossier)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m []string
	for _, e := range l {
		if !e.IsDir() && reMoisAudit.MatchString(e.Name()) {
			m = append(m, e.Name())
		}
	}
	sort.Strings(m)
	return m, nil
}

// derniereLigne: the last line of a file (nil when empty), reading its tail only.
func derniereLigne(chemin string) ([]byte, error) {
	f, err := os.Open(chemin)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	for taille := int64(64 << 10); ; taille *= 4 {
		debut := max(fi.Size()-taille, 0)
		b := make([]byte, fi.Size()-debut)
		if _, err := f.ReadAt(b, debut); err != nil && err != io.EOF {
			return nil, err
		}
		b = bytes.TrimRight(b, "\n")
		if k := bytes.LastIndexByte(b, '\n'); k >= 0 {
			return b[k+1:], nil
		}
		if debut == 0 {
			if len(b) == 0 {
				return nil, nil
			}
			return b, nil
		}
	}
}

// Ecrire appends l (time, rank, chain and, when empty, ID set here) after masking its texts.
func (a Audit) Ecrire(l LigneAudit) error {
	auditMu.Lock()
	defer auditMu.Unlock()
	if err := os.MkdirAll(a.Dossier, 0o700); err != nil {
		return err
	}
	maintenant := time.Now
	if a.Maintenant != nil {
		maintenant = a.Maintenant
	}
	t := maintenant().UTC()
	cible := t.Format("2006-01") + ".jsonl"
	m, err := a.mois()
	if err != nil {
		return err
	}
	l.N, l.Prev = 1, ""
	for i := len(m) - 1; i >= 0; i-- {
		prec, err := derniereLigne(filepath.Join(a.Dossier, m[i]))
		if err != nil {
			return err
		}
		if prec == nil {
			continue
		}
		var p LigneAudit
		_ = json.Unmarshal(prec, &p) // an unreadable last line is still chained: the check points at it
		l.N, l.Prev = p.N+1, hacherLigne(prec)
		cible = max(cible, m[i]) // a clock set back never writes before the end of the chain
		break
	}
	l.T = t.Format(time.RFC3339)
	if l.ID == "" {
		l.ID = aleatoire(8)
	}
	var secrets []string
	if a.Secrets != nil {
		secrets = a.Secrets()
		slices.SortFunc(secrets, func(x, y string) int { return len(y) - len(x) })
	}
	for _, p := range []*string{&l.Type, &l.De, &l.Vers, &l.Mission, &l.Projet, &l.Env, &l.Agent, &l.Action, &l.Resultat} {
		*p = masquerTexte(*p, secrets)
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(a.Dossier, cible), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(a.Dossier, finAudit), fmt.Appendf(nil, "%d %s\n", l.N, hacherLigne(b)), 0o600)
}

// Verifier walks the whole chain, months in order, and the end mark.
func (a Audit) Verifier() (VerifAudit, error) {
	auditMu.Lock()
	defer auditMu.Unlock()
	m, err := a.mois()
	if err != nil {
		return VerifAudit{}, err
	}
	var v VerifAudit
	casse := func(f string, n int, raison string) (VerifAudit, error) {
		v.Fichier, v.Ligne, v.Raison = f, n, raison
		return v, nil
	}
	var prec, avant []byte // the last line read and the one before it
	precN, precF, precL := 0, "", 0
	for _, nom := range m {
		b, err := os.ReadFile(filepath.Join(a.Dossier, nom))
		if err != nil {
			return VerifAudit{}, err
		}
		if b = bytes.TrimRight(b, "\n"); len(b) == 0 {
			continue
		}
		for i, ligne := range bytes.Split(b, []byte("\n")) {
			v.Lignes++
			var l LigneAudit
			if json.Unmarshal(ligne, &l) != nil {
				return casse(nom, i+1, "illisible")
			}
			switch {
			case prec == nil && l.N != 1:
				return casse(nom, i+1, "retiree") // the first lines are gone
			case prec == nil && l.Prev != "":
				return casse(nom, i+1, "modifiee")
			case prec != nil && l.Prev != hacherLigne(prec):
				switch {
				case avant != nil && l.Prev == hacherLigne(avant):
					return casse(precF, precL, "inseree")
				case l.N == precN+1:
					return casse(precF, precL, "modifiee")
				case l.N > precN+1:
					return casse(nom, i+1, "retiree") // a line is missing before this one
				default:
					return casse(nom, i+1, "inseree")
				}
			case prec != nil && l.N != precN+1:
				return casse(nom, i+1, "modifiee")
			}
			avant, prec, precN, precF, precL = prec, ligne, l.N, nom, i+1
		}
	}
	fin, err := os.ReadFile(filepath.Join(a.Dossier, finAudit))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return VerifAudit{}, err
	}
	if err == nil {
		var n int
		var h string
		_, _ = fmt.Sscanf(string(fin), "%d %s", &n, &h)
		switch {
		case n > precN:
			if prec == nil {
				return casse("", 1, "retiree")
			}
			return casse(precF, precL+1, "retiree") // the last lines are gone
		case n < precN:
			return casse(precF, precL, "inseree")
		case h != hacherLigne(prec):
			return casse(precF, precL, "modifiee")
		}
	}
	v.OK = true
	return v, nil
}

// secretsOuverts: the clear values of the vault's secrets readable now, masked out of the audit log.
func (s *Serveur) secretsOuverts() []string {
	if s.coffre == nil {
		return nil
	}
	var v []string
	for _, e := range s.coffre.Liste() {
		if e.Famille == "secret" {
			if x, err := s.coffre.Valeur(e.ID); err == nil && x != "" {
				v = append(v, x)
			}
		}
	}
	return v
}

// masquerSecrets: an agent's free text with the vault's readable values masked, as in the audit log.
func (s *Serveur) masquerSecrets(t string) string {
	v := s.secretsOuverts()
	slices.SortFunc(v, func(x, y string) int { return len(y) - len(x) })
	return masquerTexte(t, v)
}

// ecrireAudit: a line in the hub's audit log (a write failure is reported, never blocks the action).
func (s *Serveur) ecrireAudit(l LigneAudit) {
	if err := (Audit{Dossier: dossierJournalAudit(), Secrets: s.secretsOuverts}).Ecrire(l); err != nil {
		fmt.Fprintln(os.Stderr, "audit log:", err)
	}
}

func (s *Serveur) verifierAudit() (VerifAudit, error) {
	return Audit{Dossier: dossierJournalAudit()}.Verifier()
}

// auditAcces: a grant given or taken (who, project, entry and variable, never a value).
func (s *Serveur) auditAcces(de, projet, action string, e EntreeCoffre, variable, env string) {
	r := fmt.Sprintf("%s [%s, %s] → %s, %s", e.Nom, e.ID, e.Portee, variable, env)
	if variable == "" {
		r = fmt.Sprintf("%s [%s, %s], %s", e.Nom, e.ID, e.Portee, env)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "hub", Projet: projet, Action: action, Resultat: r})
}

// deQui: who acts, for the log: Athanor's window or a paired device.
func deQui(r *http.Request) string {
	if id := appareilDe(r); id != "" {
		return "appareil:" + id
	}
	return "fenetre"
}

// entreeCoffre: the vault entry with this id (metadata only), or one with just its id once deleted.
func (s *Serveur) entreeCoffre(id string) EntreeCoffre {
	for _, e := range s.coffre.Liste() {
		if e.ID == id {
			return e
		}
	}
	return EntreeCoffre{ID: id}
}

// FiltreAudit: what the #audit page asks for (empty: everything); Avant (a rank) pages back.
type FiltreAudit struct {
	Mois, Projet, Type, Q string
	Avant                 int
}

// PageAudit: one page of the log, newest first; Suite is the rank to pass as avant for the next page (0: none).
type PageAudit struct {
	Lignes  []LigneAudit `json:"lignes"`
	Mois    []string     `json:"mois"`    // the months there are, newest first ("2026-11")
	Projets []string     `json:"projets"` // the projects named in the months read
	Suite   int          `json:"suite,omitempty"`
}

const parPageAudit = 100

var typesAudit = []string{"demande", "reponse", "evenement"}

// Lire: the lines matching f, newest first, parPageAudit at most. An unreadable line is left out (the check
// points at it). shortcut: reads whole month files at each call, upgrade to reading backwards past a few MB.
func (a Audit) Lire(f FiltreAudit) (PageAudit, error) {
	auditMu.Lock()
	defer auditMu.Unlock()
	m, err := a.mois()
	if err != nil {
		return PageAudit{}, err
	}
	p := PageAudit{Lignes: []LigneAudit{}, Mois: []string{}, Projets: []string{}}
	vus, q := map[string]bool{}, strings.ToLower(f.Q)
	for i := len(m) - 1; i >= 0; i-- {
		mois := strings.TrimSuffix(m[i], ".jsonl")
		p.Mois = append(p.Mois, mois)
		if f.Mois != "" && f.Mois != mois {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.Dossier, m[i]))
		if err != nil {
			return PageAudit{}, err
		}
		lignes := bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n"))
		for j := len(lignes) - 1; j >= 0; j-- {
			var l LigneAudit
			if json.Unmarshal(lignes[j], &l) != nil {
				continue
			}
			if l.Projet != "" && !vus[l.Projet] {
				vus[l.Projet] = true
				p.Projets = append(p.Projets, l.Projet)
			}
			texte := strings.ToLower(strings.Join([]string{l.De, l.Vers, l.Projet, l.Mission, l.Agent, l.Action, l.Resultat}, " "))
			switch {
			case f.Avant > 0 && l.N >= f.Avant, f.Projet != "" && l.Projet != f.Projet, f.Type != "" && l.Type != f.Type,
				q != "" && !strings.Contains(texte, q):
			case len(p.Lignes) == parPageAudit:
				p.Suite = p.Lignes[parPageAudit-1].N
			default:
				p.Lignes = append(p.Lignes, l)
			}
		}
	}
	sort.Strings(p.Projets)
	return p, nil
}

// apiAudit: GET /api/hub/audit?mois=&projet=&type=&q=&avant= (reading, also from a paired device).
func (s *Serveur) apiAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := FiltreAudit{Mois: q.Get("mois"), Projet: q.Get("projet"), Type: q.Get("type"), Q: q.Get("q")}
	avant, err := strconv.Atoi(q.Get("avant"))
	if q.Get("avant") != "" && (err != nil || avant < 1) || f.Mois != "" && !reMoisAudit.MatchString(f.Mois+".jsonl") ||
		f.Type != "" && !slices.Contains(typesAudit, f.Type) || len(f.Q) > 200 || len(f.Projet) > 200 {
		s.erreur(w, http.StatusBadRequest, "Invalid audit log filter")
		return
	}
	f.Avant = avant
	p, err := Audit{Dossier: dossierJournalAudit()}.Lire(f)
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, p)
}

// apiAuditVerifier: POST /api/hub/audit/verifier, the whole chain recomputed (also from a paired device).
func (s *Serveur) apiAuditVerifier(w http.ResponseWriter, r *http.Request) {
	v, err := s.verifierAudit()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, v)
}
