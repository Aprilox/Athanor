package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPourVous(t *testing.T) {
	d := t.TempDir()
	p := Projet{Nom: "a", Dossier: d}
	s := nouveauServeur(Config{Kit: t.TempDir(), Projets: []Projet{p}, ProjetActif: "a", Langue: "fr"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, chemin string, corps any) (int, map[string]any) {
		var b []byte
		if corps != nil {
			b, _ = json.Marshal(corps)
		}
		r := httptest.NewRequest(methode, chemin, bytes.NewReader(b))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	os.MkdirAll(filepath.Join(d, ".athanor", "for-you"), 0o755)
	taches := `{"suivant":3,"taches":[{"id":"T1","titre":"Créer la boutique","details":"1. https://partners.shopify.com","attendu":"l'adresse","secrets":["SHOPIFY_TOKEN"],"jalons":["M2"],"statut":"ouverte","tour":0},
	 {"id":"T2","titre":"Acheter le domaine","secrets":[],"statut":"ouverte","tour":0},{"id":"../x","titre":"pirate"}]}`
	os.WriteFile(cheminPourVous(p, "tasks.json"), []byte(taches), 0o644)
	if n := p.PourVous(); n != 2 {
		t.Fatalf("pour vous: %d", n)
	}
	// a file for the answer, then the answer with a secret
	r := httptest.NewRequest("POST", "/api/pieces?p=a&cible=todo-T1", strings.NewReader("capture"))
	r.Header.Set("X-Athanor", "jeton")
	r.Header.Set("X-Nom-Fichier", url.QueryEscape("écran.png"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var pj map[string]any
	json.Unmarshal(w.Body.Bytes(), &pj)
	if w.Code != 200 || pj["chemin"] != ".athanor/attachments/for-you-T1/écran.png" {
		t.Fatalf("file: %d %s", w.Code, w.Body.String())
	}
	valeur := `sh'pat_$(rm -rf ~)"x`
	if c, v := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T1", "action": "fait", "texte": "C'est fait : ma-boutique.myshopify.com", "fichiers": []string{pj["chemin"].(string)}, "secrets": map[string]string{"SHOPIFY_TOKEN": valeur}}); c != 200 {
		t.Fatalf("answer: %d %v", c, v)
	}
	// the value is sourced by a shell: quoted, never executed
	sortie, err := exec.Command("bash", "-c", `set -a; . "$1"; set +a; printf %s "$SHOPIFY_TOKEN"`, "x", cheminSecrets(p)).Output()
	if err != nil || string(sortie) != valeur {
		t.Fatalf("secrets.env: %q %v", sortie, err)
	}
	if fi, _ := os.Stat(cheminSecrets(p)); fi.Mode().Perm() != 0o600 && runtime.GOOS != "windows" { // (no such modes on Windows)
		t.Errorf("secrets.env mode %v", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(cheminPourVous(p, "replies.json")); strings.Contains(string(b), "pat_") {
		t.Error("a secret value in replies.json")
	}
	vues := p.TachesVues()
	if vues[0].ID != "T2" || vues[1].Etat != "a_verifier" || !vues[1].Donnes["SHOPIFY_TOKEN"] || len(vues) != 2 {
		t.Fatalf("views: %+v", vues)
	}
	// answered twice: refused; another task's file or a secret it does not ask for: refused
	if c, _ := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T1", "action": "fait"}); c != 409 {
		t.Errorf("twice: %d", c)
	}
	if c, _ := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T2", "action": "fait", "fichiers": []string{pj["chemin"].(string)}}); c != 400 {
		t.Errorf("file of another task: %d", c)
	}
	if c, _ := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T2", "action": "fait", "secrets": map[string]string{"PATH": "x"}}); c != 400 {
		t.Errorf("unasked secret: %d", c)
	}
	if c, _ := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T2", "action": "fait", "fichiers": []string{".athanor/attachments/for-you-T2/../../../x"}}); c != 400 {
		t.Errorf("path: %d", c)
	}
	// Claude says something is missing: back to the user; then a skip
	os.WriteFile(cheminPourVous(p, "tasks.json"), []byte(strings.Replace(taches, `"statut":"ouverte","tour":0},
	 {"id":"T2"`, `"statut":"manque","tour":1,"note":"le jeton n'a pas write_themes"},
	 {"id":"T2"`, 1)), 0o644)
	if p.TachesVues()[0].Etat != "pour_vous" || p.PourVous() != 2 {
		t.Fatalf("reopened: %+v", p.TachesVues())
	}
	if c, _ := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T1", "action": "passer", "texte": "pas de plan payant", "secrets": map[string]string{"SHOPIFY_TOKEN": "autre"}}); c != 200 {
		t.Fatalf("skip: %d", c)
	}
	if b, _ := os.ReadFile(cheminSecrets(p)); strings.Contains(string(b), "autre") || strings.Count(string(b), "SHOPIFY_TOKEN=") != 1 {
		t.Errorf("a skip writes no secret: %s", b)
	}
	if v := p.TachesVues(); v[1].Etat != "a_contourner" || len(v[1].Reponses) != 2 || v[1].Reponses[1].Tour != 2 {
		t.Fatalf("skipped: %+v", v[1])
	}
	// the GET and the counter of the state
	if c, v := appel("GET", "/api/pourvous?p=a", nil); c != 200 || len(v["taches"].([]any)) != 2 {
		t.Errorf("get: %d %v", c, v)
	}
	if c, v := appel("GET", "/api/etat?p=a", nil); c != 200 || v["pour_vous"].(float64) != 1 {
		t.Errorf("etat: %d %v", c, v["pour_vous"])
	}
	// a link in place of replies.json: not followed
	ailleurs := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(ailleurs, []byte(`{"T2":[{"tour":1,"action":"fait"}]}`), 0o644)
	os.Remove(cheminPourVous(p, "replies.json"))
	os.Symlink(ailleurs, cheminPourVous(p, "replies.json"))
	if c, _ := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T2", "action": "fait"}); c != 200 {
		t.Errorf("link: %d", c)
	}
	if b, _ := os.ReadFile(ailleurs); strings.Count(string(b), "tour") != 1 {
		t.Errorf("written through the link: %s", b)
	}
	if fi, _ := os.Lstat(cheminPourVous(p, "replies.json")); !fi.Mode().IsRegular() {
		t.Error("the link stayed in place")
	}
	// a pipe in place of tasks.json never blocks the application
	os.Remove(cheminPourVous(p, "tasks.json"))
	exec.Command("mkfifo", cheminPourVous(p, "tasks.json")).Run()
	fini := make(chan int)
	go func() { fini <- p.PourVous() }()
	select {
	case n := <-fini:
		if n != 0 {
			t.Errorf("fifo: %d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked on a pipe")
	}
	// a field of the wrong type: the task stays, with that field empty (as system/taches.py reads it)
	os.Remove(cheminPourVous(p, "tasks.json"))
	os.WriteFile(cheminPourVous(p, "tasks.json"), []byte(`{"taches":[{"id":"T5","titre":"x","secrets":"ABC","tour":"1","statut":3},{"id":"T5","titre":"double"},{"id":"T6\n"},"oops"]}`), 0o644)
	if v := p.TachesVues(); len(v) != 1 || v[0].Titre != "x" || len(v[0].Secrets) != 0 || v[0].Etat != "pour_vous" {
		t.Errorf("lenient: %+v", v)
	}
	// a name the shell uses is never a secret; lines Claude added to secrets.env are dropped
	os.WriteFile(cheminPourVous(p, "tasks.json"), []byte(`{"taches":[{"id":"T7","titre":"x","secrets":["LD_PRELOAD","API_KEY"],"statut":"ouverte"}]}`), 0o644)
	if c, _ := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T7", "action": "fait", "secrets": map[string]string{"LD_PRELOAD": "/tmp/x.so"}}); c != 400 {
		t.Errorf("LD_PRELOAD: %d", c)
	}
	f, _ := os.OpenFile(cheminSecrets(p), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("FAUX=$(touch /tmp/pwn)\nAUTRE='ok'\n")
	f.Close()
	if c, _ := appel("POST", "/api/pourvous?p=a", map[string]any{"id": "T7", "action": "fait", "secrets": map[string]string{"API_KEY": "k"}}); c != 200 {
		t.Errorf("API_KEY: %d", c)
	}
	if b, _ := os.ReadFile(cheminSecrets(p)); strings.Contains(string(b), "FAUX") || !strings.Contains(string(b), "AUTRE='ok'") || !strings.Contains(string(b), "API_KEY='k'") {
		t.Errorf("secrets.env: %s", b)
	}
	// links
	for u, ok := range map[string]bool{"https://partners.shopify.com/signup?x=1": true, "javascript:alert(1)": false, "file:///etc/passwd": false, "https://a b": false} {
		if c, _ := appel("POST", "/api/lien", map[string]any{"url": u}); (c != 400) != ok && !(ok && c == 500) {
			t.Errorf("link %q: %d", u, c)
		}
	}
}
