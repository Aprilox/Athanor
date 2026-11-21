package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// requeteSFTPTest: a request of the window (access cookie, anti-CSRF header, session) to the test server.
func requeteSFTPTest(t *testing.T, s *Serveur, ts *httptest.Server, methode, chemin, corps string, entetes ...string) (int, []byte) {
	t.Helper()
	r, _ := http.NewRequest(methode, ts.URL+chemin, strings.NewReader(corps))
	r.Header.Set("X-Athanor", "jeton")
	for i := 0; i+1 < len(entetes); i += 2 {
		r.Header.Set(entetes[i], entetes[i+1])
	}
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	if s.session != "" {
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
	}
	rep, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Body.Close()
	b, _ := io.ReadAll(rep.Body)
	return rep.StatusCode, b
}

// TestSFTPPageServeur: the SFTP page's server side on the terminal's connection: new folder, permissions in octal
// (one path or a selection, bad values refused), a link and its target in the listing, what a deletion removes,
// a text file opened, saved (mode kept) and refused when it changed meanwhile, a binary or a too large file
// refused; the audit names paths and sizes, never the content.
func TestSFTPPageServeur(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	m := lancerSSHMemoire(t, signer.PublicKey(), "", nil)
	s, srv := outilsServeursTest(t, m.hote, m.port, m.empreinte, prive)
	mux := http.NewServeMux()
	s.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	req := func(methode, chemin, corps string) (int, []byte) {
		t.Helper()
		return requeteSFTPTest(t, s, ts, methode, chemin, corps)
	}
	c, b := req("POST", "/api/serveurs/"+srv.ID+"/terminal", `{}`)
	var o struct{ Session string }
	if json.Unmarshal(b, &o); c != http.StatusOK || o.Session == "" {
		t.Fatalf("open: %d %s", c, b)
	}
	base := "/api/serveurs/" + srv.ID + "/sftp/" + o.Session
	defer req("POST", "/api/serveurs/"+srv.ID+"/terminal/"+o.Session, `{"fermer":true}`)
	agir := func(corps string, code int) []byte {
		t.Helper()
		c, b := req("POST", base, corps)
		if c != code {
			t.Fatalf("%s: %d %s, want %d", corps, c, b, code)
		}
		return b
	}
	agir(`{"dossier":"/","nom":"site"}`, http.StatusOK)
	agir(`{"dossier":"/","nom":"site"}`, http.StatusConflict)
	agir(`{"dossier":"/","nom":"../x"}`, http.StatusBadRequest)
	agir(`{"dossier":"relatif","nom":"x"}`, http.StatusBadRequest)
	agir(`{"dossier":"/site","nom":"css"}`, http.StatusOK)
	for _, f := range []string{"/site/index.html", "/site/css/a.css"} {
		if c, b := req("POST", base+"/fichier?chemin="+url.QueryEscape(f), "contenu-secret-du-fichier"); c != http.StatusOK {
			t.Fatalf("upload %s: %d %s", f, c, b)
		}
	}
	// permissions
	agir(`{"permissions":["/site/index.html","/site/css/a.css"],"mode":"640"}`, http.StatusOK)
	for _, mauvais := range []string{`"9"`, `"75"`, `"abc"`, `"07555"`, `"-1"`} {
		agir(`{"permissions":["/site/index.html"],"mode":`+mauvais+`}`, http.StatusBadRequest)
	}
	agir(`{"permissions":["/"],"mode":"755"}`, http.StatusBadRequest)
	agir(`{"permissions":["/absent"],"mode":"755"}`, http.StatusNotFound)
	if m, ok := modeOctal("4755"); !ok || m.Perm() != 0o755 || m&os.ModeSetuid == 0 {
		t.Errorf("modeOctal 4755: %v %v", m, ok)
	}
	// a link and its target
	terminaux.Lock()
	tr := terminaux.m[o.Session]
	terminaux.Unlock()
	cs, err := tr.clientSFTP()
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.Symlink("/site/css", "/raccourci"); err != nil {
		t.Fatal(err)
	}
	c, b = req("GET", base+"?chemin=/", "")
	var l struct{ Entrees []entreeSFTP }
	if json.Unmarshal(b, &l); c != http.StatusOK || len(l.Entrees) != 2 || l.Entrees[0].Nom != "raccourci" || !l.Entrees[0].Lien || l.Entrees[0].Cible != "/site/css" || !l.Entrees[0].Dossier {
		t.Fatalf("listing with a link: %d %s", c, b)
	}
	// what a deletion removes
	var inv inventaire
	if json.Unmarshal(agir(`{"inventaire":"/site"}`, http.StatusOK), &inv); inv.Fichiers != 2 || inv.Dossiers != 2 || len(inv.Chemins) != 4 || inv.Taille != 2*int64(len("contenu-secret-du-fichier")) {
		t.Fatalf("inventory: %+v", inv)
	}
	agir(`{"inventaire":"/"}`, http.StatusBadRequest)
	// a text file
	c, b = req("GET", base+"/texte?chemin=/site/index.html", "")
	var tx struct {
		Contenu string
		Modifie int64
	}
	if json.Unmarshal(b, &tx); c != http.StatusOK || tx.Contenu != "contenu-secret-du-fichier" {
		t.Fatalf("open text: %d %s", c, b)
	}
	corps, _ := json.Marshal(map[string]any{"chemin": "/site/index.html", "contenu": "texte-modifie-secret", "modifie": tx.Modifie})
	if c, b := req("POST", base+"/texte", string(corps)); c != http.StatusOK {
		t.Fatalf("save text: %d %s", c, b)
	}
	if st, err := cs.Stat("/site/index.html"); err != nil || st.Size() != int64(len("texte-modifie-secret")) {
		t.Fatalf("saved file: %v %v", st, err)
	}
	if _, err := cs.Stat("/site/index.html.athanor-part"); err == nil {
		t.Error("part file left behind")
	}
	corps, _ = json.Marshal(map[string]any{"chemin": "/site/index.html", "contenu": "x", "modifie": tx.Modifie - 3600})
	if c, _ := req("POST", base+"/texte", string(corps)); c != http.StatusConflict {
		t.Errorf("save over a file changed meanwhile: %d", c)
	}
	if c, _ := req("POST", base+"/fichier?chemin=/binaire.bin", "a\x00b"); c != http.StatusOK {
		t.Fatal("upload binary")
	}
	if c, _ := req("GET", base+"/texte?chemin=/binaire.bin", ""); c != http.StatusUnprocessableEntity {
		t.Errorf("binary opened as text: %d", c)
	}
	if c, _ := req("POST", base+"/fichier?chemin=/gros.txt", strings.Repeat("a", texteSFTPMax+1)); c != http.StatusOK {
		t.Fatal("upload large")
	}
	if c, _ := req("GET", base+"/texte?chemin=/gros.txt", ""); c != http.StatusUnprocessableEntity {
		t.Errorf("file over 1 MB opened as text: %d", c)
	}
	if c, _ := req("GET", base+"/texte?chemin=/site", ""); c != http.StatusUnprocessableEntity {
		t.Errorf("folder opened as text: %d", c)
	}
	corps, _ = json.Marshal(map[string]any{"chemin": "/site/index.html", "contenu": "a\x00b"})
	if c, _ := req("POST", base+"/texte", string(corps)); c != http.StatusUnprocessableEntity {
		t.Errorf("binary saved as text: %d", c)
	}
	agir(`{"supprimer":"/site"}`, http.StatusOK)
	journal := journalAuditTexte(t)
	for _, x := range []string{"sftp.mkdir", "/site/css", "sftp.chmod", "/site/index.html 640", "sftp.open", "sftp.edit", "/site/index.html (20 bytes)"} {
		if !strings.Contains(journal, x) {
			t.Errorf("audit log without %q", x)
		}
	}
	for _, x := range []string{"contenu-secret-du-fichier", "texte-modifie-secret", string(prive)} {
		if strings.Contains(journal, x) {
			t.Errorf("audit log holds %q", x)
		}
	}
}

// TestSFTPLocal: the Local panel on this computer: the personal folder by default, the disks, a listing with
// sizes and dates, new folder, rename, delete with what it removes; relative, network and traversal paths
// refused; Athanor's data folder never changed (nor its parent deleted); refused from a paired device by default;
// no tool of the assistant reaches it, and a request without the window's header is refused.
func TestSFTPLocal(t *testing.T) {
	donnees := t.TempDir()
	t.Setenv("LocalAppData", donnees)
	t.Setenv("XDG_DATA_HOME", donnees)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	s := serveurCoffreTest(t)
	_ = os.RemoveAll(dossierJournalAudit())
	t.Cleanup(func() { _ = os.RemoveAll(dossierJournalAudit()) })
	mux := http.NewServeMux()
	s.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	req := func(methode, chemin, corps string) (int, []byte) {
		t.Helper()
		return requeteSFTPTest(t, s, ts, methode, chemin, corps)
	}
	type liste struct {
		Chemin, Parent, Maison string
		Racines                []string
		Entrees                []entreeLocale
	}
	lister := func(chemin string) (int, liste) {
		t.Helper()
		c, b := req("GET", "/api/serveurs/local/sftp?chemin="+url.QueryEscape(chemin), "")
		var l liste
		json.Unmarshal(b, &l)
		return c, l
	}
	maison, _ := os.UserHomeDir()
	if c, l := lister(""); c != http.StatusOK || l.Chemin != filepath.Clean(maison) || len(l.Racines) == 0 {
		t.Fatalf("home: %d %+v", c, l)
	}
	zone := t.TempDir()
	os.WriteFile(filepath.Join(zone, "a.txt"), []byte("12345"), 0o644)
	os.WriteFile(filepath.Join(zone, ".cache"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(zone, "b"), 0o755)
	c, l := lister(zone)
	if c != http.StatusOK || len(l.Entrees) != 3 || l.Entrees[0].Nom != "b" || !l.Entrees[0].Dossier || l.Parent != filepath.Dir(zone) {
		t.Fatalf("listing: %d %+v", c, l)
	}
	for _, e := range l.Entrees {
		if e.Nom == "a.txt" && (e.Taille != 5 || e.Modifie == 0 || e.Mode == "") {
			t.Errorf("a.txt: %+v", e)
		}
	}
	agir := func(v map[string]any, code int) []byte {
		t.Helper()
		corps, _ := json.Marshal(v)
		c, b := req("POST", "/api/serveurs/local/sftp", string(corps))
		if c != code {
			t.Fatalf("%s: %d %s, want %d", corps, c, b, code)
		}
		return b
	}
	agir(map[string]any{"dossier": zone, "nom": "nouveau"}, http.StatusOK)
	agir(map[string]any{"dossier": zone, "nom": "nouveau"}, http.StatusConflict)
	agir(map[string]any{"dossier": zone, "nom": ".."}, http.StatusBadRequest)
	agir(map[string]any{"dossier": zone, "nom": "x/y"}, http.StatusBadRequest)
	agir(map[string]any{"renommer": filepath.Join(zone, "a.txt"), "nom": "b"}, http.StatusConflict)
	agir(map[string]any{"renommer": filepath.Join(zone, "a.txt"), "nom": "c.txt"}, http.StatusOK)
	os.WriteFile(filepath.Join(zone, "b", "dedans.txt"), []byte("abc"), 0o644)
	var inv inventaire
	if json.Unmarshal(agir(map[string]any{"inventaire": filepath.Join(zone, "b")}, http.StatusOK), &inv); inv.Fichiers != 1 || inv.Dossiers != 1 || inv.Taille != 3 {
		t.Fatalf("inventory: %+v", inv)
	}
	agir(map[string]any{"supprimer": filepath.Join(zone, "b")}, http.StatusOK)
	if _, err := os.Stat(filepath.Join(zone, "b")); err == nil {
		t.Error("folder not deleted")
	}
	agir(map[string]any{"supprimer": filepath.Join(zone, "b")}, http.StatusNotFound)
	agir(map[string]any{"supprimer": maison}, http.StatusBadRequest)
	agir(map[string]any{"supprimer": filepath.VolumeName(zone) + string(filepath.Separator)}, http.StatusBadRequest)
	agir(map[string]any{"supprimer": filepath.Join(zone, "c.txt"), "renommer": filepath.Join(zone, "c.txt"), "nom": "d"}, http.StatusBadRequest)
	agir(map[string]any{}, http.StatusBadRequest)
	// bad paths
	for _, p := range []string{"relatif", "..", filepath.Join("a", "..", "..", "b")} {
		if c, _ := lister(p); c != http.StatusBadRequest {
			t.Errorf("list %q: %d", p, c)
		}
	}
	if c, l := lister(zone + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(zone)); c != http.StatusOK || l.Chemin != zone {
		t.Errorf("a path with .. is cleaned: %d %q", c, l.Chemin)
	}
	if c, _ := lister(filepath.Join(zone, "c.txt")); c != http.StatusBadRequest {
		t.Errorf("a file listed as a folder: %d", c)
	}
	if c, _ := lister(filepath.Join(zone, "absent")); c != http.StatusNotFound {
		t.Errorf("missing folder: %d", c)
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{`\\serveur\partage`, `\\?\C:\Windows`, `//serveur/partage`} {
			if c, _ := lister(p); c != http.StatusBadRequest {
				t.Errorf("network path %q: %d", p, c)
			}
		}
	}
	// Athanor's data folder: never changed, nor its parent deleted
	athanor := dossierAthanorDefaut()
	os.MkdirAll(athanor, 0o700)
	os.WriteFile(filepath.Join(athanor, "coffre.json"), []byte("témoin-coffre"), 0o600)
	if !protegeLocal(filepath.Join(athanor, "coffre.json"), dossiersProteges()) || !protegeLocal(filepath.Dir(athanor), dossiersProteges()) || protegeLocal(zone, dossiersProteges()) {
		t.Fatal("protegeLocal")
	}
	agir(map[string]any{"supprimer": filepath.Join(athanor, "coffre.json")}, http.StatusForbidden)
	agir(map[string]any{"renommer": athanor, "nom": "autre"}, http.StatusForbidden)
	parent := http.StatusForbidden
	if maison, _ := os.UserHomeDir(); strings.EqualFold(filepath.Dir(athanor), filepath.Clean(maison)) {
		parent = http.StatusBadRequest // Linux here: XDG_DATA_HOME is HOME, its parent is the personal folder (errLocalRacine)
	}
	agir(map[string]any{"supprimer": filepath.Dir(athanor)}, parent)
	agir(map[string]any{"dossier": athanor, "nom": "x"}, http.StatusForbidden)
	agir(map[string]any{"dossier": filepath.Dir(athanor), "nom": "a-cote"}, http.StatusOK) // beside it is fine
	if _, l := lister(filepath.Dir(athanor)); !strings.Contains(string(mustJSON(l)), `"protege":true`) {
		t.Errorf("data folder not marked in its parent: %+v", l)
	}
	if _, err := os.Stat(filepath.Join(athanor, "coffre.json")); err != nil {
		t.Fatal("the vault was touched")
	}
	// the window only: without its anti-CSRF header (an MCP client, the assistant) nothing is answered
	if c, b := requeteSFTPTest(t, s, ts, "GET", "/api/serveurs/local/sftp?chemin="+url.QueryEscape(zone), "", "X-Athanor", "", "Authorization", "Bearer jeton-mcp"); c != http.StatusForbidden || strings.Contains(string(b), "c.txt") {
		t.Errorf("local panel without the window's header: %d %s", c, b)
	}
	for nom := range outilsHub {
		if strings.Contains(strings.ToLower(nom), "local") {
			t.Errorf("assistant tool %q could reach this computer's files", nom)
		}
	}
	// refused from a paired device by default (routeShellDistance), like the terminal
	for _, m := range []string{"GET", "POST"} {
		r, _ := http.NewRequest(m, "https://abc-def.trycloudflare.com/api/serveurs/local/sftp", nil)
		if !refuseHubDistance(r) || !routeShellDistance(r.URL.Path) {
			t.Errorf("%s local panel not refused from a device by default", m)
		}
	}
	journal := journalAuditTexte(t)
	for _, x := range []string{"sftp.local.mkdir", "sftp.local.rename", "sftp.local.delete"} {
		if !strings.Contains(journal, x) {
			t.Errorf("audit log without %q", x)
		}
	}
	if strings.Contains(journal, "témoin-coffre") {
		t.Error("audit log holds the vault's content")
	}
}
