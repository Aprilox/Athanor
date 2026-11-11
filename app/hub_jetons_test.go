package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type horlogeJeton struct{ t time.Time }

func (h *horlogeJeton) maintenant() time.Time { return h.t }

// TestJetonsMagasin: creation, a session per launch, another session's token, expiry after 24 h without
// use (each use pushes it back), revocation, reopening, nothing in clear in sessions.json.
func TestJetonsMagasin(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "hub", "sessions.json")
	h := &horlogeJeton{time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)}
	j, err := ouvrirSessions(chemin)
	if err != nil {
		t.Fatal(err)
	}
	j.Maintenant = h.maintenant
	a, err := j.Creer("demo", outilsPilote)
	if err != nil || len(a) != 64 {
		t.Fatalf("token %q %v", a, err)
	}
	b, _ := j.Creer("autre", []string{"etat"})
	brut, _ := os.ReadFile(chemin)
	for _, x := range []string{a, b} {
		if strings.Contains(string(brut), x) || !strings.Contains(string(brut), hacherJeton(x)) {
			t.Fatal("sessions.json holds a token in clear, or not its hash")
		}
	}
	if fi, _ := os.Stat(chemin); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("sessions.json mode %v", fi.Mode().Perm())
	}
	if x, ok := j.Verifier(a); !ok || x.Projet != "demo" || x.Agent != "pilote" || len(x.Outils) != len(outilsPilote) {
		t.Fatalf("demo: %+v %v", x, ok)
	}
	if x, ok := j.Verifier(b); !ok || x.Projet != "autre" || len(x.Outils) != 1 {
		t.Fatalf("another session's token: %+v %v", x, ok)
	}
	for _, faux := range []string{"", "x", hacherJeton(a), strings.ToUpper(a), a + " "} {
		if _, ok := j.Verifier(faux); ok {
			t.Fatalf("token %q accepted", faux)
		}
	}
	// a new launch of demo revokes the previous one
	a2, _ := j.Creer("demo", outilsPilote)
	if _, ok := j.Verifier(a); ok {
		t.Fatal("token of the previous launch still valid")
	}
	// 23 h then 23 h more: each use pushes the expiry back; 24 h without use: expired and dropped
	h.t = h.t.Add(23 * time.Hour)
	if _, ok := j.Verifier(a2); !ok {
		t.Fatal("expired too early")
	}
	h.t = h.t.Add(23 * time.Hour)
	if x, ok := j.Verifier(a2); !ok || x.Vu != h.t.Format(time.RFC3339) {
		t.Fatalf("use not recorded: %+v", x)
	}
	h.t = h.t.Add(dureeJeton)
	if _, ok := j.Verifier(a2); ok {
		t.Fatal("expired token accepted")
	}
	for _, x := range j.Liste() {
		if x.Projet == "demo" {
			t.Fatal("expired session kept")
		}
	}
	// b is unused since its creation: expired too, dropped by a revocation sweep
	if p, _ := j.Revoquer(func(SessionHub) bool { return false }); len(p) != 1 || p[0] != "autre" {
		t.Fatalf("sweep: %v", p)
	}
	c, _ := j.Creer("trois", nil)
	if p, err := j.Revoquer(func(x SessionHub) bool { return x.Projet == "trois" }); err != nil || len(p) != 1 {
		t.Fatalf("revoke: %v %v", p, err)
	}
	if _, ok := j.Verifier(c); ok {
		t.Fatal("revoked token accepted")
	}
	// reopened from the file
	d, _ := j.Creer("quatre", nil)
	j2, err := ouvrirSessions(chemin)
	if err != nil {
		t.Fatal(err)
	}
	j2.Maintenant = h.maintenant
	if x, ok := j2.Verifier(d); !ok || x.Projet != "quatre" {
		t.Fatal("session lost on reopening")
	}
	// an unreadable file: an error, no session
	_ = os.WriteFile(chemin, []byte("{pas du json"), 0o600)
	if j3, err := ouvrirSessions(chemin); err == nil || len(j3.Liste()) != 0 {
		t.Fatal("unreadable store accepted")
	}
}

// TestJetonsServeur: the token handed in the pilot's private folder (outside the project, never in the
// logs), revoked when the pilot stops, when it is no longer seen (start-up grace kept), at Athanor's
// start, and for a project gone.
func TestJetonsServeur(t *testing.T) {
	s := serveurCoffreTest(t)
	h := &horlogeJeton{time.Now()}
	s.jetons.Maintenant = h.maintenant
	d := t.TempDir()
	demo := Projet{Nom: "demo", Dossier: filepath.Join(d, "demo")}
	autre := Projet{Nom: "autre", Dossier: filepath.Join(d, "autre")}
	s.config.Projets = []Projet{demo, autre}
	for _, p := range s.config.Projets {
		if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lireJeton := func(p Projet) string {
		b, err := os.ReadFile(s.fichierJeton(p))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if err := s.creerJetonPilote(demo); err != nil {
		t.Fatal(err)
	}
	jd := lireJeton(demo)
	if x, ok := s.verifierJeton(jd); !ok || x.Projet != "demo" {
		t.Fatalf("token of demo: %+v %v", x, ok)
	}
	if fi, _ := os.Stat(s.fichierJeton(demo)); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("hub-jeton mode %v", fi.Mode().Perm())
	}
	if strings.HasPrefix(filepath.Clean(s.fichierJeton(demo)), filepath.Clean(demo.Dossier)) {
		t.Fatal("hub-jeton inside the project")
	}
	// nowhere in clear but its file: not in the project, the audit log or sessions.json
	for _, racine := range []string{demo.Dossier, dossierJournalAudit(), filepath.Dir(cheminSessions())} {
		_ = filepath.WalkDir(racine, func(f string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				if b, _ := os.ReadFile(f); strings.Contains(string(b), jd) {
					t.Errorf("token in clear in %s", f)
				}
			}
			return nil
		})
	}
	if !strings.Contains(auditTexte(t), "jeton créé") {
		t.Fatal("creation not in the audit log")
	}

	// demo's pilot not running yet: kept during the grace, revoked after
	s.surveillerRelance(demo, false)
	if _, ok := s.verifierJeton(jd); !ok {
		t.Fatal("revoked during the start-up grace")
	}
	pid := filepath.Join(demo.Atelier(), "pilot.pid")
	_ = os.WriteFile(pid, []byte("1"), 0o644)
	h.t = h.t.Add(graceJeton + time.Minute)
	s.surveillerRelance(demo, true)
	if _, ok := s.verifierJeton(jd); !ok {
		t.Fatal("revoked while the pilot runs")
	}
	vieux := time.Now().Add(-5 * time.Minute)
	_ = os.Chtimes(pid, vieux, vieux) // the pilot is no longer seen
	s.surveillerRelance(demo, false)
	if _, ok := s.verifierJeton(jd); ok || lireJeton(demo) != "" {
		t.Fatal("token of a stopped pilot still valid, or its file kept")
	}

	// verifierJeton itself refuses the token of a pilot no longer seen
	_ = s.creerJetonPilote(autre)
	ja := lireJeton(autre)
	h.t = h.t.Add(graceJeton + time.Minute)
	if _, ok := s.verifierJeton(ja); ok {
		t.Fatal("token of a pilot not running accepted")
	}

	// a stop revokes at once; a new launch replaces the previous token
	_ = s.creerJetonPilote(autre)
	ja1 := lireJeton(autre)
	_ = s.creerJetonPilote(autre)
	ja2 := lireJeton(autre)
	if _, ok := s.verifierJeton(ja1); ok {
		t.Fatal("token of the previous launch accepted")
	}
	s.revoquerJetonProjet(autre, "pilote arrêté")
	if _, ok := s.verifierJeton(ja2); ok || lireJeton(autre) != "" {
		t.Fatal("token kept after the stop")
	}

	// Athanor's start: a running pilot keeps its token; a stopped one, or a project gone, loses it
	_ = os.Chtimes(pid, time.Now(), time.Now())
	_ = s.creerJetonPilote(demo)
	jd2 := lireJeton(demo)
	_ = s.creerJetonPilote(autre)
	ja3 := lireJeton(autre)
	h.t = h.t.Add(graceJeton + time.Minute)
	s.revoquerJetonsArretes()
	if _, ok := s.verifierJeton(jd2); !ok {
		t.Fatal("token of a running pilot revoked at start")
	}
	if _, ok := s.verifierJeton(ja3); ok || lireJeton(autre) != "" {
		t.Fatal("token of a stopped pilot kept at start")
	}
	s.config.Projets = []Projet{autre}
	s.revoquerJetonsArretes()
	if _, ok := s.verifierJeton(jd2); ok {
		t.Fatal("token of a project gone kept")
	}
	if n := strings.Count(auditTexte(t), "jeton révoqué"); n < 5 {
		t.Fatalf("%d revocations in the audit log", n)
	}
	for _, x := range []string{jd, ja, ja1, ja2, ja3, jd2} {
		if strings.Contains(auditTexte(t), x) {
			t.Fatal("a token in the audit log")
		}
	}
}

func auditTexte(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		b.WriteString(l.Action + " " + l.Resultat + "\n")
	}
	return b.String()
}
