package main

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTerminalConteneur: the Servers terminal end to end against the real sshd of the test container (M231g):
// an interactive shell in a PTY (size, resize, Ctrl+C), the files panel (upload, the file seen from the shell,
// download, delete), the assistant in its own connection (a destructive command refused without a plan and never
// run) then live in the owner's terminal (typing refused while it works, its commands shown, the keyboard given
// back); the vault's witness value, kept in a file on the server, reaches neither the screen the assistant writes,
// the API's answers, the session's instructions, the audit log nor the files Athanor keeps.
func TestTerminalConteneur(t *testing.T) {
	s, _, id, _, _ := serveurConteneurTest(t)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	var reponses strings.Builder // every answer of the API but the stream and the owner's own download
	requete := func(methode, chemin, corps string, entetes ...string) (int, []byte, *http.Response) {
		t.Helper()
		r, _ := http.NewRequest(methode, ts.URL+chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		for i := 0; i+1 < len(entetes); i += 2 {
			r.Header.Set(entetes[i], entetes[i+1])
		}
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		rep, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if methode == "GET" && strings.Contains(chemin, "/terminal/") && rep.StatusCode == http.StatusOK {
			return rep.StatusCode, nil, rep
		}
		b, _ := io.ReadAll(rep.Body)
		rep.Body.Close()
		if !strings.HasSuffix(strings.SplitN(chemin, "?", 2)[0], "/fichier") || methode != "GET" {
			reponses.Write(b)
		}
		return rep.StatusCode, b, rep
	}
	vers := func(b []byte) map[string]any {
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		return v
	}

	// 1. the interactive shell: size given at opening, then resized, Ctrl+C stops a running command
	c, b, _ := requete("POST", "/api/serveurs/"+id+"/terminal", `{"cols":100,"lignes":30}`)
	session, _ := vers(b)["session"].(string)
	if c != http.StatusOK || session == "" {
		t.Fatalf("open: %d %s", c, b)
	}
	_, _, flux := requete("GET", "/api/serveurs/"+id+"/terminal/"+session, "")
	l := &lecteurTerminal{fini: make(chan struct{}), corps: flux.Body}
	go func() {
		defer close(l.fini)
		defer flux.Body.Close()
		b := make([]byte, 4096)
		for {
			n, err := flux.Body.Read(b)
			l.mu.Lock()
			l.b.Write(b[:n])
			l.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	taper := func(corps map[string]any) int {
		t.Helper()
		j, _ := json.Marshal(corps)
		c, _, _ := requete("POST", "/api/serveurs/"+id+"/terminal/"+session, string(j))
		return c
	}
	// the shell's answers are computed ($((…))) so that the echo of what was typed never matches them
	if c := taper(map[string]any{"entree": "stty size; echo taille-$((40+2))\r"}); c != http.StatusOK {
		t.Fatalf("input: %d", c)
	}
	l.attendre(t, "30 100")
	l.attendre(t, "taille-42")
	if c := taper(map[string]any{"cols": 120, "lignes": 40}); c != http.StatusOK {
		t.Fatalf("resize: %d", c)
	}
	taper(map[string]any{"entree": "stty size\r"})
	l.attendre(t, "40 120")
	taper(map[string]any{"entree": "sleep 60; echo jamais-$((1+1))\r"})
	time.Sleep(300 * time.Millisecond)
	taper(map[string]any{"entree": "\x03"})
	taper(map[string]any{"entree": "echo apres-$((3+4))\r"})
	l.attendre(t, "apres-7")
	if strings.Contains(l.b.String(), "jamais-2") {
		t.Error("Ctrl+C did not stop the running command")
	}

	// 2. the files panel on the same connection: the home folder, an upload seen from the shell, the download
	base := "/api/serveurs/" + id + "/sftp/" + session
	c, b, _ = requete("GET", base+"?chemin=", "")
	var maison struct{ Chemin string }
	if json.Unmarshal(b, &maison); c != http.StatusOK || !strings.HasPrefix(maison.Chemin, "/") {
		t.Fatalf("home: %d %s", c, b)
	}
	dossier := strings.TrimSuffix(maison.Chemin, "/") + "/e2e-ia"
	taper(map[string]any{"entree": "rm -rf ~/e2e-ia; mkdir ~/e2e-ia && echo pret-$((5*5))\r"})
	l.attendre(t, "pret-25")
	envoyer := func(chemin, corps string, remplacer bool) int {
		t.Helper()
		q := "?chemin=" + url.QueryEscape(chemin)
		if remplacer {
			q += "&remplacer=1"
		}
		c, _, _ := requete("POST", base+"/fichier"+q, corps, "Content-Type", "application/octet-stream")
		return c
	}
	if c := envoyer(dossier+"/notes.txt", "bonjour-envoye\n", false); c != http.StatusOK {
		t.Fatalf("upload: %d", c)
	}
	if c := envoyer(dossier+"/notes.txt", "autre\n", false); c != http.StatusConflict {
		t.Errorf("upload over an existing file without asking: %d, want 409", c)
	}
	if c := envoyer(dossier+"/notes.txt", "remplace-par-sftp\n", true); c != http.StatusOK {
		t.Fatalf("replace: %d", c)
	}
	taper(map[string]any{"entree": "tr a-z A-Z < ~/e2e-ia/notes.txt\r"})
	l.attendre(t, "REMPLACE-PAR-SFTP")
	if c, b, _ := requete("GET", base+"/fichier?chemin="+url.QueryEscape(dossier+"/notes.txt"), ""); c != http.StatusOK || string(b) != "remplace-par-sftp\n" {
		t.Errorf("download: %d %q", c, b)
	}
	if c, b, _ := requete("POST", base, `{"supprimer":"`+dossier+`/notes.txt"}`); c != http.StatusOK {
		t.Errorf("delete: %d %s", c, b)
	}
	// the witness lives in a file on the server, as a real password would
	if c := envoyer(dossier+"/LISEZMOI", "mot de passe de la base : "+temoinOutils+"\n", false); c != http.StatusOK {
		t.Fatalf("upload LISEZMOI: %d", c)
	}

	// 3. the assistant, a scripted Claude calling Athanor's tools on the container
	commence, libere := make(chan struct{}, 1), make(chan struct{}, 1)
	var consignes []string
	ancien := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancien })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, d string) error {
		lire := func(f string) string { b, _ := os.ReadFile(filepath.Join(d, f)); return strings.TrimSpace(string(b)) }
		consignes = append(consignes, lire("consigne.txt"))
		jeton := lire("hub-jeton")
		appel := func(nom, args string) { appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args)) }
		appel("fichier_lire", `{"serveur":"`+id+`","chemin":"`+dossier+`/LISEZMOI"}`)
		appel("ssh_commande", `{"serveur":"`+id+`","commande":"rm -rf `+dossier+`"}`)
		appel("ssh_commande", `{"serveur":"`+id+`","commande":"uname -s"}`)
		commence <- struct{}{}
		<-libere
		j, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": "C'est un Linux."})
		return os.WriteFile(filepath.Join(d, "session.jsonl"), []byte(`{"type":"system","subtype":"init","session_id":"s1"}`+"\n"+string(j)+"\n"), 0o600)
	}
	attendreTache := func(tid string) TacheServeurs {
		t.Helper()
		for range 400 {
			for _, x := range tachesServeur(id) {
				if x.ID == tid && tacheFinieGo(x.Etat) {
					return x
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("task %s never ended", tid)
		return TacheServeurs{}
	}
	lancer := func(corps string) map[string]any {
		t.Helper()
		c, b, _ := requete("POST", "/api/serveurs/"+id+"/taches", corps)
		tache, _ := vers(b)["tache"].(map[string]any)
		if c != http.StatusOK || tache == nil {
			t.Fatalf("launch %s: %d %s", corps, c, b)
		}
		return tache
	}
	attendreDebut := func() {
		t.Helper()
		select {
		case <-commence:
		case <-time.After(30 * time.Second):
			t.Fatal("the assistant's commands never ran")
		}
	}
	verifierActions := func(x TacheServeurs) {
		t.Helper()
		a := x.Actions
		if x.Etat != "finie" || len(a) != 3 {
			t.Fatalf("task: %s, actions %+v", x.Etat, a)
		}
		if a[0].Etat != "ok" || !strings.Contains(a[0].Sortie, "mot de passe de la base") || strings.Contains(a[0].Sortie, temoinOutils) {
			t.Errorf("read: %+v", a[0])
		}
		if a[1].Etat != "refusee" || a[1].Classe != "destructrice" {
			t.Errorf("rm -rf without a plan: %+v", a[1])
		}
		if a[2].Etat != "ok" || !strings.Contains(a[2].Sortie, "Linux") {
			t.Errorf("uname: %+v", a[2])
		}
	}
	toujoursLa := func() {
		t.Helper()
		c, b, _ := requete("GET", base+"?chemin="+url.QueryEscape(dossier), "")
		if c != http.StatusOK || !strings.Contains(string(b), "LISEZMOI") {
			t.Errorf("the refused rm -rf ran anyway: %d %s", c, b)
		}
	}

	// 3a. mode "separate connection": the owner's terminal stays free
	tache := lancer(`{"texte":"Quel système ? la clé est ` + temoinOutils + `"}`)
	if tache["direct"] == true {
		t.Errorf("separate task marked live: %v", tache)
	}
	attendreDebut()
	if c := taper(map[string]any{"entree": "echo libre-$((2+3))\r"}); c != http.StatusOK {
		t.Errorf("typing during a separate task: %d", c)
	}
	l.attendre(t, "libre-5")
	libere <- struct{}{}
	verifierActions(attendreTache(tache["id"].(string)))
	toujoursLa()

	// 3b. mode "live in my terminal": its commands on the owner's screen, typing refused until it ends
	tache = lancer(`{"texte":"Quel système ?","terminal":"` + session + `"}`)
	if tache["direct"] != true {
		t.Fatalf("live task: %v", tache)
	}
	attendreDebut()
	l.attendre(t, "[assistant]\x1b[0m $ uname -s")
	l.attendre(t, "\x1b[33m✗ ")
	l.attendre(t, "mot de passe de la base")
	if c := taper(map[string]any{"entree": "echo moi\r"}); c != http.StatusLocked {
		t.Errorf("typing while the assistant works live: %d, want 423", c)
	}
	if c := taper(map[string]any{"cols": 110, "lignes": 35}); c != http.StatusOK {
		t.Errorf("resize while locked: %d", c)
	}
	libere <- struct{}{}
	verifierActions(attendreTache(tache["id"].(string)))
	l.attendre(t, "the keyboard is yours")
	toujoursLa()
	if c := taper(map[string]any{"entree": "echo rendu-$((9*9))\r"}); c != http.StatusOK {
		t.Errorf("typing after the task: %d", c)
	}
	l.attendre(t, "rendu-81")

	// 4. the witness: never on the screen the assistant wrote, in the API, the instructions, the audit or the files kept
	l.mu.Lock()
	ecran := l.b.String()
	l.mu.Unlock()
	if strings.Contains(ecran, temoinOutils) {
		t.Error("the witness is on the terminal's screen")
	}
	if c, b, _ := requete("GET", "/api/serveurs/"+id+"/taches", ""); c != http.StatusOK || !strings.Contains(string(b), `"refusee"`) {
		t.Errorf("tasks: %d %.300s", c, b)
	}
	if strings.Contains(reponses.String(), temoinOutils) {
		t.Error("the witness is in an answer of the API")
	}
	for _, x := range consignes {
		if x == "" || strings.Contains(x, temoinOutils) {
			t.Errorf("the session's instructions hold the witness (or are empty): %.200q", x)
		}
	}
	if strings.Contains(journalAuditTexte(t), temoinOutils) {
		t.Error("the witness is in the audit log")
	}
	_ = filepath.WalkDir(dossierModule("servers"), func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), temoinOutils) {
				t.Errorf("the witness is in %s", p)
			}
		}
		return nil
	})

	// 5. Disconnect ends the stream; cleanup on the server
	taper(map[string]any{"entree": "rm -rf ~/e2e-ia\r"})
	if c := taper(map[string]any{"fermer": true}); c != http.StatusOK {
		t.Errorf("disconnect: %d", c)
	}
	l.attendreFin(t)
}
