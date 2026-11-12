package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// The Servers module end to end (M81): the server's password and the vault's private key found nowhere an
// agent or the owner could read them, the perimeter enforced on every tool, and the whole P4 scenario on
// the test container.

// serveursSessionTest: a project "demo" (its logs/ folder, its pilot's private folder with its hub-jeton),
// a Servers agent's token on perimetre, and an MCP caller whose raw answers are all kept.
func serveursSessionTest(t *testing.T, s *Serveur, perimetre ...string) (Projet, func(nom, args string) (string, bool), *strings.Builder) {
	t.Helper()
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	demo := Projet{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}
	if err := os.MkdirAll(filepath.Join(demo.Atelier(), "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config.Projets = []Projet{demo}
	s.mu.Unlock()
	if err := s.creerJetonPilote(demo); err != nil {
		t.Fatal(err)
	}
	jeton, err := s.jetons.CreerSession(SessionHub{Projet: "demo", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: perimetre})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	brut := &strings.Builder{}
	appel := func(nom, args string) (string, bool) {
		t.Helper()
		r := httptest.NewRequest("POST", "http://127.0.0.1/api/mcp", strings.NewReader(appelOutil(nom, args)))
		r.Header.Set("Authorization", "Bearer "+jeton)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		brut.Write(w.Body.Bytes())
		var rep reponseMCP
		if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
			t.Fatalf("%s: %q", nom, w.Body.String())
		}
		if rep.Error != nil {
			return rep.Error.Message, true
		}
		if len(rep.Result.Content) != 1 {
			t.Fatalf("%s: %+v", nom, rep)
		}
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	return demo, appel, brut
}

// lireAPI: GET chemin with the owner's cookies; the body is kept in reponses.
func lireAPI(t *testing.T, s *Serveur, chemin string, reponses *strings.Builder) int {
	t.Helper()
	mux := http.NewServeMux()
	s.routes(mux)
	r := httptest.NewRequest("GET", chemin, nil)
	r.Header.Set("X-Athanor", "jeton")
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	reponses.Write(w.Body.Bytes())
	return w.Code
}

// TestServeursTemoin: a server added with its (witness) password gets a generated key; then a Servers agent
// uses every tool, the owner reads, refreshes and cuts the password. Neither the password nor the private key
// (whole, or any of its secret lines) is in the agent's environment, the project's files and logs/, the
// pilot's private folder, an MCP answer, an API answer, the audit log, FICHE.md, or any file of Athanor's.
func TestServeursTemoin(t *testing.T) {
	s, appelAPI, reponses := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	attente := attenteRechargeSSH
	attenteRechargeSSH = 10 * time.Millisecond
	t.Cleanup(func() { attenteRechargeSSH = attente })
	mdp := "temoin-mdp-" + aleatoire(10) // built at run time: the test binary does not hold it
	fiche := lireTestdata(t, "fiche-debian.txt")
	var srv *serveurSSHMemoire
	srv = lancerSSHMemoire(t, nil, mdp, func(cmd string, _ []byte) (string, int) {
		switch {
		case cmd == commandeFiche:
			return fiche, 0
		case strings.Contains(cmd, "PasswordAuthentication no"):
			return "", 0
		case strings.Contains(cmd, "authorized_keys") && strings.Contains(cmd, "'ssh-ed25519 "):
			ligne := cmd[strings.Index(cmd, "'ssh-ed25519 ")+1:]
			k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(ligne[:strings.Index(ligne, "'")]))
			if err != nil {
				return err.Error(), 1
			}
			srv.autoriser(k)
			return "", 0
		}
		return "ok", 0
	})

	code, v := appelAPI("/api/serveurs/ajouter", map[string]any{"nom": "VPS", "hote": srv.hote, "port": srv.port, "utilisateur": "athanor",
		"empreinte": srv.empreinte, "mode": "mot_de_passe", "mot_de_passe_serveur": mdp})
	if code != 200 {
		t.Fatalf("addition: %d %v", code, v)
	}
	x := v["serveur"].(map[string]any)
	id := x["id"].(string)
	prive, err := s.coffre.Valeur(x["cle"].(string))
	if err != nil || !strings.Contains(prive, "PRIVATE KEY") {
		t.Fatalf("the generated key: %v", err)
	}
	demo, appel, mcp := serveursSessionTest(t, s, id)

	// the agent: every tool once
	local := dossierFichiersServeur(id)
	_ = os.MkdirAll(local, 0o700)
	_ = os.WriteFile(filepath.Join(local, "envoi.txt"), []byte("contenu\n"), 0o600)
	for _, a := range [][2]string{{"serveurs_liste", `{}`}, {"serveur_fiche", ``}, {"ssh_commande", `,"commande":"cat ~/.ssh/authorized_keys"`},
		{"fichier_lire", `,"chemin":"/etc/hostname"`}, {"fichier_ecrire", `,"chemin":"/tmp/a.txt","contenu":"x"`},
		{"sftp_envoyer", `,"local":"envoi.txt","distant":"/envoi.txt"`}, {"sftp_recuperer", `,"distant":"/envoi.txt","local":"retour.txt"`},
		{"service", `,"action":"status","nom":"ssh"`}, {"docker", `,"action":"ps"`}, {"pare_feu", `,"action":"status"`}} {
		args := a[1]
		if a[0] != "serveurs_liste" {
			args = `{"serveur":"` + id + `"` + a[1] + `}`
		}
		if txt, e := appel(a[0], args); txt == errServeurInconnu.Error() || txt == errServeursAgent.Error() {
			t.Errorf("%s refused: %v %q", a[0], e, txt)
		}
	}
	if txt, _ := appel("serveur_fiche", `{"serveur":"`+id+`"}`); !strings.Contains(txt, "<server-sheet>") {
		t.Errorf("the agent did not get the sheet: %q", txt)
	}
	// the owner: the inventory, the server's page, a refresh, then password logins cut
	for _, c := range []string{"/api/serveurs", "/api/serveurs/" + id} {
		if code := lireAPI(t, s, c, reponses); code != 200 {
			t.Errorf("GET %s: %d", c, code)
		}
	}
	if code, v := appelAPI("/api/serveurs/"+id, map[string]any{"rafraichir": true}); code != 200 {
		t.Errorf("refresh: %d %v", code, v)
	}
	if code, v := appelAPI("/api/serveurs/"+id, map[string]any{"couper_mot_de_passe": true}); code != 200 {
		t.Errorf("cut: %d %v", code, v)
	}

	// the witnesses: the password, the whole key, each line holding its secret part (the first ones are
	// the format's header and the public key, the same in authorized_keys), the PEM as JSON writes it
	temoins := []string{mdp, prive, strings.ReplaceAll(prive, "\n", `\n`)}
	corps := strings.Split(strings.TrimSpace(prive), "\n")
	if len(corps) < 6 {
		t.Fatalf("the key has %d lines", len(corps))
	}
	for _, l := range corps[4 : len(corps)-1] {
		if len(l) >= 16 {
			temoins = append(temoins, l)
		}
	}
	nom := map[string]string{mdp: "the password"}
	cherche := func(ou, texte string) {
		t.Helper()
		for _, w := range temoins {
			if strings.Contains(texte, w) {
				t.Errorf("%s in %s", cmp.Or(nom[w], "the private key"), ou)
			}
		}
	}
	cherche("an API answer", reponses.String())
	cherche("an MCP answer", mcp.String())
	cherche("the agent's environment", strings.Join((&pilote{}).envClaude(), "\n"))
	cherche("Athanor's environment (inherited by an agent)", strings.Join(os.Environ(), "\n"))
	cherche("the audit log", journalAuditTexte(t))
	b, err := os.ReadFile(filepath.Join(dossierModule("servers"), id, "FICHE.md"))
	if err != nil {
		t.Fatal("no FICHE.md:", err)
	}
	cherche("FICHE.md", string(b))
	if _, err := os.Stat(s.fichierJeton(demo)); err != nil {
		t.Fatal("the pilot's hub-jeton:", err)
	}
	for _, d := range []string{demo.Dossier, s.priveProjet(demo), dossierModule("servers"), dossierJournalAudit(), dossierAthanor()} {
		if f := fichiersAvec(t, d, temoins); len(f) > 0 {
			t.Errorf("a witness in files of %s: %v", d, f)
		}
	}
}

// TestServeursPerimetre: two servers, a token limited to the first: every tool answers on it and refuses the
// second exactly as an unknown server, each refusal in the audit log, and nothing reaches the second server.
func TestServeursPerimetre(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	rep := func(cmd string, _ []byte) (string, int) { return "ok", 0 }
	memA := lancerSSHMemoire(t, signer.PublicKey(), "", rep)
	memB := lancerSSHMemoire(t, signer.PublicKey(), "", rep)
	s, a := outilsServeursTest(t, memA.hote, memA.port, memA.empreinte, prive)
	b := ServeurInventaire{ID: "cd34", Nom: "Prod", Hote: memB.hote, Port: memB.port, Utilisateur: "athanor", Cle: a.Cle, EmpreinteHote: memB.empreinte, Etat: "ok"}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { return append(l, b), nil }); err != nil {
		t.Fatal(err)
	}
	_, appel, _ := serveursSessionTest(t, s, a.ID)
	for _, id := range []string{a.ID, b.ID} {
		_ = os.MkdirAll(dossierFichiersServeur(id), 0o700)
		_ = os.WriteFile(filepath.Join(dossierFichiersServeur(id), "envoi.txt"), []byte("x\n"), 0o600)
	}

	if txt, _ := appel("serveurs_liste", `{}`); !strings.Contains(txt, `"`+a.ID+`"`) || strings.Contains(txt, b.ID) {
		t.Errorf("list: %s", txt)
	}
	outils := map[string]string{"serveur_fiche": ``, "ssh_commande": `,"commande":"id"`, "fichier_lire": `,"chemin":"/etc/hostname"`,
		"fichier_ecrire": `,"chemin":"/tmp/a.txt","contenu":"x"`, "sftp_envoyer": `,"local":"envoi.txt","distant":"/envoi.txt"`,
		"sftp_recuperer": `,"distant":"/envoi.txt","local":"retour.txt"`, "service": `,"action":"status","nom":"ssh"`,
		"docker": `,"action":"ps"`, "pare_feu": `,"action":"status"`, "confirmer_changement": `,"id":"0a1b2c3d"`,
		"proposer_plan": `,"resume":"r","etapes":[{"outil":"service","arguments":{"action":"status","nom":"ssh"},"pourquoi":"p"}]`}
	if len(outils) != len(outilsServeursMCP)-1 {
		t.Fatalf("a Servers tool is not covered: %v", outilsServeursMCP)
	}
	for o, args := range outils {
		if txt, _ := appel(o, `{"serveur":"`+a.ID+`"`+args+`}`); txt == errServeurInconnu.Error() {
			t.Errorf("%s refused on its own server", o)
		}
		hors, e1 := appel(o, `{"serveur":"`+b.ID+`"`+args+`}`)
		inconnu, e2 := appel(o, `{"serveur":"ffff"`+args+`}`)
		if !e1 || !e2 || hors != errServeurInconnu.Error() || inconnu != hors {
			t.Errorf("%s outside the perimeter: %v %q / unknown %v %q", o, e1, hors, e2, inconnu)
		}
	}
	if len(memA.commandes) == 0 {
		t.Error("nothing reached the server of the perimeter")
	}
	if n := len(memB.commandes); n > 0 {
		t.Errorf("%d commands reached the server outside the perimeter: %q", n, <-memB.commandes)
	}
	if _, err := os.Stat(filepath.Join(dossierFichiersServeur(b.ID), "retour.txt")); err == nil {
		t.Error("a file fetched from the server outside the perimeter")
	}
	refus := map[string]bool{}
	for _, l := range strings.Split(journalAuditTexte(t), "\n") {
		var x LigneAudit
		if json.Unmarshal([]byte(l), &x) == nil && x.Vers == "serveur:"+b.ID && x.Resultat == "refusé : serveur hors du périmètre" {
			refus[strings.TrimPrefix(x.Action, "serveurs.")] = true
		}
	}
	for o := range outils {
		if !refus[o] {
			t.Errorf("%s on the other server: no refusal in the audit log", o)
		}
	}
}

// TestServeursScenarioConteneur: P4 on the test container, as the owner then an agent would do it: addition
// with the password, the sheet, a read, a real change (a file and a firewall rule), password logins cut, the
// key still working; in less than a minute, the password in no answer and no audit line.
func TestServeursScenarioConteneur(t *testing.T) {
	cible := os.Getenv("ATHANOR_SSH_TEST")
	if cible == "" {
		t.Skip("ATHANOR_SSH_TEST absent (bash .athanor/tools/serveur_ssh.sh start)")
	}
	hote, portTxt, err := net.SplitHostPort(cible)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portTxt)
	s, appelAPI, reponses := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	ctx := context.Background()
	debut := time.Now()

	code, v := appelAPI("/api/serveurs/empreinte", map[string]any{"hote": hote, "port": port})
	if code != 200 {
		t.Fatalf("fingerprint: %d %v", code, v)
	}
	empreinte := v["empreinte"].(string)
	code, v = appelAPI("/api/serveurs/ajouter", map[string]any{"nom": "Conteneur", "hote": hote, "port": port, "utilisateur": "athanor",
		"empreinte": empreinte, "mode": "mot_de_passe", "mot_de_passe_serveur": motDePasseConteneurSSH})
	if code != 200 || v["fiche"] == nil {
		t.Fatalf("addition with the password: %d %v", code, v)
	}
	id := v["serveur"].(map[string]any)["id"].(string)
	l, _ := lireServeurs()
	client, err := s.connecterServeur(ctx, l[0])
	if err != nil {
		t.Fatalf("key login: %v", err)
	}
	t.Cleanup(func() { // password logins back for the other container tests
		defer client.Close()
		if r, err := executerSSH(ctx, client, scriptRetourCoupure, nil, time.Minute); err != nil || r.Code != 0 {
			t.Errorf("password logins not restored: %v %d %s", err, r.Code, r.Sortie)
		}
	})
	_, appel, mcp := serveursSessionTest(t, s, id)
	outil := func(nom, args string) ResultatOutil {
		t.Helper()
		txt, e := appel(nom, `{"serveur":"`+id+`"`+args+`}`)
		var r ResultatOutil
		if e || json.Unmarshal([]byte(jsonDeDonnees(txt)), &r) != nil {
			t.Fatalf("%s %s: %v %q", nom, args, e, txt)
		}
		return r
	}

	// the sheet, then a read
	if txt, e := appel("serveur_fiche", `{"serveur":"`+id+`"}`); e || !strings.Contains(txt, "<server-sheet>") || !strings.Contains(strings.ToLower(txt), "debian") {
		t.Errorf("sheet: %v %q", e, txt)
	}
	if r := outil("fichier_lire", `,"chemin":"/etc/os-release"`); r.Code != 0 || !strings.Contains(r.Sortie, "ID=debian") {
		t.Errorf("read: %+v", r)
	}
	// M89: an agent's change is refused "plan required" before any connection (plans come in M90), so the
	// real change below runs on the code's path, after the hook
	if txt, e := appel("fichier_ecrire", `{"serveur":"`+id+`","chemin":"/tmp/athanor-scenario.txt","contenu":"x"}`); !e || !strings.HasPrefix(txt, "plan required") {
		t.Errorf("a change without a plan: %v %q", e, txt)
	}
	if r := outil("ssh_commande", `,"commande":"test -e /tmp/athanor-scenario.txt && echo present"`); r.Sortie != "" {
		t.Errorf("written without a plan: %q", r.Sortie)
	}
	change := func(nom, args string) ResultatOutil {
		t.Helper()
		r, err := s.appelerOutilServeur(ctx, agentServeurs, id, nom, json.RawMessage(args), Sauvegarde{})
		if err != nil {
			t.Fatalf("%s %s: %v", nom, args, err)
		}
		return r
	}
	// a real change: a file written and read back, a firewall rule added then deleted
	contenu := fmt.Sprintf("scenario %d\n", debut.UnixNano())
	change("fichier_ecrire", `{"chemin":"/tmp/athanor-scenario.txt","contenu":"`+strings.TrimSpace(contenu)+`\n"}`)
	if r := outil("fichier_lire", `,"chemin":"/tmp/athanor-scenario.txt"`); r.Sortie != contenu {
		t.Errorf("file written: %q", r.Sortie)
	}
	r := change("pare_feu", `{"action":"allow","port":8081,"proto":"tcp"}`)
	if r.Code != 0 {
		t.Errorf("ufw allow: %+v", r)
	}
	if r := outil("pare_feu", `,"action":"status"`); !strings.Contains(r.Sortie, "8081/tcp") {
		t.Errorf("ufw rule missing: %+v", r)
	}
	// M93: the agent keeps the change, so the next one of the same kind can run
	if txt, e := appel("confirmer_changement", `{"serveur":"`+id+`","id":"`+r.Annulation+`"}`); e || !strings.Contains(txt, "kept") {
		t.Errorf("confirmer_changement: %v %q", e, txt)
	}
	r = change("pare_feu", `{"action":"delete","port":8081,"proto":"tcp","regle":"allow"}`)
	if _, err := s.confirmerChangement(ctx, "test", id, r.Annulation); err != nil {
		t.Errorf("delete kept: %v", err)
	}
	change("ssh_commande", `{"commande":"rm -f /tmp/athanor-scenario.txt"}`)

	// password logins cut: the password refused, the agent's tools still working with the key
	if code, v := appelAPI("/api/serveurs/"+id, map[string]any{"couper_mot_de_passe": true}); code != 200 || v["serveur"].(map[string]any)["mot_de_passe_coupe"] != true {
		t.Fatalf("cut: %d %v", code, v)
	}
	if c, err := connecterSSH(ctx, AccesSSH{Hote: hote, Port: port, Utilisateur: "athanor", MotDePasse: motDePasseConteneurSSH, Empreinte: empreinte}); err == nil {
		c.Close()
		t.Error("the password still opens the server after the cut")
	}
	if r := outil("ssh_commande", `,"commande":"id -un"`); strings.TrimSpace(r.Sortie) != "athanor" {
		t.Errorf("the key after the cut: %+v", r)
	}
	if d := time.Since(debut); d > time.Minute {
		t.Errorf("scenario in %s (P4: less than a minute)", d.Round(time.Second))
	} else {
		t.Logf("P4 scenario in %s", d.Round(100*time.Millisecond))
	}
	for ou, txt := range map[string]string{"an API answer": reponses.String(), "an MCP answer": mcp.String(), "the audit log": journalAuditTexte(t)} {
		if strings.Contains(txt, motDePasseConteneurSSH) {
			t.Errorf("the server's password in %s", ou)
		}
	}
	if !slices.ContainsFunc(strings.Split(journalAuditTexte(t), "\n"), func(l string) bool { return strings.Contains(l, `"action":"serveurs.pare_feu"`) }) {
		t.Error("the firewall change is not in the audit log")
	}
}
