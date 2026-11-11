package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestServeursAjout: the host fingerprint to confirm, an addition with a vault key (missing or changed
// fingerprint refused, closed infra key 423, no password 409 with where to choose one, native mode asks for
// Athanor's password), an addition with a generated key (public part and command, then Verify), and the
// private keys found in no response, audit line or file of modules/servers.
func TestServeursAjout(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	natif := modeNatif.Load()
	t.Cleanup(func() { modeNatif.Store(natif) })
	modeNatif.Store(false)
	var reponses strings.Builder
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
