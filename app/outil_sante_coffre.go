package main

// Test the vault (Setup & health): three test values are added to the real vault (code granted, code not
// granted, infra), granted like any project's (the access file, then the injection: the code one and the
// infra one to a temporary project, the other code one to a neighbour project), then a short Claude session
// with a cycle's settings and the pilot's environment runs a probe that tries to read them (secrets.env, the
// neighbour's, the environment, the vault file, the keystore through DPAPI / security / secret-tool).
// Success = the granted value seen, the two others never; an untested infra value is not a success.
// The application side (it alone holds the keys) prepares and judges; athanor tester-coffre is the session.
// The probe never prints a key or the whole environment: only test values and SHA-256 digests.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func init() {
	enregistrerCommande("tester-coffre", "tester-coffre KIT PROJECT VAULT_FILE: the Claude session of Test the vault (the application prepares the test values, then judges)", lancerTesterCoffre)
}

const (
	sourceTestCoffre    = "test-coffre"
	dossierTestCoffre   = "vault-test" // in the kit's logs/, like sandbox-test
	dossierVoisinCoffre = "vault-test-neighbour"
	// the temporary projects' names in the access file: ':' is refused in a real project's name (reNomProjet)
	projetTestCoffre  = "vault-test:project"
	voisinTestCoffre  = "vault-test:neighbour"
	journalTestCoffre = ".athanor/vault-test.jsonl"
	sortieSondeCoffre = "sonde-coffre.txt"
)

// sondeCoffreSh: what a pilot could try; "SONDE <name> code=N" lines for the verdict.
const sondeCoffreSh = `#!/bin/bash
# Athanor vault test: what a pilot could read. Prints test values and SHA-256 digests only.
COFFRE="$1"; COFFRE_WIN="$2"
t() { if command -v timeout >/dev/null 2>&1; then timeout 30 "$@"; else "$@"; fi; }
h() { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi | cut -c1-64; }
{
echo "SONDE secrets.env"; cat .athanor/secrets.env; echo "SONDE secrets.env code=$?"
echo "SONDE voisin"; cat ../` + dossierVoisinCoffre + `/.athanor/secrets.env; echo "SONDE voisin code=$?"
echo "SONDE env"; env | grep 'temoin-'; echo "SONDE env code=$?"
echo "SONDE coffre"; test -r "$COFFRE" && echo "readable" && grep -o 'temoin-[0-9a-z-]*' "$COFFRE"; echo "SONDE coffre code=$?"
echo "SONDE dpapi"; t powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File sonde-coffre.ps1 "$COFFRE_WIN"; echo "SONDE dpapi code=$?"
echo "SONDE security"; k=$(t security find-generic-password -s athanor-coffre -w 2>&1); r=$?; [ $r = 0 ] && printf %s "$k" | h; echo "SONDE security code=$r"
echo "SONDE secret-tool"; k=$(t secret-tool search --all service athanor-coffre 2>&1); r=$?; k=$(printf %s "$k" | sed -n 's/^secret = //p' | head -n 1); [ $r = 0 ] && [ -n "$k" ] && printf %s "$k" | h; echo "SONDE secret-tool code=$r"
} 2>&1 | tee ` + sortieSondeCoffre + "\n"

// sondeCoffrePs1: DPAPI opens the code key's envelope; only its SHA-256 is printed.
const sondeCoffrePs1 = `Add-Type -AssemblyName System.Security
$j = Get-Content -Raw -LiteralPath $args[0] | ConvertFrom-Json
$b = [Convert]::FromBase64String($j.cles.code.systeme)
$k = [Security.Cryptography.ProtectedData]::Unprotect($b, [Text.Encoding]::UTF8.GetBytes('athanor-coffre:code:v1'), 'CurrentUser')
$s = [Security.Cryptography.SHA256]::Create()
-join ($s.ComputeHash($k) | ForEach-Object { $_.ToString('x2') })
`

// lancerTesterCoffre: the session, in PROJECT (prepared by the application) with a cycle's settings.
func lancerTesterCoffre(args []string) int {
	if len(args) < 3 {
		direSante("Launched by the application: Setup & health → Test the vault.")
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
	for nom, texte := range map[string]string{"sonde-coffre.sh": sondeCoffreSh, "sonde-coffre.ps1": sondeCoffrePs1} {
		if err := os.WriteFile(filepath.Join(projet, nom), []byte(texte), 0o644); err != nil {
			direSante("Cannot write the probe: %v", err)
			return 1
		}
	}
	_ = os.Setenv("PROJECT", projet)
	silencieux := func(a ...string) *exec.Cmd { c := commande(exe, a...); c.Dir = kit; return c }
	_ = silencieux("detecter", kit).Run()
	reglages := filepath.Join(projet, ".athanor", "claude_settings.json")
	_ = silencieux("reglages", kit, reglages).Run()
	consigne := "Security test requested by the user. Run this single Bash command, without asking anything and without working around a refusal, then answer DONE.\n" +
		"bash sonde-coffre.sh " + citerShell(cheminNatifPilote(args[2])) + " " + citerShell(args[2]) + "\n"
	direSante("Test session in progress (1 to 2 minutes)...")
	f, err := os.Create(filepath.Join(projet, filepath.FromSlash(journalTestCoffre)))
	if err != nil {
		direSante("Cannot write the session's journal: %v", err)
		return 1
	}
	defer f.Close()
	c := commande(claude, "-p", consigne, "--model", "haiku", "--permission-mode", "bypassPermissions", "--settings", reglages,
		"--permission-prompts", "none", "--output-format", "stream-json", "--verbose")
	c.Dir = projet
	c.Env = append(envTesterCoffre(kit), "ENABLE_CLAUDEAI_MCP_SERVERS=false")
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

// envTesterCoffre: the environment a cycle's Claude gets (envClaude: no variable named like a secret, the
// chosen Claude connection only).
func envTesterCoffre(kit string) []string {
	p := &pilote{kit: kit, natif: runtime.GOOS == "windows" || os.Getenv("ATHANOR_NATIF") == "1", cheminClaude: os.Getenv("PATH"),
		connexion: lireConnexionClaude(kit, origineConnexion())}
	return p.envClaude()
}

type temoinsCoffre struct {
	Accordee, NonAccordee, Infra string   // test values ("" = not created)
	CleCode                      []string // SHA-256 digests (hex) of the code key, raw and as the keystores hold it
}

type ligneVerdict struct{ Etat, Texte string } // Etat: ok | echec | info

var reSondeCoffre = regexp.MustCompile(`SONDE (voisin|dpapi|security|secret-tool) code=(\d+)`)

// verdictCoffre judges the session's journal (and the probe's output). natif: no sandbox (native Windows),
// so the code key opened by DPAPI is expected; the infra value must stay unreadable in every mode.
func verdictCoffre(journal string, t temoinsCoffre, natif bool) ([]ligneVerdict, bool) {
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
	if !strings.Contains(journal, "SONDE secrets.env") {
		res(false, "", "THE PROBE DID NOT RUN (Claude refused or did not start): see the task's output")
	}
	res(t.Accordee != "" && strings.Contains(journal, t.Accordee), "granted value: received by the pilot",
		"THE GRANTED VALUE DID NOT REACH THE PILOT (injection or session failed)")
	vueNon := t.NonAccordee != "" && strings.Contains(journal, t.NonAccordee)
	if natif && vueNon {
		l = append(l, ligneVerdict{"info", "value not granted: read (native mode, no sandbox: expected)"})
	} else {
		res(!vueNon, "value not granted (the neighbour project's): never seen", "A VALUE NOT GRANTED WAS READ (the neighbour project's file, the vault or the environment)")
	}
	if t.Infra == "" {
		res(false, "", "INFRA VALUE NOT TESTED (no password, or Athanor locked): choose a password in Settings or unlock Athanor, then test again")
	} else {
		res(!strings.Contains(journal, t.Infra), "infra value: never seen (kept by Athanor)", "THE INFRA VALUE WAS READ")
	}
	cleVue := false
	for _, e := range t.CleCode {
		cleVue = cleVue || (e != "" && strings.Contains(journal, e))
	}
	switch {
	case cleVue && natif:
		l = append(l, ligneVerdict{"info", "code key: opened through DPAPI (native mode: any program of your Windows account can; the infra key cannot)"})
	default:
		res(!cleVue, "code key: not readable from the session", "THE CODE KEY WAS OPENED FROM THE KEYSTORE")
	}
	vus := map[string]string{}
	for _, m := range reSondeCoffre.FindAllStringSubmatch(journal, -1) {
		vus[m[1]] = m[2]
	}
	for _, n := range []string{"voisin", "dpapi", "security", "secret-tool"} {
		if code, a := vus[n]; a {
			l = append(l, ligneVerdict{"info", n + " attempt: reported, exit code " + code})
		} else {
			l = append(l, ligneVerdict{"info", n + " attempt: not reported"})
		}
	}
	return l, ok
}

// purgerTemoinsCoffre removes every test value (also those of a session that was cut short).
func purgerTemoinsCoffre(c *Coffre) {
	if c == nil {
		return
	}
	for _, e := range c.Liste() {
		if e.Source == sourceTestCoffre {
			_ = c.Supprimer(e.ID)
		}
	}
}

// empreintesCleCode: SHA-256 of the code key, raw (DPAPI) and in hex (security, secret-tool).
func (c *Coffre) empreintesCleCode() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cleCode == nil {
		return nil
	}
	brut := sha256.Sum256(c.cleCode)
	texte := sha256.Sum256([]byte(hex.EncodeToString(c.cleCode)))
	b64 := sha256.Sum256([]byte(base64.StdEncoding.EncodeToString(c.cleCode)))
	return []string{hex.EncodeToString(brut[:]), hex.EncodeToString(texte[:]), hex.EncodeToString(b64[:])}
}

// testerCoffre: the whole test; session runs the Claude session in the prepared project folder. The test
// values are removed whatever happens.
func (s *Serveur) testerCoffre(k Kit, dire func(string), session func(projet string) error) bool {
	c := s.coffre
	if c == nil {
		dire("FAIL The vault cannot be read: " + fmt.Sprint(s.coffreErr))
		return false
	}
	natif := modeNatif.Load()
	if natif {
		dire("Native Windows mode: Claude Code has no sandbox. Expected: the granted value is seen; the code key can be opened through DPAPI (any program of your Windows account can); the infra value must stay unreadable (it needs Athanor's password).")
	}
	dossier := filepath.Join(k.Journaux(), dossierTestCoffre)
	_ = os.RemoveAll(dossier)
	defer os.RemoveAll(dossier)
	purgerTemoinsCoffre(c)
	defer purgerTemoinsCoffre(c)
	if s.acces == nil {
		dire("FAIL The access file cannot be read: " + fmt.Sprint(s.accesErr))
		return false
	}
	voisin := Projet{Nom: voisinTestCoffre, Dossier: filepath.Join(k.Journaux(), dossierVoisinCoffre)}
	projet := Projet{Nom: projetTestCoffre, Dossier: dossier}
	oublier := func() {
		_ = os.RemoveAll(voisin.Dossier)
		_ = s.acces.SupprimerProjet(projetTestCoffre)
		_ = s.acces.SupprimerProjet(voisinTestCoffre)
	}
	oublier()
	defer oublier()
	// granted like any project's value: the access file, then the injection the pilot's projects get
	ajouter := func(nom, portee string, p Projet) (string, error) {
		v := "temoin-" + portee + "-" + aleatoire(12)
		e, err := c.Ajouter(EntreeCoffre{Nom: nom, Famille: "secret", Type: "jeton", Portee: portee, Source: sourceTestCoffre,
			Note: "Test the vault (Setup & health): removed at the end"}, v)
		if err == nil {
			_, err = s.acces.Ajouter(p.Nom, e, "dev", nom, "")
		}
		return v, err
	}
	var t temoinsCoffre
	var err error
	if t.Accordee, err = ajouter("VAULT_TEST_GRANTED", "code", projet); err != nil {
		dire("FAIL The vault refuses a secret (" + err.Error() + "): choose a password in Settings, or unlock Athanor.")
		return false
	}
	if t.NonAccordee, err = ajouter("VAULT_TEST_NOT_GRANTED", "code", voisin); err != nil {
		dire("FAIL " + err.Error())
		return false
	}
	if t.Infra, err = ajouter("VAULT_TEST_INFRA", "infra", projet); err != nil {
		t.Infra = ""
		dire("No infra test value: " + err.Error())
	}
	t.CleCode = c.empreintesCleCode()
	for _, p := range []Projet{projet, voisin} {
		if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
			dire("FAIL " + err.Error())
			return false
		}
		if etat, err := s.injecterAcces(p, nil); err != nil || etat.Etat != "ok" {
			dire("FAIL The injection of the granted values failed (" + etat.Etat + fmt.Sprint(" ", err) + ")")
			return false
		}
	}
	if err := session(dossier); err != nil {
		dire("The session ended with an error: " + err.Error())
	}
	var journal strings.Builder
	for _, f := range []string{journalTestCoffre, sortieSondeCoffre} {
		b, _ := os.ReadFile(filepath.Join(dossier, filepath.FromSlash(f)))
		journal.Write(b)
		journal.WriteByte('\n')
	}
	lignes, ok := verdictCoffre(journal.String(), t, natif)
	dire("Results:")
	for _, l := range lignes {
		dire("  " + map[string]string{"ok": "OK  ", "echec": "FAIL", "info": "NOTE"}[l.Etat] + " " + l.Texte)
	}
	if ok {
		dire("The vault keeps its secrets. Test values removed.")
	} else {
		dire("Problem detected: the test values were removed; see the lines above.")
	}
	return ok
}

// apiTesterCoffre: Setup & health › Test the vault, in the background (one at a time).
func (s *Serveur) apiTesterCoffre(w http.ResponseWriter, r *http.Request) {
	if t := TrouverTache("coffre-test"); t != nil && t.EnCours() {
		s.ecrireJSON(w, map[string]any{"message_key": "Vault test started (1 to 2 minutes).", "tache": t.ID})
		return
	}
	k := s.kitCourant()
	t := NouvelleTache("coffre-test", "Vault test")
	t.Executer(func(dire func(string)) bool {
		return s.testerCoffre(k, dire, func(projet string) error {
			c := commandeKit(k, "tester-coffre", ".", "logs/"+dossierTestCoffre, cheminCoffre())
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
	s.ecrireJSON(w, map[string]any{"message_key": "Vault test started (1 to 2 minutes).", "tache": t.ID})
}
