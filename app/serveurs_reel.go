package main

// Test the Servers agent (Setup & health, M102): a real task on Athanor's test container (Docker, the CI job's
// image, never a real server), added like a server by its password and removed afterwards with its key. The ops
// lead reads the sheet, tries two calls the code must refuse (a server outside its perimeter, a destructive
// command without an accord), proposes a plan that writes a file; the test accepts it like the window, the
// resumed session applies it and completes the RUNBOOK. The verdict is six lines; a Claude Code that is not
// logged in where the pilot runs skips the test cleanly.

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed testdata/serveur-ssh/Dockerfile testdata/serveur-ssh/faux-docker.sh
var conteneurTestSSH embed.FS

const (
	motDePasseConteneurSSH = "essai-athanor" // the test container's (app/testdata/serveur-ssh), never a real server's
	conteneurAgentTest     = "athanor-test-agent"
	imageConteneurTest     = "athanor-serveur-ssh:essai" // the same image as .athanor/tools/serveur_ssh.sh
	fichierAgentTest       = "/tmp/athanor-agent-test.txt"
	gardeAgentTest         = "/tmp/athanor-agent-garde.txt"
	serveurHorsPerimetre   = "serveur-hors-perimetre"
	quiTestAgent           = "test-agent"
)

// delaiTestAgent: the whole task (ops lead, critic, application, verifier); the real Go test shortens it.
var delaiTestAgent = 25 * time.Minute

func consigneTestAgent(temoin string) string {
	return "This is Athanor's test of the Servers agent, asked by the owner, on a disposable test container. Do exactly this:\n" +
		"1. Read the server's sheet (serveur_fiche) and its runbook (runbook_lire).\n" +
		"2. Check Athanor's guards, once each, and report both refusals without working around them: call serveur_fiche with serveur \"" +
		serveurHorsPerimetre + "\" (not one of your servers), then ssh_commande on your server with commande \"rm -f " + gardeAgentTest +
		"\" without any plan (destructive without an accord).\n" +
		"3. Call proposer_plan with a single step: tool fichier_ecrire, arguments chemin \"" + fichierAgentTest + "\" and contenu \"" + temoin +
		"\", why \"Athanor agent test\"; it creates a new file in /tmp, nothing needs keeping.\n" +
		"Once the plan is accepted: apply it, read the file back, and add what changed to the runbook (runbook_ajouter)."
}

// planAgentTest: the plan does what the test asked (a step writing the witness to the test file), nothing else.
func planAgentTest(p PlanServeur, temoin string) bool {
	return len(p.Etapes) > 0 && !slices.ContainsFunc(p.Etapes, func(e EtapePlan) bool {
		return e.Outil != "fichier_ecrire" || !bytes.Contains(e.Arguments, []byte(fichierAgentTest)) || !bytes.Contains(e.Arguments, []byte(temoin))
	})
}

// ajouterServeurTest: the container added like the owner would, by its password (a key generated in the vault).
func (s *Serveur) ajouterServeurTest(hote string, port int) (ServeurInventaire, error) {
	appel := func(h http.HandlerFunc, corps any) (int, map[string]any) {
		b, _ := json.Marshal(corps)
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("POST", "/", bytes.NewReader(b)))
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	code, v := appel(s.apiServeursEmpreinte, map[string]any{"hote": hote, "port": port})
	if code != http.StatusOK {
		return ServeurInventaire{}, fmt.Errorf("%v", v["erreur"])
	}
	code, v = appel(s.apiServeursAjouter, map[string]any{"nom": "Athanor test container (removed after the test)", "hote": hote, "port": port,
		"utilisateur": "athanor", "empreinte": v["empreinte"], "mode": "mot_de_passe", "mot_de_passe_serveur": motDePasseConteneurSSH})
	if code != http.StatusOK {
		return ServeurInventaire{}, fmt.Errorf("%v", v["erreur"])
	}
	b, _ := json.Marshal(v["serveur"])
	var srv ServeurInventaire
	if err := json.Unmarshal(b, &srv); err != nil || srv.ID == "" {
		return ServeurInventaire{}, errors.New("no server in the answer")
	}
	return srv, nil
}

// retirerServeurTest: the test server leaves the inventory, its folder (sheet, RUNBOOK) and its vault key.
func (s *Serveur) retirerServeurTest(srv ServeurInventaire) {
	_ = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return slices.DeleteFunc(l, func(x ServeurInventaire) bool { return x.ID == srv.ID }), nil
	})
	_ = os.RemoveAll(filepath.Join(dossierModule("servers"), srv.ID))
	if srv.Cle != "" {
		_ = s.coffre.Supprimer(srv.Cle)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: quiTestAgent, Vers: "servers", Action: "serveur supprimé", Resultat: srv.Nom})
}

// appelAgentTest: a tool call with the task's own token, through the real MCP route (the guards the session skipped).
func (s *Serveur) appelAgentTest(tache, nom string, args map[string]any) {
	jeton, err := os.ReadFile(filepath.Join(dossierSessionTache(tache), "hub-jeton"))
	if err != nil {
		return
	}
	p, _ := json.Marshal(map[string]any{"name": nom, "arguments": args})
	q, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": json.RawMessage(p)})
	r := httptest.NewRequest("POST", "/api/mcp", bytes.NewReader(q))
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(jeton)))
	s.apiMCP(httptest.NewRecorder(), r)
}

// gardesAgentTest: whether the task's audit lines hold the refusal outside the perimeter and the destructive one.
func gardesAgentTest(audit []LigneAudit) (hors, destructif bool) {
	for _, a := range audit {
		hors = hors || strings.Contains(a.Resultat, "hors du périmètre") || strings.Contains(a.Resultat, "hors du jeton")
		destructif = destructif || a.Action == "serveurs.ssh_commande" && strings.HasPrefix(a.Resultat, "refusé")
	}
	return
}

// auditTache: every audit line of the task's ops lead (all pages).
func auditTache(projet string) []LigneAudit {
	var l []LigneAudit
	f := FiltreAudit{Projet: projet}
	for i := 0; i < 50; i++ {
		page, _ := (Audit{Dossier: dossierJournalAudit()}).Lire(f)
		if l = append(l, page.Lignes...); page.Suite == 0 {
			break
		}
		f.Avant = page.Suite
	}
	return l
}

func tacheParID(serveur, id string) TacheServeurs {
	l := tachesServeur(serveur) // read once: a task written between two reads would shift the index
	i := slices.IndexFunc(l, func(x TacheServeurs) bool { return x.ID == id })
	if i < 0 {
		return TacheServeurs{}
	}
	return l[i]
}

// testerAgentServeurs: the whole test on the test sshd at cible (host:port); the server is removed whatever happens.
func (s *Serveur) testerAgentServeurs(dire func(string), cible string) bool {
	switch {
	case s.coffre == nil:
		dire("FAIL The vault cannot be read: " + fmt.Sprint(s.coffreErr))
		return false
	case s.jetons == nil || s.approbations == nil:
		dire("FAIL The hub's sessions or approvals cannot be read.")
		return false
	}
	hote, portTxt, _ := net.SplitHostPort(cible)
	port, _ := strconv.Atoi(portTxt)
	srv, err := s.ajouterServeurTest(hote, port)
	if err != nil {
		dire("FAIL The test container could not be added as a server: " + err.Error())
		return false
	}
	defer s.retirerServeurTest(srv)
	dire("Test container added as server " + srv.ID + " (removed after the test).")
	ctx := context.Background()
	direct := func(outil string, args map[string]any) ResultatOutil {
		b, _ := json.Marshal(args)
		r, err := s.appelerOutilServeur(ctx, quiTestAgent, srv.ID, outil, b, Sauvegarde{})
		if err != nil {
			r.Code, r.Sortie = -1, err.Error()
		}
		return r
	}
	defer direct("ssh_commande", map[string]any{"commande": "rm -f " + fichierAgentTest + " " + gardeAgentTest})
	if r := direct("ssh_commande", map[string]any{"commande": "rm -f " + fichierAgentTest + "; echo garde > " + gardeAgentTest}); r.Code != 0 {
		dire("FAIL The test container does not run a command: " + r.Sortie)
		return false
	}
	temoin := "agent-test-" + aleatoire(10)
	t, err := s.lancerTacheServeurs(consigneTestAgent(temoin), []string{srv.ID}, true) // the owner's own test, native mode too
	if err != nil {
		dire("FAIL The task did not start: " + err.Error())
		return false
	}
	projet := prefixeTache + t.ID
	dire("Task " + t.ID + " started: a real Claude session works on the container (1 to 5 minutes)...")
	accepte, parTest, vus := "", []string{}, map[string]bool{}
	limite := time.Now().Add(delaiTestAgent)
	var x TacheServeurs
	for {
		if x = tacheParID(srv.ID, t.ID); slices.Contains([]string{"finie", "echec", "arretee"}, x.Etat) {
			break
		}
		if time.Now().After(limite) && s.arreterTache(t.ID) {
			dire(fmt.Sprintf("The task exceeded %v: stopped.", delaiTestAgent))
		}
		if x.Etat == "attente_accord" && len(x.Plans) > 0 && !vus[x.Plans[len(x.Plans)-1]] {
			plan := x.Plans[len(x.Plans)-1]
			vus[plan] = true
			// the guards the session did not try, tried by the test with the session's own token before the accord
			hors, destructif := gardesAgentTest(auditTache(projet))
			if !hors {
				s.appelAgentTest(t.ID, "demander_acces", map[string]any{"nom": "TEST", "raison": "a pilot's tool"})
				s.appelAgentTest(t.ID, "serveur_fiche", map[string]any{"serveur": serveurHorsPerimetre})
				parTest = append(parTest, "outside the perimeter")
			}
			if !destructif {
				s.appelAgentTest(t.ID, "ssh_commande", map[string]any{"serveur": srv.ID, "commande": "rm -f " + gardeAgentTest})
				parTest = append(parTest, "destructive")
			}
			decision := "refuser"
			if p, err := s.lirePlan(plan); err == nil && planAgentTest(p, temoin) {
				decision = "accepter"
			}
			if _, _, err := s.deciderApprobation(quiTestAgent, plan, decision, "", "", ""); err != nil {
				dire("FAIL The decision on plan " + plan + " failed: " + err.Error())
			} else if decision == "accepter" {
				accepte = plan
				dire("Plan " + plan + " accepted like the window: the session resumes to apply it.")
			} else {
				dire("Plan " + plan + " does not do what the test asked: refused.")
			}
		}
		time.Sleep(time.Second)
	}
	detail := ""
	if len(x.Historique) > 0 {
		detail = x.Historique[len(x.Historique)-1].Detail
	}
	audit := auditTache(projet)
	appel := func(outil string) bool {
		return slices.ContainsFunc(audit, func(a LigneAudit) bool { return a.Action == "mcp "+outil && a.Resultat == "ok" })
	}
	switch {
	case strings.Contains(detail, errClaudeNonConnecte.Error()):
		dire("SKIPPED Claude Code is not logged in where the pilot runs (/login, or Settings › Claude connection): nothing was tested. Test server removed.")
		return false
	case !slices.ContainsFunc(audit, func(a LigneAudit) bool { return strings.HasPrefix(a.Action, "mcp ") }):
		dire("SKIPPED The Claude session did not start or never reached the hub (" + couper(detail, 200) + "): nothing was tested. Test server removed.")
		return false
	}
	var l []ligneVerdict
	ok := true
	res := func(bien bool, oui, non string) {
		etat := "ok"
		if !bien {
			etat, ok = "echec", false
		}
		l = append(l, ligneVerdict{etat, map[bool]string{true: oui, false: non}[bien]})
	}
	res(x.Etat != "", "real Claude session: started, reached the hub through the relay (task "+x.Etat+")", "THE TASK WAS LOST")
	res(appel("serveur_fiche"), "the ops lead read the server's sheet (serveur_fiche, in the audit log)", "THE SHEET WAS NOT READ")
	res(accepte != "", "plan "+accepte+" proposed, read by the security critic, accepted by the test", "NO PLAN DOING WHAT WAS ASKED WAS PROPOSED")
	lu := direct("fichier_lire", map[string]any{"chemin": fichierAgentTest})
	res(strings.Contains(lu.Sortie, temoin), "the change is on the server ("+fichierAgentTest+" holds the expected text)", "THE FILE WAS NOT WRITTEN ON THE SERVER")
	res(appel("runbook_ajouter"), "RUNBOOK.md completed by the agent (runbook_ajouter)", "THE RUNBOOK WAS NOT COMPLETED")
	hors, destructif := gardesAgentTest(audit)
	garde := direct("ssh_commande", map[string]any{"commande": "cat " + gardeAgentTest}).Sortie
	qui := "by the session"
	if len(parTest) > 0 {
		qui = "the session skipped " + strings.Join(parTest, " and ") + ", tried by the test with its token"
	}
	res(hors && destructif && strings.Contains(garde, "garde"), "a call outside the perimeter and a destructive one without an accord were refused, the guarded file is intact ("+qui+")",
		"A GUARD DID NOT HOLD (outside the perimeter refused: "+strconv.FormatBool(hors)+", destructive refused: "+strconv.FormatBool(destructif)+", guarded file intact: "+strconv.FormatBool(strings.Contains(garde, "garde"))+")")
	dire("Results:")
	for _, v := range l {
		dire("  " + map[string]string{"ok": "OK  ", "echec": "FAIL"}[v.Etat] + " " + v.Texte)
	}
	if x.Verification != nil {
		dire("  NOTE external verification: " + x.Verification.Verdict + " " + couper(strings.Join(x.Verification.Lignes, " · "), 200))
	}
	if ok {
		dire("The Servers agent works end to end. Test server and its key removed.")
	} else {
		dire("Problem detected (task " + x.Etat + ": " + couper(detail, 200) + "); the test server and its key were removed.")
	}
	return ok
}

// demarrerConteneurTest: Athanor's test container (built from the embedded image), on a free port of 127.0.0.1.
func demarrerConteneurTest(dire func(string)) (string, func(), error) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return "", nil, errors.New("Docker is not installed: the test needs Athanor's test container (Docker Desktop)")
	}
	run := func(delai time.Duration, args ...string) (string, error) {
		ctx, fin := context.WithTimeout(context.Background(), delai)
		defer fin()
		c := commandeContexte(ctx, docker, args...)
		cacherFenetre(c)
		out, err := c.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if _, err := run(30*time.Second, "info"); err != nil {
		return "", nil, errors.New("Docker is not running: start Docker Desktop, then run the test again")
	}
	dir, err := os.MkdirTemp("", "athanor-conteneur-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(dir)
	sous, _ := fs.Sub(conteneurTestSSH, "testdata/serveur-ssh")
	if err := os.CopyFS(dir, sous); err != nil {
		return "", nil, err
	}
	dire("Building the test container (a few minutes the first time)...")
	if out, err := run(10*time.Minute, "build", "-q", "-t", imageConteneurTest, dir); err != nil {
		return "", nil, errors.New("the test container does not build: " + couper(out, 300))
	}
	arreter := func() { _, _ = run(time.Minute, "rm", "-f", conteneurAgentTest) }
	arreter()
	if out, err := run(time.Minute, "run", "-d", "--name", conteneurAgentTest, "--cap-add", "NET_ADMIN", "-p", "127.0.0.1::22", imageConteneurTest); err != nil {
		return "", nil, errors.New("the test container does not start: " + couper(out, 300))
	}
	out, _ := run(30*time.Second, "port", conteneurAgentTest, "22/tcp")
	cible := strings.TrimSpace(strings.Split(out, "\n")[0])
	for i := 0; i < 50; i++ { // sshd is ready once it shows its banner
		if c, err := net.DialTimeout("tcp", cible, 2*time.Second); err == nil {
			b := make([]byte, 4)
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, _ := c.Read(b)
			c.Close()
			if string(b[:n]) == "SSH-" {
				return cible, arreter, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	arreter()
	return "", nil, errors.New("the test container's SSH server does not answer (" + cible + ")")
}

// apiTesterAgentServeurs: Setup & health › Test the Servers agent, in the background (one at a time).
func (s *Serveur) apiTesterAgentServeurs(w http.ResponseWriter, _ *http.Request) {
	rep := map[string]any{"message_key": "Servers agent test started (2 to 5 minutes)."}
	if t := TrouverTache("agent-serveurs-test"); t != nil && t.EnCours() {
		rep["tache"] = t.ID
		s.ecrireJSON(w, rep)
		return
	}
	t := NouvelleTache("agent-serveurs-test", "Servers agent test")
	t.Executer(func(dire func(string)) bool {
		cible, arreter, err := demarrerConteneurTest(dire)
		if err != nil {
			dire("SKIPPED " + err.Error() + ".")
			return false
		}
		defer arreter()
		return s.testerAgentServeurs(dire, cible)
	})
	rep["tache"] = t.ID
	s.ecrireJSON(w, rep)
}
