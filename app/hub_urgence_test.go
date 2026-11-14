package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestArretUrgence: each effect of the emergency stop (tokens, pilots, approvals, vault, lock, audit),
// the resume paths (password; Resume without one), and athanor arret-urgence through instance.json.
func TestArretUrgence(t *testing.T) {
	s := serveurCoffreTest(t)
	// the password first: no pilot runs yet (preuveAbsence)
	if w := appelCoffre(t, s.apiMotDePasse, "", `{"nouveau":"alpha-1-phrase"}`, nil); w.Code != 200 {
		t.Fatal("set password:", w.Code, w.Body.String())
	}
	d := t.TempDir()
	var projets []Projet
	for _, n := range []string{"demo", "autre", "dormant"} {
		p := Projet{Nom: n, Dossier: filepath.Join(d, n)}
		if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
			t.Fatal(err)
		}
		if n != "dormant" { // a running pilot touches pilot.pid, and restarts with Athanor when "auto" exists
			_ = os.WriteFile(filepath.Join(p.Atelier(), "pilot.pid"), []byte("1"), 0o644)
			marqueAuto(p, true)
		}
		projets = append(projets, p)
	}
	s.config.Projets = projets
	var mu sync.Mutex
	var arretes []string
	ancien := arreterPiloteUrgence
	arreterPiloteUrgence = func(_ *Serveur, p Projet) error {
		mu.Lock()
		defer mu.Unlock()
		arretes = append(arretes, p.Nom)
		if p.Nom == "autre" {
			return errors.New("kit absent")
		}
		return nil
	}
	t.Cleanup(func() { arreterPiloteUrgence = ancien })

	// a password, a code and an infra entry, two tokens, two pending approvals and one decided
	code, err := s.coffre.Ajouter(EntreeCoffre{Nom: "ANTHROPIC_API_KEY", Famille: "secret", Type: "anthropic"}, "temoin-urg-code")
	if err != nil {
		t.Fatal(err)
	}
	infra, err := s.coffre.Ajouter(EntreeCoffre{Nom: "SSH_PROD", Famille: "secret", Type: "ssh"}, "temoin-urg-infra")
	if err != nil || infra.Portee != "infra" {
		t.Fatal("infra entry:", infra.Portee, err)
	}
	for _, p := range projets[:2] {
		if err := s.creerJetonPilote(p); err != nil {
			t.Fatal(err)
		}
	}
	var attente []string
	for _, env := range []string{"dev", "prod"} {
		x, err := s.demanderApprobation(Approbation{Projet: "demo", Env: env, Agent: "pilote", Action: "demander_acces"})
		if err != nil {
			t.Fatal(err)
		}
		attente = append(attente, x.ID)
	}
	decidee, _ := s.demanderApprobation(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "demander_acces"})
	if _, err := s.approbations.Decider(decidee.ID, "accepter", "fenetre"); err != nil {
		t.Fatal(err)
	}
	regle, _ := s.demanderApprobation(Approbation{Projet: "demo", Env: "prod", Agent: "pilote", Action: "deployer"})
	if _, err := s.approbations.Decider(regle.ID, "toujours", "fenetre"); err != nil {
		t.Fatal(err)
	}
	s.session = "ouverte"
	s.dist().deverrouilles = map[string]bool{"tel": true}

	w := appelCoffre(t, s.apiUrgence, "tel", "", nil) // from a paired device, empty body
	if w.Code != 200 {
		t.Fatal("stop:", w.Code, w.Body.String())
	}
	var rep struct {
		Resultat ResultatUrgence `json:"resultat"`
		Cle      string          `json:"message_key"`
		Args     []string        `json:"args"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &rep)
	r := rep.Resultat
	if r.Jetons != 2 || r.Approbations != 2 || r.Regles != 1 || r.Pilotes != 1 || !slices.Equal(r.Echecs, []string{"autre"}) || !r.Verrouille ||
		rep.Cle != texteUrgence || !slices.Equal(rep.Args, []string{"1", "2", "2", "1"}) {
		t.Fatalf("result: %s", w.Body)
	}
	if _, regles := s.approbations.Liste(); len(regles) != 0 {
		t.Errorf("rules left: %+v", regles)
	}
	// tokens: none left, their files removed
	if l := s.jetons.Liste(); len(l) != 0 {
		t.Errorf("tokens left: %+v", l)
	}
	for _, p := range projets {
		if _, err := os.Stat(s.fichierJeton(p)); !os.IsNotExist(err) {
			t.Errorf("hub-jeton of %s left", p.Nom)
		}
	}
	// pilots: the running ones stopped, the dormant one untouched, none restarts with Athanor
	slices.Sort(arretes)
	if !slices.Equal(arretes, []string{"autre", "demo"}) {
		t.Errorf("stopped: %v", arretes)
	}
	for _, p := range projets {
		if _, err := os.Stat(filepath.Join(p.Atelier(), "auto")); !os.IsNotExist(err) {
			t.Errorf("%s still restarts with Athanor", p.Nom)
		}
	}
	// approvals: the pending ones refused by the device, the decided one unchanged
	for _, id := range attente {
		if x, _ := s.approbations.Lire(id); x.Etat != "refusee" || x.Par != "appareil:tel" || s.approbations.PourAgent(id) != "refusee" {
			t.Errorf("approval %s: %+v", id, x)
		}
	}
	if x, _ := s.approbations.Lire(decidee.ID); x.Etat != "acceptee" {
		t.Errorf("decided approval changed: %+v", x)
	}
	// vault: both keys closed, the keystore no longer reopens the code key
	if _, err := s.coffre.Valeur(code.ID); !errors.Is(err, errCoffreVerrouille) {
		t.Errorf("code entry readable: %v", err)
	}
	if _, err := s.coffre.Valeur(infra.ID); err == nil {
		t.Error("infra entry readable")
	}
	if err := s.coffre.OuvrirCode(); !errors.Is(err, errCoffreVerrouille) {
		t.Errorf("keystore reopened the code key: %v", err)
	}
	// window and devices locked
	if s.session != "" || len(s.dist().deverrouilles) != 0 {
		t.Error("window or device still unlocked")
	}
	// the bell: an alert without Resume (a password exists), the audit log has the stop and the refusals
	a := s.alerteUrgence()
	if len(a) != 1 || len(a[0].Actions) != 0 {
		t.Errorf("alert: %+v", a)
	}
	j := auditTexte(t)
	if !strings.Contains(j, "arrêt d'urgence 2 jeton(s) révoqué(s), 2 approbation(s) refusée(s), 1 règle(s) retirée(s), 1 pilote(s) arrêté(s), coffre verrouillé, 0 secrets.env vidé(s), 1 pilote(s) non arrêté(s)") ||
		strings.Count(j, "→ refusee (arrêt d'urgence)") != 2 || !strings.Contains(j, "jeton révoqué arrêt d'urgence") {
		t.Errorf("audit log:\n%s", j)
	}
	if strings.Contains(j, "temoin-urg") {
		t.Error("vault value in the audit log")
	}
	// resume with a password: Resume refused, unlocking reopens everything
	if w := appelCoffre(t, s.apiUrgence, "", `{"reprendre":true}`, nil); w.Code != http.StatusConflict {
		t.Errorf("resume with a password: %d", w.Code)
	}
	if err := s.coffre.Deverrouiller("alpha-1-phrase"); err != nil {
		t.Fatal(err)
	}
	if lisible(s.coffre, code.ID, "temoin-urg-code") != nil || lisible(s.coffre, infra.ID, "temoin-urg-infra") != nil || s.alerteUrgence() != nil {
		t.Error("unlocking did not reopen the vault")
	}

	// without a password: the bell's Resume reopens the code key through the keystore
	s.config.MotDePasseHash = ""
	s.arretUrgence("fenetre")
	a = s.alerteUrgence()
	if len(a) != 1 || len(a[0].Actions) != 1 || a[0].Actions[0].Route != "hub/urgence" {
		t.Fatalf("alert without a password: %+v", a)
	}
	// C27: "autre" was not stopped, its agent could undo the stop: Resume waits for it
	if w := appelCoffre(t, s.apiUrgence, "", `{"reprendre":true}`, nil); w.Code != http.StatusConflict || s.alerteUrgence() == nil {
		t.Fatal("resume while a pilot runs:", w.Code, w.Body.String())
	}
	for _, p := range projets {
		_ = os.Remove(filepath.Join(p.Atelier(), "pilot.pid"))
	}
	if w := appelCoffre(t, s.apiUrgence, "", `{"reprendre":true}`, nil); w.Code != 200 {
		t.Fatal("resume:", w.Code, w.Body.String())
	}
	if err := lisible(s.coffre, code.ID, "temoin-urg-code"); err != nil || s.alerteUrgence() != nil {
		t.Errorf("resume: %v", err)
	}
	if !strings.Contains(auditTexte(t), "reprise après arrêt d'urgence") {
		t.Error("resume not in the audit log")
	}

	// athanor arret-urgence: through the running instance's secret only
	mux := http.NewServeMux()
	s.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	_ = os.WriteFile(filepath.Join(projets[0].Atelier(), "pilot.pid"), []byte("1"), 0o644)
	if code := lancerArretUrgence(nil); code != 1 {
		t.Errorf("without instance.json: %d", code)
	}
	if err := os.MkdirAll(filepath.Dir(fichierInstance()), 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(infoInstance{port, "faux", os.Getpid()})
	_ = os.WriteFile(fichierInstance(), b, 0o600)
	t.Cleanup(func() { _ = os.Remove(fichierInstance()) })
	if code := lancerArretUrgence(nil); code != 1 || s.coffre.Urgence() != "" {
		t.Errorf("wrong secret: %d, stopped %v", code, s.coffre.Urgence() != "")
	}
	b, _ = json.Marshal(infoInstance{port, jetonInstance, os.Getpid()})
	_ = os.WriteFile(fichierInstance(), b, 0o600)
	arretes = nil
	if code := lancerArretUrgence(nil); code != 0 || s.coffre.Urgence() == "" || !slices.Contains(arretes, "demo") {
		t.Errorf("arret-urgence: %d, vault %q, stopped %v", code, s.coffre.Urgence(), arretes)
	}
	if strings.Count(auditTexte(t), "coffre verrouillé") != 3 {
		t.Error("CLI stop not in the audit log")
	}
	if !strings.Contains(auditTexte(t), "instance urgence") {
		t.Error("the instance call's source not in the audit log")
	}
	// C27: instance.json's secret stops, never resumes
	req, _ := http.NewRequest("POST", srv.URL+"/api/instance/urgence", strings.NewReader(`{"reprendre":true}`))
	req.Header.Set("X-Athanor-Instance", jetonInstance)
	if rep, err := http.DefaultClient.Do(req); err != nil || rep.StatusCode != 200 || s.coffre.Urgence() == "" {
		t.Errorf("instance urgence with reprendre: %v, vault %q", err, s.coffre.Urgence())
	} else {
		rep.Body.Close()
	}
}
