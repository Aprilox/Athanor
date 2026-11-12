package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestServeursPlans (M90): a change runs only as a step of a plan the owner accepted, once, with exactly its
// arguments (canonical), before the plan expires; destructive or prod: an accord every time, never a rule; a
// change on a dev server: "do not ask again for this server"; a refused, expired or stopped plan runs nothing.
func TestServeursPlans(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	var tarEchoue atomic.Bool
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, _ []byte) (string, int) {
		if tarEchoue.Load() && strings.Contains(cmd, "tar -czf") {
			return "tar: srv/vieux: Cannot stat", 2
		}
		return "ok:" + cmd, 0
	})
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}}
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	maintenant := time.Now().UTC()
	s.approbations.Maintenant = func() time.Time { return maintenant }
	mux := http.NewServeMux()
	s.routes(mux)
	jeton, err := s.jetons.CreerSession(SessionHub{Projet: "demo", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{srv.ID}})
	if err != nil {
		t.Fatal(err)
	}
	appel := func(nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args))
		if rep.Error != nil {
			return rep.Error.Message, true
		}
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	// proposer: a plan of the given steps on ab12, its approval's id
	proposer := func(etapes string) (string, string) {
		t.Helper()
		txt, e := appel("proposer_plan", `{"serveur":"ab12","resume":"Mettre à jour nginx","etapes":[`+etapes+`]}`)
		var r struct{ Plan, Etat string }
		if e || json.Unmarshal([]byte(txt), &r) != nil || r.Plan == "" {
			t.Fatalf("proposer_plan: %v %s", e, txt)
		}
		return r.Plan, r.Etat
	}
	decider := func(id, decision string) error {
		_, _, err := s.deciderApprobation("fenetre", id, decision, "", "", "")
		return err
	}
	vider := func() int {
		n := 0
		for len(mem.commandes) > 0 {
			<-mem.commandes
			n++
		}
		return n
	}
	restart := `{"outil":"ssh_commande","arguments":{"commande":"systemctl restart nginx","delai_s":30},"pourquoi":"recharger la conf"}`

	// a plan waits for the owner: its step is refused with the plan's state, nothing reaches the server
	plan, etat := proposer(restart)
	if etat != "attente" {
		t.Fatalf("new plan: %s", etat)
	}
	ap, _ := s.approbations.Lire(plan)
	if ap.Action != actionPlanServeur || ap.Env != "dev" || ap.Details["regle"] != "oui" || ap.Details["classe"] != "changement" || ap.Details["nom"] != "VPS" {
		t.Errorf("approval: %+v", ap)
	}
	if exp, _ := time.Parse(time.RFC3339, ap.Expire); exp.Sub(maintenant) > dureePlan+time.Second {
		t.Errorf("a plan expires in 2 h, not at %s", ap.Expire)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","delai_s":30,"commande":"systemctl restart nginx"}`); !e || !strings.Contains(txt, "waits for the owner's decision") {
		t.Errorf("step before the accord: %v %q", e, txt)
	}
	// accepted: the step runs once, its arguments in any order; a changed step and a replay are refused
	if err := decider(plan, "accepter"); err != nil {
		t.Fatal(err)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"systemctl restart nginx; rm -rf /"}`); !e || !strings.Contains(txt, "propose a plan") {
		t.Errorf("changed step: %v %q", e, txt)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"systemctl restart nginx"}`); !e || !strings.Contains(txt, "plan required") {
		t.Errorf("step with another delay: %v %q", e, txt)
	}
	if n := vider(); n != 0 {
		t.Errorf("%d refused calls reached the server", n)
	}
	if txt, e := appel("ssh_commande", `{"delai_s":30, "commande":"systemctl restart nginx","serveur":"ab12"}`); e || !strings.Contains(txt, "ok:") {
		t.Errorf("accepted step: %v %q", e, txt)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","delai_s":30,"commande":"systemctl restart nginx"}`); !e || !strings.Contains(txt, "already ran this step") {
		t.Errorf("replay: %v %q", e, txt)
	}
	if n := vider(); n != 1 {
		t.Errorf("the accepted step ran %d times", n)
	}
	if p, _ := s.lirePlan(plan); p.Etapes[0].Executee == "" || p.Etapes[0].Etat != "ok" || p.Etapes[0].Essais != 1 {
		t.Errorf("step state after the call (C52): %+v", p.Etapes[0])
	}

	// destructive: no rule possible; refused → nothing runs
	rm := `{"outil":"ssh_commande","arguments":{"commande":"rm -rf /srv/vieux"},"pourquoi":"place","sauvegarde":["/srv/vieux"]}`
	plan2, _ := proposer(rm)
	if ap, _ := s.approbations.Lire(plan2); ap.Details["regle"] != "non" || ap.Details["classe"] != "destructrice" {
		t.Errorf("destructive plan: %+v", ap)
	}
	if err := decider(plan2, "toujours"); err == nil || err.Error() != errPlanSansRegle.Error() {
		t.Errorf("a rule on a destructive plan: %v", err)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"rm -rf /srv/vieux"}`); !e || !strings.Contains(txt, "waits for the owner") {
		t.Errorf("destructive without an accord: %v %q", e, txt)
	}
	if err := decider(plan2, "refuser"); err != nil {
		t.Fatal(err)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"rm -rf /srv/vieux"}`); !e || !strings.Contains(txt, "was refused") {
		t.Errorf("refused plan: %v %q", e, txt)
	}

	// expired: an accepted plan's step no longer runs after 2 h
	plan3, _ := proposer(`{"outil":"ssh_commande","arguments":{"commande":"systemctl restart cron"},"pourquoi":"p"}`)
	_ = decider(plan3, "accepter")
	maintenant = maintenant.Add(dureePlan + time.Minute)
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"systemctl restart cron"}`); !e || !strings.Contains(txt, "has expired") {
		t.Errorf("expired plan: %v %q", e, txt)
	}
	// the emergency stop's refusal reaches an accepted plan too
	plan4, _ := proposer(`{"outil":"ssh_commande","arguments":{"commande":"systemctl restart cron"},"pourquoi":"p"}`)
	_ = decider(plan4, "accepter")
	if l, _ := s.approbations.RefuserTout("fenetre"); len(l) != 1 || l[0].ID != plan4 {
		t.Errorf("emergency stop: %+v", l)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"systemctl restart cron"}`); !e || !strings.Contains(txt, "was refused") {
		t.Errorf("plan after the emergency stop: %v %q", e, txt)
	}
	if n := vider(); n != 0 {
		t.Errorf("%d refused calls reached the server", n)
	}

	// M92: an accepted step archives its paths first, its output (and the audit log) says where; a failed
	// archive runs nothing; a destructive step without backup says why
	rmSauve := `{"outil":"ssh_commande","arguments":{"commande":"rm -rf /srv/vieux"},"pourquoi":"place","sauvegarde":["/srv/vieux/"]}`
	plan8, _ := proposer(rmSauve)
	_ = decider(plan8, "accepter")
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"rm -rf /srv/vieux"}`); e || !strings.Contains(txt, "backup: ") || !strings.Contains(txt, "(/srv/vieux)") {
		t.Errorf("step with a backup: %v %q", e, txt)
	}
	if c1, c2 := <-mem.commandes, <-mem.commandes; !strings.Contains(c1, "tar -czf") || !strings.Contains(c1, plan8+"-1") || !strings.Contains(c1, "./srv/vieux") || c2 != commandeBornee("rm -rf /srv/vieux", delaiCommande) {
		t.Errorf("archive then step: %q, %q", c1, c2)
	}
	plan9, _ := proposer(rmSauve)
	_ = decider(plan9, "accepter")
	tarEchoue.Store(true)
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"rm -rf /srv/vieux"}`); !e || !strings.Contains(txt, "backup of /srv/vieux failed, the step did not run") {
		t.Errorf("failed backup: %v %q", e, txt)
	}
	tarEchoue.Store(false)
	if n := vider(); n != 1 {
		t.Errorf("a failed backup let %d commands run", n)
	}
	// C52: the failed step says so on its plan and runs once more, not a third time
	if p, _ := s.lirePlan(plan9); p.Etapes[0].Etat != "echec" || !strings.Contains(p.Etapes[0].Message, "backup of /srv/vieux failed") {
		t.Errorf("failed step state: %+v", p.Etapes[0])
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"rm -rf /srv/vieux"}`); e || vider() != 2 {
		t.Errorf("a failed step replayed: %v %q", e, txt)
	}
	if p, _ := s.lirePlan(plan9); p.Etapes[0].Etat != "ok" || p.Etapes[0].Message != "" || p.Etapes[0].Essais != 2 {
		t.Errorf("replayed step state: %+v", p.Etapes[0])
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"rm -rf /srv/vieux"}`); !e || !strings.Contains(txt, "already ran this step") || vider() != 0 {
		t.Errorf("a third run: %v %q", e, txt)
	}
	plan10, _ := proposer(`{"outil":"ssh_commande","arguments":{"commande":"rm -rf /tmp/cache"},"pourquoi":"p","sans_sauvegarde":"le cache se refait au démarrage"}`)
	_ = decider(plan10, "accepter")
	if p, _ := s.lirePlan(plan10); p.Etapes[0].SansSauvegarde == "" {
		t.Errorf("no reason kept: %+v", p.Etapes[0])
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"rm -rf /tmp/cache"}`); e || strings.Contains(txt, "backup") || vider() != 1 {
		t.Errorf("step without backup: %v %q", e, txt)
	}

	// dev server: "do not ask again" → later changes run, and plans of changes are accepted at once;
	// a destructive call still needs its own plan
	plan5, _ := proposer(`{"outil":"pare_feu","arguments":{"action":"allow","port":8080},"pourquoi":"p"}`)
	if err := decider(plan5, "toujours"); err != nil || !s.approbations.Regle(reglePlan("demo", "ab12")) {
		t.Fatalf("rule: %v", err)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"systemctl restart php-fpm"}`); e {
		t.Errorf("a change under the rule: %q", txt)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"rm -rf /tmp/x"}`); !e || !strings.Contains(txt, "plan required: this is a destructrice") {
		t.Errorf("destructive under the rule: %v %q", e, txt)
	}
	if _, etat := proposer(restart); etat != "acceptee" {
		t.Errorf("a plan of changes under the rule: %s", etat)
	}
	// prod: the rule no longer counts, a plan has no rule
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		for i := range l {
			if l[i].ID == "ab12" {
				l[i].Roles = []string{"prod"}
			}
		}
		return l, nil
	}); err != nil {
		t.Fatal(err)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"systemctl restart php-fpm"}`); !e || !strings.Contains(txt, "plan required") {
		t.Errorf("a change on prod under a dev rule: %v %q", e, txt)
	}
	plan6, etat := proposer(restart)
	if ap, _ := s.approbations.Lire(plan6); etat != "attente" || ap.Env != "prod" || ap.Details["regle"] != "non" {
		t.Errorf("prod plan: %s %+v", etat, ap)
	}
	if err := decider(plan6, "toujours"); err == nil {
		t.Error("a rule on a prod plan")
	}
	if l, _ := s.approbations.Liste(); len(alertesApprobations(l)) != 1 || len(alertesApprobations(l)[0].Actions) != 2 {
		t.Errorf("the bell offers a rule on a prod plan: %+v", alertesApprobations(l))
	}

	// bad plans: unknown tool, another tool's argument, a refused path, no reason
	for _, c := range []struct{ etapes, attendu string }{
		{`{"outil":"rm","arguments":{},"pourquoi":"p"}`, "unknown tool"},
		{`{"outil":"ssh_commande","arguments":{"commande":"id","port":22},"pourquoi":"p"}`, "invalid arguments"},
		{`{"outil":"ssh_commande","arguments":{"commande":"id","serveur":"cd34"},"pourquoi":"p"}`, "invalid arguments"},
		{`{"outil":"fichier_ecrire","arguments":{"chemin":"/etc/shadow","contenu":"x"},"pourquoi":"p"}`, "is refused"},
		{`{"outil":"ssh_commande","arguments":{"commande":"id"},"pourquoi":" "}`, "pourquoi is required"},
		{`{"outil":"ssh_commande","arguments":{"commande":"rm -rf /srv/a"},"pourquoi":"p"}`, "step 1 is destructive: list in sauvegarde"},
		{`{"outil":"ssh_commande","arguments":{"commande":"rm -rf /srv/a"},"pourquoi":"p","sauvegarde":["/etc/shadow"]}`, "is refused"},
		{`{"outil":"ssh_commande","arguments":{"commande":"rm -rf /srv/a"},"pourquoi":"p","sauvegarde":["/var/backups/athanor/x"]}`, "is refused"},
		{`{"outil":"ssh_commande","arguments":{"commande":"rm -rf /srv/a"},"pourquoi":"p","sauvegarde":["/srv/.."]}`, "not the whole disk"},
		{`{"outil":"ssh_commande","arguments":{"commande":"rm -rf /srv/a"},"pourquoi":"p","sauvegarde":["srv/a"]}`, "absolute path"},
	} {
		if txt, e := appel("proposer_plan", `{"serveur":"ab12","resume":"r","etapes":[`+c.etapes+`]}`); !e || !strings.Contains(txt, c.attendu) {
			t.Errorf("%s: %v %q", c.etapes, e, txt)
		}
	}
	// C51: a command longer than 2000 characters is refused, in a plan and as a call
	long := "apt-get update" + strings.Repeat(" ", 2000) + "; rm -rf /var/www"
	vider()
	if txt, e := appel("proposer_plan", `{"serveur":"ab12","resume":"r","etapes":[{"outil":"ssh_commande","arguments":{"commande":"`+long+`"},"pourquoi":"p"}]}`); !e || !strings.Contains(txt, "2000 characters at most") {
		t.Errorf("long command in a plan: %v %q", e, txt)
	}
	if txt, e := appel("ssh_commande", `{"serveur":"ab12","commande":"`+long+`"}`); !e || !strings.Contains(txt, "2000 characters at most") || vider() != 0 {
		t.Errorf("long command: %v %q", e, txt)
	}
	if txt, e := appel("proposer_plan", `{"serveur":"cd34","resume":"r","etapes":[`+restart+`]}`); !e || txt != errServeurInconnu.Error() {
		t.Errorf("plan outside the perimeter: %v %q", e, txt)
	}

	// the page's plans: arguments masked; the audit log tells the story
	plan7, _ := proposer(`{"outil":"fichier_ecrire","arguments":{"chemin":"/srv/app/conf","contenu":"cle=` + temoinOutils + `"},"pourquoi":"p"}`)
	l, _ := s.approbations.Liste()
	p := s.plansAffiches(l)[plan7]
	if len(p.Etapes) != 1 || strings.Contains(p.Etapes[0].Lisible, temoinOutils) || !strings.Contains(p.Etapes[0].Lisible, "/srv/app/conf") || p.Etapes[0].Arguments != nil {
		t.Errorf("page plan: %+v", p)
	}
	j := journalAuditTexte(t)
	for _, attendu := range []string{"serveurs.proposer_plan", "accordé : plan " + plan, "accordé : règle", "approbation demandée: plan_serveur", "already ran this step", "backup: ", "the step did not run"} {
		if !strings.Contains(j, attendu) {
			t.Errorf("audit log lacks %q", attendu)
		}
	}
	if strings.Contains(j, temoinOutils) {
		t.Error("the witness value reached the audit log")
	}
	// C51: the critic reads the whole arguments, never cut
	contenu := strings.Repeat("x", 5000) + "FIN"
	if d := s.planEnDonnees(PlanServeur{Etapes: []EtapePlan{{Outil: "fichier_ecrire", Arguments: json.RawMessage(`{"contenu":"` + contenu + `"}`)}}}); !strings.Contains(d, contenu) {
		t.Error("the critic's plan is cut")
	}
	// C54: "do not ask again" on a task's plan is the server's rule, not the task's
	if r := reglePlan(prefixeTache+"t1", "ab12"); r.Projet != prefixeServeur+"ab12" || r != reglePlan(prefixeTache+"t2", "ab12") {
		t.Errorf("a task's rule: %+v", r)
	}
}
