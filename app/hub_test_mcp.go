package main

// Test the hub's MCP (Setup & health): a temporary project the hub knows while the test runs, a test value
// (a code secret) added to the real vault and always removed, a pilot token handed over like at a pilot's
// start, then a short Claude session (Haiku, a cycle's settings, the pilot's mcp.json with the real relay).
// It calls environnements then demander_acces; the application accepts like the window (deciderApprobation),
// the injection writes secrets.env and the session reads the value there without restarting.
// Success = only the pilot's hub tools listed, the value read, each call in the audit log.
// The application side prepares, decides and judges; athanor tester-mcp is the session.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

func init() {
	enregistrerCommande("tester-mcp", "tester-mcp KIT PROJECT [INSTANCE_FILE]: the Claude session of Test the hub's MCP (the application prepares, accepts the request, then judges)", lancerTesterMCP)
}

const (
	sourceTestMCP   = "test-mcp"
	dossierTestMCP  = "hub-mcp-test" // in the kit's logs/, like vault-test
	projetTestMCP   = "hub-mcp-test:project"
	variableTestMCP = "HUB_MCP_TEST_KEY"
	journalTestMCP  = ".athanor/hub-mcp-test.jsonl"
	quiTestMCP      = "test-mcp"
)

// attenteMCPSh: the session waits for the accepted value in secrets.env (100 s at most, under Bash's 2 min).
const attenteMCPSh = `#!/bin/bash
# Athanor hub MCP test: waits for the accepted key in .athanor/secrets.env, without restarting.
for i in $(seq 1 50); do
  v=$(sed -n 's/^` + variableTestMCP + `=//p' .athanor/secrets.env 2>/dev/null | head -n 1)
  if [ -n "$v" ]; then echo "ATTENTE vue $v"; exit 0; fi
  sleep 2
done
echo "ATTENTE absente"
`

// lancerTesterMCP: the session, in PROJECT (prepared by the application) with a cycle's settings and the
// pilot's mcp.json; INSTANCE_FILE (the relay's view) names the Athanor that runs the test, if not this one.
func lancerTesterMCP(args []string) int {
	if len(args) < 2 {
		direSante("Launched by the application: Setup & health → Test the hub's MCP.")
		return 2
	}
	kit := kitSante(args[:1])
	_ = os.Chdir(kit)
	projet := cheminNatifPilote(args[1])
	if !filepath.IsAbs(projet) {
		projet = filepath.Join(kit, projet)
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		direSante("Claude Code NOT FOUND: Setup & health → Install everything.")
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		direSante("Athanor's executable is not reachable: Setup & health → Rebuild Athanor.")
		return 1
	}
	if err := os.WriteFile(filepath.Join(projet, "attente-mcp.sh"), []byte(attenteMCPSh), 0o644); err != nil {
		direSante("Cannot write the waiting script: %v", err)
		return 1
	}
	_ = os.Setenv("PROJECT", projet)
	silencieux := func(a ...string) *exec.Cmd { c := commande(exe, a...); c.Dir = kit; return c }
	_ = silencieux("detecter", kit).Run()
	reglages := filepath.Join(projet, ".athanor", "claude_settings.json")
	_ = silencieux("reglages", kit, reglages).Run()
	// the pilot's MCP file, built like at a cycle (the hub token is next to it, in the private folder)
	mcp := filepath.Join(priveDePilote(kit, projet, runtime.GOOS == "windows"), "mcp.json")
	if noms, _ := construireMCP(kit, nil, mcp, projet, ""); !slices.Contains(noms, nomMCPHub) {
		direSante("FAIL No hub token in the pilot's private folder: the athanor-hub server was not configured.")
		return 1
	}
	if len(args) > 2 && args[2] != "" {
		serveurs := lireJSONOrdonne(mcp).objet("mcpServers")
		h := serveurs.objet(nomMCPHub)
		var a []string
		_ = json.Unmarshal(h.valeur["args"], &a)
		h.mettre("args", append(a, args[2]))
		serveurs.mettre(nomMCPHub, h)
		if err := ecrireMCP(mcp, serveurs); err != nil {
			direSante("Cannot write the MCP file: %v", err)
			return 1
		}
	}
	consigne := "Test of Athanor's hub tools requested by the user. Do exactly this, without asking anything and without working around a refusal:\n" +
		"1. Call the athanor-hub tool environnements.\n" +
		"2. Call the athanor-hub tool demander_acces with nom " + variableTestMCP + " and raison \"Test the hub's MCP\".\n" +
		"3. Run this single Bash command: bash attente-mcp.sh\n" +
		"4. Answer with the names of the athanor-hub tools you have, then DONE.\n"
	direSante("Test session in progress (1 to 2 minutes)...")
	f, err := os.Create(filepath.Join(projet, filepath.FromSlash(journalTestMCP)))
	if err != nil {
		direSante("Cannot write the session's journal: %v", err)
		return 1
	}
	defer f.Close()
	c := commande(claude, "-p", consigne, "--model", "haiku", "--permission-mode", "bypassPermissions", "--settings", reglages,
		"--mcp-config", mcp, "--strict-mcp-config", "--output-format", "stream-json", "--verbose")
	c.Dir = projet
	// tools loaded up front: the init event then lists them (no ToolSearch step for Haiku)
	c.Env = append(envTesterCoffre(kit), "ENABLE_CLAUDEAI_MCP_SERVERS=false", "ENABLE_TOOL_SEARCH=false")
	c.Stdout, c.Stderr = f, f
	groupeSante(c)
	if err := c.Start(); err != nil {
		direSante("Claude Code does not start: %v", err)
		return 1
	}
	minuteur := time.AfterFunc(5*time.Minute, func() { tuerGroupeSante(c) })
	_ = c.Wait()
	minuteur.Stop()
	direSante("Session finished.")
	return 0
}

// outilsHubVus: the athanor-hub tools of the session's init event, and whether the server was connected.
func outilsHubVus(journal string) ([]string, bool) {
	sc := bufio.NewScanner(strings.NewReader(journal))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var e struct {
			Type, Subtype string
			Tools         []string
			MCPServers    []struct{ Name, Status string } `json:"mcp_servers"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Type != "system" || e.Subtype != "init" {
			continue
		}
		var l []string
		for _, n := range e.Tools {
			if o, ok := strings.CutPrefix(n, "mcp__"+nomMCPHub+"__"); ok {
				l = append(l, o)
			}
		}
		slices.Sort(l)
		connecte := false
		for _, m := range e.MCPServers {
			connecte = connecte || (m.Name == nomMCPHub && m.Status == "connected")
		}
		return l, connecte
	}
	return nil, false
}

// verdictMCP judges the session's journal, the decision and the audit lines of the test project.
func verdictMCP(journal, temoin string, accepte bool, audit []LigneAudit) ([]ligneVerdict, bool) {
	var l []ligneVerdict
	ok := true
	res := func(bien bool, oui, non string) {
		if bien {
			l = append(l, ligneVerdict{"ok", oui})
		} else {
			l = append(l, ligneVerdict{"echec", non})
			ok = false
		}
	}
	outils, connecte := outilsHubVus(journal)
	attendus := slices.Sorted(slices.Values(outilsPilote))
	res(connecte, "athanor-hub server: connected through the relay", "THE ATHANOR-HUB SERVER DID NOT CONNECT (relay, token or Athanor unreachable)")
	res(slices.Equal(outils, attendus), "tools listed: the pilot's only ("+strings.Join(outils, ", ")+")",
		"TOOLS LISTED: "+fmt.Sprint(outils)+", expected "+fmt.Sprint(attendus))
	appel := func(outil string) bool {
		return slices.ContainsFunc(audit, func(a LigneAudit) bool { return a.Action == "mcp "+outil && a.Resultat == "ok" })
	}
	res(appel("environnements"), "environnements: called, in the audit log", "ENVIRONNEMENTS WAS NOT CALLED (or not in the audit log)")
	res(appel("demander_acces"), "demander_acces: called, in the audit log", "DEMANDER_ACCES WAS NOT CALLED (or not in the audit log)")
	res(accepte && slices.ContainsFunc(audit, func(a LigneAudit) bool { return a.Action == "approbation accepter" }),
		"request accepted like the window, in the audit log", "NO REQUEST TO ACCEPT (or the decision failed)")
	res(temoin != "" && (strings.Contains(journal, "ATTENTE vue "+temoin) || strings.Contains(journal, "ATTENTE vue '"+temoin+"'")), "the session read the value in secrets.env without restarting",
		"THE ACCEPTED VALUE DID NOT REACH THE SESSION")
	return l, ok
}

// testerMCP: the whole test; session runs the Claude session in the prepared project folder. The test value,
// the token, the grant and the temporary project are removed whatever happens.
func (s *Serveur) testerMCP(k Kit, dire func(string), session func(projet string) error) bool {
	c := s.coffre
	switch {
	case c == nil:
		dire("FAIL The vault cannot be read: " + fmt.Sprint(s.coffreErr))
		return false
	case s.acces == nil:
		dire("FAIL The access file cannot be read: " + fmt.Sprint(s.accesErr))
		return false
	case s.jetons == nil || s.approbations == nil:
		dire("FAIL The hub's sessions or approvals cannot be read.")
		return false
	}
	dossier := filepath.Join(k.Journaux(), dossierTestMCP)
	p := Projet{Nom: projetTestMCP, Dossier: dossier}
	oublier := func() {
		s.revoquerJetonProjet(p, "test terminé")
		_ = s.acces.SupprimerProjet(projetTestMCP)
		purgerTemoinsCoffre(c)
		_ = os.RemoveAll(dossier)
		_ = os.RemoveAll(s.priveProjet(p))
	}
	s.mu.Lock()
	s.projetTestMCP = p
	s.mu.Unlock()
	oublier()
	defer func() {
		oublier()
		s.mu.Lock()
		s.projetTestMCP = Projet{}
		s.mu.Unlock()
	}()
	temoin := "temoin-mcp-" + aleatoire(12)
	e, err := c.Ajouter(EntreeCoffre{Nom: variableTestMCP, Famille: "secret", Type: "jeton", Portee: "code", Source: sourceTestMCP,
		Note: "Test the hub's MCP (Setup & health): removed at the end"}, temoin)
	if err != nil {
		dire("FAIL The vault refuses a secret (" + err.Error() + "): choose a password in Settings, or unlock Athanor.")
		return false
	}
	pid := filepath.Join(p.Atelier(), "pilot.pid") // the token stays valid while the test's "pilot" runs
	if err := os.MkdirAll(p.Atelier(), 0o755); err == nil {
		err = os.WriteFile(pid, []byte("0\n"), 0o644)
	}
	if err == nil {
		err = s.creerJetonPilote(p)
	}
	if err != nil {
		dire("FAIL " + err.Error())
		return false
	}
	debut := time.Now().UTC().Format(time.RFC3339)
	fin := make(chan error, 1)
	go func() { fin <- session(dossier) }()
	accepte := false
	for attente := true; attente; {
		select {
		case err := <-fin:
			attente = false
			if err != nil {
				dire("The session ended with an error: " + err.Error())
			}
		case <-time.After(time.Second):
			now := time.Now()
			_ = os.Chtimes(pid, now, now)
			l, _ := s.approbations.Liste()
			for _, d := range l {
				if d.Projet == p.Nom && d.Action == "demander_acces" && d.Etat == "attente" {
					if _, _, err := s.deciderApprobation(quiTestMCP, d.ID, "accepter", e.ID, "", ""); err != nil {
						dire("FAIL The acceptance failed: " + err.Error())
					} else {
						accepte = true
						dire("Request " + d.ID + " accepted like the window (" + d.Details["nom"] + ", " + d.Env + ").")
					}
				}
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(dossier, filepath.FromSlash(journalTestMCP)))
	if err != nil {
		dire("SKIPPED The Claude session did not start (see above): nothing was tested. Test values removed.")
		return false
	}
	_ = os.WriteFile(filepath.Join(k.Journaux(), dossierTestMCP+".jsonl"), b, 0o600) // the last session, kept for a look
	if strings.Contains(string(b), `"error":"authentication_failed"`) {
		dire("SKIPPED Claude Code is not logged in where the pilot runs (/login, or Settings › Claude connection): nothing was tested. Test values removed.")
		return false
	}
	page, _ := (Audit{Dossier: dossierJournalAudit()}).Lire(FiltreAudit{Projet: p.Nom})
	var audit []LigneAudit
	for _, a := range page.Lignes {
		if a.T >= debut {
			audit = append(audit, a)
		}
	}
	lignes, ok := verdictMCP(string(b), temoin, accepte, audit)
	dire("Results:")
	for _, l := range lignes {
		dire("  " + map[string]string{"ok": "OK  ", "echec": "FAIL", "info": "NOTE"}[l.Etat] + " " + l.Texte)
	}
	if ok {
		dire("The hub's tools work end to end. Test value, token and grant removed.")
	} else {
		dire("Problem detected: the test value, token and grant were removed; see the lines above.")
	}
	return ok
}

// apiTesterMCP: Setup & health › Test the hub's MCP, in the background (one at a time).
func (s *Serveur) apiTesterMCP(w http.ResponseWriter, r *http.Request) {
	if t := TrouverTache("mcp-test"); t != nil && t.EnCours() {
		s.ecrireJSON(w, map[string]any{"message_key": "Hub MCP test started (1 to 2 minutes).", "tache": t.ID})
		return
	}
	k := s.kitCourant()
	t := NouvelleTache("mcp-test", "Hub MCP test")
	t.Executer(func(dire func(string)) bool {
		return s.testerMCP(k, dire, func(projet string) error {
			c := commandeKit(k, "tester-mcp", ".", "logs/"+dossierTestMCP)
			cacherFenetre(c)
			minuteur := time.AfterFunc(6*time.Minute, func() { arreterArbre(c) })
			defer minuteur.Stop()
			out, err := c.CombinedOutput()
			for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				dire(strings.TrimRight(l, "\r"))
			}
			return err
		})
	})
	s.ecrireJSON(w, map[string]any{"message_key": "Hub MCP test started (1 to 2 minutes).", "tache": t.ID})
}
