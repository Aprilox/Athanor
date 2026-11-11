package main

import (
	"context"
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
)

func lireTestdata(t *testing.T, nom string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", nom))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func portsTexte(f FicheServeur) string {
	var l []string
	for _, p := range f.Ports {
		l = append(l, strconv.Itoa(p.Port)+"/"+p.Proto+"@"+p.Adresse+"="+p.Processus)
	}
	return strings.Join(l, " ")
}

func logicielsTexte(f FicheServeur) string {
	var l []string
	for _, x := range f.Logiciels {
		if x.Present {
			l = append(l, x.Nom+":"+x.Version)
		}
	}
	return strings.Join(l, " ")
}

// TestAnalyseFiche: recorded outputs (Debian with sudo and systemd, Alpine busybox with OpenRC, no ss
// (/proc/net) and no service manager, no sudo) and broken ones: never a failure, what is missing told.
func TestAnalyseFiche(t *testing.T) {
	cas := []struct {
		fichier, systeme, ports, logiciels, pareFeu, services, conteneurs string
		sudo                                                              bool
		ressources                                                        RessourcesFiche
		remarques                                                         []string
	}{
		{"fiche-debian.txt", "debian 12 Debian GNU/Linux 12 (bookworm) 6.1.0-26-amd64 x86_64 vps-prod-1 93784",
			"22/tcp@0.0.0.0=sshd 22/tcp@::=sshd 53/udp@127.0.0.53=systemd-resolve 80/tcp@0.0.0.0=nginx 443/tcp@0.0.0.0=nginx 5432/tcp@127.0.0.1=postgres 8080/tcp@*=docker-proxy",
			"docker:27.3.1 compose:2.29.7 nginx:1.22.1 ufw:0.36.2", "actif",
			"containerd cron docker nginx postgresql@15-main ssh", "api redis", true,
			RessourcesFiche{4, 7946, 5120, 80506, 55421}, nil},
		{"fiche-alpine.txt", "alpine 3.20.3 Alpine Linux v3.20 6.6.54-0-virt aarch64 petit 512",
			"22/tcp@0.0.0.0= 22/tcp@::= 123/udp@0.0.0.0= 2019/tcp@0.0.0.0=caddy", "caddy:2.8.4", "absent",
			"caddy chronyd sshd sysfs", "", false, RessourcesFiche{1, 481, 294, 3862, 3047}, []string{"No sudo"}},
		{"fiche-sans-ss.txt", "rocky 9.4 Rocky Linux 9.4 (Blue Onyx) 5.14.0-427.13.1.el9_4.x86_64 x86_64 boite 1200",
			"22/tcp@0.0.0.0= 22/tcp@::= 68/udp@0.0.0.0= 8080/tcp@127.0.0.1=", "docker: k3s:1.30.4 kubectl:1.30.4", "absent",
			"containerd k3s-server sshd systemd", "", true, RessourcesFiche{2, 1819, 1000, 17394, 14418},
			[]string{"Neither ss nor netstat", "No service manager", "Docker refused"}},
		{"fiche-sans-sudo.txt", "ubuntu 24.04 Ubuntu 24.04.1 LTS 6.8.0-45-generic x86_64 web-2 3600",
			"22/tcp@0.0.0.0= 443/tcp@*=", "docker:26.1.3 caddy:2.7.6 ufw:0.36.2", "inconnu",
			"caddy ssh", "", false, RessourcesFiche{2, 3911, 2802, 39522, 30500}, []string{"No sudo", "Docker refused"}},
	}
	for _, c := range cas {
		f := analyserFiche(lireTestdata(t, c.fichier))
		s := f.Systeme
		if got := strings.Join([]string{s.ID, s.Version, s.Nom, s.Noyau, s.Architecture, s.NomHote, strconv.FormatInt(s.Allume, 10)}, " "); got != c.systeme {
			t.Errorf("%s system: %q", c.fichier, got)
		}
		if got := portsTexte(f); got != c.ports {
			t.Errorf("%s ports: %q", c.fichier, got)
		}
		if got := logicielsTexte(f); got != c.logiciels {
			t.Errorf("%s software: %q", c.fichier, got)
		}
		if len(f.Logiciels) != len(logicielsFiche) {
			t.Errorf("%s: %d software lines", c.fichier, len(f.Logiciels))
		}
		var noms []string
		for _, x := range f.Conteneurs {
			noms = append(noms, x.Nom)
		}
		if f.PareFeu != c.pareFeu || strings.Join(f.Services, " ") != c.services || strings.Join(noms, " ") != c.conteneurs || f.Sudo != c.sudo || f.Ressources != c.ressources {
			t.Errorf("%s: firewall %q, services %q, containers %q, sudo %v, resources %+v", c.fichier, f.PareFeu, f.Services, noms, f.Sudo, f.Ressources)
		}
		if len(f.Remarques) != len(c.remarques) {
			t.Errorf("%s remarks: %q", c.fichier, f.Remarques)
		}
		for i, r := range c.remarques {
			if i < len(f.Remarques) && !strings.HasPrefix(f.Remarques[i], r) {
				t.Errorf("%s remark %d: %q", c.fichier, i, f.Remarques[i])
			}
		}
		md := markdownFiche(ServeurInventaire{Nom: "VPS", Utilisateur: "athanor", Hote: "h", Port: 22}, f)
		if !strings.Contains(md, s.Nom) || !strings.Contains(md, "## Listening ports") || len(f.Ports) > 0 && !strings.Contains(md, "- 22/tcp on ") {
			t.Errorf("%s FICHE.md:\n%s", c.fichier, md)
		}
	}

	// cut short, empty, garbage: a sheet with its remarks, never a panic
	debian := lireTestdata(t, "fiche-debian.txt")
	coupee := analyserFiche(debian[:strings.Index(debian, "@@logiciels")])
	if coupee.Systeme.ID != "debian" || len(coupee.Ports) != 7 || !strings.HasPrefix(coupee.Remarques[0], "The sheet is incomplete") || coupee.Logiciels == nil {
		t.Errorf("cut sheet: %+v", coupee)
	}
	for _, x := range []string{"", "@@", "@@ports-ss\ntcp LISTEN 0 0 :x :y\nudp\n@@ports-proc\ntcp 0: zz:qq 0 0A\n@@logiciels\n|||\n@@disque\na b c d e f\n@@memoire\nMemTotal:\n"} {
		f := analyserFiche(x)
		if len(f.Ports) != 0 || len(f.Remarques) < 3 || f.Ressources != (RessourcesFiche{}) {
			t.Errorf("garbage %q: %+v", x, f)
		}
	}
}

// TestFicheServeur: the sheet made at the addition (fiche.json, FICHE.md, the server ok), on Refresh,
// unreadable (state unchanged), unreachable (state told, sheet kept), every 6 hours only while the vault
// is unlocked, and gone with its server.
func TestFicheServeur(t *testing.T) {
	s, appel, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	signer, prive := nouvelleCleSSH(t)
	var mu sync.Mutex
	reponse := lireTestdata(t, "fiche-debian.txt")
	srv := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, _ []byte) (string, int) {
		mu.Lock()
		defer mu.Unlock()
		if cmd == commandeFiche {
			return reponse, 0
		}
		return "", 0
	})
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Clé VPS", Famille: "secret", Type: "ssh", Portee: "infra"}, string(prive))
	if err != nil {
		t.Fatal(err)
	}
	code, v := appel("/api/serveurs/ajouter", map[string]any{"nom": "VPS", "hote": srv.hote, "port": srv.port, "utilisateur": "athanor", "empreinte": srv.empreinte, "mode": "coffre", "cle": cle.ID})
	if code != 200 || v["fiche"] == nil || v["fiche_erreur"] != nil {
		t.Fatalf("addition: %d %v", code, v)
	}
	if f := v["fiche"].(map[string]any); f["systeme"].(map[string]any)["id"] != "debian" || len(f["ports"].([]any)) != 7 {
		t.Errorf("sheet of the addition: %v", f)
	}
	id := v["serveur"].(map[string]any)["id"].(string)
	dossier := filepath.Join(dossierModule("servers"), id)
	md, err := os.ReadFile(filepath.Join(dossier, "FICHE.md"))
	if err != nil || !strings.Contains(string(md), "Debian GNU/Linux 12") || !strings.Contains(string(md), "athanor@"+adresseSSH(srv.hote, srv.port)) {
		t.Errorf("FICHE.md: %v %s", err, md)
	}
	if st, err := os.Stat(filepath.Join(dossier, "fiche.json")); err != nil || st.Size() == 0 {
		t.Errorf("fiche.json: %v", err)
	}
	l, _ := lireServeurs()
	premierVu := l[0].Vu
	if l[0].Etat != "ok" || premierVu.IsZero() {
		t.Errorf("after the addition: %+v", l[0])
	}

	// Refresh: another output, the sheet follows
	mu.Lock()
	reponse = lireTestdata(t, "fiche-sans-sudo.txt")
	mu.Unlock()
	code, v = appel("/api/serveurs/"+id, map[string]any{"rafraichir": true})
	if code != 200 || v["fiche"].(map[string]any)["systeme"].(map[string]any)["id"] != "ubuntu" || v["serveur"].(map[string]any)["etat"] != "ok" {
		t.Fatalf("refresh: %d %v", code, v)
	}
	if b, _ := os.ReadFile(filepath.Join(dossier, "fiche.json")); !strings.Contains(string(b), `"ubuntu"`) {
		t.Errorf("fiche.json after refresh: %s", b)
	}
	if code, _ := appel("/api/serveurs/inconnu0", map[string]any{"rafraichir": true}); code != http.StatusNotFound {
		t.Errorf("refresh of an unknown server: %d", code)
	}

	// unreadable output: 502, the state and the sheet stay
	mu.Lock()
	reponse = "bash: sh: command not found"
	mu.Unlock()
	if code, v := appel("/api/serveurs/"+id, map[string]any{"rafraichir": true}); code != http.StatusBadGateway || v["erreur"] != errFicheIllisible.Error() {
		t.Errorf("unreadable: %d %v", code, v)
	}
	if l, _ := lireServeurs(); l[0].Etat != "ok" {
		t.Errorf("state after an unreadable sheet: %s", l[0].Etat)
	}

	// every 6 hours: not while the sheet is young, not with the vault locked, then made again
	mu.Lock()
	reponse = lireTestdata(t, "fiche-alpine.txt")
	mu.Unlock()
	ctx := context.Background()
	s.rafraichirFichesAnciennes(ctx, 6*time.Hour)
	if b, _ := os.ReadFile(filepath.Join(dossier, "fiche.json")); !strings.Contains(string(b), `"ubuntu"`) {
		t.Error("a young sheet was made again")
	}
	vieux := time.Now().Add(-7 * time.Hour)
	_ = os.Chtimes(filepath.Join(dossier, "fiche.json"), vieux, vieux)
	s.coffre.Verrouiller()
	s.rafraichirFichesAnciennes(ctx, 6*time.Hour)
	if b, _ := os.ReadFile(filepath.Join(dossier, "fiche.json")); !strings.Contains(string(b), `"ubuntu"`) {
		t.Error("a sheet was made with the vault locked")
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	s.rafraichirFichesAnciennes(ctx, 6*time.Hour)
	if b, _ := os.ReadFile(filepath.Join(dossier, "fiche.json")); !strings.Contains(string(b), `"alpine"`) {
		t.Errorf("old sheet not made again: %s", b)
	}

	// unreachable: 502, the state told (the home tile counts it), the last sheet kept
	ferme := l[0]
	ferme.Port = func() int {
		x, _ := net.Listen("tcp", "127.0.0.1:0")
		defer x.Close()
		return x.Addr().(*net.TCPAddr).Port
	}()
	_ = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { l[0].Port = ferme.Port; return l, nil })
	if code, v := appel("/api/serveurs/"+id, map[string]any{"rafraichir": true}); code != http.StatusBadGateway || v["erreur"] != errServeurInjoignable.Error() {
		t.Errorf("unreachable: %d %v", code, v)
	}
	if l, _ := lireServeurs(); l[0].Etat != "injoignable" || l[0].Vu.Before(premierVu) {
		t.Errorf("state when unreachable: %+v", l[0])
	}
	if r := (moduleServeurs{}).Resume(s); r.Besoin != 1 || r.Etat != "attention" {
		t.Errorf("tile: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dossier, "FICHE.md")); err != nil {
		t.Errorf("last sheet lost: %v", err)
	}

	// a changed host fingerprint (C43): its own state, one audit line however many refreshes, an alert in the bell
	_ = modifierServeurs(func(x []ServeurInventaire) ([]ServeurInventaire, error) {
		x[0].Port, x[0].EmpreinteHote = l[0].Port, "SHA256:"+strings.Repeat("A", 43)
		return x, nil
	})
	for range 2 {
		appel("/api/serveurs/"+id, map[string]any{"rafraichir": true})
	}
	if l2, _ := lireServeurs(); l2[0].Etat != "empreinte_changee" {
		t.Errorf("state when the fingerprint changed: %+v", l2[0])
	}
	if n := strings.Count(journalAuditTexte(t), `"action":"empreinte_changee"`); n != 1 {
		t.Errorf("%d audit lines for the changed fingerprint", n)
	}
	if el := (moduleServeurs{}).Elements(s); len(el) != 1 || el[0].Attente == nil || el[0].Attente.Page != "#serveur/"+id || len(alertesDe(el)) != 1 {
		t.Errorf("bell: %+v", el)
	}
	if r := (moduleServeurs{}).Resume(s); r.Besoin != 1 {
		t.Errorf("tile with a changed fingerprint: %+v", r)
	}

	// removed: its sheet goes, and a refresh finishing late writes none
	req := httptest.NewRequest("DELETE", "/api/serveurs/"+id, nil)
	req.SetPathValue("id", id)
	s.apiServeurSupprimer(httptest.NewRecorder(), req)
	if _, err := os.Stat(dossier); !os.IsNotExist(err) {
		t.Errorf("sheet left after the removal: %v", err)
	}
	if _, err := s.rafraichirFiche(ctx, l[0]); err != errServeurInconnu {
		t.Error("a removed server's sheet was made")
	}
	if _, err := os.Stat(dossier); !os.IsNotExist(err) {
		t.Errorf("a removed server got a sheet: %v", err)
	}
	if slices.ContainsFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.ID == cle.ID }) == false {
		t.Error("the vault key left with the server")
	}
}
