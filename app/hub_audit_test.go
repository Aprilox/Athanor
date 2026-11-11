package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// lignesAudit: every line of the log in dossier, months in order.
func lignesAudit(t *testing.T, dossier string) []LigneAudit {
	t.Helper()
	m, err := Audit{Dossier: dossier}.mois()
	if err != nil {
		t.Fatal(err)
	}
	var l []LigneAudit
	for _, nom := range m {
		b, err := os.ReadFile(filepath.Join(dossier, nom))
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n")) {
			var la LigneAudit
			if err := json.Unmarshal(x, &la); err != nil {
				t.Fatalf("line %q: %v", x, err)
			}
			l = append(l, la)
		}
	}
	return l
}

func horloge(t *time.Time) func() time.Time { return func() time.Time { return *t } }

// TestAuditChaine: lines chained by the hash of the one before, ranks from 1, the vault's values and key
// formats masked, the file 0600.
func TestAuditChaine(t *testing.T) {
	d := t.TempDir()
	temoin := "TEMOIN-" + aleatoire(12)
	stripe := "sk_" + "live_abcdefgh1234" // split: a literal key would trip the secret scan of the history
	a := Audit{Dossier: d, Secrets: func() []string { return []string{temoin, "court"} }}
	for i := range 4 {
		if err := a.Ecrire(LigneAudit{Type: "evenement", De: "fenetre", Vers: "hub", Projet: "demo", Action: "test",
			Resultat: fmt.Sprintf("%d %s %s ghp_0123456789abcdefABCD password=hunter2 court", i, temoin, stripe)}); err != nil {
			t.Fatal(err)
		}
	}
	nom := filepath.Join(d, time.Now().UTC().Format("2006-01")+".jsonl")
	b, err := os.ReadFile(nom)
	if err != nil {
		t.Fatal(err)
	}
	for _, fuite := range []string{temoin, stripe, "ghp_0123456789", "hunter2"} {
		if bytes.Contains(b, []byte(fuite)) {
			t.Fatalf("%q not masked: %s", fuite, b)
		}
	}
	if !bytes.Contains(b, []byte("password=***")) || !bytes.Contains(b, []byte("court")) {
		t.Fatalf("masking too wide or missing: %s", b)
	}
	lignes := bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n"))
	l := lignesAudit(t, d)
	if len(l) != 4 || l[0].Prev != "" || l[0].N != 1 || l[0].ID == "" || l[0].T == "" {
		t.Fatalf("first line: %+v", l)
	}
	for i := 1; i < 4; i++ {
		if l[i].N != i+1 || l[i].Prev != hacherLigne(lignes[i-1]) {
			t.Fatalf("line %d not chained: %+v", i+1, l[i])
		}
	}
	if v, err := a.Verifier(); err != nil || !v.OK || v.Lignes != 4 {
		t.Fatalf("check: %+v %v", v, err)
	}
	if fi, err := os.Stat(nom); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("mode: %v %v", fi, err)
	}
}

// TestAuditFalsifications: a line modified, removed or inserted (anywhere, the last one included) is the
// line the check gives.
func TestAuditFalsifications(t *testing.T) {
	ecrire := func(t *testing.T) (Audit, string, [][]byte) {
		d := t.TempDir()
		a := Audit{Dossier: d}
		for i := range 6 {
			if err := a.Ecrire(LigneAudit{Type: "evenement", Action: "test", Resultat: fmt.Sprint("ligne ", i+1)}); err != nil {
				t.Fatal(err)
			}
		}
		nom := filepath.Join(d, time.Now().UTC().Format("2006-01")+".jsonl")
		b, _ := os.ReadFile(nom)
		return a, nom, bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n"))
	}
	poser := func(t *testing.T, nom string, l [][]byte) {
		if err := os.WriteFile(nom, append(bytes.Join(l, []byte("\n")), '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cas := []struct {
		nom    string
		change func(l [][]byte) [][]byte
		ligne  int
		raison string
	}{
		{"modifiee", func(l [][]byte) [][]byte {
			l[2] = bytes.Replace(l[2], []byte("ligne 3"), []byte("ligne X"), 1)
			return l
		}, 3, "modifiee"},
		{"derniere modifiee", func(l [][]byte) [][]byte {
			l[5] = bytes.Replace(l[5], []byte("ligne 6"), []byte("ligne X"), 1)
			return l
		}, 6, "modifiee"},
		{"retiree", func(l [][]byte) [][]byte { return append(l[:2:2], l[3:]...) }, 3, "retiree"},
		{"premiere retiree", func(l [][]byte) [][]byte { return l[1:] }, 1, "retiree"},
		{"derniere retiree", func(l [][]byte) [][]byte { return l[:5] }, 6, "retiree"},
		{"inseree (copie)", func(l [][]byte) [][]byte {
			return append(append(l[:3:3], l[1]), l[3:]...)
		}, 4, "inseree"},
		{"inseree (chaînée)", func(l [][]byte) [][]byte {
			var p LigneAudit
			_ = json.Unmarshal(l[2], &p)
			x, _ := json.Marshal(LigneAudit{T: p.T, N: p.N + 1, ID: "faux", Type: "evenement", Action: "ajout", Prev: hacherLigne(l[2])})
			return append(append(l[:3:3], x), l[3:]...)
		}, 4, "inseree"},
		{"illisible", func(l [][]byte) [][]byte { l[1] = []byte("{pas du json"); return l }, 2, "illisible"},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			a, nom, l := ecrire(t)
			poser(t, nom, c.change(l))
			v, err := a.Verifier()
			if err != nil || v.OK || v.Ligne != c.ligne || v.Raison != c.raison || v.Fichier != filepath.Base(nom) {
				t.Fatalf("got %+v %v, want line %d %s", v, err, c.ligne, c.raison)
			}
		})
	}
}

// TestAuditMois: the first line of a month chains the last one of the month before (a month without any
// line skipped); a clock set back still writes at the end of the chain.
func TestAuditMois(t *testing.T) {
	d := t.TempDir()
	h := time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC)
	a := Audit{Dossier: d, Maintenant: horloge(&h)}
	ecrire := func() {
		if err := a.Ecrire(LigneAudit{Type: "evenement", Action: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	ecrire()
	ecrire()
	h = time.Date(2026, 11, 1, 0, 1, 0, 0, time.UTC)
	ecrire()
	h = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) // set back
	ecrire()
	sept, _ := os.ReadFile(filepath.Join(d, "2026-09.jsonl"))
	nov, _ := os.ReadFile(filepath.Join(d, "2026-11.jsonl"))
	if _, err := os.Stat(filepath.Join(d, "2026-10.jsonl")); err == nil {
		t.Fatal("a line written before the end of the chain")
	}
	ls := bytes.Split(bytes.TrimRight(sept, "\n"), []byte("\n"))
	var premiere LigneAudit
	_ = json.Unmarshal(bytes.SplitN(nov, []byte("\n"), 2)[0], &premiere)
	if len(ls) != 2 || premiere.N != 3 || premiere.Prev != hacherLigne(ls[1]) || bytes.Count(nov, []byte("\n")) != 2 {
		t.Fatalf("months not chained: %s / %s", sept, nov)
	}
	if v, err := a.Verifier(); err != nil || !v.OK || v.Lignes != 4 {
		t.Fatalf("check: %+v %v", v, err)
	}
	// the last line of September changed: found across the month change
	if err := os.WriteFile(filepath.Join(d, "2026-09.jsonl"), bytes.Replace(sept, []byte(`"n":2`), []byte(`"n":2,"x":1`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, _ := a.Verifier(); v.OK || v.Fichier != "2026-09.jsonl" || v.Ligne != 2 || v.Raison != "modifiee" {
		t.Fatalf("broken September: %+v", v)
	}
}

// TestAuditConcurrent: 20 goroutines writing at once never mix two lines, and the chain stays whole.
func TestAuditConcurrent(t *testing.T) {
	d := t.TempDir()
	a := Audit{Dossier: d}
	var g sync.WaitGroup
	for i := range 20 {
		g.Add(1)
		go func() {
			defer g.Done()
			for j := range 10 {
				if err := a.Ecrire(LigneAudit{Type: "demande", De: fmt.Sprint("g", i), Action: strings.Repeat("x", 200), Resultat: fmt.Sprint(j)}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	g.Wait()
	l := lignesAudit(t, d)
	if len(l) != 200 {
		t.Fatalf("%d lines", len(l))
	}
	for i, x := range l {
		if x.N != i+1 || len(x.Action) != 200 {
			t.Fatalf("line %d: %+v", i+1, x)
		}
	}
	if v, err := a.Verifier(); err != nil || !v.OK || v.Lignes != 200 {
		t.Fatalf("check: %+v %v", v, err)
	}
}

// TestAuditServeurMasque: the server's log masks the values of the vault readable now.
func TestAuditServeurMasque(t *testing.T) {
	s := serveurCoffreTest(t)
	temoin := "TEMOIN-" + aleatoire(12)
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Témoin", Famille: "secret", Type: "autre", Portee: "infra"}, temoin); err != nil {
		t.Fatal(err)
	}
	s.ecrireAudit(LigneAudit{Type: "reponse", De: "dev", Vers: "pilote", Projet: "demo", Action: "etat", Resultat: "valeur " + temoin})
	l := lignesAudit(t, dossierJournalAudit())
	if len(l) != 1 || strings.Contains(l[0].Resultat, temoin) || l[0].Resultat != "valeur ***" {
		t.Fatalf("server log: %+v", l)
	}
	if v, err := s.verifierAudit(); err != nil || !v.OK {
		t.Fatalf("check: %+v %v", v, err)
	}
}

// TestAuditLire: newest first, 100 a page then the next one from Suite, filters by month, project, type
// and text, the months and projects listed, an unreadable line left out.
func TestAuditLire(t *testing.T) {
	d := t.TempDir()
	quand := time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC)
	a := Audit{Dossier: d, Maintenant: horloge(&quand)}
	for i := 1; i <= 150; i++ {
		if i == 41 {
			quand = time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
		}
		l := LigneAudit{Type: "evenement", De: "fenetre", Vers: "hub", Projet: "alpha", Action: "acces_accorde", Resultat: fmt.Sprint("Stripe ", i)}
		if i%3 == 0 {
			l.Type, l.De, l.Projet, l.Action = "demande", "dev", "beta", "demander_acces"
		}
		if err := a.Ecrire(l); err != nil {
			t.Fatal(err)
		}
	}
	p, err := a.Lire(FiltreAudit{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lignes) != 100 || p.Lignes[0].N != 150 || p.Lignes[99].N != 51 || p.Suite != 51 {
		t.Fatalf("first page: %d lines, %d..%d, suite %d", len(p.Lignes), p.Lignes[0].N, p.Lignes[len(p.Lignes)-1].N, p.Suite)
	}
	if strings.Join(p.Mois, " ") != "2026-11 2026-10" || strings.Join(p.Projets, " ") != "alpha beta" {
		t.Fatalf("months %v, projects %v", p.Mois, p.Projets)
	}
	if p, _ = a.Lire(FiltreAudit{Avant: 51}); len(p.Lignes) != 50 || p.Lignes[0].N != 50 || p.Lignes[49].N != 1 || p.Suite != 0 {
		t.Fatalf("second page: %d lines, suite %d", len(p.Lignes), p.Suite)
	}
	if p, _ = a.Lire(FiltreAudit{Mois: "2026-10"}); len(p.Lignes) != 40 || p.Lignes[0].N != 40 || len(p.Mois) != 2 {
		t.Fatalf("month filter: %d lines", len(p.Lignes))
	}
	if p, _ = a.Lire(FiltreAudit{Projet: "beta", Type: "demande"}); len(p.Lignes) != 50 || p.Lignes[0].N != 150 {
		t.Fatalf("project and type filter: %d lines", len(p.Lignes))
	}
	if p, _ = a.Lire(FiltreAudit{Q: "STRIPE 15"}); len(p.Lignes) != 2 || p.Lignes[0].N != 150 || p.Lignes[1].N != 15 {
		t.Fatalf("text filter: %+v", p.Lignes)
	}
	// a broken line is left out of the reading (the check points at it)
	f := filepath.Join(d, "2026-11.jsonl")
	b, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(b, []byte("pas du json\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, err = a.Lire(FiltreAudit{}); err != nil || p.Lignes[0].N != 150 {
		t.Fatalf("with an unreadable line: %v %+v", err, p.Lignes[0])
	}
	if p, err = (Audit{Dossier: filepath.Join(d, "absent")}).Lire(FiltreAudit{}); err != nil || len(p.Lignes) != 0 || p.Lignes == nil {
		t.Fatalf("no log yet: %v %+v", err, p)
	}
}

// TestAuditAPI: GET /api/hub/audit reads with its filters (bad ones refused), POST …/verifier gives the
// first broken line once the log is changed.
func TestAuditAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	for _, p := range []string{"alpha", "beta"} {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "fenetre", Vers: "hub", Projet: p, Action: "acces_accorde", Resultat: "Stripe test"})
	}
	appel := func(h http.HandlerFunc, methode, url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(methode, url, nil))
		return w
	}
	w := appel(s.apiAudit, "GET", "/api/hub/audit?projet=beta")
	var p PageAudit
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || len(p.Lignes) != 1 || p.Lignes[0].Projet != "beta" {
		t.Fatalf("read: %d %s", w.Code, w.Body)
	}
	for _, q := range []string{"mois=2026-1", "mois=../x", "type=autre", "avant=0", "avant=x", "q=" + strings.Repeat("a", 201)} {
		if w := appel(s.apiAudit, "GET", "/api/hub/audit?"+q); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
	var v VerifAudit
	if w := appel(s.apiAuditVerifier, "POST", "/api/hub/audit/verifier"); json.Unmarshal(w.Body.Bytes(), &v) != nil || !v.OK || v.Lignes != 2 {
		t.Fatalf("check: %s", w.Body)
	}
	m, _ := Audit{Dossier: dossierJournalAudit()}.mois()
	f := filepath.Join(dossierJournalAudit(), m[0])
	b, _ := os.ReadFile(f)
	if err := os.WriteFile(f, bytes.Replace(b, []byte(`"projet":"alpha"`), []byte(`"projet":"gamma"`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if w := appel(s.apiAuditVerifier, "POST", "/api/hub/audit/verifier"); json.Unmarshal(w.Body.Bytes(), &v) != nil || v.OK || v.Ligne != 1 || v.Raison != "modifiee" || v.Fichier != m[0] {
		t.Fatalf("changed log: %s", w.Body)
	}
}
