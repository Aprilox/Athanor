package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestClasserDeploiement(t *testing.T) {
	sha := strings.Repeat("ab12", 10)
	for _, c := range []struct {
		outil, args string
		classe      Classe
		erreur      string
	}{
		{"deployer", `{"env":"dev","methode":"statique","source":"artefact","commit":"` + strings.ToUpper(sha) + `"}`, ClasseChangement, ""},
		{"deployer", `{"env":"prod","methode":"statique","source":"artefact","dossier":"dist","construire":"npm ci && npm run build","commit":"` + sha + `"}`, ClasseChangement, ""},
		{"deployer", `{"env":"dev","methode":"statique","source":"artefact","construire":"rm -rf / ; npm run build","commit":"` + sha + `"}`, ClasseDestructrice, ""},
		{"deployer", `{"env":"dev","methode":"statique","source":"git","construire":"npm run build","commit":"` + sha + `"}`, 0, "artefact source"},
		{"deployer", `{"env":"dev","methode":"k3s","source":"artefact","commit":"` + sha + `"}`, 0, "statique or compose"},
		{"deployer", `{"env":"dev","methode":"statique","source":"artefact","commit":"` + sha[:12] + `"}`, 0, "full commit hash"},
		{"deployer", `{"env":"qa","methode":"statique","source":"artefact","commit":"` + sha + `"}`, 0, "env:"},
		{"deployer", `{"env":"dev","methode":"statique","source":"artefact","commit":"` + sha + `","serveur":"x"}`, 0, "invalid arguments"},
		{"revenir_version", `{"env":"prod"}`, ClasseChangement, ""},
	} {
		var a map[string]json.RawMessage
		_ = json.Unmarshal([]byte(c.args), &a)
		e, err := classerEtape(c.outil, a)
		if c.erreur != "" {
			if err == nil || !strings.Contains(err.Error(), c.erreur) {
				t.Errorf("%s %s: %v", c.outil, c.args, err)
			}
			continue
		}
		if err != nil || e.Classe != c.classe || strings.Contains(string(e.Arguments), strings.ToUpper(sha)) {
			t.Errorf("%s %s: %v %+v", c.outil, c.args, err, e)
		}
	}
}

// TestDeploiementAgentConteneur (M135): on the test container, the chef ops deploys with a build command only as
// a step of an accepted plan, then goes back; a Dev request in dev "auto" runs the recorded method without
// the owner and is followed by deploiement_etat; prod is never deployed without the owner's accord, by the
// tool (plan waiting, no rule), a dev task's plan, the Dev request or a Dev token calling the tool.
func TestDeploiementAgentConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, sur := serveurConteneurTest(t)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	depot, c1, _ := depotSiteExemple(t)
	// a third commit: dist/ is ignored, so only the build makes it
	if err := os.WriteFile(filepath.Join(depot, ".gitignore"), []byte("dist/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "ignore dist"}} {
		if out, err := exec.Command("git", append([]string{"-C", depot, "-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	c3, _ := gitSortie(depot, "rev-parse", "HEAD")
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m135", Dossier: depot})
	s.mu.Unlock()
	const hote, hoteProd = "m135.test", "m135p.test"
	resoudreTest(t, hote+":80", web)
	for env, e := range map[string]Environnement{"dev": {URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "auto"},
		"prod": {URL: "http://" + hoteProd, Serveurs: []string{id}, Protection: "aucune", Politique: "accord"}} {
		if err := s.acces.DefinirEnvironnement("m135", env, e); err != nil {
			t.Fatal(err)
		}
	}
	nettoyer := "sudo -n rm -rf /srv/athanor/m135-dev /srv/athanor/m135-prod /etc/caddy/sites/" + hote + ".caddy; sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; true"
	sur("ssh_commande", map[string]any{"commande": nettoyer})
	t.Cleanup(func() { sur("ssh_commande", map[string]any{"commande": nettoyer}) })
	page := func(chemin string) string {
		t.Helper()
		r, _ := http.NewRequest("GET", "http://"+web+chemin, nil)
		r.Host = hote
		rep, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer rep.Body.Close()
		b, _ := io.ReadAll(rep.Body)
		return string(b)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	jeton := func(x SessionHub) string {
		t.Helper()
		j, err := s.jetons.CreerSession(x)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	appel := func(j, nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, "Bearer "+j, appelOutil(nom, args))
		if rep.Error != nil {
			return rep.Error.Message, true
		}
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	accepter := func(txt string) {
		t.Helper()
		var p struct{ Plan string }
		if err := json.Unmarshal([]byte(txt), &p); err != nil || p.Plan == "" {
			t.Fatalf("plan: %q", txt)
		}
		if _, _, err := s.deciderApprobation("fenetre", p.Plan, "accepter", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	ops := jeton(SessionHub{Projet: "m135", Mission: "t1", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{id}, Env: "m135/dev", Hotes: []string{hote}})
	dep := `{"env":"dev","methode":"statique","source":"artefact","dossier":"dist","construire":"mkdir -p dist && cp -R site/. dist/ && echo construit-ok > dist/construit.txt","commit":"` + c3 + `"}`

	// the chef ops: refused without a plan, outside its environment; deployed as a step of an accepted plan
	if txt, e := appel(ops, "deployer", dep); !e || !strings.Contains(txt, "plan required") {
		t.Fatalf("without a plan: %v %s", e, txt)
	}
	if txt, e := appel(ops, "deployer", strings.Replace(dep, `"dev"`, `"prod"`, 1)); !e || txt != errEnvInconnu.Error() {
		t.Errorf("prod from a dev task: %v %s", e, txt)
	}
	if txt, e := appel(ops, "proposer_plan", `{"serveur":"`+id+`","resume":"r","etapes":[{"outil":"deployer","arguments":`+strings.Replace(dep, `"dev"`, `"prod"`, 1)+`,"pourquoi":"p"}]}`); !e || !strings.Contains(txt, errEnvInconnu.Error()) {
		t.Errorf("a dev task's plan for prod: %v %s", e, txt)
	}
	txt, e := appel(ops, "proposer_plan", `{"serveur":"`+id+`","resume":"Publier le site","etapes":[{"outil":"deployer","arguments":`+dep+`,"pourquoi":"p"}]}`)
	if e {
		t.Fatalf("plan: %s", txt)
	}
	accepter(txt)
	if txt, e := appel(ops, "deployer", dep); e || !strings.Contains(txt, `"etat": "ok"`) || !strings.Contains(txt, `"auteur": "agent"`) {
		t.Fatalf("deployer: %v %s", e, txt)
	}
	if p := page("/"); !strings.Contains(p, "site-exemple v2") || strings.TrimSpace(page("/construit.txt")) != "construit-ok" {
		t.Fatalf("served after the build: %.200q", p)
	}
	if m := s.acces.Environnements("m135")["dev"].Deploiement; m == nil || m.Dossier != "dist" || !strings.Contains(m.Construire, "construit-ok") {
		t.Errorf("method recorded: %+v", m)
	}
	if txt, e := appel(ops, "deployer", dep); !e || !strings.Contains(txt, "already ran this step") {
		t.Errorf("the step again: %v %s", e, txt)
	}

	// the Dev agent: dev in auto, the recorded method (its build too), no owner
	dev := jeton(SessionHub{Projet: "m135", Mission: "M1", Agent: "pilote", Outils: outilsPilote})
	txt, e = appel(dev, "demander_deploiement", `{"environnement":"dev","commit":"`+c1+`"}`)
	idDep, _, ok := strings.Cut(strings.TrimPrefix(txt, "deployment "), " ")
	if e || !ok || !strings.Contains(txt, "accepted (policy auto) and started") {
		t.Fatalf("demander_deploiement: %v %s", e, txt)
	}
	etat := ""
	for range 240 {
		if txt, e = appel(dev, "deploiement_etat", `{"id":"`+idDep+`"}`); e || !strings.Contains(txt, `"etat": "en_cours"`) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if etat = txt; e || !strings.Contains(etat, `"etat": "ok"`) || !strings.Contains(etat, c1) || !strings.Contains(page("/"), "site-exemple v1") {
		t.Fatalf("deploiement_etat: %v %s", e, etat)
	}
	if l, _ := s.approbations.Liste(); slices.ContainsFunc(l, func(a Approbation) bool { return a.Action == actionDeploiement }) {
		t.Errorf("dev auto asked the owner: %+v", l)
	}
	if txt, e := appel(dev, "deploiement_etat", `{"id":"inconnu"}`); !e || !strings.Contains(txt, "no deployment") {
		t.Errorf("unknown id: %v %s", e, txt)
	}

	// back to the version before, a step of a plan too
	txt, e = appel(ops, "proposer_plan", `{"serveur":"`+id+`","resume":"Revenir","etapes":[{"outil":"revenir_version","arguments":{"env":"dev"},"pourquoi":"p"}]}`)
	if e {
		t.Fatalf("plan: %s", txt)
	}
	accepter(txt)
	if txt, e := appel(ops, "revenir_version", `{"env":"dev"}`); e || !strings.Contains(txt, `"etat": "ok"`) || !strings.Contains(page("/"), "site-exemple v2") {
		t.Fatalf("revenir_version: %v %s", e, txt)
	}

	// prod: never without the owner
	prod := jeton(SessionHub{Projet: "m135", Mission: "t2", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{id}, Env: "m135/prod", Hotes: []string{hoteProd}})
	depProd := strings.Replace(dep, `"dev"`, `"prod"`, 1)
	s.approbations.mu.Lock() // "do not ask again" for this server
	s.approbations.regles = append(s.approbations.regles, reglePlan("m135", id))
	s.approbations.mu.Unlock()
	txt, e = appel(prod, "proposer_plan", `{"serveur":"`+id+`","resume":"Mise en prod","etapes":[{"outil":"deployer","arguments":`+depProd+`,"pourquoi":"p"}]}`)
	if e || !strings.Contains(txt, `"etat": "attente"`) {
		t.Fatalf("prod plan: %v %s", e, txt)
	}
	if txt, e := appel(prod, "deployer", depProd); !e || !strings.Contains(txt, "waits for the owner") {
		t.Errorf("prod before the accord: %v %s", e, txt)
	}
	if txt, e := appel(dev, "demander_deploiement", `{"environnement":"prod","commit":"`+c3+`"}`); !strings.Contains(txt, "has no deployment method") && !strings.Contains(txt, "waiting for the owner") {
		t.Errorf("Dev request for prod: %v %s", e, txt)
	}
	if txt, e := appel(dev, "deployer", depProd); !e {
		t.Errorf("a Dev token called deployer: %s", txt)
	}
	if h := historiqueDeploiements("m135", "prod"); len(h) != 0 {
		t.Errorf("prod deployed: %+v", h)
	}
	if got := sur("ssh_commande", map[string]any{"commande": "test -e /srv/athanor/m135-prod && echo present; true"}).Sortie; got != "" {
		t.Errorf("prod folder on the server: %q", got)
	}
}
