package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// depotCompose: a git repository whose commits are the given trees (path → content), one commit each.
func depotCompose(t *testing.T, arbres ...map[string]string) (string, []string) {
	t.Helper()
	depot := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", depot, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "core.autocrlf=false"}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	var commits []string
	for i, a := range arbres {
		git("rm", "-rq", "--ignore-unmatch", ".")
		for p, c := range a {
			f := filepath.Join(depot, filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		git("add", "-A")
		git("commit", "-q", "--allow-empty", "-m", "v"+string(rune('1'+i)))
		commits = append(commits, git("rev-parse", "HEAD"))
	}
	return depot, commits
}

// TestDetecterMethode (M134): a compose file or a Dockerfile → compose, else statique; with only a Dockerfile,
// a compose.yaml is generated from its EXPOSE ports.
func TestDetecterMethode(t *testing.T) {
	depot, c := depotCompose(t, map[string]string{
		"site/index.html":        "<p>v1</p>",
		"api/Dockerfile":         "FROM scratch\nexpose 8080 9000/udp\nEXPOSE 8080 $PORT\n",
		"web/compose.yaml":       "services: {}\n",
		"old/docker-compose.yml": "services: {}\n",
		"vide/x.txt":             "",
	})
	for dossier, want := range map[string]string{"": "statique", "site": "statique", "api": "compose", "web": "compose", "old": "compose"} {
		if m, err := detecterMethode(depot, c[0], dossier); err != nil || m != want {
			t.Errorf("%q: %q %v, want %q", dossier, m, err, want)
		}
	}
	if _, err := detecterMethode(depot, c[0], "absent"); err == nil {
		t.Error("a missing folder is not refused")
	}
	if g, err := composeGenere(depot, c[0], "web"); g != "" || err != nil {
		t.Errorf("own compose file: %q %v", g, err)
	}
	g, err := composeGenere(depot, c[0], "api")
	y, _ := base64.StdEncoding.DecodeString(g)
	want := "# Written by Athanor: the project has a Dockerfile and no compose file.\nservices:\n  app:\n    build: .\n    env_file: .env\n    restart: unless-stopped\n    ports:\n      - \"127.0.0.1:8080:8080\"\n      - \"127.0.0.1:9000:9000/udp\"\n"
	if err != nil || string(y) != want {
		t.Errorf("generated compose:\n got  %q %v\n want %q", y, err, want)
	}
	if _, err := composeGenere(depot, c[0], "vide"); err == nil || !strings.Contains(err.Error(), "compose.yaml or a Dockerfile") {
		t.Errorf("no compose file nor Dockerfile: %v", err)
	}
}

// TestFichierEnvCompose (M134): the code entries granted to this environment only, quoted, sorted; a value
// Compose cannot hold is refused.
func TestFichierEnvCompose(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	accorder := func(nom, valeur, portee, env, variable string) {
		t.Helper()
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nom, Famille: "secret", Type: "jeton", Portee: portee}, valeur)
		if err == nil {
			_, err = s.acces.Ajouter("m134-env", e, env, variable, "")
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if v, err := s.fichierEnvCompose("m134-env", "dev"); v != "" || err != nil {
		t.Errorf("no grant: %q %v", v, err)
	}
	accorder("db", "p@ss $HOME `x`", "code", "dev", "DB_PASSWORD")
	accorder("api", "cle-api", "code", "dev", "API_KEY")
	accorder("prod", "valeur-prod", "code", "prod", "PROD_ONLY")
	accorder("infra", "valeur-infra", "infra", "dev", "INFRA_KEY")
	if v, err := s.fichierEnvCompose("m134-env", "dev"); err != nil || v != "API_KEY='cle-api'\nDB_PASSWORD='p@ss $HOME `x`'\n" {
		t.Errorf(".env of dev: %q %v", v, err)
	}
	accorder("quote", "l'apostrophe", "code", "dev", "QUOTE")
	if v, err := s.fichierEnvCompose("m134-env", "dev"); err == nil || v != "" || !strings.Contains(err.Error(), "QUOTE") || strings.Contains(err.Error(), "apostrophe") {
		t.Errorf("value with a quote: %q %v", v, err)
	}
}

// TestDeploiementComposeConteneur (M134): on the test container (fake docker), the release extracted, its .env
// 0600 root from the environment's grants, `docker compose -p m134-dev up -d --build` run in it, then the
// switch; a failed up leaves the version served as it was and starts it again; going back runs up in the
// previous release; the granted value appears in no answer, history, audit line or docker argument.
func TestDeploiementComposeConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, appel := serveurConteneurTest(t)
	app := func(v string, extra ...string) map[string]string {
		a := map[string]string{"app/compose.yaml": "services:\n  web:\n    build: .\n    ports: [\"8080:80\"]\n", "app/Dockerfile": "FROM nginx\n", "app/version.txt": v}
		for _, f := range extra {
			a["app/"+f] = "1"
		}
		return a
	}
	racine := app("v4")
	racine["app/config.json"] = `{"services":{"web":{"privileged":true,"volumes":[{"type":"bind","source":"/","target":"/host"}]}}}`
	depot, c := depotCompose(t, app("v1"), app("v2"), app("v3", "ECHEC"), racine)
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m134", Dossier: depot})
	s.mu.Unlock()
	const hote, base, temoin = "m134.test", "/srv/athanor/m134-dev", "temoin-compose-7311"
	resoudreTest(t, hote+":80", web)
	if err := s.acces.DefinirEnvironnement("m134", "dev", Environnement{URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "DB m134", Famille: "secret", Type: "jeton", Portee: "code"}, temoin)
	if err == nil {
		_, err = s.acces.Ajouter("m134", e, "dev", "DB_PASSWORD", "")
	}
	if err != nil {
		t.Fatal(err)
	}
	sur := func(cmd string) string { return appel("ssh_commande", map[string]any{"commande": cmd}).Sortie }
	nettoyer := "sudo -n rm -rf " + base + "; : | sudo -n tee /var/log/faux-docker.log >/dev/null; true"
	sur(nettoyer)
	t.Cleanup(func() { sur(nettoyer) })
	ctx := context.Background()
	journal := func() string {
		return sur("grep -A1 'compose -p.* up' /var/log/faux-docker.log; : | sudo -n tee /var/log/faux-docker.log >/dev/null")
	}

	d1, err := s.deployer(ctx, "m134", "dev", c[0], OptionsDeploiement{Config: &ConfigDeploiement{Methode: "compose", Source: "artefact", Dossier: "app"}, Auteur: "proprietaire"})
	if err != nil || d1.Etat != "ok" || d1.Methode != "compose" {
		t.Fatalf("v1: %v %+v", err, d1)
	}
	r1 := base + "/releases/" + d1.Version + "/app"
	if got, want := journal(), "docker compose -p m134-dev up -d --build\n  dans "+r1+"\n"; got != want {
		t.Errorf("docker for v1:\n got  %q\n want %q", got, want)
	}
	if got, want := sur("sudo -n stat -c '%a %U' "+r1+"/.env && sudo -n sha256sum "+r1+"/.env | cut -c1-64 && cd "+base+" && readlink current"), "600 root\n"+sha256Hex("DB_PASSWORD='"+temoin+"'\n")+"\nreleases/"+d1.Version+"\n"; got != want { // the tool masks vault values
		t.Errorf(".env and link of v1:\n got  %q\n want %q", got, want)
	}
	d2, err := s.deployer(ctx, "m134", "dev", c[1], OptionsDeploiement{Auteur: "relais"}) // the recorded method
	r2 := base + "/releases/" + d2.Version + "/app"
	if err != nil || journal() != "docker compose -p m134-dev up -d --build\n  dans "+r2+"\n" {
		t.Fatalf("v2: %v %+v", err, d2)
	}
	// a failed up: the release removed, v2 started again, nothing switched
	d3, err := s.deployer(ctx, "m134", "dev", c[2], OptionsDeploiement{Auteur: "proprietaire"})
	if err == nil || d3.Etat != "echec" || !strings.Contains(err.Error(), "docker compose up failed") || !strings.Contains(err.Error(), "essai d'échec") {
		t.Fatalf("failed up: %v %+v", err, d3)
	}
	r3 := base + "/releases/" + d3.Version + "/app"
	if got, want := journal(), "docker compose -p m134-dev up -d --build\n  dans "+r3+"\ndocker compose -p m134-dev up -d --build\n  dans "+r2+"\n"; got != want {
		t.Errorf("docker after the failed up:\n got  %q\n want %q", got, want)
	}
	if got, want := sur("cd "+base+" && readlink current previous && ls releases | wc -l"), "releases/"+d2.Version+"\nreleases/"+d1.Version+"\n2\n"; got != want {
		t.Errorf("after the failed up: %q, want %q", got, want)
	}
	// C69: a configuration that gives root, without the owner's accord (the relay): refused before any up
	d4, err := s.deployer(ctx, "m134", "dev", c[3], OptionsDeploiement{Auteur: "relais"})
	if err == nil || d4.Etat != "echec" || !strings.Contains(err.Error(), "refused without the owner's accord") || !strings.Contains(err.Error(), "service web: mounts the host's /; service web: privileged") {
		t.Fatalf("root by compose: %v %+v", err, d4)
	}
	// annulled: the version served (v2) started again, the refused release removed, nothing switched
	if got, want := journal()+sur("cd "+base+" && readlink current && ls releases | wc -l"), "docker compose -p m134-dev up -d --build\n  dans "+r2+"\nreleases/"+d2.Version+"\n2\n"; got != want {
		t.Errorf("after the refused configuration: %q, want %q", got, want)
	}
	// going back: up in the previous release (v1)
	rr, err := s.revenir(ctx, "m134", "dev", "proprietaire")
	if err != nil || rr.Etat != "ok" || rr.Version != d1.Version || rr.Methode != "compose" {
		t.Fatalf("back to v1: %v %+v", err, rr)
	}
	if got, want := journal(), "docker compose -p m134-dev up -d --build\n  dans "+r1+"\n"; got != want {
		t.Errorf("docker when going back:\n got  %q\n want %q", got, want)
	}
	// witness: the value is nowhere but in the .env files
	h, _ := json.Marshal(historiqueDeploiements("m134", "dev"))
	reponses, _ := json.Marshal([]any{d1, d2, d3, rr})
	for nom, texte := range map[string]string{"history": string(h), "answers": string(reponses), "docker": sur("cat /var/log/faux-docker.log"), "processes": sur("ps -eo args")} {
		if strings.Contains(texte, temoin) {
			t.Errorf("the granted value is in the %s", nom)
		}
	}
	for _, d := range []string{dossierModule("servers"), dossierJournalAudit()} {
		_ = filepath.WalkDir(d, func(p string, x fs.DirEntry, err error) error {
			if err == nil && !x.IsDir() {
				if b, _ := os.ReadFile(p); strings.Contains(string(b), temoin) {
					t.Errorf("the granted value is in %s", p)
				}
			}
			return nil
		})
	}
}
