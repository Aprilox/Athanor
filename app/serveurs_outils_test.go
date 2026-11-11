package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const temoinOutils = "témoin-valeur-du-coffre-4242"

// temoinEchappe: a vault value JSON escapes (B54): it must not reach the audit log in its escaped form either.
const temoinEchappe = `témoin-échappé-4343&<x>"q\z`

// journalAuditTexte: every line of the hub's audit log, as written.
func journalAuditTexte(t *testing.T) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dossierJournalAudit(), "*.jsonl"))
	var b strings.Builder
	for _, f := range m {
		x, _ := os.ReadFile(f)
		b.Write(x)
	}
	return b.String()
}

// outilsServeursTest: an open vault holding the server's key and a witness value, the server in the inventory.
func outilsServeursTest(t *testing.T, hote string, port int, empreinte string, prive []byte) (*Serveur, ServeurInventaire) {
	t.Helper()
	s, _, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Clé VPS", Famille: "secret", Type: "ssh", Portee: "infra"}, string(prive))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Témoin", Famille: "secret", Type: "jeton", Portee: "code"}, temoinOutils); err != nil {
		t.Fatal(err)
	}
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Témoin échappé", Famille: "secret", Type: "jeton", Portee: "code"}, temoinEchappe); err != nil {
		t.Fatal(err)
	}
	srv := ServeurInventaire{ID: "ab12", Nom: "VPS", Hote: hote, Port: port, Utilisateur: "athanor", Cle: cle.ID, EmpreinteHote: empreinte, Etat: "ok"}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { return append(l, srv), nil }); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dossierJournalAudit())
	t.Cleanup(func() { _ = os.RemoveAll(dossierJournalAudit()) })
	return s, srv
}

// TestServeursOutils: the tools against the in-memory server (exec and SFTP): arguments checked, paths
// quoted, sizes and the local folder enforced, every call in the audit log with the vault's values masked.
func TestServeursOutils(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	fichier := strings.Repeat("ligne\n", 10)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, entree []byte) (string, int) {
		switch {
		case strings.Contains(cmd, "dort"):
			time.Sleep(3 * time.Second)
		case strings.Contains(cmd, "gros-fichier"):
			return strings.Repeat("x", tailleMaxFichier+1), 0
		case strings.Contains(cmd, "head -c"):
			return fichier, 0
		case strings.Contains(cmd, "mktemp"):
			return string(entree), 0
		case strings.Contains(cmd, "beaucoup"):
			return strings.Repeat("y", sortieMaxOutil+10), 0
		case strings.Contains(cmd, "temoin"):
			return "la valeur " + temoinOutils + " sort", 0
		case strings.Contains(cmd, "echoue"):
			return "raté", 5
		}
		return "ok:" + cmd, 0
	})
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	var err error
	ctx := context.Background()
	appel := func(outil string, args any) (ResultatOutil, error) {
		t.Helper()
		b, _ := json.Marshal(args)
		return s.appelerOutilServeur(ctx, "test", srv.ID, outil, b)
	}
	derniere := func() string { // the last command the server ran
		t.Helper()
		var c string
		for {
			select {
			case c = <-mem.commandes:
			default:
				return c
			}
		}
	}

	// unknown server, unknown tool, unknown argument
	if _, err := s.appelerOutilServeur(ctx, "test", "ffff", "ssh_commande", json.RawMessage(`{"commande":"id"}`)); !errors.Is(err, errServeurInconnu) {
		t.Errorf("unknown server: %v", err)
	}
	if _, err := appel("formater", map[string]any{}); err == nil {
		t.Error("unknown tool accepted")
	}
	if _, err := appel("ssh_commande", map[string]any{"commande": "id", "hote": "ailleurs"}); err == nil {
		t.Error("unknown argument accepted")
	}

	// ssh_commande: output and code, time limit, output cut
	if r, err := appel("ssh_commande", map[string]any{"commande": "uname -a"}); err != nil || r.Sortie != "ok:uname -a" || r.Code != 0 {
		t.Errorf("command: %v %+v", err, r)
	}
	if r, err := appel("ssh_commande", map[string]any{"commande": "echoue"}); err != nil || r.Code != 5 {
		t.Errorf("failing command: %v %+v", err, r)
	}
	if _, err := appel("ssh_commande", map[string]any{"commande": "dort", "delai_s": 1}); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Errorf("time limit: %v", err)
	}
	if r, _ := appel("ssh_commande", map[string]any{"commande": "beaucoup"}); len(r.Sortie) != sortieMaxOutil || !r.Coupee {
		t.Errorf("output not cut: %d %v", len(r.Sortie), r.Coupee)
	}
	if _, err := appel("ssh_commande", map[string]any{"commande": "  "}); err == nil {
		t.Error("empty command accepted")
	}
	// a value of the vault coming back from the server is masked, for the caller and in the log
	if r, _ := appel("ssh_commande", map[string]any{"commande": "temoin"}); strings.Contains(r.Sortie, temoinOutils) || !strings.Contains(r.Sortie, masqueAudit) {
		t.Errorf("witness value not masked: %q", r.Sortie)
	}
	derniere()

	// fichier_lire / fichier_ecrire: absolute path, passed quoted; sizes; sudo
	piege := "/tmp/a'; touch pwned; '$(touch pwned2)`touch pwned3`"
	if r, err := appel("fichier_lire", map[string]any{"chemin": piege}); err != nil || r.Sortie != fichier {
		t.Errorf("read: %v %+v", err, r)
	}
	if c := derniere(); !strings.Contains(c, citerShell(piege)) || strings.HasPrefix(c, "sudo") {
		t.Errorf("path not quoted: %s", c)
	}
	for _, p := range []string{"etc/passwd", "", "/tmp/a\nb", "/tmp/\x00", "/tmp/a‮b", "/tmp/a​b", "/tmp/a b", "/tmp/a b"} {
		if _, err := appel("fichier_lire", map[string]any{"chemin": p}); err == nil {
			t.Errorf("path %q accepted", p)
		}
	}
	if _, err := appel("fichier_lire", map[string]any{"chemin": "/gros-fichier"}); err == nil || !strings.Contains(err.Error(), "sftp_recuperer") {
		t.Errorf("file too large: %v", err)
	}
	if r, err := appel("fichier_ecrire", map[string]any{"chemin": "/etc/app.conf", "contenu": "cle=" + temoinOutils, "sudo": true}); err != nil || r.Code != 0 {
		t.Errorf("write: %v %+v", err, r)
	}
	if c := derniere(); !strings.HasPrefix(c, "sudo -n sh -c ") || !strings.Contains(c, "mv -f") {
		t.Errorf("write command: %s", c)
	}
	if r, err := appel("fichier_ecrire", map[string]any{"chemin": "/tmp/b.env", "contenu": "SPECIAL=" + temoinEchappe + "\n"}); err != nil || strings.Contains(r.Sortie, "4343") {
		t.Errorf("write with an escaped witness: %v %+v", err, r)
	}
	derniere()
	if _, err := appel("fichier_ecrire", map[string]any{"chemin": "/etc/app.conf"}); err == nil {
		t.Error("write without contenu accepted")
	}
	if _, err := appel("fichier_ecrire", map[string]any{"chemin": "/x", "contenu": strings.Repeat("z", tailleMaxFichier+1)}); err == nil {
		t.Error("content too large accepted")
	}

	// SFTP: only modules/servers/<id>/fichiers/, no "..", no absolute path, no link
	local := dossierFichiersServeur(srv.ID)
	if err := os.MkdirAll(filepath.Join(local, "conf"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "conf", "site.conf"), []byte("server {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := appel("sftp_envoyer", map[string]any{"local": "conf/site.conf", "distant": "/site.conf"}); err != nil || !strings.Contains(r.Sortie, "10 bytes") {
		t.Fatalf("send: %v %+v", err, r)
	}
	if r, err := appel("sftp_recuperer", map[string]any{"distant": "/site.conf", "local": "retour/site.conf"}); err != nil || r.Code != 0 {
		t.Fatalf("fetch: %v %+v", err, r)
	}
	if b, err := os.ReadFile(filepath.Join(local, "retour", "site.conf")); err != nil || string(b) != "server {}\n" {
		t.Errorf("fetched file: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(local, "retour", "site.conf.athanor-part")); err == nil {
		t.Error("partial file left")
	}
	dehors := filepath.Join(filepath.Dir(local), "dehors.txt")
	for _, l := range []string{"../dehors.txt", "conf/../../dehors.txt", dehors, "/etc/passwd", ""} {
		if _, err := appel("sftp_recuperer", map[string]any{"distant": "/site.conf", "local": l}); err == nil {
			t.Errorf("local %q accepted", l)
		}
		if _, err := appel("sftp_envoyer", map[string]any{"local": l, "distant": "/x"}); err == nil {
			t.Errorf("local %q sent", l)
		}
	}
	if _, err := os.Stat(dehors); err == nil {
		t.Error("a file was written outside fichiers/")
	}
	if _, err := appel("sftp_envoyer", map[string]any{"local": "conf/site.conf", "distant": "relatif"}); err == nil {
		t.Error("relative remote path accepted")
	}
	if _, err := appel("sftp_recuperer", map[string]any{"distant": "/absent", "local": "a"}); err == nil {
		t.Error("missing remote file fetched")
	}
	cible := filepath.Join(filepath.Dir(local), "cible")
	_ = os.MkdirAll(cible, 0o700)
	lien := filepath.Join(local, "lien")
	err = os.Symlink(cible, lien)
	if err != nil && runtime.GOOS == "windows" { // no symlink privilege: a junction is a link too
		err = exec.Command("cmd", "/c", "mklink", "/J", lien, cible).Run()
	}
	if err == nil {
		if _, err := appel("sftp_recuperer", map[string]any{"distant": "/site.conf", "local": "lien/x"}); err == nil || !strings.Contains(err.Error(), "links") {
			t.Errorf("link followed: %v", err)
		}
	} else {
		t.Logf("link not testable here: %v", err)
	}

	// service, docker, pare_feu: a trapped argument is refused before any command reaches the server
	derniere()
	for _, c := range []struct {
		outil string
		args  map[string]any
	}{
		{"service", map[string]any{"action": "restart", "nom": "nginx; reboot"}},
		{"service", map[string]any{"action": "restart", "nom": "$(reboot)"}},
		{"service", map[string]any{"action": "restart", "nom": "nginx\nreboot"}},
		{"service", map[string]any{"action": "rm -rf", "nom": "nginx"}},
		{"docker", map[string]any{"action": "logs", "nom": "web;id"}},
		{"docker", map[string]any{"action": "pull", "nom": "nginx$(id)"}},
		{"docker", map[string]any{"action": "restart", "nom": "-v"}},
		{"docker", map[string]any{"action": "compose_up", "dossier": "/srv/app;id"}},
		{"docker", map[string]any{"action": "compose_up", "dossier": "/srv/$(id)"}},
		{"docker", map[string]any{"action": "compose_up", "dossier": "/srv/app\nid"}},
		{"docker", map[string]any{"action": "compose_up", "dossier": "srv/app"}},
		{"docker", map[string]any{"action": "logs", "nom": "web", "lignes": 100000}},
		{"pare_feu", map[string]any{"action": "allow", "port": 0}},
		{"pare_feu", map[string]any{"action": "allow", "port": 65536}},
		{"pare_feu", map[string]any{"action": "allow", "port": "80;id"}},
		{"pare_feu", map[string]any{"action": "allow", "port": 80, "proto": "tcp;id"}},
		{"pare_feu", map[string]any{"action": "delete", "port": 80}},
		{"pare_feu", map[string]any{"action": "deny", "port": srv.Port}},
		{"pare_feu", map[string]any{"action": "delete", "port": srv.Port, "proto": "tcp", "regle": "allow"}},
	} {
		if _, err := appel(c.outil, c.args); err == nil {
			t.Errorf("%s %v accepted", c.outil, c.args)
		}
	}
	if c := derniere(); c != "" {
		t.Errorf("a refused call reached the server: %q", c)
	}
	// valid ones: fixed script, every argument quoted; a change tries to make the sheet again
	for _, c := range []struct {
		outil, attendu string
		args           map[string]any
		modifie        bool
	}{
		{"service", ` sh 'restart' 'nginx'`, map[string]any{"action": "restart", "nom": "nginx"}, true},
		{"service", ` sh 'status' 'postgresql@15-main'`, map[string]any{"action": "status", "nom": "postgresql@15-main"}, false},
		{"docker", ` sh 'logs' 'web' '50'`, map[string]any{"action": "logs", "nom": "web", "lignes": 50}, false},
		{"docker", ` sh 'pull' 'ghcr.io/org/app:1.2' '200'`, map[string]any{"action": "pull", "nom": "ghcr.io/org/app:1.2"}, true},
		{"docker", ` sh 'compose_up' '/srv/mon app' '200'`, map[string]any{"action": "compose_up", "dossier": "/srv/mon app"}, true},
		{"pare_feu", ` sh 'allow' '' '8080/tcp'`, map[string]any{"action": "allow", "port": 8080, "proto": "tcp"}, true},
		{"pare_feu", ` sh 'deny' '' '` + strconv.Itoa(srv.Port) + `/udp'`, map[string]any{"action": "deny", "port": srv.Port, "proto": "udp"}, true},
		{"pare_feu", ` sh 'delete' 'deny' '25'`, map[string]any{"action": "delete", "port": 25, "regle": "deny"}, true},
	} {
		r, err := appel(c.outil, c.args)
		if err != nil || !strings.Contains(r.Sortie, c.attendu+"\n") && !strings.HasSuffix(r.Sortie, c.attendu) {
			t.Errorf("%s %v: %v %q", c.outil, c.args, err, r.Sortie)
		}
		if strings.Contains(r.Sortie, "sheet could not be refreshed") != c.modifie {
			t.Errorf("%s %v: sheet refresh %v expected: %q", c.outil, c.args, c.modifie, r.Sortie)
		}
	}
	// a program the sheet says is missing: a clear message, nothing run
	d := filepath.Join(dossierModule("servers"), srv.ID)
	_ = os.MkdirAll(d, 0o700)
	b, _ := json.Marshal(FicheServeur{Logiciels: []LogicielFiche{{Nom: "docker", Present: true}, {Nom: "compose"}, {Nom: "ufw"}}})
	if err := os.WriteFile(filepath.Join(d, "fiche.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	derniere()
	if _, err := appel("pare_feu", map[string]any{"action": "status"}); err == nil || !strings.Contains(err.Error(), "ufw is not installed on VPS") {
		t.Errorf("ufw missing: %v", err)
	}
	if _, err := appel("docker", map[string]any{"action": "compose_ps", "dossier": "/srv/app"}); err == nil || !strings.Contains(err.Error(), "compose is not installed") {
		t.Errorf("compose missing: %v", err)
	}
	if c := derniere(); c != "" {
		t.Errorf("a command ran for a missing program: %q", c)
	}
	if _, err := appel("docker", map[string]any{"action": "ps"}); err != nil {
		t.Errorf("docker ps with docker present: %v", err)
	}

	// the audit log: one line per call, the vault's values (witness and private key) never in it
	journal := journalAuditTexte(t)
	for _, o := range []string{"serveurs.ssh_commande", "serveurs.fichier_lire", "serveurs.fichier_ecrire", "serveurs.sftp_envoyer", "serveurs.sftp_recuperer",
		"serveurs.service", "serveurs.docker", "serveurs.pare_feu"} {
		if !strings.Contains(journal, `"action":"`+o+`"`) {
			t.Errorf("no audit line for %s", o)
		}
	}
	if !strings.Contains(journal, `"vers":"serveur:`+srv.ID+`"`) || !strings.Contains(journal, "code 5") {
		t.Errorf("audit lines without server or code: %s", journal)
	}
	for _, x := range []string{temoinOutils, "4343", "PRIVATE KEY", strings.Repeat("y", auditMaxOutil+1)} {
		if strings.Contains(journal, x) {
			t.Errorf("audit log holds %.40q", x)
		}
	}
	if v, _ := s.verifierAudit(); !v.OK {
		t.Errorf("audit chain: %+v", v)
	}
}

// TestServeursOutilsConteneur: the same tools against the real sshd of the test container (the server is
// added with its password, as the owner would).
func TestServeursOutilsConteneur(t *testing.T) {
	cible := os.Getenv("ATHANOR_SSH_TEST")
	if cible == "" {
		t.Skip("ATHANOR_SSH_TEST absent (bash .athanor/tools/serveur_ssh.sh start)")
	}
	hote, portTxt, err := net.SplitHostPort(cible)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portTxt)
	s, appelAPI, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Témoin", Famille: "secret", Type: "jeton", Portee: "code"}, temoinOutils); err != nil {
		t.Fatal(err)
	}
	_, v := appelAPI("/api/serveurs/empreinte", map[string]any{"hote": hote, "port": port})
	code, v := appelAPI("/api/serveurs/ajouter", map[string]any{"nom": "Conteneur", "hote": hote, "port": port, "utilisateur": "athanor",
		"empreinte": v["empreinte"], "mode": "mot_de_passe", "mot_de_passe_serveur": motDePasseConteneurSSH})
	if code != 200 {
		t.Fatalf("addition: %d %v", code, v)
	}
	id := v["serveur"].(map[string]any)["id"].(string)
	_ = os.RemoveAll(dossierJournalAudit())
	t.Cleanup(func() { _ = os.RemoveAll(dossierJournalAudit()) })
	ctx := context.Background()
	appel := func(outil string, args any) ResultatOutil {
		t.Helper()
		b, _ := json.Marshal(args)
		r, err := s.appelerOutilServeur(ctx, "test", id, outil, b)
		if err != nil {
			t.Fatalf("%s %s: %v", outil, b, err)
		}
		return r
	}

	if r := appel("ssh_commande", map[string]any{"commande": "id -un; exit 3"}); strings.TrimSpace(r.Sortie) != "athanor" || r.Code != 3 {
		t.Errorf("command: %+v", r)
	}
	// a trapped path is a file name, never a command
	piege := "/tmp/a'; touch pwned; '$(touch pwned2)`touch pwned3`"
	if r := appel("fichier_ecrire", map[string]any{"chemin": piege, "contenu": "x=" + temoinOutils + "\n"}); r.Code != 0 {
		t.Fatalf("write: %+v", r)
	}
	if r := appel("fichier_lire", map[string]any{"chemin": piege}); r.Sortie != "x="+masqueAudit+"\n" {
		t.Errorf("read back: %q", r.Sortie)
	}
	if r := appel("ssh_commande", map[string]any{"commande": "ls -a ~ /tmp | grep -c -x -e 'pwned[23]*' -e '.*[.]athanor[.].*'"}); strings.TrimSpace(r.Sortie) != "0" {
		t.Errorf("injection or temporary file left: %q", r.Sortie)
	}
	// sudo: a root file replaced in place, its mode and owner kept
	appel("ssh_commande", map[string]any{"commande": "echo ancien | sudo -n tee /etc/athanor-test.conf >/dev/null && sudo -n chmod 640 /etc/athanor-test.conf"})
	if r := appel("fichier_ecrire", map[string]any{"chemin": "/etc/athanor-test.conf", "contenu": "nouveau\n", "sudo": true}); r.Code != 0 {
		t.Errorf("write with sudo: %+v", r)
	}
	if r := appel("ssh_commande", map[string]any{"commande": "stat -c '%a %U' /etc/athanor-test.conf; sudo -n cat /etc/athanor-test.conf; sudo -n rm -f /etc/athanor-test.conf"}); r.Sortie != "640 root\nnouveau\n" {
		t.Errorf("root file: %q", r.Sortie)
	}
	if r := appel("fichier_ecrire", map[string]any{"chemin": "/tmp", "contenu": "x"}); r.Code == 0 || !strings.Contains(r.Sortie, "is a directory") {
		t.Errorf("write on a folder: %+v", r)
	}
	if r := appel("ssh_commande", map[string]any{"commande": "ls /tmp | grep -c athanor || true"}); strings.TrimSpace(r.Sortie) != "0" {
		t.Errorf("write on a folder left a file: %q", r.Sortie)
	}
	if r := appel("fichier_ecrire", map[string]any{"chemin": "/etc/refuse.conf", "contenu": "x"}); r.Code == 0 {
		t.Errorf("write without sudo in /etc: %+v", r)
	}
	// C32: a link to a root file 0640 is written through: the link stays, the target keeps its mode and owner
	appel("ssh_commande", map[string]any{"commande": "printf 'v1\\n' | sudo -n tee /etc/athanor-cible.conf >/dev/null; sudo -n chmod 640 /etc/athanor-cible.conf; sudo -n ln -sf /etc/athanor-cible.conf /etc/athanor-lien.conf"})
	if r := appel("fichier_ecrire", map[string]any{"chemin": "/etc/athanor-lien.conf", "contenu": "v2\n", "sudo": true}); r.Code != 0 {
		t.Errorf("write through a link: %+v", r)
	}
	if r := appel("ssh_commande", map[string]any{"commande": "stat -c '%F %U' /etc/athanor-lien.conf; stat -c '%F %a %U' /etc/athanor-cible.conf; sudo -n cat /etc/athanor-cible.conf; ls /etc | grep -c athanor-cible.conf.athanor; sudo -n rm -f /etc/athanor-lien.conf /etc/athanor-cible.conf"}); r.Sortie != "symbolic link root\nregular file 640 root\nv2\n0\n" {
		t.Errorf("link written through: %q", r.Sortie)
	}
	// C41: behind a published port, the port sshd itself listens on is guarded too
	if port != 22 {
		if _, err := s.appelerOutilServeur(ctx, "test", id, "pare_feu", json.RawMessage(`{"action":"deny","port":22,"proto":"tcp"}`)); err == nil || !strings.Contains(err.Error(), "SSH port") {
			t.Errorf("deny on sshd's own port: %v", err)
		}
	}
	// SFTP there and back
	local := dossierFichiersServeur(id)
	_ = os.MkdirAll(local, 0o700)
	if err := os.WriteFile(filepath.Join(local, "envoi.txt"), []byte("contenu sftp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appel("sftp_envoyer", map[string]any{"local": "envoi.txt", "distant": "/tmp/recu.txt"})
	appel("sftp_recuperer", map[string]any{"distant": "/tmp/recu.txt", "local": "retour.txt"})
	if b, _ := os.ReadFile(filepath.Join(local, "retour.txt")); string(b) != "contenu sftp\n" {
		t.Errorf("SFTP round trip: %q", b)
	}
	appel("ssh_commande", map[string]any{"commande": "rm -f /tmp/recu.txt " + citerShell(piege)})
	// C40: sending onto a link to a 0600 file keeps the link and the mode
	appel("ssh_commande", map[string]any{"commande": "printf 'S=1\\n' > /tmp/vrai.env; chmod 600 /tmp/vrai.env; ln -sf /tmp/vrai.env /tmp/lien.env"})
	appel("sftp_envoyer", map[string]any{"local": "envoi.txt", "distant": "/tmp/lien.env"})
	if r := appel("ssh_commande", map[string]any{"commande": "stat -c '%F' /tmp/lien.env; stat -c '%F %a' /tmp/vrai.env; cat /tmp/vrai.env; rm -f /tmp/lien.env /tmp/vrai.env"}); r.Sortie != "symbolic link\nregular file 600\ncontenu sftp\n" {
		t.Errorf("SFTP onto a link: %q", r.Sortie)
	}

	// service (SysV here), docker (the image's fake one logs its calls), ufw for real; a change makes the sheet again
	if r := appel("service", map[string]any{"action": "status", "nom": "ssh"}); r.Code != 0 || !strings.Contains(r.Sortie, "sshd is running") {
		t.Errorf("service status: %+v", r)
	}
	appel("ssh_commande", map[string]any{"commande": "mkdir -p '/tmp/app 1'"})
	fiche := filepath.Join(dossierModule("servers"), id, "fiche.json")
	_ = os.Remove(fiche)
	for _, a := range []map[string]any{{"action": "logs", "nom": "web", "lignes": 50}, {"action": "pull", "nom": "nginx:1.27"}, {"action": "compose_up", "dossier": "/tmp/app 1"}} {
		if r := appel("docker", a); r.Code != 0 {
			t.Errorf("docker %v: %+v", a, r)
		}
	}
	if _, err := os.Stat(fiche); err != nil {
		t.Errorf("sheet not made again after a change: %v", err)
	}
	r := appel("ssh_commande", map[string]any{"commande": "cat /var/log/faux-docker.log"})
	for _, l := range []string{"docker logs --tail 50 web\n", "docker pull nginx:1.27\n", "docker compose up -d\n"} {
		if !strings.Contains(r.Sortie, l) {
			t.Errorf("docker did not run %q: %q", l, r.Sortie)
		}
	}
	if r := appel("pare_feu", map[string]any{"action": "allow", "port": 8080, "proto": "tcp"}); r.Code != 0 {
		t.Errorf("ufw allow: %+v", r)
	}
	if r := appel("pare_feu", map[string]any{"action": "status"}); !strings.Contains(r.Sortie, "Status: inactive") || !strings.Contains(r.Sortie, "ufw allow 8080/tcp") {
		t.Errorf("ufw status: %+v", r)
	}
	appel("pare_feu", map[string]any{"action": "delete", "port": 8080, "proto": "tcp", "regle": "allow"})
	if r := appel("pare_feu", map[string]any{"action": "status"}); strings.Contains(r.Sortie, "8080") {
		t.Errorf("ufw rule not deleted: %+v", r)
	}

	journal := journalAuditTexte(t)
	if strings.Contains(journal, temoinOutils) || strings.Contains(journal, motDePasseConteneurSSH) || strings.Contains(journal, "PRIVATE KEY") {
		t.Error("a secret is in the audit log")
	}
	if strings.Count(journal, `"action":"serveurs.`) < 9 {
		t.Errorf("audit lines missing: %s", journal)
	}
}
