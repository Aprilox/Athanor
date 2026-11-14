package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// M212: the open bugs of the grants (B18, B20, B21, A6, A7, A8, A9, B51), one witness value followed
// through secrets.env, the workers' copies, a remote For you answer and the emergency stop.
func TestM212Acces(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	d := t.TempDir()
	demo := Projet{Nom: "demo", Dossier: filepath.Join(d, "demo")}
	deux := Projet{Nom: "deux", Dossier: filepath.Join(d, "deux")}
	for _, p := range []Projet{demo, deux} {
		if err := os.MkdirAll(filepath.Join(p.Atelier(), "logs"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	s.config.Projets = []Projet{demo, deux}
	s.mu.Unlock()
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	temoin := "temoin-m212-" + aleatoire(6)
	nouvelle := func(nom, typ, valeur string) EntreeCoffre {
		t.Helper()
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nom, Famille: "secret", Type: typ}, valeur)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	accorder := func(p string, e EntreeCoffre, variable string) {
		t.Helper()
		if _, err := s.acces.Ajouter(p, e, "dev", variable, ""); err != nil {
			t.Fatal(err)
		}
	}
	lire := func(p Projet) string { b, _ := os.ReadFile(cheminSecrets(p)); return string(b) }
	stripe := nouvelle("Stripe", "stripe", temoin)
	accorder("demo", stripe, "STRIPE_KEY")

	// B18: a comment written by someone else stays, its other lines go to secrets.env.ancien (never sourced)
	autres := []string{"LEGACY_KEY=sansguillemets", `export K="v"`}
	autrui := []string{"# note de l'utilisateur"}
	ecrireFichier(t, cheminSecrets(demo), strings.Join(append(autrui, autres...), "\n")+"\n", 0o600)
	if _, err := s.injecterAcces(demo, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ecrireSecrets(demo, map[string]string{"AUTRE": "x"}); err != nil {
		t.Fatal(err)
	}
	f := lire(demo)
	for _, l := range append(autrui, "STRIPE_KEY='"+temoin+"'", "AUTRE='x'") {
		if !strings.Contains(f, l+"\n") {
			t.Fatalf("B18: %q lost:\n%s", l, f)
		}
	}
	if _, err := s.injecterAcces(demo, nil); err != nil || strings.Count(lire(demo), "# Values ") != 1 {
		t.Fatalf("B18: header doubled or error %v:\n%s", err, lire(demo))
	}
	ancien, _ := os.ReadFile(cheminSecrets(demo) + ".ancien")
	for _, l := range autres {
		if strings.Contains(lire(demo), l) || strings.Count(string(ancien), l+"\n") != 1 {
			t.Fatalf("B18: %q not set aside once:\n%s\n---\n%s", l, lire(demo), ancien)
		}
	}

	// B20: in Windows, the file no longer inherits the project folder's rights
	if runtime.GOOS == "windows" {
		out, err := exec.Command("icacls", cheminSecrets(demo)).CombinedOutput()
		if err != nil || strings.Contains(string(out), "(I)") || strings.Count(strings.TrimSpace(string(out)), ":(") != 1 {
			t.Fatalf("B20: rights of secrets.env (%v):\n%s", err, out)
		}
	}

	// B21: a granted value on several lines is named on the panel instead of vanishing
	accorder("demo", nouvelle("Clé PEM", "jeton", "-----BEGIN KEY-----\nabc\n-----END KEY-----"), "PEM_KEY")
	if e := s.etatInjection(demo); !slices.Contains(e.Ignorees, "PEM_KEY") || slices.Contains(e.Donnees, "PEM_KEY") {
		t.Fatalf("B21: %+v", e)
	}

	// A6: a worker's copy follows a removed grant, and comes back with it
	copie := filepath.Join(demo.Atelier(), "work", "M5", ".athanor", "secrets.env")
	ecrireFichier(t, copie, "STRIPE_KEY='"+temoin+"'\n", 0o600)
	if err := s.acces.Retirer("demo", stripe.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.injecterAcces(demo, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(copie); strings.Contains(string(b), temoin) || !strings.Contains(string(b), "# note") {
		t.Fatalf("A6: worker copy after the removal:\n%s", b)
	}
	accorder("demo", stripe, "STRIPE_KEY")
	if _, err := s.injecterAcces(demo, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(copie); !strings.Contains(string(b), temoin) {
		t.Fatalf("A6: worker copy after the new grant:\n%s", b)
	}
	// A6: a project removed from the list keeps its files, not the granted values
	accorder("deux", stripe, "STRIPE_KEY")
	if _, err := s.injecterAcces(deux, nil); err != nil || !strings.Contains(lire(deux), temoin) {
		t.Fatalf("A6: setup of deux: %v", err)
	}
	if err := s.retirerProjet(deux); err != nil {
		t.Fatal(err)
	}
	if f := lire(deux); strings.Contains(f, temoin) {
		t.Fatalf("A6: removed project still holds the value:\n%s", f)
	}

	// A7: from a remote device, a For you answer never replaces a grant made from the vault
	if _, _, refus := s.pourVousAuCoffre(demo, "T1", map[string]string{"STRIPE_KEY": "autre-" + aleatoire(6)}, true); refus != errPourVousDistant {
		t.Fatalf("A7: remote answer not refused: %v", refus)
	}
	if pa := s.acces.Projet("demo"); !slices.ContainsFunc(pa.Acces, func(a AccesProjet) bool { return a.Entree == stripe.ID && a.Variable == "STRIPE_KEY" }) {
		t.Fatalf("A7: the grant changed: %+v", pa.Acces)
	}
	if _, _, refus := s.pourVousAuCoffre(demo, "T1", map[string]string{"STRIPE_KEY": temoin}, true); refus != nil {
		t.Fatalf("A7: the same value refused: %v", refus)
	}

	// A8: the other projects' secrets.env are out of the pilot's reach
	var fs struct{ DenyRead []string }
	if b, _ := json.Marshal(filesystemBarriere(d, filepath.Join(d, "tools"), nil, "/srv/b/.athanor/secrets.env")); json.Unmarshal(b, &fs) != nil || !slices.Contains(fs.DenyRead, "/srv/b/.athanor/secrets.env") {
		t.Fatalf("A8: denyRead %v", fs.DenyRead)
	}

	// A9: an infra link made under WSL gives nothing once in native mode without forcing
	ancienNatif := modeNatif.Load()
	modeNatif.Store(false)
	accorder("demo", nouvelle("Cloudflare", "cloudflare", "temoin-infra-"+aleatoire(6)), "")
	modeNatif.Store(true)
	t.Cleanup(func() { modeNatif.Store(ancienNatif) })
	if err := s.accesServeursAgent(SessionHub{Agent: agentServeurs, Projet: "demo"}); err != errServeursNatif {
		t.Fatalf("A9: the Servers tools in native mode without forcing: %v", err)
	}
	modeNatif.Store(ancienNatif)

	// B51: the emergency stop takes the granted values out of the files; the others stay
	arretAvant := arreterPiloteUrgence
	arreterPiloteUrgence = func(*Serveur, Projet) error { return nil }
	t.Cleanup(func() { arreterPiloteUrgence = arretAvant })
	s.arretUrgence("fenetre")
	if f := lire(demo); strings.Contains(f, temoin) || !strings.Contains(f, "# note") || !strings.Contains(f, "AUTRE='x'") {
		t.Fatalf("B51: after the emergency stop:\n%s", f)
	}
	if b, _ := os.ReadFile(copie); strings.Contains(string(b), temoin) {
		t.Fatalf("B51: worker copy after the emergency stop:\n%s", b)
	}
}
