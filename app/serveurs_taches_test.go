package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestServeursTaches (M97): a task's life with a scripted fake Claude that plays MCP calls through the hub's real
// route: a read-only task ends, a change waits for the owner then is applied by the resumed session, a stop cuts
// everything; no vault value in the session's folder or environment; native mode refused unless forced.
func TestServeursTaches(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, _ []byte) (string, int) { return "ok:" + cmd, 0 })
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	t.Setenv("ATHANOR_DIVERS", temoinOutils) // a vault value in Athanor's environment under an innocent name

	type sessionFausse struct{ dossier, jeton, consigne, reprise string }
	var jouer func(ctx context.Context, n int, x sessionFausse) (string, string)
	n := 0
	ancien := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancien })
	sessionTacheServeurs = func(_ *Serveur, ctx context.Context, dossier string) error {
		lire := func(f string) string {
			b, _ := os.ReadFile(filepath.Join(dossier, f))
			return strings.TrimSpace(string(b))
		}
		_ = filepath.Walk(dossier, func(p string, i os.FileInfo, _ error) error { // nothing of the vault in the folder
			if b, _ := os.ReadFile(p); i != nil && !i.IsDir() && strings.Contains(string(b), temoinOutils) {
				t.Errorf("vault value in the session folder: %s", p)
			}
			return nil
		})
		if e, _ := os.ReadDir(filepath.Join(dossier, "travail")); len(e) != 0 {
			t.Errorf("the session's working folder is not empty: %v", e)
		}
		var id, res string
		switch c := lire("consigne.txt"); { // the critic's and the verifier's sessions (M98) pass
		case strings.Contains(c, "security critic"):
			id, res = "sess-critique", "VERDICT: ok"
		case strings.Contains(c, "external verifier"):
			id, res = "sess-verif", "VERIFICATION: ok"
		default:
			id, res = jouer(ctx, n, sessionFausse{dossier, lire("hub-jeton"), c, lire("reprise")})
			n++
		}
		j := `{"type":"system","subtype":"init","session_id":"` + id + `"}` + "\n" + `{"type":"result","is_error":false,"result":"` + res + `"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	appel := func(jeton, nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args))
		if rep.Error != nil {
			return rep.Error.Message, true
		}
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	attendre := func(id string, etats ...string) TacheServeurs {
		t.Helper()
		for i := 0; i < 100; i++ {
			for _, x := range tachesServeur(srv.ID) {
				if x.ID == id && slices.Contains(etats, x.Etat) {
					return x
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("task %s never reached %v: %+v", id, etats, tachesServeur(srv.ID))
		return TacheServeurs{}
	}
	lancer := func(texte string) TacheServeurs {
		t.Helper()
		n = 0
		x, err := s.lancerTacheServeurs(texte, []string{srv.ID}, false)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}

	// the session's command line and environment: the hub's tools only, no vault value
	a := strings.Join(argsSessionServeurs("c", "m.json", "r1"), " ")
	for _, attendu := range []string{"--strict-mcp-config", "--tools  ", "--allowedTools mcp__athanor-hub", "--resume r1", "--mcp-config m.json"} {
		if !strings.Contains(a, attendu) {
			t.Errorf("session arguments %q lack %q", a, attendu)
		}
	}
	if env := strings.Join(envSansSecrets(s, nil), "\n"); strings.Contains(env, temoinOutils) || !strings.Contains(env, "PATH=") {
		t.Error("the session's environment holds a vault value, or lost PATH")
	}

	// 1. a read-only task: the session reads and answers, the task ends, its token is revoked, its folder removed
	var jeton1, dossier1 string
	jouer = func(_ context.Context, _ int, x sessionFausse) (string, string) {
		jeton1, dossier1 = x.jeton, x.dossier
		if !strings.Contains(x.consigne, "ops lead") || !strings.Contains(x.consigne, "<owner-request>") || x.reprise != "" {
			t.Errorf("first session: %q (resume %q)", x.consigne, x.reprise)
		}
		if txt, e := appel(x.jeton, "ssh_commande", `{"serveur":"ab12","commande":"uptime"}`); e || !strings.Contains(txt, "ok:") {
			t.Errorf("read-only call: %v %s", e, txt)
		}
		if txt, e := appel(x.jeton, "ssh_commande", `{"serveur":"autre","commande":"uptime"}`); !e {
			t.Errorf("outside the perimeter: %s", txt)
		}
		return "sess-1", "Rien à changer."
	}
	x := lancer("Le serveur va-t-il bien ?")
	fin := attendre(x.ID, "finie", "echec", "arretee")
	if fin.Etat != "finie" || fin.Resultat != "Rien à changer." || len(fin.Plans) != 0 {
		t.Fatalf("read-only task: %+v", fin)
	}
	if txt, e := appel(jeton1, "serveurs_liste", `{}`); !e {
		t.Errorf("the task's token still works after its end: %s", txt)
	}
	if _, err := os.Stat(dossier1); !os.IsNotExist(err) {
		t.Errorf("session folder left: %v", err)
	}

	// 2. a change: plan proposed, the task waits; accepted, the resumed session applies it, then verification
	etape := `{"outil":"ssh_commande","arguments":{"commande":"systemctl restart nginx","delai_s":30},"pourquoi":"recharger"}`
	jouer = func(_ context.Context, i int, x sessionFausse) (string, string) {
		if i == 0 {
			if txt, e := appel(x.jeton, "proposer_plan", `{"serveur":"ab12","resume":"Redémarrer nginx","etapes":[`+etape+`]}`); e {
				t.Errorf("proposer_plan: %s", txt)
			}
			if txt, e := appel(x.jeton, "ssh_commande", `{"serveur":"ab12","commande":"systemctl restart nginx","delai_s":30}`); !e {
				t.Errorf("a step before the accord ran: %s", txt)
			}
			return "sess-2", "Plan proposé."
		}
		if x.reprise != "sess-2" || !strings.Contains(x.consigne, "ACCEPTED") {
			t.Errorf("resumed session: %q %q", x.reprise, x.consigne)
		}
		if txt, e := appel(x.jeton, "ssh_commande", `{"serveur":"ab12","commande":"systemctl restart nginx","delai_s":30}`); e {
			t.Errorf("accepted step refused: %s", txt)
		}
		for j := 0; j < 30 && !slices.ContainsFunc(tachesServeur(srv.ID), func(y TacheServeurs) bool { return y.Etat == "verification" }); j++ {
			time.Sleep(100 * time.Millisecond)
		}
		return "sess-2", "Nginx redémarré et vérifié."
	}
	x = lancer("Redémarre nginx")
	att := attendre(x.ID, "attente_accord")
	if len(att.Plans) != 1 {
		t.Fatalf("waiting task: %+v", att)
	}
	if _, _, err := s.deciderApprobation("fenetre", att.Plans[0], "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	fin = attendre(x.ID, "finie", "echec", "arretee")
	var etats []string
	for _, h := range fin.Historique {
		etats = append(etats, h.Etat)
	}
	if fin.Etat != "finie" || strings.Join(etats, ",") != "preparation,critique,attente_accord,application,verification,finie" {
		t.Fatalf("change task: %s %v %s", fin.Etat, etats, fin.Resultat)
	}
	if p, err := s.lirePlan(fin.Plans[0]); err != nil || p.Etapes[0].Executee == "" {
		t.Errorf("the plan's step was not run: %v %+v", err, p)
	}
	// the page's view (M99): the task's plan, its step run, its arguments only as masked text
	w0 := httptest.NewRecorder()
	r0 := httptest.NewRequest("GET", "/api/serveurs/ab12/taches", nil)
	r0.SetPathValue("id", srv.ID)
	s.apiTachesServeur(w0, r0)
	var vue struct {
		Plans map[string]PlanServeur `json:"plans"`
	}
	if json.Unmarshal(w0.Body.Bytes(), &vue) != nil || len(vue.Plans[fin.Plans[0]].Etapes) != 1 {
		t.Fatalf("tasks' view: %d %s", w0.Code, w0.Body)
	}
	if e := vue.Plans[fin.Plans[0]].Etapes[0]; e.Executee == "" || e.Arguments != nil || !strings.Contains(e.Lisible, "restart nginx") {
		t.Errorf("plan step in the view: %+v", e)
	}

	// 3. a stop: the session is cut, its token revoked at once, the task "arretee"
	arrive := make(chan string, 1)
	jouer = func(ctx context.Context, _ int, x sessionFausse) (string, string) {
		arrive <- x.jeton
		<-ctx.Done()
		if txt, e := appel(x.jeton, "ssh_commande", `{"serveur":"ab12","commande":"uptime"}`); !e {
			t.Errorf("a call after the stop ran: %s", txt)
		}
		return "sess-3", "coupé"
	}
	x = lancer("Surveille les journaux")
	<-arrive
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/serveurs/ab12/taches/"+x.ID+"/arreter", nil)
	r.SetPathValue("id", srv.ID)
	r.SetPathValue("tache", x.ID)
	s.apiArreterTacheServeur(w, r)
	if w.Code != 200 {
		t.Fatalf("stop: %d %s", w.Code, w.Body)
	}
	if fin = attendre(x.ID, "finie", "echec", "arretee"); fin.Etat != "arretee" {
		t.Fatalf("stopped task: %+v", fin)
	}

	// 4. native Windows mode: refused with the WSL proposal unless forced
	modeNatif.Store(true)
	if _, err := s.lancerTacheServeurs("x", []string{srv.ID}, false); err != errServeursNatif {
		t.Errorf("native mode not refused: %v", err)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/serveurs/ab12/taches", strings.NewReader(`{"texte":"x"}`))
	r.SetPathValue("id", srv.ID)
	s.apiLancerTacheServeur(w, r)
	var rep struct {
		ProposerWSL bool `json:"proposer_wsl"`
	}
	if w.Code != http.StatusConflict || json.Unmarshal(w.Body.Bytes(), &rep) != nil || !rep.ProposerWSL {
		t.Errorf("native mode answer: %d %s", w.Code, w.Body)
	}
	// M240r2: native mode's reminders turned off by the owner: forced without the question nor the password
	jouer = func(context.Context, int, sessionFausse) (string, string) { return "sess-4", "nothing to do" }
	s.mu.Lock()
	s.config.SansRappelsNatif = true
	s.mu.Unlock()
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/serveurs/ab12/taches", strings.NewReader(`{"texte":"x"}`))
	r.SetPathValue("id", srv.ID)
	s.apiLancerTacheServeur(w, r)
	var lance struct {
		Tache TacheServeurs `json:"tache"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &lance) != nil || !lance.Tache.Forcee {
		t.Errorf("task without the reminder: %d %s", w.Code, w.Body)
	} else {
		attendre(lance.Tache.ID, "finie", "echec", "arretee")
	}
	s.mu.Lock()
	s.config.SansRappelsNatif = false
	s.mu.Unlock()
	modeNatif.Store(false)

	// the task files: no vault value
	fichiers, _ := filepath.Glob(filepath.Join(dossierTaches(srv.ID), "*.json"))
	for _, f := range fichiers {
		if b, _ := os.ReadFile(f); strings.Contains(string(b), temoinOutils) {
			t.Errorf("vault value in %s", f)
		}
	}
	if len(fichiers) != 4 {
		t.Errorf("%d task files, 4 expected", len(fichiers))
	}
}

// N15 (M315): only a last call that could not be run at all turns the task into a failure.
func TestDerniereActionEnPanne(t *testing.T) {
	ok, code, panne := ActionTache{Etat: "ok"}, ActionTache{Etat: "erreur", Code: 1}, ActionTache{Etat: "erreur", Detail: "dial tcp: refused"}
	for _, c := range []struct {
		a    []ActionTache
		veut string
	}{{nil, ""}, {[]ActionTache{ok, code}, ""}, {[]ActionTache{panne, ok}, ""}, {[]ActionTache{ok, panne}, "dial tcp: refused"}} {
		if d := derniereActionEnPanne(c.a); d != c.veut {
			t.Fatalf("%+v: %q, wanted %q", c.a, d, c.veut)
		}
	}
}
