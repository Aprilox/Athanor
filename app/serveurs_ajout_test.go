package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// serveursAjoutTest: a server with its vault, an empty Servers module, local mode, and a POST caller
// (path, body → status, JSON) whose answers are all kept.
func serveursAjoutTest(t *testing.T) (*Serveur, func(string, map[string]any) (int, map[string]any), *strings.Builder) {
	t.Helper()
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	natif := modeNatif.Load()
	t.Cleanup(func() { modeNatif.Store(natif) })
	modeNatif.Store(false)
	reponses := &strings.Builder{}
	appel := func(chemin string, corps map[string]any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest("POST", chemin, strings.NewReader(string(b)))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		if s.session != "" {
			r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.Write(w.Body.Bytes())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	return s, appel, reponses
}

// motDePasseCoffreTest: Athanor gets a password and its vault is open (infra keys can be kept).
func motDePasseCoffreTest(t *testing.T, s *Serveur) {
	t.Helper()
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	s.config.MotDePasseHash, s.session = hacherMotDePasse("mdp-test"), "session"
}

// TestServeursAjoutMotDePasse: the server's password given once (refused, then a generated key put in
// authorized_keys and the server reached again with it), the password found nowhere afterwards, then
// cutting password logins: refused before a key login, the script's failures, undone when the key no
// longer opens the server, then done.
func TestServeursAjoutMotDePasse(t *testing.T) {
	s, appel, reponses := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	attente := attenteRechargeSSH
	attenteRechargeSSH = 10 * time.Millisecond
	t.Cleanup(func() { attenteRechargeSSH = attente })
	temoin := "temoin-" + aleatoire(8) // built at run time: the test binary itself does not hold it

	var srv *serveurSSHMemoire
	var mu sync.Mutex
	codeCoupure, refuserApres, retours := 0, false, 0
	srv = lancerSSHMemoire(t, nil, temoin, func(cmd string, _ []byte) (string, int) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(cmd, "PasswordAuthentication no"):
			if codeCoupure == 0 && refuserApres {
				srv.mu.Lock()
				srv.autorisees = nil
				srv.mu.Unlock()
			}
			return "", codeCoupure
		case strings.Contains(cmd, `rm -f "$F" && recharger`):
			retours++
		case strings.Contains(cmd, "authorized_keys"):
			if debut := strings.Index(cmd, "'ssh-ed25519 "); debut >= 0 {
				ligne := cmd[debut+1:]
				k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(ligne[:strings.Index(ligne, "'")]))
				if err != nil {
					return err.Error(), 1
				}
				srv.autoriser(k)
			}
		}
		return "", 0
	})
	corps := func(m map[string]any) map[string]any {
		c := map[string]any{"nom": "VPS", "hote": srv.hote, "port": srv.port, "utilisateur": "athanor", "empreinte": srv.empreinte, "mode": "mot_de_passe"}
		for k, v := range m {
			c[k] = v
		}
		return c
	}
	entrees := len(s.coffre.Liste())

	// 1) no password, a wrong one: refused, nothing kept
	if code, v := appel("/api/serveurs/ajouter", corps(nil)); code != http.StatusBadRequest || v["erreur"] != errServeurMdpVide.Error() {
		t.Errorf("no password: %d %v", code, v)
	}
	if code, v := appel("/api/serveurs/ajouter", corps(map[string]any{"mot_de_passe_serveur": "mauvais"})); code != http.StatusConflict || v["erreur"] != errServeurMdpRefuse.Error() {
		t.Errorf("wrong password: %d %v", code, v)
	}
	if l, _ := lireServeurs(); len(l) != 0 || len(s.coffre.Liste()) != entrees {
		t.Fatalf("a refused addition left something: %d servers, %d vault entries", len(l), len(s.coffre.Liste()))
	}

	// 2) the right password: key generated, put once in authorized_keys, the server reached with it
	code, v := appel("/api/serveurs/ajouter", corps(map[string]any{"mot_de_passe_serveur": temoin}))
	if code != 200 {
		t.Fatalf("password: %d %v", code, v)
	}
	x := v["serveur"].(map[string]any)
	id := x["id"].(string)
	if x["etat"] != "ok" || x["mot_de_passe_coupe"] != false {
		t.Errorf("added server: %v", x)
	}
	if i := slices.IndexFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.ID == x["cle"] }); i < 0 || s.coffre.Liste()[i].Source != "serveurs:"+id || s.coffre.Liste()[i].Portee != "infra" {
		t.Errorf("its vault key: %v", x["cle"])
	}
	var vues []string
	for len(srv.commandes) > 0 {
		if c := <-srv.commandes; c != commandeFiche { // the sheet that follows the addition
			vues = append(vues, c)
		}
	}
	if len(vues) != 1 || !strings.Contains(vues[0], "chmod 700 ~/.ssh") || !strings.Contains(vues[0], "grep -qxF 'ssh-ed25519 ") {
		t.Errorf("authorized_keys command: %q", vues)
	}

	// 3) cutting password logins: not before a key login (generated key not verified yet)
	code, v = appel("/api/serveurs/ajouter", map[string]any{"nom": "VPS 2", "hote": srv.hote, "port": srv.port, "utilisateur": "athanor", "empreinte": srv.empreinte, "mode": "generee"})
	if code != 200 {
		t.Fatalf("generated key: %d %v", code, v)
	}
	if code, v := appel("/api/serveurs/"+v["serveur"].(map[string]any)["id"].(string), map[string]any{"couper_mot_de_passe": true}); code != http.StatusConflict || v["erreur"] != errCoupureAvantCle.Error() {
		t.Errorf("cut before a key login: %d %v", code, v)
	}
	for c, e := range map[int]error{3: errCoupureSudo, 4: errCoupureConfig, 5: errCoupureIgnoree, 6: errCoupureRecharge} {
		mu.Lock()
		codeCoupure = c
		mu.Unlock()
		if code, v := appel("/api/serveurs/"+id, map[string]any{"couper_mot_de_passe": true}); code != http.StatusConflict || v["erreur"] != e.Error() {
			t.Errorf("script code %d: %d %v", c, code, v)
		}
	}
	// the key no longer opens the server after the change: undone through the session still open
	srv.mu.Lock()
	gardees := slices.Clone(srv.autorisees)
	srv.mu.Unlock()
	mu.Lock()
	codeCoupure, refuserApres = 0, true
	mu.Unlock()
	if code, v := appel("/api/serveurs/"+id, map[string]any{"couper_mot_de_passe": true}); code != http.StatusConflict || v["erreur"] != errCoupureAnnulee.Error() || retours != 1 {
		t.Errorf("undone cut: %d %v (%d undo)", code, v, retours)
	}
	srv.mu.Lock()
	srv.autorisees = gardees
	srv.mu.Unlock()
	mu.Lock()
	refuserApres = false
	mu.Unlock()
	if code, v := appel("/api/serveurs/"+id, map[string]any{"couper_mot_de_passe": true}); code != 200 || v["serveur"].(map[string]any)["mot_de_passe_coupe"] != true {
		t.Errorf("cut: %d %v", code, v)
	}
	if l, _ := lireServeurs(); len(l) != 2 || !l[slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })].MotDePasseCoupe {
		t.Errorf("inventory after the cut: %+v", l)
	}

	// 4) the password: in no response, nor any file of Athanor's folder (inventory, audit log, vault…)
	if strings.Contains(reponses.String(), temoin) {
		t.Error("the server's password is in a response")
	}
	var audit strings.Builder
	for _, d := range []string{dossierAthanor(), dossierJournalAudit(), dossierModule("servers")} {
		_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				b, _ := os.ReadFile(p)
				if strings.Contains(string(b), temoin) {
					t.Errorf("the server's password is in %s", p)
				}
				if strings.HasPrefix(p, dossierJournalAudit()) {
					audit.Write(b)
				}
			}
			return nil
		})
	}
	for _, a := range []string{"serveur_ajoute", "(mot_de_passe)", "mot_de_passe_coupe", "coupure du mot de passe refusée"} {
		if !strings.Contains(audit.String(), a) {
			t.Errorf("audit log without %q", a)
		}
	}
}

// TestServeursAjout: the host fingerprint to confirm, an addition with a vault key (missing or changed
// fingerprint refused, closed infra key 423, no password 409 with where to choose one, native mode asks for
// Athanor's password), an addition with a generated key (public part and command, then Verify), and the
// private keys found in no response, audit line or file of modules/servers.
func TestServeursAjout(t *testing.T) {
	s, appel, reponses := serveursAjoutTest(t)

	signer, prive := nouvelleCleSSH(t)
	srv := lancerSSHMemoire(t, signer.PublicKey(), "", nil)
	base := func(m map[string]any) map[string]any {
		c := map[string]any{"nom": "VPS", "hote": srv.hote, "port": srv.port, "utilisateur": "athanor", "empreinte": srv.empreinte}
		for k, v := range m {
			c[k] = v
		}
		return c
	}

	// 1) the fingerprint to confirm; a bad address, an unreachable port
	if code, v := appel("/api/serveurs/empreinte", map[string]any{"hote": srv.hote, "port": srv.port}); code != 200 || v["empreinte"] != srv.empreinte {
		t.Fatalf("fingerprint: %d %v", code, v)
	}
	for _, c := range []map[string]any{{"hote": "-oProxyCommand=x"}, {"hote": "a b"}, {"hote": srv.hote, "port": 70000}} {
		if code, _ := appel("/api/serveurs/empreinte", c); code != http.StatusBadRequest {
			t.Errorf("fingerprint of %v: %d", c, code)
		}
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	ferme := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if code, v := appel("/api/serveurs/empreinte", map[string]any{"hote": "127.0.0.1", "port": ferme}); code != http.StatusBadGateway || v["erreur"] != errServeurInjoignable.Error() {
		t.Errorf("closed port: %d %v", code, v)
	}

	// 2) no password for Athanor yet: no infra key, 409 naming where to choose one
	if code, v := appel("/api/serveurs/ajouter", base(map[string]any{"mode": "generee"})); code != http.StatusConflict || v["erreur"] != errCoffreSansMdp.Error() {
		t.Fatalf("generated key without a password: %d %v", code, v)
	}
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	s.config.MotDePasseHash, s.session = hacherMotDePasse("mdp-test"), "session"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Clé VPS", Famille: "secret", Type: "ssh", Portee: "infra"}, string(prive))
	if err != nil {
		t.Fatal(err)
	}
	pasSSH, _ := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton", Famille: "secret", Type: "jeton", Portee: "code"}, "x")
	cleCode, _ := s.coffre.Ajouter(EntreeCoffre{Nom: "Clé code", Famille: "secret", Type: "ssh", Portee: "code"}, string(prive))

	// 3) mode coffre: refusals, then added
	refus := []struct {
		nom   string
		corps map[string]any
		code  int
		err   string
	}{
		{"no fingerprint", base(map[string]any{"mode": "coffre", "cle": cle.ID, "empreinte": ""}), 400, errServeurEmpreinte.Error()},
		{"changed fingerprint", base(map[string]any{"mode": "coffre", "cle": cle.ID, "empreinte": "SHA256:autre"}), 409, errEmpreinteChangee.Error()},
		{"unknown mode", base(map[string]any{"mode": "devine", "cle": cle.ID}), 400, errServeurMode.Error()},
		{"bad user", base(map[string]any{"mode": "coffre", "cle": cle.ID, "utilisateur": "a;b"}), 400, errServeurUtilisateur.Error()},
		{"bad role", base(map[string]any{"mode": "coffre", "cle": cle.ID, "roles": []string{"test"}}), 400, "Unknown server role"},
		{"not an SSH key", base(map[string]any{"mode": "coffre", "cle": pasSSH.ID}), 400, errServeurPasCle.Error()},
		{"code-scope key (C35)", base(map[string]any{"mode": "coffre", "cle": cleCode.ID}), 400, errServeurCleCode.Error()},
		{"unknown entry", base(map[string]any{"mode": "coffre", "cle": "inconnue"}), 404, errCoffreInconnu.Error()},
	}
	for _, r := range refus {
		if code, v := appel("/api/serveurs/ajouter", r.corps); code != r.code || v["erreur"] != r.err {
			t.Errorf("%s: %d %v", r.nom, code, v)
		}
	}
	s.coffre.Fermer()
	if code, v := appel("/api/serveurs/ajouter", base(map[string]any{"mode": "coffre", "cle": cle.ID})); code != http.StatusLocked {
		t.Errorf("closed infra key: %d %v", code, v)
	}
	s.coffre.Rouvrir()
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	if l, _ := lireServeurs(); len(l) != 0 {
		t.Fatalf("a refused addition was kept: %+v", l)
	}
	code, v := appel("/api/serveurs/ajouter", base(map[string]any{"mode": "coffre", "cle": cle.ID, "roles": []string{"prod", "dev", "prod"}}))
	if code != 200 {
		t.Fatalf("vault key: %d %v", code, v)
	}
	if code, v := appel("/api/coffre/"+cle.ID, map[string]any{"portee": "code", "mot_de_passe": "mdp-test"}); code != http.StatusConflict || v["args"] == nil {
		t.Errorf("a server's key moved to code (C35): %d %v", code, v)
	}
	if x := v["serveur"].(map[string]any); x["etat"] != "ok" || x["cle"] != cle.ID || x["empreinte_hote"] != srv.empreinte || len(x["roles"].([]any)) != 2 {
		t.Errorf("added server: %v", x)
	}

	// 4) native mode: an existing infra key needs Athanor's password
	modeNatif.Store(true)
	for _, m := range []string{"", "mauvais"} {
		if code, _ := appel("/api/serveurs/ajouter", base(map[string]any{"mode": "coffre", "cle": cle.ID, "mot_de_passe": m})); code != http.StatusUnauthorized {
			t.Errorf("native mode, password %q: %d", m, code)
		}
	}
	if code, v := appel("/api/serveurs/ajouter", base(map[string]any{"nom": "VPS 2", "mode": "coffre", "cle": cle.ID, "mot_de_passe": "mdp-test"})); code != 200 {
		t.Errorf("native mode with the password: %d %v", code, v)
	}

	// 5) generated key: public part and command, refused until it is in authorized_keys, then Verify
	code, v = appel("/api/serveurs/ajouter", base(map[string]any{"nom": "VPS 3", "mode": "generee"}))
	if code != 200 {
		t.Fatalf("generated key: %d %v", code, v)
	}
	x := v["serveur"].(map[string]any)
	id, pub := x["id"].(string), v["cle_publique"].(string)
	if x["etat"] != "inconnu" || !strings.HasPrefix(pub, "ssh-ed25519 ") || !strings.Contains(v["commande"].(string), pub) {
		t.Fatalf("generated key answer: %v", v)
	}
	var generee EntreeCoffre
	for _, e := range s.coffre.Liste() {
		if e.ID == x["cle"] {
			generee = e
		}
	}
	if generee.Portee != "infra" || generee.Type != "ssh" || generee.Source != "serveurs:"+id {
		t.Errorf("generated vault entry: %+v", generee)
	}
	if code, v := appel("/api/serveurs/ajouter", map[string]any{"id": id}); code != http.StatusConflict || v["erreur"] != errServeurCleRefusee.Error() {
		t.Errorf("Verify before the key is in: %d %v", code, v)
	}
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub))
	if err != nil {
		t.Fatal(err)
	}
	srv.autoriser(k)
	if code, v := appel("/api/serveurs/ajouter", map[string]any{"id": id}); code != 200 || v["serveur"].(map[string]any)["etat"] != "ok" {
		t.Errorf("Verify (native mode, its own key: no password): %d %v", code, v)
	}
	if code, _ := appel("/api/serveurs/ajouter", map[string]any{"id": "abc"}); code != http.StatusNotFound {
		t.Errorf("Verify an unknown server: %d", code)
	}
	if l, _ := lireServeurs(); len(l) != 3 {
		t.Errorf("inventory: %d servers", len(l))
	}

	// 6) the private keys: in no response, audit line or file of modules/servers
	privee2, err := s.coffre.Valeur(generee.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ecrits strings.Builder
	for _, d := range []string{dossierJournalAudit(), dossierModule("servers")} {
		_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				b, _ := os.ReadFile(p)
				ecrits.Write(b)
			}
			return nil
		})
	}
	if !strings.Contains(ecrits.String(), "serveur_ajoute") {
		t.Error("no audit line for the addition")
	}
	for _, pem := range []string{string(prive), privee2} {
		lignes := strings.Split(strings.TrimSpace(pem), "\n")
		for _, ligne := range lignes[2 : len(lignes)-1] { // line 1 is the format's constant prefix
			if len(ligne) > 20 && (strings.Contains(reponses.String(), ligne) || strings.Contains(ecrits.String(), ligne)) {
				t.Fatalf("a private key line leaked (%d chars)", len(ligne))
			}
		}
	}
}

// TestServeursMotDePasseConteneur: on the test server's real sshd (ATHANOR_SSH_TEST): addition with the
// password, the key put once (.ssh 700), then password logins cut (password refused, key accepted), and
// restored at the end for the other container tests.
func TestServeursMotDePasseConteneur(t *testing.T) {
	cible := os.Getenv("ATHANOR_SSH_TEST")
	if cible == "" {
		t.Skip("ATHANOR_SSH_TEST absent (bash .athanor/tools/serveur_ssh.sh start)")
	}
	hote, portTxt, err := net.SplitHostPort(cible)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portTxt)
	s, appel, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	ctx := context.Background()
	debut := time.Now() // P4: from the address to the complete sheet in less than a minute
	code, v := appel("/api/serveurs/empreinte", map[string]any{"hote": hote, "port": port})
	if code != 200 {
		t.Fatalf("fingerprint: %d %v", code, v)
	}
	empreinte := v["empreinte"].(string)
	code, v = appel("/api/serveurs/ajouter", map[string]any{"nom": "Conteneur", "hote": hote, "port": port, "utilisateur": "athanor",
		"empreinte": empreinte, "mode": "mot_de_passe", "mot_de_passe_serveur": motDePasseConteneurSSH})
	if code != 200 {
		t.Fatalf("addition with the password: %d %v", code, v)
	}
	if d := time.Since(debut); d > time.Minute {
		t.Errorf("addition and sheet in %s (P4: less than a minute)", d)
	}
	// the sheet of the real server: Debian, sshd on 22 with its program (sudo), ufw and the fake docker
	if v["fiche"] == nil {
		t.Fatalf("no sheet after the addition: %v", v["fiche_erreur"])
	}
	fiche := v["fiche"].(map[string]any)
	j, _ := json.Marshal(fiche)
	var f FicheServeur
	_ = json.Unmarshal(j, &f)
	if f.Systeme.ID != "debian" || !f.Sudo || f.Ressources.Processeurs < 1 || f.Ressources.MemoireMo < 1 || f.Ressources.DisqueMo < 1 ||
		!slices.ContainsFunc(f.Ports, func(p PortFiche) bool { return p.Port == 22 && p.Proto == "tcp" && p.Processus == "sshd" }) ||
		!strings.Contains(logicielsTexte(f), "ufw:") || !strings.Contains(logicielsTexte(f), "docker:") || !slices.Contains(f.Services, "sshd") {
		t.Errorf("sheet of the container: %s", j)
	}
	x := v["serveur"].(map[string]any)
	l, _ := lireServeurs()
	srv := l[0]
	client, err := s.connecterServeur(ctx, srv)
	if err != nil {
		t.Fatalf("key login: %v", err)
	}
	t.Cleanup(func() { // password logins back for TestConnexionSSHConteneur
		defer client.Close()
		if r, err := executerSSH(ctx, client, scriptRetourCoupure, nil, time.Minute); err != nil || r.Code != 0 {
			t.Errorf("password logins not restored: %v %d %s", err, r.Code, r.Sortie)
		}
	})

	// the key once in authorized_keys, even put again
	prive, _ := s.coffre.Valeur(srv.Cle)
	signer, err := ssh.ParsePrivateKey([]byte(prive))
	if err != nil {
		t.Fatal(err)
	}
	if err := poserCle(ctx, client, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))+" athanor-"+srv.ID); err != nil {
		t.Fatal(err)
	}
	r, err := executerSSH(ctx, client, "grep -c athanor-"+srv.ID+" ~/.ssh/authorized_keys; stat -c %a ~/.ssh ~/.ssh/authorized_keys", nil, 10*time.Second)
	if err != nil || strings.Fields(string(r.Sortie)) == nil || strings.Join(strings.Fields(string(r.Sortie)), " ") != "1 700 600" {
		t.Errorf("authorized_keys: %v %q", err, r.Sortie)
	}

	// cut: password refused, key accepted
	if code, v := appel("/api/serveurs/"+x["id"].(string), map[string]any{"couper_mot_de_passe": true}); code != 200 || v["serveur"].(map[string]any)["mot_de_passe_coupe"] != true {
		t.Fatalf("cut: %d %v", code, v)
	}
	acces := AccesSSH{Hote: hote, Port: port, Utilisateur: "athanor", MotDePasse: motDePasseConteneurSSH, Empreinte: empreinte}
	if c, err := connecterSSH(ctx, acces); err == nil {
		c.Close()
		t.Error("the password still opens the server after the cut")
	}
	if err := s.essayerCle(httptest.NewRequest("GET", "/", nil), srv); err != nil {
		t.Errorf("the key after the cut: %v", err)
	}
	r, _ = executerSSH(ctx, client, "cat /etc/ssh/sshd_config.d/00-athanor.conf", nil, 10*time.Second)
	if !strings.Contains(string(r.Sortie), "PasswordAuthentication no") {
		t.Errorf("drop-in file: %q", r.Sortie)
	}
}
